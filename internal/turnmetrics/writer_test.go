package turnmetrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

func newTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := db.Migrate(Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// waitForWritten polls Written() until it reaches n or the deadline passes —
// the writer goroutine drains asynchronously, so tests must not assume
// synchronous persistence.
func waitForWritten(t *testing.T, w *Writer, n int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.Written() >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Written() >= %d (got %d)", n, w.Written())
}

func TestWriter_EnqueueAndPersist(t *testing.T) {
	db := newTestDB(t)
	w := newWriterForTest(db, 16, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	now := time.Now()
	w.Enqueue(TurnMetric{
		SessionID:     "sess-1",
		AgentName:     "steve",
		Model:         "claude-sonnet-4-5",
		Provider:      "anthropic",
		TurnKind:      "agent-loop",
		PromptChars:   42,
		MessageCount:  3,
		ToolCallCount: 2,
		TRequest:      now,
		HadFirstToken: true,
		FirstToken:    now.Add(50 * time.Millisecond),
		FirstSignal:   now.Add(50 * time.Millisecond),
		Complete:      now.Add(500 * time.Millisecond),
	})
	waitForWritten(t, w, 1)

	resp, err := w.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(resp.Turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(resp.Turns))
	}
	row := resp.Turns[0]
	if row.SessionID != "sess-1" || row.Model != "claude-sonnet-4-5" {
		t.Errorf("unexpected row: %+v", row)
	}
	if !row.HadFirstToken || row.FirstTokenMs < 40 || row.FirstTokenMs > 200 {
		t.Errorf("first_token_ms out of expected range: %+v", row)
	}
	if row.CompleteMs < 400 || row.CompleteMs > 1000 {
		t.Errorf("complete_ms out of expected range: %+v", row)
	}
	if len(resp.Summary) != 1 || resp.Summary[0].Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
}

// TestWriter_ZeroTokenTurnRecordedNotDropped covers a pure-tool or error turn:
// no token was ever streamed, but the row must still land with
// had_first_token=false / first_token_ms=-1, not be silently dropped.
func TestWriter_ZeroTokenTurnRecordedNotDropped(t *testing.T) {
	db := newTestDB(t)
	w := newWriterForTest(db, 16, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	now := time.Now()
	w.Enqueue(TurnMetric{
		SessionID:     "sess-2",
		Model:         "gpt-4o",
		TurnKind:      "agent-loop",
		TRequest:      now,
		HadFirstToken: false,
		Complete:      now.Add(200 * time.Millisecond),
		IsError:       true,
	})
	waitForWritten(t, w, 1)

	resp, err := w.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(resp.Turns) != 1 {
		t.Fatalf("expected 1 turn (recorded, not dropped), got %d", len(resp.Turns))
	}
	row := resp.Turns[0]
	if row.HadFirstToken {
		t.Errorf("expected had_first_token=false, got true")
	}
	if row.FirstTokenMs != -1 {
		t.Errorf("expected first_token_ms=-1 for a tokenless turn, got %d", row.FirstTokenMs)
	}
	if !row.IsError {
		t.Errorf("expected is_error=true")
	}
}

// TestWriter_DropsOnFullBuffer proves Enqueue never blocks: a full channel
// increments the drop counter instead of stalling the caller.
func TestWriter_DropsOnFullBuffer(t *testing.T) {
	db := newTestDB(t)
	w := newWriterForTest(db, 1, 100)
	// No Start(): the channel fills after the first Enqueue and every
	// subsequent Enqueue must return immediately via the drop path.
	w.Enqueue(TurnMetric{Model: "m", TRequest: time.Now(), Complete: time.Now()})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			w.Enqueue(TurnMetric{Model: "m", TRequest: time.Now(), Complete: time.Now()})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue blocked on a full buffer instead of dropping")
	}
	if w.Dropped() == 0 {
		t.Errorf("expected dropped count > 0, got 0")
	}
}

// TestWriter_PruneKeepsTableBounded drives more inserts than the retention
// cap and asserts the row count never exceeds it once pruning has run.
func TestWriter_PruneKeepsTableBounded(t *testing.T) {
	db := newTestDB(t)
	const retention = 25
	w := newWriterForTest(db, 512, retention)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Prune only runs every pruneEvery inserts, so land exactly on a multiple
	// (2x) to guarantee the last batch of inserts is pruned before we assert.
	const total = pruneEvery * 2
	for i := 0; i < total; i++ {
		now := time.Now()
		w.Enqueue(TurnMetric{Model: "m", TRequest: now, Complete: now})
	}
	waitForWritten(t, w, int64(total))

	// Give the prune-on-every-Nth-insert path a moment to run its DELETE.
	deadline := time.Now().Add(2 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		if err := db.Read().QueryRow(`SELECT COUNT(*) FROM turn_metrics`).Scan(&count); err != nil {
			t.Fatalf("count query: %v", err)
		}
		if count <= retention {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected row count <= %d after pruning, got %d", retention, count)
}

// TestTurnMetric_StampOrdering verifies the derived durations are sane
// (t_request <= t_first_token <= t_complete) for a normal streamed turn.
func TestTurnMetric_StampOrdering(t *testing.T) {
	now := time.Now()
	m := TurnMetric{
		TRequest:      now,
		HadFirstToken: true,
		FirstToken:    now.Add(10 * time.Millisecond),
		FirstSignal:   now.Add(10 * time.Millisecond),
		Complete:      now.Add(100 * time.Millisecond),
	}
	ft := m.firstTokenMs()
	cm := m.completeMs()
	if ft < 0 {
		t.Fatalf("expected non-negative first_token_ms, got %d", ft)
	}
	if cm < ft {
		t.Fatalf("expected complete_ms (%d) >= first_token_ms (%d)", cm, ft)
	}
}
