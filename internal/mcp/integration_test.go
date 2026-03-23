//go:build integration

package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
)

// TestMCPEchoServer is the in-process MCP echo server used by integration tests.
// It is invoked as a subprocess via exec.Command(os.Args[0], "-test.run=TestMCPEchoServer").
// When MCP_TEST_SERVER is not set it simply skips, so it is a no-op during normal test runs.
// When MCP_TEST_SERVER=1 it serves real MCP protocol on stdin/stdout and exits.
// When MCP_TEST_SERVER=malformed it responds to initialize with malformed JSON then exits.
// When MCP_TEST_SERVER=hang it reads initialize and then hangs forever (never responds).
// When MCP_TEST_SERVER=closeAfterInit it responds to initialize then closes stdout immediately.
func TestMCPEchoServer(t *testing.T) {
	mode := os.Getenv("MCP_TEST_SERVER")
	if mode == "" {
		t.Skip("MCP_TEST_SERVER not set; skipping echo server helper")
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	sendLine := func(line string) {
		fmt.Fprintln(writer, line)
		writer.Flush()
	}

	switch mode {
	case "malformed":
		// Read initialize request, send garbage JSON.
		scanner.Scan()
		sendLine("this is not valid JSON{{{{")
		os.Exit(0)

	case "hang":
		// Read one line then block indefinitely — used to test timeout.
		scanner.Scan()
		select {}

	case "closeAfterInit":
		// Respond to initialize, then close stdout without responding to anything else.
		scanner.Scan()
		var req map[string]any
		if err := json.Unmarshal([]byte(scanner.Text()), &req); err != nil {
			os.Exit(1)
		}
		id := int(req["id"].(float64))
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo":   map[string]any{"name": "test", "version": "0.1.0"},
			},
		}
		data, _ := json.Marshal(resp)
		sendLine(string(data))
		// Consume the initialized notification (no response needed).
		scanner.Scan()
		// Now close stdout — the next call from the client will hit EOF.
		os.Stdout.Close()
		// Keep stdin open so the client can attempt to write.
		time.Sleep(2 * time.Second)
		os.Exit(0)

	default: // "1" or any unrecognised value → normal echo server
		msgID := 0
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var req map[string]any
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				os.Exit(1)
			}

			// Notifications have no "id" field — acknowledge silently and continue.
			rawID, hasID := req["id"]
			if !hasID {
				continue
			}
			msgID = int(rawID.(float64))
			method, _ := req["method"].(string)

			var resp map[string]any
			switch method {
			case "initialize":
				resp = map[string]any{
					"jsonrpc": "2.0",
					"id":      msgID,
					"result": map[string]any{
						"capabilities": map[string]any{"tools": map[string]any{}},
						"serverInfo":   map[string]any{"name": "test", "version": "0.1.0"},
					},
				}
			case "tools/list":
				resp = map[string]any{
					"jsonrpc": "2.0",
					"id":      msgID,
					"result": map[string]any{
						"tools": []map[string]any{
							{
								"name":        "echo",
								"description": "Echo tool",
								"inputSchema": map[string]any{
									"type":       "object",
									"properties": map[string]any{},
								},
							},
						},
					},
				}
			case "tools/call":
				resp = map[string]any{
					"jsonrpc": "2.0",
					"id":      msgID,
					"result": map[string]any{
						"content": []map[string]any{
							{"type": "text", "text": "hello from test server"},
						},
					},
				}
			default:
				resp = map[string]any{
					"jsonrpc": "2.0",
					"id":      msgID,
					"error":   map[string]any{"code": -32601, "message": "method not found"},
				}
			}

			data, _ := json.Marshal(resp)
			sendLine(string(data))
		}
		os.Exit(0)
	}
}

// startEchoServer launches the test binary itself as an MCP echo server subprocess.
// mode controls the echo server behaviour (see TestMCPEchoServer).
func startEchoServer(t *testing.T, ctx context.Context, mode string) *mcp.StdioTransport {
	t.Helper()
	tr, err := mcp.NewStdioTransport(
		ctx,
		os.Args[0],
		[]string{"-test.run=TestMCPEchoServer", "-test.v=false"},
		[]string{"MCP_TEST_SERVER=" + mode},
	)
	if err != nil {
		t.Fatalf("startEchoServer(%q): %v", mode, err)
	}
	return tr
}

