package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIngesterSkipsAgentOwnedTranscripts(t *testing.T) {
	ing, sink, _ := newTestIngester(t)

	const extID = "55555555-5555-4555-8555-555555555555"
	b, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := filepath.Join(t.TempDir(), extID+".jsonl")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// An agent owns this session: its chat path already persists these turns,
	// so ingesting them would duplicate every message.
	ing.SetAgentOwned([]string{extID})

	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n != 0 {
		t.Errorf("appended %d messages for an agent-owned transcript, want 0", n)
	}
	if sink.session(extID) != nil {
		t.Error("a session was created for an agent-owned transcript; the agent already has one")
	}
}

func TestIngesterStillIngestsUnownedTranscripts(t *testing.T) {
	ing, sink, _ := newTestIngester(t)
	ing.SetAgentOwned([]string{"somebody-else"})

	const extID = "11111111-2222-3333-4444-555555555555"
	b, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := filepath.Join(t.TempDir(), extID+".jsonl")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n == 0 {
		t.Error("an unowned transcript must still be ingested normally")
	}
	if sink.session(extID) == nil {
		t.Error("no session created for an unowned transcript")
	}
}

// TestSetAgentOwnedReplacesTheWholeSet pins the documented contract: callers
// pass the full list on every change, so an id dropped from a later call must
// stop being skipped. A merging implementation would keep skipping it forever
// and silently lose a de-bound agent's transcript.
func TestSetAgentOwnedReplacesTheWholeSet(t *testing.T) {
	ing, _, _ := newTestIngester(t)

	const extID = "11111111-2222-3333-4444-555555555555"
	ing.SetAgentOwned([]string{extID})
	if !ing.isAgentOwned(extID) {
		t.Fatal("id not marked owned after SetAgentOwned")
	}
	ing.SetAgentOwned([]string{"another-id"})
	if ing.isAgentOwned(extID) {
		t.Error("SetAgentOwned merged instead of replacing: a de-bound session is still skipped")
	}
}
