package claudecode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func agentBackendCfg(t *testing.T) AgentBackendConfig {
	t.Helper()
	return AgentBackendConfig{
		Binary:       fakeClaude(t),
		SessionID:    "11111111-2222-3333-4444-555555555555",
		CWD:          t.TempDir(),
		Model:        "opus",
		SystemPrompt: "You are Elena.",
		AllowedTools: []string{"Read"},
		GatedTools:   []string{"Write"},
		HookCommand:  "huginn claude-approve",
	}
}

// writeFakeCLI writes a stand-in `claude` that records the exact argv it was
// handed — one argument per line, so values containing spaces or quotes stay
// intact — and then emits the given stream-json lines. It returns the binary
// path and the argv file path.
func writeFakeCLI(t *testing.T, streamLines []string) (binary, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "fake-claude.sh")
	argvFile = filepath.Join(dir, "argv.txt")

	script := fmt.Sprintf(
		"#!/bin/sh\nfor a in \"$@\"; do printf '%%s\\n' \"$a\" >> %q; done\ncat <<'HUGINN_EOF'\n%s\nHUGINN_EOF\nexit 0\n",
		argvFile, strings.Join(streamLines, "\n"),
	)
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return binary, argvFile
}

func readArgvFile(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func TestAgentBackendSendsOnlyTheNewestMessage(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{
			{Role: "user", Content: "old turn one"},
			{Role: "assistant", Content: "old reply"},
			{Role: "user", Content: "NEWEST TURN"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.Contains(resp.Content, "Done:") {
		t.Errorf("Content = %q, want the fake CLI's result text", resp.Content)
	}
	argv := b.lastArgs()
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "NEWEST TURN") {
		t.Errorf("newest message not passed to the CLI: %v", argv)
	}
	if strings.Contains(joined, "old turn one") {
		t.Error("older history was replayed — Claude Code owns the conversation, only the newest turn is sent")
	}
}

func TestAgentBackendNeverReturnsDispatchableToolCalls(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %d, want 0 — populating it would make the agent loop run Claude Code's tools a second time", len(resp.ToolCalls))
	}
	if len(resp.ExecutedTools) == 0 {
		t.Error("ExecutedTools empty; the fake CLI emits a tool_use and it must be reported for history")
	}
	if resp.DoneReason != "stop" {
		t.Errorf("DoneReason = %q, want stop", resp.DoneReason)
	}
}

func TestAgentBackendUsesResumeAfterFirstTurn(t *testing.T) {
	cfg := agentBackendCfg(t)
	cfg.FirstTurn = true
	first := NewAgentBackend(cfg)
	if _, err := first.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if j := strings.Join(first.lastArgs(), " "); !strings.Contains(j, "--session-id") || strings.Contains(j, "--resume") {
		t.Errorf("first turn must use --session-id, not --resume: %v", first.lastArgs())
	}

	cfg.FirstTurn = false
	later := NewAgentBackend(cfg)
	if _, err := later.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi again"}},
	}); err != nil {
		t.Fatalf("later turn: %v", err)
	}
	if j := strings.Join(later.lastArgs(), " "); !strings.Contains(j, "--resume") {
		t.Errorf("later turns must use --resume: %v", later.lastArgs())
	}
}

func TestAgentBackendPassesPromptAllowedToolsAndHooks(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	j := strings.Join(b.lastArgs(), " ")
	for _, want := range []string{"--append-system-prompt", "You are Elena.", "--allowedTools", "Read", "--settings"} {
		if !strings.Contains(j, want) {
			t.Errorf("args missing %q: %v", want, b.lastArgs())
		}
	}
	if strings.Contains(j, "dangerously-skip-permissions") {
		t.Error("an agent backend must never pass --dangerously-skip-permissions")
	}
}

func TestAgentBackendEmptyMessagesIsAnError(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{}); err == nil {
		t.Error("expected an error when there is no message to send")
	}
}

