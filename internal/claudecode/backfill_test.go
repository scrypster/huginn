package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedProjects(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, "-tmp-proj"+string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Give each project a distinct session uuid.
		content := strings.ReplaceAll(string(fixture),
			"11111111-2222-3333-4444-555555555555",
			"1111111"+string(rune('a'+i))+"-2222-3333-4444-555555555555")
		name := "1111111" + string(rune('a'+i)) + "-2222-3333-4444-555555555555.jsonl"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

func TestBackfillImportsEveryTranscript(t *testing.T) {
	root := seedProjects(t, 3)
	ing, sink, _ := newTestIngester(t)

	res, err := Backfill(context.Background(), root, ing, 50)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if res.Files != 3 {
		t.Errorf("Files = %d, want 3", res.Files)
	}
	if res.Messages != 9 {
		t.Errorf("Messages = %d, want 9 (3 per transcript)", res.Messages)
	}
	if len(sink.sessions) != 3 {
		t.Errorf("created %d sessions, want 3", len(sink.sessions))
	}
}

func TestBackfillIsIdempotent(t *testing.T) {
	root := seedProjects(t, 2)
	ing, sink, _ := newTestIngester(t)

	if _, err := Backfill(context.Background(), root, ing, 50); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	res, err := Backfill(context.Background(), root, ing, 50)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if res.Messages != 0 {
		t.Errorf("second backfill appended %d messages, want 0", res.Messages)
	}

	total := 0
	for _, msgs := range sink.msgs {
		total += len(msgs)
	}
	if total != 6 {
		t.Errorf("total messages = %d, want 6", total)
	}
}

func TestBackfillSkipsOversizedFiles(t *testing.T) {
	root := seedProjects(t, 1)
	ing, _, _ := newTestIngester(t)

	// maxFileMB of 0 means every file exceeds the limit.
	res, err := Backfill(context.Background(), root, ing, 0)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Messages != 0 {
		t.Errorf("Messages = %d, want 0", res.Messages)
	}
}

func TestBackfillHonoursCancellationAndWaits(t *testing.T) {
	root := seedProjects(t, 3)
	ing, _, _ := newTestIngester(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the sweep starts

	res, err := Backfill(ctx, root, ing, 50)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if res.Messages != 0 {
		t.Errorf("Messages = %d, want 0 — no work should start after cancellation", res.Messages)
	}
}

// TestBackfillSkipsSubagentTranscripts pins the ruling that sub-agent
// transcripts under a "subagents" directory are never ingested: they would
// key by basename into junk top-level sessions (e.g. "agent-deadbeef") that
// the watcher can never tail live anyway (see transcriptPaths).
func TestBackfillSkipsSubagentTranscripts(t *testing.T) {
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	proj := filepath.Join(root, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mainPath := filepath.Join(proj, "11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(mainPath, fixture, 0o600); err != nil {
		t.Fatalf("write main transcript: %v", err)
	}

	subDir := filepath.Join(proj, "11111111-2222-3333-4444-555555555555", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir subagents: %v", err)
	}
	subContent := strings.ReplaceAll(string(fixture),
		"11111111-2222-3333-4444-555555555555", "agent-deadbeef")
	subPath := filepath.Join(subDir, "agent-deadbeef.jsonl")
	if err := os.WriteFile(subPath, []byte(subContent), 0o600); err != nil {
		t.Fatalf("write subagent transcript: %v", err)
	}

	ing, sink, _ := newTestIngester(t)
	res, err := Backfill(context.Background(), root, ing, 50)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if res.Files != 1 {
		t.Errorf("Files = %d, want 1 (the subagent transcript must be skipped)", res.Files)
	}
	if len(sink.sessions) != 1 {
		t.Errorf("created %d sessions, want 1", len(sink.sessions))
	}
	for extID := range sink.sessions {
		if strings.HasPrefix(extID, "agent-") {
			t.Errorf("found a session for a subagent transcript: %q", extID)
		}
	}
}

func TestBackfillMissingRootIsNotAnError(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	res, err := Backfill(context.Background(), filepath.Join(t.TempDir(), "nope"), ing, 50)
	if err != nil {
		t.Errorf("Backfill on a missing root returned %v, want nil", err)
	}
	if res.Files != 0 {
		t.Errorf("Files = %d, want 0", res.Files)
	}
}
