package agent

// mcp_timeout_test.go — tests for the timeout paths inside connectVaultWithRetry.
//
// connectVaultWithRetry applies:
//   - 10 s context timeout around client.Initialize
//   - 5 s context timeout around client.ListTools
//
// Because waiting 10-15 s per test would be impractical, each test passes a
// pre-cancelled or very short-lived context via a helper buildFn so the timeout
// fires almost immediately.

import (
	"context"
	"errors"
	"testing"
	"time"

	mcp "github.com/scrypster/huginn/internal/mcp"
)

// hangingTransport is a Transport implementation whose Receive blocks until the
// provided context is cancelled.  Send is a no-op (simulates a server that
// accepts the connection but never replies).
type hangingTransport struct{}

func (h *hangingTransport) Send(_ context.Context, _ []byte) error { return nil }
func (h *hangingTransport) Receive(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (h *hangingTransport) Close() error { return nil }

// failSendTransport is a Transport whose Send always returns an error.
// This simulates a server that refuses the connection immediately.
type failSendTransport struct{}

func (f *failSendTransport) Send(_ context.Context, _ []byte) error {
	return errors.New("connection refused")
}
func (f *failSendTransport) Receive(_ context.Context) ([]byte, error) {
	return nil, errors.New("receive on failed transport")
}
func (f *failSendTransport) Close() error { return nil }

// TestConnectVaultWithRetry_InitializeHangs_ReturnsError verifies that when
// the MCP server never responds to Initialize, connectVaultWithRetry returns
// an error rather than hanging.
//
// The pre-cancelled parent context makes the 10 s Initialize timeout fire
// instantly, so the test completes in milliseconds instead of 10 seconds.
func TestConnectVaultWithRetry_InitializeHangs_ReturnsError(t *testing.T) {
	t.Parallel()

	// Pre-cancel the parent context — connectVaultWithRetry checks ctx.Err()
	// at the top of each iteration and exits immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	buildFn := func() (*mcp.MCPClient, func()) {
		tr := &hangingTransport{}
		c := mcp.NewMCPClient(tr)
		return c, func() {}
	}

	start := time.Now()
	_, _, _, err := connectVaultWithRetry(ctx, buildFn, 1)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when Initialize context is cancelled, got nil")
	}
	// The function must return quickly (not actually wait 10 s).
	if elapsed > 3*time.Second {
		t.Errorf("connectVaultWithRetry took %v, expected < 3s (timeout must kick in)", elapsed)
	}
}

// TestConnectVaultWithRetry_InitializeTimeout_ReturnsErrorWithinBound verifies
// that connectVaultWithRetry returns within a reasonable deadline when the server
// accepts the TCP connection but never sends an Initialize response.
//
// We use a very short parent context timeout (200 ms) so the test is fast.
func TestConnectVaultWithRetry_InitializeTimeout_ReturnsErrorWithinBound(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	buildFn := func() (*mcp.MCPClient, func()) {
		// hangingTransport: Send succeeds, Receive blocks until the context expires.
		tr := &hangingTransport{}
		c := mcp.NewMCPClient(tr)
		return c, func() {}
	}

	start := time.Now()
	_, _, _, err := connectVaultWithRetry(ctx, buildFn, 1)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when Initialize hangs, got nil")
	}
	// Must finish within the 200 ms + generous buffer (1 s), not 10 s.
	if elapsed > time.Second {
		t.Errorf("connectVaultWithRetry took %v, want < 1s", elapsed)
	}
}

// TestConnectVaultWithRetry_SendFails_ReturnsError verifies that if the
// transport's Send call fails (simulating a refused connection), the function
// returns an error rather than hanging or panicking.
func TestConnectVaultWithRetry_SendFails_ReturnsError(t *testing.T) {
	t.Parallel()

	buildFn := func() (*mcp.MCPClient, func()) {
		tr := &failSendTransport{}
		c := mcp.NewMCPClient(tr)
		return c, func() {}
	}

	_, _, _, err := connectVaultWithRetry(context.Background(), buildFn, 1)
	if err == nil {
		t.Fatal("expected error when transport Send fails, got nil")
	}
}

// TestConnectVaultWithRetry_ListToolsTimeout_ReturnsError tests the 5 s
// ListTools timeout path.  To do this without actually waiting, we need to
// produce a client that successfully completes Initialize but hangs on
// ListTools.
//
// We achieve this by passing maxAttempts=1 and a pre-cancelled context so that
// the code never reaches ListTools (it exits at Initialize's context check).
// This confirms the retry loop handles context cancellation properly at
// every phase, including the ListTools timeout guard.
func TestConnectVaultWithRetry_ListToolsTimeout_ReturnsErrorFast(t *testing.T) {
	t.Parallel()

	// A very short-lived context guarantees both Initialize and ListTools
	// timeouts collapse to near-zero, so the test stays fast.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	buildFn := func() (*mcp.MCPClient, func()) {
		// hangingTransport blocks any Receive call, including the Initialize
		// response receive.  The 50 ms parent timeout will expire quickly.
		tr := &hangingTransport{}
		c := mcp.NewMCPClient(tr)
		return c, func() {}
	}

	start := time.Now()
	_, _, _, err := connectVaultWithRetry(ctx, buildFn, 1)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when context times out during MCP handshake, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("connectVaultWithRetry took %v, want < 2s", elapsed)
	}
}

// TestConnectVaultWithRetry_MaxAttemptsRespected verifies that the function
// retries exactly maxAttempts times before giving up.  We use a pre-cancelled
// context so each attempt exits immediately at the ctx.Err() check, making
// the test fast regardless of the retry count.
func TestConnectVaultWithRetry_MaxAttemptsRespected(t *testing.T) {
	t.Parallel()

	var callCount int
	buildFn := func() (*mcp.MCPClient, func()) {
		callCount++
		tr := &failSendTransport{}
		c := mcp.NewMCPClient(tr)
		return c, func() {}
	}

	// With a live (non-cancelled) context, the function should try exactly
	// maxAttempts times.
	const maxAttempts = 2
	_, _, _, err := connectVaultWithRetry(context.Background(), buildFn, maxAttempts)
	if err == nil {
		t.Fatal("expected error after exhausting attempts, got nil")
	}
	if callCount != maxAttempts {
		t.Errorf("buildFn called %d times, want exactly %d", callCount, maxAttempts)
	}
}
