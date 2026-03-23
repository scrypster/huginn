package threadmgr

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// TestCreateFromMentions_NilSess_DoesNotPanic verifies that passing a nil
// *session.Session does not cause a nil-pointer dereference.
// The registry is kept empty so no mentions match — the nil-sess guard at the
// top of the function is tested without entering the spawn loop (which requires
// a valid store).
func TestCreateFromMentions_NilSess_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("CreateFromMentions panicked with nil sess: %v", r)
		}
	}()

	// Empty registry → ParseMentions returns nil → loop never runs → store never used.
	reg := agents.NewRegistry()
	tm := New()

	CreateFromMentions(
		context.Background(),
		"sess-1",
		"@Aria do something",
		"",
		reg,
		nil, // store: safe because no agent matches
		nil, // sess: this is what we're testing
		&mentionsStubBackend{},
		func(_ string, _ string, _ map[string]any) {},
		NewCostAccumulator(0),
		tm,
	)
}

// mentionsStubBackend satisfies backend.Backend with minimal stubs.
type mentionsStubBackend struct{}

func (s *mentionsStubBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	return nil, nil
}
func (s *mentionsStubBackend) Health(_ context.Context) error          { return nil }
func (s *mentionsStubBackend) Shutdown(_ context.Context) error        { return nil }
func (s *mentionsStubBackend) ContextWindow() int                      { return 0 }
