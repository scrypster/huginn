package turnmetrics

import (
	"math"
	"sort"
	"time"
)

// TurnRow is one persisted turn, shaped for JSON/REST consumption.
type TurnRow struct {
	ID            int64     `json:"id"`
	SessionID     string    `json:"session_id"`
	AgentName     string    `json:"agent_name"`
	Model         string    `json:"model"`
	Provider      string    `json:"provider"`
	TurnKind      string    `json:"turn_kind"`
	PromptChars   int       `json:"prompt_chars"`
	MessageCount  int       `json:"message_count"`
	ToolCallCount int       `json:"tool_call_count"`
	HadFirstToken bool      `json:"had_first_token"`
	FirstTokenMs  int64     `json:"first_token_ms"` // -1 when had_first_token is false
	FirstSignalMs int64     `json:"first_signal_ms"`
	CompleteMs    int64     `json:"complete_ms"`
	IsError       bool      `json:"is_error"`
	TRequest      time.Time `json:"t_request"`
}

// ModelSummary is the p50/p95 rollup for one model within the queried window.
type ModelSummary struct {
	Model            string `json:"model"`
	Count            int    `json:"count"`
	FirstTokenP50Ms  int64  `json:"first_token_p50_ms"`
	FirstTokenP95Ms  int64  `json:"first_token_p95_ms"`
	CompleteP50Ms    int64  `json:"complete_p50_ms"`
	CompleteP95Ms    int64  `json:"complete_p95_ms"`
	ErrorCount       int    `json:"error_count"`
	ToolCallCountAvg int    `json:"tool_call_count_avg"`
}

// TurnsResponse is the full /api/v1/metrics/turns payload.
type TurnsResponse struct {
	Turns   []TurnRow      `json:"turns"`
	Summary []ModelSummary `json:"summary"`
	Dropped int64          `json:"dropped_total"`
	Written int64          `json:"written_total"`
}

// Recent returns up to limit most-recent turns (newest first) and the
// model-grouped p50/p95 summary computed over that same set. limit is
// clamped to [1, 1000]; a caller doing dashboarding should keep it modest —
// this issues a single SELECT plus in-process sorting, not a query language.
func (w *Writer) Recent(limit int) (TurnsResponse, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := w.db.Read().Query(`
		SELECT id, session_id, agent_name, model, provider, turn_kind,
		       prompt_chars, message_count, tool_call_count,
		       had_first_token, first_token_ms, first_signal_ms, complete_ms,
		       is_error, t_request_unix_ms
		FROM turn_metrics
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return TurnsResponse{}, err
	}
	defer rows.Close()

	var turns []TurnRow
	byModel := map[string][]TurnRow{}
	for rows.Next() {
		var t TurnRow
		var hadFirstToken, isError int
		var tReqMs int64
		if err := rows.Scan(&t.ID, &t.SessionID, &t.AgentName, &t.Model, &t.Provider, &t.TurnKind,
			&t.PromptChars, &t.MessageCount, &t.ToolCallCount,
			&hadFirstToken, &t.FirstTokenMs, &t.FirstSignalMs, &t.CompleteMs,
			&isError, &tReqMs); err != nil {
			return TurnsResponse{}, err
		}
		t.HadFirstToken = hadFirstToken != 0
		t.IsError = isError != 0
		t.TRequest = time.UnixMilli(tReqMs).UTC()
		turns = append(turns, t)
		byModel[t.Model] = append(byModel[t.Model], t)
	}
	if err := rows.Err(); err != nil {
		return TurnsResponse{}, err
	}

	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)

	summary := make([]ModelSummary, 0, len(models))
	for _, m := range models {
		summary = append(summary, summarizeModel(m, byModel[m]))
	}

	return TurnsResponse{
		Turns:   turns,
		Summary: summary,
		Dropped: w.Dropped(),
		Written: w.Written(),
	}, nil
}

func summarizeModel(model string, rows []TurnRow) ModelSummary {
	s := ModelSummary{Model: model, Count: len(rows)}
	var firstTokens, completes []int64
	toolSum := 0
	for _, r := range rows {
		if r.HadFirstToken {
			firstTokens = append(firstTokens, r.FirstTokenMs)
		}
		completes = append(completes, r.CompleteMs)
		toolSum += r.ToolCallCount
		if r.IsError {
			s.ErrorCount++
		}
	}
	s.FirstTokenP50Ms = percentile(firstTokens, 0.50)
	s.FirstTokenP95Ms = percentile(firstTokens, 0.95)
	s.CompleteP50Ms = percentile(completes, 0.50)
	s.CompleteP95Ms = percentile(completes, 0.95)
	if len(rows) > 0 {
		s.ToolCallCountAvg = toolSum / len(rows)
	}
	return s
}

// percentile returns the p-th percentile (0..1) of vals using nearest-rank
// (idx = ceil(p*n) - 1), or 0 for an empty slice. vals is sorted in place —
// callers pass a slice built fresh per summary so this is safe.
//
// D6: the previous formula (idx = int(p*(n-1))) is a linear-interpolation
// index that rounds DOWN toward the body of the distribution — for
// [1..19, 9999] (n=20) it computed idx=18 and returned 19, the second-to-
// largest value, silently hiding a 9999ms outlier sitting in the true top
// 5%. Nearest-rank is the standard definition for this exact "don't hide
// the tail" requirement.
func percentile(vals []int64, p float64) int64 {
	if len(vals) == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	idx := int(math.Ceil(p*float64(len(vals)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}
