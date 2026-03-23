package agent

import (
	"context"
	"errors"
	"testing"

	mcp "github.com/scrypster/huginn/internal/mcp"
)

// buildFailFn returns a buildFn that always produces a client whose
// Initialize call fails with the given error.
func buildFailFn(initErr error) func() (*mcp.MCPClient, func()) {
	return func() (*mcp.MCPClient, func()) {
		// A zero-value MCPClient points to no server so Initialize will fail.
		c := &mcp.MCPClient{}
		return c, func() {}
	}
}

// TestConnectVaultWithRetry_ZeroAttempts_ReturnsError verifies that
// maxAttempts=0 returns an error rather than silently returning (nil,nil,nil,nil).
func TestConnectVaultWithRetry_ZeroAttempts_ReturnsError(t *testing.T) {
	called := false
	fn := func() (*mcp.MCPClient, func()) {
		called = true
		return &mcp.MCPClient{}, func() {}
	}
	_, _, _, err := connectVaultWithRetry(context.Background(), fn, 0)
	if err == nil {
		t.Fatal("expected error for maxAttempts=0, got nil")
	}
	if called {
		t.Error("buildFn should not be called when maxAttempts=0")
	}
}

// TestConnectVaultWithRetry_NegativeAttempts_ReturnsError mirrors the zero case.
func TestConnectVaultWithRetry_NegativeAttempts_ReturnsError(t *testing.T) {
	_, _, _, err := connectVaultWithRetry(context.Background(), buildFailFn(errors.New("x")), -1)
	if err == nil {
		t.Fatal("expected error for negative maxAttempts, got nil")
	}
}

// TestConnectVaultWithRetry_CancelledContext_ReturnsCtxErr verifies that a
// pre-cancelled context is detected at the top of each iteration and the loop
// exits with the context error rather than continuing.
func TestConnectVaultWithRetry_CancelledContext_ReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, _, _, err := connectVaultWithRetry(ctx, buildFailFn(errors.New("init")), 3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
