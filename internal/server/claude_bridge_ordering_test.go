package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/claudecode"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// orderingSink records WHEN the ingester first touched the store. The embedded
// nil interface satisfies session.StoreInterface at compile time; only the
// methods claudecode.SessionSink actually calls are implemented, so anything
// else would panic loudly rather than pass silently.
type orderingSink struct {
	session.StoreInterface

	seq *atomic.Int64

	mu           sync.Mutex
	firstReadSeq int64
	seen         []string
}

func (f *orderingSink) LoadByExternalID(_, extID string) (*session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.firstReadSeq == 0 {
		f.firstReadSeq = f.seq.Add(1)
	}
	f.seen = append(f.seen, extID)
	return nil, nil
}
func (f *orderingSink) New(title, workspaceRoot, model string) *session.Session {
	return &session.Session{
		ID: "sess-" + title,
		Manifest: session.Manifest{
			Title:         title,
			WorkspaceRoot: workspaceRoot,
			Model:         model,
		},
	}
}
func (f *orderingSink) SaveManifest(*session.Session) error                   { return nil }
func (f *orderingSink) Append(*session.Session, session.SessionMessage) error { return nil }
func (f *orderingSink) AppendToThread(string, string, session.SessionMessage) error {
	return nil
}

func (f *orderingSink) snapshot() (int64, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.firstReadSeq, append([]string(nil), f.seen...)
}

// TestOwnedSetIsPublishedBeforeBackfillReadsAnything asserts the ORDERING, not
// the outcome.
//
// StartClaudeBridge creates the ingester and launches backfill in one call. A
// caller that publishes the agent-owned set afterwards leaves a window in which
// backfill can ingest an agent-owned transcript — and ingestion is append-only,
// so those duplicate messages are permanent. A "no duplicates" assertion could
// pass by luck; this compares sequence numbers, so it cannot.
func TestOwnedSetIsPublishedBeforeBackfillReadsAnything(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	// TestMain sandboxes HOME, so this is claudecode.DefaultRoot() under a
	// temp dir, never the developer's real ~/.claude.
	root := claudecode.DefaultRoot()
	if root == "" || !filepathHasPrefix(root, home) {
		t.Fatalf("DefaultRoot() = %q is not inside the sandboxed home %q", root, home)
	}
	proj := filepath.Join(root, "-tmp-project")
	if err := os.MkdirAll(proj, 0o750); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	const ownedID = "11111111-1111-4111-8111-111111111111"
	const otherID = "22222222-2222-4222-8222-222222222222"
	line := []byte(`{"type":"mode","mode":"default"}` + "\n")
	for _, id := range []string{ownedID, otherID} {
		if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), line, 0o600); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}

	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var seq atomic.Int64
	sink := &orderingSink{seq: &seq}
	s := &Server{store: sink}

	// The source deliberately dawdles before recording its step, and it dawdles
	// by WATCHING for backfill to touch the store. This is what makes the
	// ordering assertion discriminating rather than lucky: if the bridge
	// publishes the owned set only after launching backfill, backfill gets this
	// whole window to read the store first and the sequence numbers invert. If
	// the bridge publishes before launching anything — the correct order —
	// nothing is running yet, the watch simply times out, and the ordering
	// holds structurally no matter how long the window is.
	var sourceSeq atomic.Int64
	s.SetClaudeAgentOwnedSource(func() []string {
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if first, _ := sink.snapshot(); first != 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		sourceSeq.Store(seq.Add(1))
		return []string{ownedID}
	})

	cfg := claudecode.DefaultConfig()
	cfg.Enabled = true
	cfg.Watch.Backfill = true
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.StartClaudeBridge(ctx, cfg, db); err != nil {
		t.Fatalf("StartClaudeBridge: %v", err)
	}

	// Wait for backfill to touch the store.
	deadline := time.Now().Add(10 * time.Second)
	var firstRead int64
	var seen []string
	for time.Now().Before(deadline) {
		firstRead, seen = sink.snapshot()
		if firstRead != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if firstRead == 0 {
		t.Fatal("backfill never read the store; the ordering assertion below would be vacuous")
	}

	got := sourceSeq.Load()
	if got == 0 {
		t.Fatal("the owned-set source was never consulted: nothing told the bridge which transcripts to skip")
	}
	if got >= firstRead {
		t.Errorf("owned set published at step %d but backfill read the store at step %d: "+
			"an agent-owned transcript can be ingested in that window, and ingestion is append-only", got, firstRead)
	}

	// And the consequence of that ordering: the owned transcript was skipped
	// while the unowned one was not.
	for _, id := range seen {
		if id == ownedID {
			t.Errorf("backfill resolved a session for the agent-owned transcript %q", ownedID)
		}
	}
	var sawOther bool
	for _, id := range seen {
		if id == otherID {
			sawOther = true
		}
	}
	if !sawOther {
		t.Errorf("backfill never reached the unowned transcript; suppression cannot be distinguished from doing nothing (seen=%v)", seen)
	}
}

// filepathHasPrefix reports whether p lies inside prefix. Used only to prove
// the test is writing under the sandboxed HOME, never the real ~/.claude.
func filepathHasPrefix(p, prefix string) bool {
	rel, err := filepath.Rel(prefix, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
