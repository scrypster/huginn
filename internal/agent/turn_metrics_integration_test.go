package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/turnmetrics"
)

func newTestTurnMetricsWriter(t *testing.T) *turnmetrics.Writer {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := db.Migrate(turnmetrics.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	w := turnmetrics.NewWriter(db)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w.Start(ctx)
	return w
}

func waitForTurnMetricsRow(t *testing.T, w *turnmetrics.Writer) turnmetrics.TurnRow {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := w.Recent(10)
		if err != nil {
			t.Fatalf("Recent: %v", err)
		}
		if len(resp.Turns) > 0 {
			return resp.Turns[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for a turn_metrics row")
	return turnmetrics.TurnRow{}
}

// TestRunLoop_MetricsHook_RecordsPlausibleRow drives a real RunLoop through a
// fake streaming backend and asserts a metrics row lands with plausible
// values: had_first_token, sane orderings, and the dimensions RunLoop had at
// hand (session, agent, model, tool call count).
func TestRunLoop_MetricsHook_RecordsPlausibleRow(t *testing.T) {
	w := newTestTurnMetricsWriter(t)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "hello from the model", DoneReason: "stop"},
		},
	}

	cfg := RunLoopConfig{
		MaxTurns:      1,
		ModelName:     "claude-sonnet-4-5",
		Messages:      []backend.Message{{Role: "user", Content: "hi there"}},
		Backend:       mb,
		AgentName:     "steve",
		SessionID:     "sess-abc",
		MetricsWriter: w,
		TurnKind:      "agent-loop",
	}

	res, err := RunLoop(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if res.FinalContent != "hello from the model" {
		t.Fatalf("unexpected final content: %q", res.FinalContent)
	}

	row := waitForTurnMetricsRow(t, w)
	if row.SessionID != "sess-abc" {
		t.Errorf("session_id = %q, want sess-abc", row.SessionID)
	}
	if row.AgentName != "steve" {
		t.Errorf("agent_name = %q, want steve", row.AgentName)
	}
	if row.Model != "claude-sonnet-4-5" {
		t.Errorf("model = %q, want claude-sonnet-4-5", row.Model)
	}
	if row.TurnKind != "agent-loop" {
		t.Errorf("turn_kind = %q, want agent-loop", row.TurnKind)
	}
	if !row.HadFirstToken {
		t.Error("expected had_first_token=true for a streamed turn")
	}
	if row.FirstTokenMs < 0 {
		t.Errorf("first_token_ms = %d, want >= 0", row.FirstTokenMs)
	}
	if row.CompleteMs < row.FirstTokenMs {
		t.Errorf("complete_ms (%d) < first_token_ms (%d)", row.CompleteMs, row.FirstTokenMs)
	}
	if row.IsError {
		t.Error("expected is_error=false for a successful turn")
	}
}

// TestRunLoop_MetricsHook_ErrorTurnRecordedWithoutFirstToken drives a RunLoop
// whose backend call fails outright — asserting the row still lands
// (recorded, not dropped) with had_first_token=false and is_error=true.
func TestRunLoop_MetricsHook_ErrorTurnRecordedWithoutFirstToken(t *testing.T) {
	w := newTestTurnMetricsWriter(t)

	mb := &mockBackend{
		errors: []error{context.DeadlineExceeded},
	}

	cfg := RunLoopConfig{
		MaxTurns:      1,
		ModelName:     "gpt-4o",
		Messages:      []backend.Message{{Role: "user", Content: "hi"}},
		Backend:       mb,
		SessionID:     "sess-err",
		MetricsWriter: w,
		TurnKind:      "agent-loop",
	}

	_, err := RunLoop(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected RunLoop to return an error")
	}

	row := waitForTurnMetricsRow(t, w)
	if row.HadFirstToken {
		t.Error("expected had_first_token=false for a turn with no streamed content")
	}
	if row.FirstTokenMs != -1 {
		t.Errorf("first_token_ms = %d, want -1", row.FirstTokenMs)
	}
	if !row.IsError {
		t.Error("expected is_error=true")
	}
}

// TestRunLoop_MetricsHook_NilWriterIsNoop proves the hook is a true no-op
// when MetricsWriter is unset — the common case for every existing test and
// call site that hasn't opted in.
func TestRunLoop_MetricsHook_NilWriterIsNoop(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{{Content: "ok", DoneReason: "stop"}},
	}
	cfg := RunLoopConfig{
		MaxTurns:  1,
		ModelName: "m",
		Messages:  []backend.Message{{Role: "user", Content: "hi"}},
		Backend:   mb,
	}
	if _, err := RunLoop(context.Background(), cfg); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
}
