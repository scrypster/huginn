package claudecode

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// fakeSink records what the ingester writes. From Task 7 on, the watcher's
// goroutine writes to it concurrently with the test goroutine polling it, so
// every access is guarded by mu.
type fakeSink struct {
	mu       sync.Mutex
	sessions map[string]*session.Session // keyed by external id
	msgs     map[string][]session.SessionMessage
	threads  map[string][]session.SessionMessage
	nextID   int
}

func newFakeSink() *fakeSink {
	return &fakeSink{
		sessions: map[string]*session.Session{},
		msgs:     map[string][]session.SessionMessage{},
		threads:  map[string][]session.SessionMessage{},
	}
}

func (f *fakeSink) LoadByExternalID(kind, extID string) (*session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[extID], nil
}

func (f *fakeSink) New(title, workspaceRoot, model string) *session.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := "SESS" + string(rune('0'+f.nextID))
	return &session.Session{ID: id, Manifest: session.Manifest{
		ID: id, SessionID: id, Title: title,
		WorkspaceRoot: workspaceRoot, Model: model, Status: "active",
	}}
}

func (f *fakeSink) SaveManifest(s *session.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.Manifest.ExternalID] = s
	return nil
}

func (f *fakeSink) Append(s *session.Session, m session.SessionMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs[s.ID] = append(f.msgs[s.ID], m)
	return nil
}

func (f *fakeSink) AppendToThread(sessionID, threadID string, m session.SessionMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.threads[sessionID+"/"+threadID] = append(f.threads[sessionID+"/"+threadID], m)
	return nil
}

// session safely reads back a session recorded by SaveManifest, keyed by
// external id. Tests should poll through this rather than sink.sessions.
func (f *fakeSink) session(extID string) *session.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[extID]
}

// messageCount safely reads the number of messages appended for a session,
// keyed by the Huginn session id. Tests should poll through this rather than
// sink.msgs.
func (f *fakeSink) messageCount(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs[sessionID])
}

type fakeBroadcaster struct {
	mu    sync.Mutex
	calls int
}

func (b *fakeBroadcaster) BroadcastToSession(string, string, map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
}

func (b *fakeBroadcaster) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func newTestIngester(t *testing.T) (*Ingester, *fakeSink, *fakeBroadcaster) {
	t.Helper()
	sink := newFakeSink()
	bc := &fakeBroadcaster{}
	return NewIngester(sink, NewIngestStore(newTestDB(t)), bc), sink, bc
}

// copyFixture copies testdata/basic.jsonl into a temp dir under the file name
// Claude Code would use, so the ingester derives the session UUID from it.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := filepath.Join(t.TempDir(), "11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestIngestFileCreatesSessionAndMessages(t *testing.T) {
	ing, sink, bc := newTestIngester(t)
	p := copyFixture(t, "basic.jsonl")

	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n != 3 {
		t.Errorf("appended %d messages, want 3", n)
	}

	sess := sink.session("11111111-2222-3333-4444-555555555555")
	if sess == nil {
		t.Fatal("no session created for the transcript's session id")
	}
	if sess.Manifest.ExternalKind != "claude-code" {
		t.Errorf("ExternalKind = %q", sess.Manifest.ExternalKind)
	}
	if sess.Manifest.WorkspaceRoot != "/tmp/proj" {
		t.Errorf("WorkspaceRoot = %q, want /tmp/proj", sess.Manifest.WorkspaceRoot)
	}
	if sess.Manifest.Title != "Fix the auth bug" {
		t.Errorf("Title = %q, want the custom-title value", sess.Manifest.Title)
	}
	if sess.Manifest.Model != "claude-opus-5" {
		t.Errorf("Model = %q", sess.Manifest.Model)
	}
	if bc.callCount() == 0 {
		t.Error("expected at least one broadcast")
	}
}

func TestIngestFileIsIdempotent(t *testing.T) {
	ing, sink, _ := newTestIngester(t)
	p := copyFixture(t, "basic.jsonl")

	if _, err := ing.IngestFile(p); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if n != 0 {
		t.Errorf("second ingest appended %d messages, want 0", n)
	}

	sess := sink.session("11111111-2222-3333-4444-555555555555")
	if got := sink.messageCount(sess.ID); got != 3 {
		t.Errorf("total messages = %d, want 3", got)
	}
}

