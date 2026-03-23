package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/scrypster/huginn/internal/stats"
)

// statsCollector returns the stats.Collector for this server, or nil if no
// StatsRegistry is wired. Thread-safe.
func (s *Server) statsCollector() stats.Collector {
	s.mu.Lock()
	reg := s.statsReg
	s.mu.Unlock()
	if reg == nil {
		return nil
	}
	return reg.Collector()
}

// handleMetrics serves a JSON snapshot of all collected in-process metrics.
//
// GET /api/v1/metrics
//
// Response schema:
//
//	{
//	  "ts": "2026-03-15T...",
//	  "records":    [{"metric":"...", "value":1.0, "tags":[...], "time":"..."}],
//	  "histograms": [{"metric":"...", "value":12.3, "tags":[...], "time":"..."}]
//	}
//
// Returns an empty snapshot when no StatsRegistry is wired.
// The endpoint is unauthenticated (binds to 127.0.0.1 only).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Metric string   `json:"metric"`
		Value  float64  `json:"value"`
		Tags   []string `json:"tags,omitempty"`
		Time   string   `json:"time"`
	}
	type response struct {
		TS         string  `json:"ts"`
		Records    []entry `json:"records"`
		Histograms []entry `json:"histograms"`
	}

	resp := response{
		TS:         time.Now().UTC().Format(time.RFC3339),
		Records:    []entry{},
		Histograms: []entry{},
	}

	s.mu.Lock()
	reg := s.statsReg
	s.mu.Unlock()

	if reg != nil {
		snap := reg.Snapshot()
		for _, m := range snap.Records {
			resp.Records = append(resp.Records, entry{
				Metric: m.Metric,
				Value:  m.Value,
				Tags:   m.Tags,
				Time:   m.Time.UTC().Format(time.RFC3339Nano),
			})
		}
		for _, m := range snap.Histograms {
			resp.Histograms = append(resp.Histograms, entry{
				Metric: m.Metric,
				Value:  m.Value,
				Tags:   m.Tags,
				Time:   m.Time.UTC().Format(time.RFC3339Nano),
			})
		}
	}

	jsonOK(w, resp)
}

// handleStatsHistory serves historical stat snapshots from the SQLite stats_snapshots table.
//
// GET /api/v1/stats/history?since=<unix_seconds>
//
// Returns up to 10 000 rows since the given unix timestamp (default: last 24h).
func (s *Server) handleStatsHistory(w http.ResponseWriter, r *http.Request) {
	type statPoint struct {
		TS    int64   `json:"ts"`
		Key   string  `json:"key"`
		Kind  string  `json:"kind"`
		Value float64 `json:"value"`
	}
	type costPoint struct {
		TS               int64   `json:"ts"`
		SessionID        string  `json:"session_id"`
		CostUSD          float64 `json:"cost_usd"`
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
	}
	type response struct {
		Stats []statPoint `json:"stats"`
		Cost  []costPoint `json:"cost"`
	}

	resp := response{Stats: []statPoint{}, Cost: []costPoint{}}

	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		jsonOK(w, resp)
		return
	}

	sinceDefault := time.Now().Add(-24 * time.Hour).Unix()
	since := sinceDefault
	if raw := r.URL.Query().Get("since"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			since = v
		}
	}

	rdb := db.Read()
	if rdb == nil {
		jsonOK(w, resp)
		return
	}

	rows, err := rdb.QueryContext(r.Context(),
		`SELECT ts, key, kind, value FROM stats_snapshots WHERE ts >= ? ORDER BY ts ASC LIMIT 10000`,
		since,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p statPoint
			if err := rows.Scan(&p.TS, &p.Key, &p.Kind, &p.Value); err == nil {
				resp.Stats = append(resp.Stats, p)
			}
		}
	}

	costRows, err := rdb.QueryContext(r.Context(),
		`SELECT ts, session_id, cost_usd, prompt_tokens, completion_tokens FROM cost_history WHERE ts >= ? ORDER BY ts ASC LIMIT 1000`,
		since,
	)
	if err == nil {
		defer costRows.Close()
		for costRows.Next() {
			var p costPoint
			if err := costRows.Scan(&p.TS, &p.SessionID, &p.CostUSD, &p.PromptTokens, &p.CompletionTokens); err == nil {
				resp.Cost = append(resp.Cost, p)
			}
		}
	}

	jsonOK(w, resp)
}