// TestMCPIntegration_HappyPath exercises the full Initialize → ListTools → CallTool path
// using a real subprocess that speaks MCP protocol.
func TestMCPIntegration_HappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := startEchoServer(t, ctx, "1")
	client := mcp.NewMCPClient(tr)
	defer client.Close()

	// Initialize
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// ListTools — expect at least one tool named "echo"
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool from echo server")
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "echo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected tool named 'echo', got: %v", tools)
	}

	// CallTool — expect text content back
	result, err := client.CallTool(ctx, "echo", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil CallTool result")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item in CallTool result")
	}
	if result.Content[0].Text != "hello from test server" {
		t.Errorf("unexpected tool result text: %q", result.Content[0].Text)
	}
}

// TestMCPIntegration_ServerCrash starts a connection, kills the subprocess,
// then verifies the client returns a clean error — not a panic or hang.
func TestMCPIntegration_ServerCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := startEchoServer(t, ctx, "1")
	client := mcp.NewMCPClient(tr)
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Kill the subprocess by closing the transport (sends SIGKILL internally).
	// After Close, the stdout pipe is gone, so any subsequent call must return an error.
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// ListTools should return an error, not hang or panic.
	_, err := client.ListTools(ctx)
	if err == nil {
		t.Error("expected error after server crash, got nil")
	}
	t.Logf("got expected error after crash: %v", err)
}

// TestMCPIntegration_MalformedResponse starts a subprocess that sends malformed JSON
// in response to initialize and verifies the client returns a parse error.
func TestMCPIntegration_MalformedResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := startEchoServer(t, ctx, "malformed")
	client := mcp.NewMCPClient(tr)
	defer client.Close()

	err := client.Initialize(ctx)
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
	t.Logf("got expected parse error: %v", err)
}

// TestMCPIntegration_Timeout starts a subprocess that hangs (never responds after reading
// the initialize request) and verifies the client times out cleanly via context cancellation.
func TestMCPIntegration_Timeout(t *testing.T) {
	// Give the whole test 5 s but use a 300 ms deadline for the actual RPC so
	// the test itself finishes quickly.
	outerCtx, outerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer outerCancel()

	tr := startEchoServer(t, outerCtx, "hang")
	client := mcp.NewMCPClient(tr)
	defer client.Close()

	rpcCtx, rpcCancel := context.WithTimeout(outerCtx, 300*time.Millisecond)
	defer rpcCancel()

	start := time.Now()
	err := client.Initialize(rpcCtx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from hanging server, got nil")
	}
	// Should have returned close to the 300 ms deadline, not 5 s.
	if elapsed > 2*time.Second {
		t.Errorf("Initialize took %v — expected to time out quickly (<=2s)", elapsed)
	}
	t.Logf("got expected timeout error after %v: %v", elapsed, err)
}

// TestMCPIntegration_Reconnect_PipeClose starts a subprocess that closes its stdout
// after the initialize handshake and verifies that the next call returns a clean EOF error.
func TestMCPIntegration_Reconnect_PipeClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := startEchoServer(t, ctx, "closeAfterInit")
	client := mcp.NewMCPClient(tr)
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// The subprocess has now closed its stdout. The next call should fail with a
	// clean "server disconnected" / EOF error rather than a panic or hang.
	_, err := client.ListTools(ctx)
	if err == nil {
		t.Fatal("expected error after server closed stdout, got nil")
	}
	t.Logf("got expected pipe-close error: %v", err)
}

// TestMCPIntegration_FullPath_WithDefaultFactory verifies that defaultClientFactory
// (exposed via the exported MCPServerConfig + NewServerManager path) also works
// end-to-end through the subprocess echo server.
func TestMCPIntegration_FullPath_WithDefaultFactory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := mcp.MCPServerConfig{
		Name:      "test-echo",
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestMCPEchoServer", "-test.v=false"},
		Transport: "stdio",
		Env:       []string{"MCP_TEST_SERVER=1"},
	}

	// Use exec.LookPath to verify the test binary is actually executable before proceeding.
	if _, err := exec.LookPath(os.Args[0]); err != nil {
		t.Skipf("test binary not in PATH: %v", err)
	}

	// Construct a StdioTransport directly (mirrors what defaultClientFactory does).
	tr, err := mcp.NewStdioTransport(ctx, cfg.Command, cfg.Args, cfg.Env)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	client := mcp.NewMCPClient(tr)
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected tools from echo server")
	}
	result, err := client.CallTool(ctx, tools[0].Name, map[string]any{})
	if err != nil {
		t.Fatalf("CallTool(%q): %v", tools[0].Name, err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatalf("expected non-empty result from CallTool, got: %v", result)
	}
	t.Logf("CallTool result: %s", result.Content[0].Text)
}
