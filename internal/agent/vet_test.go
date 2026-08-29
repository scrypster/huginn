package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// TestParseVetVerdict_Pass fails without the feature: prior to
// parseVetVerdict existing there was no way to turn a reviewer's reply into
// a structured "no findings" verdict.
func TestParseVetVerdict_Pass(t *testing.T) {
	r := parseVetVerdict("PASS: no findings")
	if r.Label != "no findings" {
		t.Errorf("Label = %q, want %q", r.Label, "no findings")
	}
	if r.Findings != "" {
		t.Errorf("Findings = %q, want empty", r.Findings)
	}
	if r.DidNotComplete() {
		t.Errorf("DidNotComplete() = true for a real pass")
	}
}

func TestParseVetVerdict_Findings(t *testing.T) {
	r := parseVetVerdict("FINDINGS:\n- missing error check in foo.go\n- untested branch in bar.go")
	if r.Label != "2 findings" {
		t.Errorf("Label = %q, want %q", r.Label, "2 findings")
	}
	if !strings.Contains(r.Findings, "missing error check") {
		t.Errorf("Findings dropped content: %q", r.Findings)
	}
}

func TestParseVetVerdict_SingularFinding(t *testing.T) {
	r := parseVetVerdict("FINDINGS:\n- one real problem")
	if r.Label != "1 finding" {
		t.Errorf("Label = %q, want singular %q", r.Label, "1 finding")
	}
}

func TestParseVetVerdict_EmptyReply_DidNotComplete(t *testing.T) {
	r := parseVetVerdict("   ")
	if !r.DidNotComplete() {
		t.Errorf("expected DidNotComplete() for empty reply, got Label=%q", r.Label)
	}
}

func TestParseVetVerdict_MessyReplyStillCountsAsFindings(t *testing.T) {
	// Doesn't match either exact format — must not be silently treated as a pass.
	r := parseVetVerdict("well the diff looks mostly fine but there's a race condition somewhere")
	if r.Label == "no findings" || r.DidNotComplete() {
		t.Errorf("messy reply must not resolve to pass or did-not-complete, got %q", r.Label)
	}
	if r.Findings == "" {
		t.Errorf("messy reply should carry its text forward as findings")
	}
}

// TestRunVetPass_HonestOnBackendError is the fail-open-WITH-HONESTY
// requirement: a backend error must produce "did not complete", never a
// silent pass and never a hang.
func TestRunVetPass_HonestOnBackendError(t *testing.T) {
	b := &mockBackend{errors: []error{errors.New("connection refused")}}
	ag := &agents.Agent{Name: "coder", ModelID: "qwen2.5-coder:14b"}
	res := RunVetPass(context.Background(), b, ag, "add a widget", "diff --git a/x.go b/x.go\n+widget")
	if !res.DidNotComplete() {
		t.Errorf("expected did-not-complete on backend error, got Label=%q", res.Label)
	}
}

// TestRunVetPass_HonestOnTimeout: a reviewer backend slower than the
// caller's deadline must not block RunVetPass past that bound, and must
// report honestly. RunVetPass derives its internal deadline via
// context.WithTimeout(ctx, VetTimeout) — passing a ctx that already carries
// a much shorter deadline than VetTimeout (120s) lets this test observe
// real timeout behavior without waiting anywhere near that long.
func TestRunVetPass_HonestOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	b := &slowBackend{delay: 2 * time.Second}
	ag := &agents.Agent{Name: "coder", ModelID: "qwen2.5-coder:14b"}

	start := time.Now()
	res := RunVetPass(ctx, b, ag, "task", "diff")
	elapsed := time.Since(start)

	if !res.DidNotComplete() {
		t.Errorf("expected did-not-complete on timeout, got Label=%q", res.Label)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("RunVetPass did not respect the caller's deadline: took %v", elapsed)
	}
}

func TestRunVetPass_UsesSameModelAsOwningAgent(t *testing.T) {
	b := &mockBackend{responses: []*backend.ChatResponse{{Content: "PASS: no findings", DoneReason: "stop"}}}
	ag := &agents.Agent{Name: "coder", ModelID: "qwen2.5-coder:14b", Provider: "ollama"}
	res := RunVetPass(context.Background(), b, ag, "task", "diff")
	if res.Label != "no findings" {
		t.Fatalf("Label = %q, want no findings", res.Label)
	}
	if len(b.lastRequests) != 1 {
		t.Fatalf("expected exactly 1 backend call, got %d", len(b.lastRequests))
	}
	if b.lastRequests[0].Model != ag.GetModelID() {
		t.Errorf("reviewer model = %q, want same model as owning agent %q", b.lastRequests[0].Model, ag.GetModelID())
	}
}

func TestRunVetPass_NilInputsFailHonestly(t *testing.T) {
	if r := RunVetPass(context.Background(), nil, &agents.Agent{}, "t", "d"); !r.DidNotComplete() {
		t.Errorf("nil backend should fail honestly, got %q", r.Label)
	}
	if r := RunVetPass(context.Background(), &mockBackend{}, nil, "t", "d"); !r.DidNotComplete() {
		t.Errorf("nil agent should fail honestly, got %q", r.Label)
	}
}

// slowBackend blocks until ctx is cancelled or delay elapses, whichever is
// first — used to prove RunVetPass respects its timeout bound.
type slowBackend struct{ delay time.Duration }

func (s *slowBackend) ChatCompletion(ctx context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	select {
	case <-time.After(s.delay):
		return &backend.ChatResponse{Content: "PASS: no findings", DoneReason: "stop"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowBackend) Health(_ context.Context) error   { return nil }
func (s *slowBackend) Shutdown(_ context.Context) error { return nil }
func (s *slowBackend) ContextWindow() int               { return 128_000 }