// The stream the richer fake emits: a text block, a tool_use, its tool_result
// on the following user line, and a success result.
var execToolStream = []string{
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Editing."}]}}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Write","input":{"file_path":"/tmp/a.txt","content":"hi"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"File written."}]}}`,
	`{"type":"result","subtype":"success","result":"Wrote the file.","total_cost_usd":0.01,"duration_ms":10,"num_turns":2,"session_id":"S"}`,
}

func TestAgentBackendExecutedToolsCarryNameArgumentsAndResult(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, execToolStream)
	cfg.Binary = bin

	b := NewAgentBackend(cfg)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "write it"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %v, want empty — the loop must not re-dispatch tools Claude Code already ran", resp.ToolCalls)
	}
	if len(resp.ExecutedTools) != 1 {
		t.Fatalf("ExecutedTools = %d, want 1: %+v", len(resp.ExecutedTools), resp.ExecutedTools)
	}
	got := resp.ExecutedTools[0]
	if got.Call.ID != "tu_1" {
		t.Errorf("Call.ID = %q, want tu_1", got.Call.ID)
	}
	if got.Call.Function.Name != "Write" {
		t.Errorf("Call.Function.Name = %q, want Write (Claude Code's tool namespace, not Huginn's)", got.Call.Function.Name)
	}
	if fp, _ := got.Call.Function.Arguments["file_path"].(string); fp != "/tmp/a.txt" {
		t.Errorf("Arguments = %v, want file_path=/tmp/a.txt", got.Call.Function.Arguments)
	}
	if got.Result != "File written." {
		t.Errorf("Result = %q, want the tool_result content — an unfilled result orphans the tool message in history", got.Result)
	}
	if resp.Content != "Wrote the file." {
		t.Errorf("Content = %q, want the result text", resp.Content)
	}
}

func TestAgentBackendActuallyPassesRecordedArgsToTheProcess(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, argvFile := writeFakeCLI(t, execToolStream)
	cfg.Binary = bin
	cfg.FirstTurn = true

	b := NewAgentBackend(cfg)
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	real := readArgvFile(t, argvFile)
	recorded := b.lastArgs()
	if strings.Join(real, "\x00") != strings.Join(recorded, "\x00") {
		t.Fatalf("argv the process received != lastArgs()\n got: %q\nwant: %q", real, recorded)
	}

	// Spot-check the safety-critical argv the process really saw, not just the
	// copy the backend kept for tests.
	joined := strings.Join(real, " ")
	if !strings.Contains(joined, "--session-id "+cfg.SessionID) {
		t.Errorf("session id not passed: %q", real)
	}
	if strings.Contains(joined, "dangerously") {
		t.Errorf("process was handed a skip-permissions flag: %q", real)
	}
	wantSettings, err := BuildHookSettings(cfg.GatedTools, cfg.HookCommand)
	if err != nil {
		t.Fatalf("BuildHookSettings: %v", err)
	}
	found := false
	for i, a := range real {
		if a == "--settings" && i+1 < len(real) && real[i+1] == wantSettings {
			found = true
		}
	}
	if !found {
		t.Errorf("--settings did not carry the hook JSON intact; argv = %q, want %q", real, wantSettings)
	}
}

func TestAgentBackendStreamsTextTokens(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, execToolStream)
	cfg.Binary = bin

	var tokens []string
	if _, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
		OnToken:  func(s string) { tokens = append(tokens, s) },
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if strings.Join(tokens, "") != "Editing." {
		t.Errorf("streamed tokens = %q, want the assistant text block", tokens)
	}
}

func TestAgentBackendSurfacesCLIErrors(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, []string{
		`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"","session_id":"S"}`,
	})
	cfg.Binary = bin

	resp, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err == nil {
		t.Fatalf("want an error when the CLI reports is_error, got %+v", resp)
	}
	if !strings.Contains(err.Error(), "error_during_execution") {
		t.Errorf("error = %v, want it to carry the CLI's failure subtype", err)
	}
}

func TestDefaultGatedToolsGateEveryMutatingTool(t *testing.T) {
	has := func(name string) bool {
		for _, g := range DefaultGatedTools {
			if g == name {
				return true
			}
		}
		return false
	}
	// An unconfigured agent runs unattended: everything that can mutate state
	// or reach the network must require approval.
	for _, want := range []string{"Bash", "Write", "Edit", "NotebookEdit", "WebFetch", "Task"} {
		if !has(want) {
			t.Errorf("DefaultGatedTools missing %q: %v", want, DefaultGatedTools)
		}
	}
	// Huginn's own tool names must never leak into Claude Code's namespace.
	for _, never := range []string{"bash", "write_file", "read_file", "web_search"} {
		if has(never) {
			t.Errorf("DefaultGatedTools contains Huginn tool name %q; Claude Code's namespace is Bash/Write/Read/WebFetch: %v", never, DefaultGatedTools)
		}
	}
	if len(DefaultGatedTools) == 0 {
		t.Fatal("an empty gated list emits no hooks and gates nothing at all")
	}
}
