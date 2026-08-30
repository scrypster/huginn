package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// vetTestRepo creates a tiny real git repo so captureDiff has something to
// see, mirroring initCheckpointTestRepo's shape.
func vetTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "initial")
	return dir
}

// countingBackend counts ChatCompletion calls and always returns a fixed
// reply — used to assert the reviewer runs exactly once (no recursion, no
// duplicate spawn).
type countingBackend struct {
	mu      sync.Mutex
	calls   int
	content string
	delay   time.Duration
}

func (c *countingBackend) ChatCompletion(ctx context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &backend.ChatResponse{Content: c.content, DoneReason: "stop"}, nil
}
func (c *countingBackend) Health(_ context.Context) error   { return nil }
func (c *countingBackend) Shutdown(_ context.Context) error { return nil }
func (c *countingBackend) ContextWindow() int               { return 128_000 }

func (c *countingBackend) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func waitForVetLabel(t *testing.T, tm *threadmgr.ThreadManager, threadID string) *threadmgr.Thread {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if th, ok := tm.Get(threadID); ok && th.Summary != nil && th.Summary.VetLabel != "" {
			return th
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("thread %s never got a vet label within the deadline", threadID)
	return nil
}

// TestInitVet_SpawnsReviewerExactlyOnceOnFileChanges fails without the
// feature: before initVet existed, a completed thread with vet_work on and
// file changes never got a reviewer pass at all — Summary.VetLabel would
// stay empty forever and the backend would see zero extra calls.
func TestInitVet_SpawnsReviewerExactlyOnceOnFileChanges(t *testing.T) {
	dir := vetTestRepo(t)
	tm := threadmgr.New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "coder", ModelID: "qwen2.5-coder:14b", VetWork: true})

	b := &countingBackend{content: "PASS: no findings"}
	teardown := initVet(dir, reg, tm, nil, b)
	defer teardown()

	th, err := tm.Create(threadmgr.CreateParams{SessionID: "s1", AgentID: "coder", Task: "bump version"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.Start(th.ID, nil, func() {})
	tm.Complete(th.ID, threadmgr.FinishSummary{
		Summary:       "bumped the version",
		FilesModified: []string{"app.go"},
		Status:        "completed",
	})

	got := waitForVetLabel(t, tm, th.ID)
	if got.Summary.VetLabel != "no findings" {
		t.Errorf("VetLabel = %q, want %q", got.Summary.VetLabel, "no findings")
	}
	if b.callCount() != 1 {
		t.Errorf("expected exactly 1 reviewer call, got %d", b.callCount())
	}
	if !contains(got.Summary.Summary, "Vetted: no findings") {
		t.Errorf("Summary text missing vet verdict: %q", got.Summary.Summary)
	}
}

// TestInitVet_SkipsWhenVetWorkOff proves the harness respects the
// per-agent opt-in: no file-change thread for an agent with vet_work off
// should ever trigger a reviewer call.
func TestInitVet_SkipsWhenVetWorkOff(t *testing.T) {
	dir := vetTestRepo(t)
	tm := threadmgr.New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "builder", ModelID: "qwen2.5-coder:14b", VetWork: false})

	b := &countingBackend{content: "PASS: no findings"}
	teardown := initVet(dir, reg, tm, nil, b)
	defer teardown()

	th, err := tm.Create(threadmgr.CreateParams{SessionID: "s1", AgentID: "builder", Task: "ship it"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.Start(th.ID, nil, func() {})
	tm.Complete(th.ID, threadmgr.FinishSummary{
		Summary:       "shipped",
		FilesModified: []string{"app.go"},
		Status:        "completed",
	})

	// Give any (incorrect) async spawn a moment, then assert it never happened.
	time.Sleep(150 * time.Millisecond)
	if b.callCount() != 0 {
		t.Errorf("expected 0 reviewer calls for vet_work=false, got %d", b.callCount())
	}
	got, ok := tm.Get(th.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Summary.VetLabel != "" {
		t.Errorf("VetLabel should be empty when vet_work is off, got %q", got.Summary.VetLabel)
	}
}

// TestInitVet_SkipsWhenNoFileChanges proves a completed thread with no
// FilesModified never spawns a reviewer, even with vet_work on.
func TestInitVet_SkipsWhenNoFileChanges(t *testing.T) {
	dir := vetTestRepo(t)
	tm := threadmgr.New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "coder", ModelID: "qwen2.5-coder:14b", VetWork: true})

	b := &countingBackend{content: "PASS: no findings"}
	teardown := initVet(dir, reg, tm, nil, b)
	defer teardown()

	th, err := tm.Create(threadmgr.CreateParams{SessionID: "s1", AgentID: "coder", Task: "answer a question"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.Start(th.ID, nil, func() {})
	tm.Complete(th.ID, threadmgr.FinishSummary{Summary: "here's the answer", Status: "completed"})

	time.Sleep(150 * time.Millisecond)
	if b.callCount() != 0 {
		t.Errorf("expected 0 reviewer calls with no file changes, got %d", b.callCount())
	}
}

// TestAttachVetResult_Idempotent proves the one-vet-per-thread cap: a
// second AttachVetResult call for the same thread must be a no-op.
func TestAttachVetResult_Idempotent(t *testing.T) {
	tm := threadmgr.New()
	th, err := tm.Create(threadmgr.CreateParams{SessionID: "s1", AgentID: "coder", Task: "x"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.Start(th.ID, nil, func() {})
	tm.Complete(th.ID, threadmgr.FinishSummary{Summary: "done", Status: "completed"})

	if !tm.AttachVetResult(th.ID, "no findings", "") {
		t.Fatal("first AttachVetResult should succeed")
	}
	if tm.AttachVetResult(th.ID, "3 findings", "should not land") {
		t.Fatal("second AttachVetResult should be a no-op (cap: one vet per thread)")
	}
	got, _ := tm.Get(th.ID)
	if got.Summary.VetLabel != "no findings" {
		t.Errorf("VetLabel was overwritten by the second call: %q", got.Summary.VetLabel)
	}
}

// TestCaptureDiff_UntrackedFileIncludesContent fails without the fix: the
// old captureDiff ran a plain `git diff -- <files>` which is structurally
// blind to untracked (newly-created) files — a run that only CREATES a
// file got an empty diff and the reviewer never saw the file at all.
func TestCaptureDiff_UntrackedFileIncludesContent(t *testing.T) {
	dir := vetTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "brand_new.go"), []byte("package app\n\nfunc DoTheThing() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := captureDiff(dir, []string{"brand_new.go"})
	if got == "" {
		t.Fatal("captureDiff returned empty for a newly-created (untracked) file")
	}
	if !contains(got, "DoTheThing") {
		t.Errorf("captureDiff did not include the untracked file's content: %q", got)
	}
}

// TestCaptureDiff_CommittedWorkIsCaptured fails without the fix: plain
// `git diff` is empty once the working tree matches HEAD (i.e. the agent
// committed its change), so the old captureDiff returned "" for work the
// agent already committed.
func TestCaptureDiff_CommittedWorkIsCaptured(t *testing.T) {
	dir := vetTestRepo(t)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n\nfunc CommittedChange() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "add CommittedChange")

	got := captureDiff(dir, []string{"app.go"})
	if got == "" {
		t.Fatal("captureDiff returned empty for work the agent already committed")
	}
	if !contains(got, "CommittedChange") {
		t.Errorf("captureDiff did not include the committed change: %q", got)
	}
}

// TestInitVet_HonestyBackstop_NoDiffCaptured is the HONESTY BACKSTOP: when
// capture yields nothing at all (file never existed, never touched), the
// thread must NEVER end up labelled as if it passed review — even though
// the reviewer backend below would happily reply "PASS: no findings" if it
// were ever called. Before the fix, RunVetPass substituted a placeholder
// string for the empty diff, the model was asked to grade that placeholder,
// and a "PASS" reply became "Vetted: no findings" — a green from a check
// that never looked at any code.
func TestInitVet_HonestyBackstop_NoDiffCaptured(t *testing.T) {
	dir := vetTestRepo(t)
	tm := threadmgr.New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "coder", ModelID: "qwen2.5-coder:14b", VetWork: true})

	b := &countingBackend{content: "PASS: no findings"}
	teardown := initVet(dir, reg, tm, nil, b)
	defer teardown()

	th, err := tm.Create(threadmgr.CreateParams{SessionID: "s1", AgentID: "coder", Task: "bump version"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.Start(th.ID, nil, func() {})
	tm.Complete(th.ID, threadmgr.FinishSummary{
		Summary:       "bumped the version",
		FilesModified: []string{"ghost-file-that-never-existed.go"},
		Status:        "completed",
	})

	got := waitForVetLabel(t, tm, th.ID)
	if got.Summary.VetLabel != "not vetted — no diff captured" {
		t.Errorf("VetLabel = %q, want the honesty-backstop label", got.Summary.VetLabel)
	}
	if b.callCount() != 0 {
		t.Errorf("reviewer backend should never be called when nothing was captured, got %d calls", b.callCount())
	}
}

// panickingBackend panics on every call — used to prove the vet goroutine
// recovers instead of crashing the process.
type panickingBackend struct{}

func (p *panickingBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	panic("simulated backend panic")
}
func (p *panickingBackend) Health(_ context.Context) error   { return nil }
func (p *panickingBackend) Shutdown(_ context.Context) error { return nil }
func (p *panickingBackend) ContextWindow() int               { return 128_000 }

// TestInitVet_ReviewerPanicIsRecovered fails without the fix: before the
// goroutine had a recover(), a panic inside the reviewer pass (e.g. a
// backend that panics instead of returning an error) would crash the whole
// process — a single misbehaving backend taking down a server with many
// other threads in flight. With the fix, the panic is caught and logged;
// the thread simply never gets a VetLabel, and the test process survives.
func TestInitVet_ReviewerPanicIsRecovered(t *testing.T) {
	dir := vetTestRepo(t)
	tm := threadmgr.New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "coder", ModelID: "qwen2.5-coder:14b", VetWork: true})

	teardown := initVet(dir, reg, tm, nil, &panickingBackend{})
	defer teardown()

	th, err := tm.Create(threadmgr.CreateParams{SessionID: "s1", AgentID: "coder", Task: "bump version"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.Start(th.ID, nil, func() {})
	tm.Complete(th.ID, threadmgr.FinishSummary{
		Summary:       "bumped the version",
		FilesModified: []string{"app.go"},
		Status:        "completed",
	})

	// If the panic wasn't recovered, this test process would already be
	// dead by now. Just give the goroutine a moment and confirm we're
	// still alive with no VetLabel ever landing.
	time.Sleep(150 * time.Millisecond)
	got, ok := tm.Get(th.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Summary.VetLabel != "" {
		t.Errorf("expected no VetLabel after a panicking reviewer pass, got %q", got.Summary.VetLabel)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
