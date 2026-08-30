package turnmetrics

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// TurnMetric is one row of turn-latency telemetry. Every field is metadata —
// NEVER put token or message content in here (see package doc and CHANGELOG:
// this table is queried by /api/v1/metrics/turns, which is not access-scoped
// to the same audience as transcripts).
type TurnMetric struct {
	SessionID string
	AgentName string
	Model     string
	Provider  string
	TurnKind  string // "agent-loop", "agent-chat", "coder", "delegated", etc. — reuses recordLLMLatency's slot vocabulary

	PromptChars   int // sum of message content lengths at turn start (cheap, already in memory)
	MessageCount  int
	ToolCallCount int

	TRequest      time.Time
	HadFirstToken bool
	FirstToken    time.Time // zero when HadFirstToken is false (tool-only/error turns)
	FirstSignal   time.Time // zero when nothing was ever observed before completion
	Complete      time.Time
	IsError       bool
}

// firstTokenMs returns the t_request→t_first_token duration in ms, or -1 when
// no token was ever streamed (tool-only or error turn — recorded, not dropped).
func (m TurnMetric) firstTokenMs() int64 {
	if !m.HadFirstToken || m.FirstToken.Before(m.TRequest) {
		return -1
	}
	return m.FirstToken.Sub(m.TRequest).Milliseconds()
}

func (m TurnMetric) firstSignalMs() int64 {
	if m.FirstSignal.IsZero() || m.FirstSignal.Before(m.TRequest) {
		return -1
	}
	return m.FirstSignal.Sub(m.TRequest).Milliseconds()
}

func (m TurnMetric) completeMs() int64 {
	d := m.Complete.Sub(m.TRequest).Milliseconds()
	if d < 0 {
		return 0
	}
	return d
}

const (
	// defaultBufferSize bounds the async channel. At this depth a burst of
	// concurrent turns (swarm delegation, batch chat) never blocks the hot
	// path waiting for the single writer goroutine to catch up; anything
	// beyond it is dropped and counted rather than backing up turn latency.
	defaultBufferSize = 2048
	// defaultRetention is the row cap enforced by the writer after every
	// pruneEvery inserts — "keep last 10k rows" per the perf-wave spec.
	defaultRetention = 10_000
	pruneEvery       = 100
)

// Writer is the single async writer for turn_metrics. Enqueue is non-blocking
// (buffered channel, drop-with-counter on overflow) so the hot agent-turn path
// never performs blocking I/O. A single goroutine owns all writes.
type Writer struct {
	db        *sqlitedb.DB
	ch        chan TurnMetric
	retention int

	written atomic.Int64
	dropped atomic.Int64
}

// NewWriter constructs a Writer. Call Start to begin draining; Enqueue is
// safe to call before Start (it just buffers until the goroutine is running).
// db must already have Migrations() applied.
func NewWriter(db *sqlitedb.DB) *Writer {
	return &Writer{
		db:        db,
		ch:        make(chan TurnMetric, defaultBufferSize),
		retention: defaultRetention,
	}
}

// newWriterForTest allows tests to shrink the buffer/retention without
// waiting on defaultBufferSize-sized channels.
func newWriterForTest(db *sqlitedb.DB, bufSize, retention int) *Writer {
	return &Writer{
		db:        db,
		ch:        make(chan TurnMetric, bufSize),
		retention: retention,
	}
}

// Start runs the writer loop until ctx is cancelled. Call once, typically
// from main.go right after the writer is constructed.
func (w *Writer) Start(ctx context.Context) {
	go w.run(ctx)
}

// Enqueue submits a metric for async persistence. Never blocks: a full buffer
// increments the drop counter instead of applying backpressure to the caller
// (the agent turn that is still streaming to the user).
func (w *Writer) Enqueue(m TurnMetric) {
	if w == nil {
		return
	}
	select {
	case w.ch <- m:
	default:
		w.dropped.Add(1)
		slog.Warn("turnmetrics: buffer full, dropping metric", "dropped_total", w.dropped.Load())
	}
}

// Dropped returns the cumulative number of metrics dropped due to a full buffer.
func (w *Writer) Dropped() int64 { return w.dropped.Load() }

// Written returns the cumulative number of metrics successfully persisted.
func (w *Writer) Written() int64 { return w.written.Load() }

func (w *Writer) run(ctx context.Context) {
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-w.ch:
			if err := w.insert(m); err != nil {
				slog.Warn("turnmetrics: insert failed", "err", err)
				continue
			}
			w.written.Add(1)
			n++
			if n%pruneEvery == 0 {
				if err := w.prune(); err != nil {
					slog.Warn("turnmetrics: prune failed", "err", err)
				}
			}
		}
	}
}

func (w *Writer) insert(m TurnMetric) error {
	_, err := w.db.Write().Exec(`
		INSERT INTO turn_metrics (
		    session_id, agent_name, model, provider, turn_kind,
		    prompt_chars, message_count, tool_call_count,
		    had_first_token, first_token_ms, first_signal_ms, complete_ms,
		    is_error, t_request_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.SessionID, m.AgentName, m.Model, m.Provider, m.TurnKind,
		m.PromptChars, m.MessageCount, m.ToolCallCount,
		boolToInt(m.HadFirstToken), m.firstTokenMs(), m.firstSignalMs(), m.completeMs(),
		boolToInt(m.IsError), m.TRequest.UnixMilli(),
	)
	return err
}

// prune keeps at most w.retention rows, deleting the oldest by id.
func (w *Writer) prune() error {
	_, err := w.db.Write().Exec(`
		DELETE FROM turn_metrics WHERE id NOT IN (
		    SELECT id FROM turn_metrics ORDER BY id DESC LIMIT ?
		)`, w.retention)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
