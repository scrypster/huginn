package permissions

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

func TestCheckDetailed_DeniesProviderWithReasonCode(t *testing.T) {
	g := NewGate(false, nil)
	t.Cleanup(g.Close)
	g.SetAllowedProviders(map[string]bool{"github": true})

	res := g.CheckDetailed(PermissionRequest{
		ToolName: "slack_post",
		Level:    tools.PermWrite,
		Provider: "slack",
	})
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.ReasonCode != ReasonProviderNotAllowed {
		t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonProviderNotAllowed)
	}
	if res.Reason == "" {
		t.Fatal("expected non-empty safe reason")
	}
}

func TestCheckDetailed_DeniesWhenPromptUnavailable(t *testing.T) {
	g := NewGate(false, nil)
	t.Cleanup(g.Close)

	res := g.CheckDetailed(PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
	})
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.ReasonCode != ReasonPromptUnavailable {
		t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonPromptUnavailable)
	}
}

func TestCheckDetailed_DeniesWhenUserRejects(t *testing.T) {
	g := NewGate(false, func(PermissionRequest) Decision { return Deny })
	t.Cleanup(g.Close)

	res := g.CheckDetailed(PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
	})
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.ReasonCode != ReasonUserDenied {
		t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonUserDenied)
	}
}

// TestCheckDetailedCtx_CancelledMidPromptDeniesImmediately verifies that
// cancelling the caller's context while a permission prompt is pending
// (e.g. chat_cancel arriving mid-prompt) unblocks CheckDetailedCtx right
// away with ReasonCancelled, rather than waiting out the full
// promptFuncTimeout before denying.
func TestCheckDetailedCtx_CancelledMidPromptDeniesImmediately(t *testing.T) {
	promptStarted := make(chan struct{})
	blockForever := make(chan Decision) // never sent to — promptFunc hangs until the test ends
	g := NewGate(false, func(PermissionRequest) Decision {
		close(promptStarted)
		return <-blockForever
	})
	t.Cleanup(g.Close)

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan CheckResult, 1)
	go func() {
		resultCh <- g.CheckDetailedCtx(ctx, PermissionRequest{
			ToolName: "bash",
			Level:    tools.PermExec,
		})
	}()

	<-promptStarted
	cancel()

	select {
	case res := <-resultCh:
		if res.Allowed {
			t.Fatal("expected deny on cancellation")
		}
		if res.ReasonCode != ReasonCancelled {
			t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonCancelled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CheckDetailedCtx did not unblock promptly after ctx cancellation")
	}
}

// TestCheckDetailedCtx_PromptFuncCtxReceivesCancellation verifies that when a
// context-aware bridge is wired via SetPromptFuncCtx, cancelling the caller's
// context is observed inside the bridge itself — not just abandoned by the
// gate's outer select — so a bridge like the server's WS round trip can stop
// blocking on its own resolve channel.
func TestCheckDetailedCtx_PromptFuncCtxReceivesCancellation(t *testing.T) {
	g := NewGate(false, nil)
	t.Cleanup(g.Close)

	bridgeSawCancel := make(chan bool, 1)
	g.SetPromptFuncCtx(func(ctx context.Context, req PermissionRequest) Decision {
		<-ctx.Done()
		bridgeSawCancel <- true
		return Deny
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	res := g.CheckDetailedCtx(ctx, PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
	})
	if res.Allowed {
		t.Fatal("expected deny on cancellation")
	}

	select {
	case <-bridgeSawCancel:
	case <-time.After(2 * time.Second):
		t.Fatal("promptFuncCtx bridge never observed ctx cancellation")
	}
}

// TestCheckDetailedCtx_NormalApproveUnaffected verifies plain approve/deny
// flows are unaffected by the ctx plumbing when the context is never
// cancelled.
func TestCheckDetailedCtx_NormalApproveUnaffected(t *testing.T) {
	g := NewGate(false, func(PermissionRequest) Decision { return Allow })
	t.Cleanup(g.Close)

	res := g.CheckDetailedCtx(context.Background(), PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
	})
	if !res.Allowed {
		t.Fatalf("expected allow, got deny: %+v", res)
	}
}