func TestIngestFileIgnoresNonTranscriptNames(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n != 0 {
		t.Errorf("appended %d messages for a non-transcript file, want 0", n)
	}
}

func TestIngestFilePersistsToolResultFromLaterRead(t *testing.T) {
	ing, sink, _ := newTestIngester(t)

	const extID = "22222222-2222-3333-4444-555555555555"
	p := filepath.Join(t.TempDir(), extID+".jsonl")

	u1 := `{"type":"user","uuid":"x1","sessionId":"` + extID + `","cwd":"/tmp/p2","timestamp":"2026-08-25T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"read it"}]}}` + "\n"
	a1 := `{"type":"assistant","uuid":"x2","parentUuid":"x1","sessionId":"` + extID + `","timestamp":"2026-08-25T10:00:01.000Z","message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"reading"},{"type":"tool_use","id":"tt1","name":"Read","input":{"file_path":"/tmp/p2/a.go"}}],"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	u2 := `{"type":"user","uuid":"x3","parentUuid":"x2","sessionId":"` + extID + `","timestamp":"2026-08-25T10:00:09.000Z","toolUseResult":{"stdout":"package main"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tt1","content":"package main"}]}}` + "\n"

	// Read 1: the assistant turn is written, but its tool is still running.
	writeFile(t, p, u1+a1)
	n1, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("first ingest appended %d, want 1 (the assistant turn must be held back until its tool result arrives)", n1)
	}

	// Read 2: the tool finished and its result was appended to the transcript.
	appendFile(t, p, u2)
	n2, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if n2 != 1 {
		t.Fatalf("second ingest appended %d, want 1 (the held assistant turn, now resolved)", n2)
	}

	sess := sink.session(extID)
	if sess == nil {
		t.Fatal("no session created")
	}
	msgs := sink.msgs[sess.ID]
	if len(msgs) != 2 {
		t.Fatalf("persisted %d messages, want 2", len(msgs))
	}

	var found bool
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		if len(m.ToolCalls) != 1 {
			t.Fatalf("assistant message has %d tool calls, want 1", len(m.ToolCalls))
		}
		if m.ToolCalls[0].Result != "package main" {
			t.Errorf("PERSISTED tool result = %q, want %q — the result arrived in a later read and must reach the stored message", m.ToolCalls[0].Result, "package main")
		}
		found = true
	}
	if !found {
		t.Error("no assistant message was persisted")
	}
}

func TestFlushIdlePersistsUnresolvedHeldMessage(t *testing.T) {
	ing, sink, _ := newTestIngester(t)

	const extID = "44444444-4444-3333-4444-555555555555"
	p := filepath.Join(t.TempDir(), extID+".jsonl")

	u1 := `{"type":"user","uuid":"z1","sessionId":"` + extID + `","cwd":"/tmp/p4","timestamp":"2026-08-25T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"go"}]}}` + "\n"
	a1 := `{"type":"assistant","uuid":"z2","parentUuid":"z1","sessionId":"` + extID + `","timestamp":"2026-08-25T10:00:01.000Z","message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"running a tool"},{"type":"tool_use","id":"zz1","name":"Bash","input":{"command":"true"}}]}}` + "\n"

	writeFile(t, p, u1+a1)
	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if n != 1 {
		t.Fatalf("appended %d, want 1 (assistant turn held: its tool call never resolves)", n)
	}

	sess := sink.session(extID)
	if sess == nil {
		t.Fatal("no session created")
	}
	if got := len(sink.msgs[sess.ID]); got != 1 {
		t.Fatalf("persisted %d before flush, want 1", got)
	}

	// The conversation ends here — the tool result never arrives. Without
	// FlushIdle the assistant turn would sit in memory forever.
	ing.FlushIdle()

	msgs := sink.msgs[sess.ID]
	if len(msgs) != 2 {
		t.Fatalf("persisted %d after FlushIdle, want 2", len(msgs))
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "running a tool" {
		t.Errorf("flushed message = %q/%q, want assistant/\"running a tool\"", msgs[1].Role, msgs[1].Content)
	}
	if len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].Result != "" {
		t.Errorf("flushed message should keep its unresolved tool call with an empty result, got %+v", msgs[1].ToolCalls)
	}

	// FlushIdle must be idempotent — a second call appends nothing.
	ing.FlushIdle()
	if got := len(sink.msgs[sess.ID]); got != 2 {
		t.Errorf("second FlushIdle appended more messages: %d, want 2", got)
	}
}
