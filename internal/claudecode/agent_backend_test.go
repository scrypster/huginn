package claudecode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// fakeCLI describes a stand-in `claude` binary.
type fakeCLI struct {
	stream []string // stream-json lines to print on stdout
	stderr string   // text to print on stderr
	exit   int      // exit status
	// hang replaces the whole run with `exec sleep <hang>`, after argv is
	// recorded. exec, not a plain sleep, so the sleeping process IS the one
	// exec.CommandContext will signal on cancel.
	hang string
	// floodBytes emits one line of this many bytes and then writes forever, so
	// a reader that stops draining leaves the child blocked on a full pipe.
	floodBytes int
}

// writeFakeCLI writes a stand-in `claude` that records the exact argv of every
// invocation — one argument per line, preceded by a marker, so repeated turns
// stay distinguishable and values containing spaces or quotes stay intact.
func writeFakeCLI(t *testing.T, f fakeCLI) (binary, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "fake-claude.sh")
	argvFile = filepath.Join(dir, "argv.txt")

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&sb, "printf '%%s\\n' '--RUN--' >> %q\n", argvFile)
	fmt.Fprintf(&sb, "for a in \"$@\"; do printf '%%s\\n' \"$a\" >> %q; done\n", argvFile)
	if f.hang != "" {
		fmt.Fprintf(&sb, "exec sleep %s\n", f.hang)
	}
	if f.floodBytes > 0 {
		fmt.Fprintf(&sb, "awk 'BEGIN{s=\"\";while(length(s)<%d)s=s \"x\";print s}'\n", f.floodBytes)
		sb.WriteString("while :; do printf 'still writing to a pipe nobody is reading\\n'; done\n")
	}
	if f.stderr != "" {
		fmt.Fprintf(&sb, "printf '%%s\\n' %q >&2\n", f.stderr)
	}
	if len(f.stream) > 0 {
		fmt.Fprintf(&sb, "cat <<'HUGINN_EOF'\n%s\nHUGINN_EOF\n", strings.Join(f.stream, "\n"))
	}
	fmt.Fprintf(&sb, "exit %d\n", f.exit)

	if err := os.WriteFile(binary, []byte(sb.String()), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return binary, argvFile
}

// readArgvRuns returns the argv of each invocation, in order.
func readArgvRuns(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	var runs [][]string
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if line == "--RUN--" {
			runs = append(runs, nil)
			continue
		}
		if len(runs) == 0 {
			t.Fatalf("argv line before any run marker: %q", line)
		}
		runs[len(runs)-1] = append(runs[len(runs)-1], line)
	}
	return runs
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

// This is the proof for the FirstTurn fix: ONE backend, TWO turns. Backends are
// cached per agent, so a config flag that nothing ever flips would leave every
// turn after the first re-creating a session that already exists.
func TestAgentBackendSwitchesToResumeOnItsSecondTurn(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, argvFile := writeFakeCLI(t, fakeCLI{stream: execToolStream})
	cfg.Binary = bin
	cfg.FirstTurn = true

	b := NewAgentBackend(cfg)
	for _, msg := range []string{"turn one", "turn two"} {
		if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
			Messages: []backend.Message{{Role: "user", Content: msg}},
		}); err != nil {
			t.Fatalf("turn %q: %v", msg, err)
		}
	}

	runs := readArgvRuns(t, argvFile)
	if len(runs) != 2 {
		t.Fatalf("the CLI ran %d times, want 2: %v", len(runs), runs)
	}
	one, two := strings.Join(runs[0], " "), strings.Join(runs[1], " ")
	if !strings.Contains(one, "--session-id "+cfg.SessionID) || strings.Contains(one, "--resume") {
		t.Errorf("turn 1 must create the session with --session-id: %v", runs[0])
	}
	if !strings.Contains(two, "--resume "+cfg.SessionID) {
		t.Errorf("turn 2 must resume the session, not re-create it: %v", runs[1])
	}
	if strings.Contains(two, "--session-id") {
		t.Errorf("turn 2 still passed --session-id for a session that already exists: %v", runs[1])
	}
}

// A backend told the session already exists must resume from its very first turn.
func TestAgentBackendResumesImmediatelyWhenSessionAlreadyExists(t *testing.T) {
	cfg := agentBackendCfg(t)
	cfg.FirstTurn = false
	b := NewAgentBackend(cfg)
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi again"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if j := strings.Join(b.lastArgs(), " "); !strings.Contains(j, "--resume") {
		t.Errorf("want --resume: %v", b.lastArgs())
	}
}

// The one case that must still CREATE: the CLI was never launched at all, so
// the single --session-id chance was never spent.
func TestAgentBackendKeepsCreatingWhenTheCLINeverLaunched(t *testing.T) {
	cfg := agentBackendCfg(t)
	cfg.Binary = filepath.Join(t.TempDir(), "definitely-not-here")
	cfg.FirstTurn = true

	b := NewAgentBackend(cfg)
	for _, msg := range []string{"one", "two"} {
		if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
			Messages: []backend.Message{{Role: "user", Content: msg}},
		}); err == nil {
			t.Fatalf("turn %q: want an error when the binary does not exist", msg)
		}
	}
	if j := strings.Join(b.lastArgs(), " "); strings.Contains(j, "--resume") {
		t.Errorf("turn 2 resumed a session no launch ever had the chance to create: %v", b.lastArgs())
	} else if !strings.Contains(j, "--session-id") {
		t.Errorf("turn 2 should still be creating the session: %v", b.lastArgs())
	}
}

// Once the CLI has been LAUNCHED with --session-id, that chance is spent and
// every later turn must resume — however the launch turned out. We cannot tell
// from out here whether a CLI that died early wrote the session first, and only
// the wrong --resume is recoverable; a wrong --session-id collides forever.
func TestAgentBackendResumesAfterAnyLaunchHoweverItEnded(t *testing.T) {
	cases := []struct {
		name string
		cli  fakeCLI
	}{
		{"clean exit but no session id anywhere in the stream", fakeCLI{stream: []string{
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
			`{"type":"result","subtype":"success","result":"done"}`,
		}}},
		{"killed before it ever emitted its init line", fakeCLI{stderr: "killed", exit: 137}},
		{"emitted an init line, then died", fakeCLI{
			stream: []string{`{"type":"system","subtype":"init","session_id":"11111111-2222-3333-4444-555555555555"}`},
			stderr: "died mid-turn",
			exit:   1,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := agentBackendCfg(t)
			bin, argvFile := writeFakeCLI(t, tc.cli)
			cfg.Binary = bin
			cfg.FirstTurn = true

			b := NewAgentBackend(cfg)
			for _, msg := range []string{"one", "two"} {
				_, _ = b.ChatCompletion(context.Background(), backend.ChatRequest{
					Messages: []backend.Message{{Role: "user", Content: msg}},
				})
			}

			runs := readArgvRuns(t, argvFile)
			if len(runs) != 2 {
				t.Fatalf("runs = %d, want 2: %v", len(runs), runs)
			}
			two := strings.Join(runs[1], " ")
			if strings.Contains(two, "--session-id") {
				t.Errorf("turn 2 re-spent the one --session-id chance, which fails permanently: %v", runs[1])
			}
			if !strings.Contains(two, "--resume "+cfg.SessionID) {
				t.Errorf("turn 2 should resume: %v", runs[1])
			}
		})
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

// A blank newest turn must fail loudly rather than quietly re-sending an older
// prompt the user has already had answered.
func TestAgentBackendBlankNewestMessageDoesNotReplayAnOlderOne(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, argvFile := writeFakeCLI(t, fakeCLI{stream: execToolStream})
	cfg.Binary = bin

	b := NewAgentBackend(cfg)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{
			{Role: "user", Content: "an older question"},
			{Role: "assistant", Content: "an older answer"},
			{Role: "user", Content: "   "},
		},
	})
	if err == nil {
		t.Fatal("want an error for a blank newest user message")
	}
	if _, statErr := os.Stat(argvFile); statErr == nil {
		t.Errorf("the CLI was invoked anyway, with: %v", readArgvRuns(t, argvFile))
	}
}

// The stream the richer fake emits: a text block, a tool_use, its tool_result
// on the following user line, and a success result.
var execToolStream = []string{
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Editing."}],"usage":{"input_tokens":1200,"output_tokens":30}}}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"Write","input":{"file_path":"/tmp/a.txt","content":"hi"}}],"usage":{"input_tokens":1500,"output_tokens":12}}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"File written."}]}}`,
	`{"type":"result","subtype":"success","result":"Wrote the file.","total_cost_usd":0.01,"duration_ms":10,"num_turns":2,"session_id":"11111111-2222-3333-4444-555555555555"}`,
}

func TestAgentBackendExecutedToolsCarryNameArgumentsAndResult(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{stream: execToolStream})
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

// Token usage must reach the loop, which sums it into LoopResult.
func TestAgentBackendReportsTokenUsage(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{stream: execToolStream})
	cfg.Binary = bin

	resp, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	// Input is the last reported context size, not a sum: each assistant
	// message restates the whole prompt.
	if resp.PromptTokens != 1500 {
		t.Errorf("PromptTokens = %d, want 1500 (last reported, not 2700)", resp.PromptTokens)
	}
	if resp.CompletionTokens != 42 {
		t.Errorf("CompletionTokens = %d, want 42 (30+12, summed)", resp.CompletionTokens)
	}
}

func TestAgentBackendCorrelatesToolResultsSafely(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{stream: []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu_dup","name":"Read","input":{"file_path":"/a"}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu_dup","name":"Read","input":{"file_path":"/b"}}]}}`,
		// An id nobody declared, an empty id, and a duplicate id.
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_nobody","content":"orphan"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"","content":"anonymous"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_dup","content":"the one result"}]}}`,
		`{"type":"result","subtype":"success","result":"ok","session_id":"S"}`,
	}})
	cfg.Binary = bin

	resp, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ExecutedTools) != 3 {
		t.Fatalf("ExecutedTools = %d, want 3 (every tool that ran is reported): %+v", len(resp.ExecutedTools), resp.ExecutedTools)
	}
	// The empty-id call must NOT have absorbed the empty-id tool_result.
	if got := resp.ExecutedTools[0]; got.Result != "" {
		t.Errorf("empty-id call picked up %q; an empty tool_use_id correlates to nothing", got.Result)
	}
	// A duplicate id fills exactly one call, not both.
	filled := 0
	for _, et := range resp.ExecutedTools[1:] {
		if et.Result != "" {
			filled++
			if et.Result != "the one result" {
				t.Errorf("Result = %q, want %q", et.Result, "the one result")
			}
		}
	}
	if filled != 1 {
		t.Errorf("a duplicate tool_use_id filled %d calls, want exactly 1: %+v", filled, resp.ExecutedTools)
	}
	// The orphan result must not have been attached to anything.
	for _, et := range resp.ExecutedTools {
		if et.Result == "orphan" {
			t.Errorf("an unmatched tool_result was attached to %q", et.Call.Function.Name)
		}
	}
}

func TestAgentBackendActuallyPassesRecordedArgsToTheProcess(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, argvFile := writeFakeCLI(t, fakeCLI{stream: execToolStream})
	cfg.Binary = bin
	cfg.FirstTurn = true

	b := NewAgentBackend(cfg)
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	runs := readArgvRuns(t, argvFile)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	real := runs[0]
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

// Production sets BOTH callbacks. The relay builds its live token messages from
// OnToken and ignores StreamText in its OnEvent handler, so an else-if here
// means the user sees nothing stream.
func TestAgentBackendCallsBothCallbacksWhenBothAreSet(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{stream: execToolStream})
	cfg.Binary = bin

	var tokens []string
	var eventText []string
	var toolEvents []string
	if _, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
		OnToken:  func(s string) { tokens = append(tokens, s) },
		OnEvent: func(e backend.StreamEvent) {
			switch e.Type {
			case backend.StreamText:
				eventText = append(eventText, e.Content)
			case backend.StreamToolCall:
				name, _ := e.Payload["tool"].(string)
				toolEvents = append(toolEvents, name)
			}
		},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if strings.Join(tokens, "") != "Editing." {
		t.Errorf("OnToken got %q, want the assistant text — OnEvent being set must not suppress it", tokens)
	}
	if strings.Join(eventText, "") != "Editing." {
		t.Errorf("OnEvent StreamText got %q, want the assistant text", eventText)
	}
	if len(toolEvents) != 1 || toolEvents[0] != "Write" {
		t.Errorf("tool_call events = %v, want [Write]", toolEvents)
	}
}

func TestAgentBackendStreamsTextTokensWithOnlyOnToken(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{stream: execToolStream})
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
	bin, _ := writeFakeCLI(t, fakeCLI{stream: []string{
		`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"","session_id":"S"}`,
	}})
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

// A non-zero exit with the cause on stderr is the failure mode a misconfigured
// session actually produces. Throwing stderr away leaves a bare "exit status 1".
func TestAgentBackendNonZeroExitIncludesStderr(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{
		stderr: "Error: session ID already in use",
		exit:   1,
	})
	cfg.Binary = bin

	_, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err == nil {
		t.Fatal("want an error for a non-zero exit")
	}
	if !strings.Contains(err.Error(), "session ID already in use") {
		t.Errorf("error = %v, want it to carry the CLI's stderr", err)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("error = %v, want it to keep the exit status too", err)
	}
}

func TestAgentBackendStderrTailIsBounded(t *testing.T) {
	tb := &tailBuffer{max: 32}
	for i := 0; i < 100; i++ {
		if _, err := tb.Write([]byte("0123456789")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if len(tb.buf) > 32 {
		t.Errorf("retained %d bytes, want at most 32", len(tb.buf))
	}
	if !strings.Contains(tb.String(), "truncated") {
		t.Errorf("String() = %q, want it to flag truncation", tb.String())
	}
	if !strings.HasSuffix(tb.String(), "0123456789") {
		t.Errorf("String() = %q, want the TAIL of the stream (where the error is)", tb.String())
	}
}

func TestAgentBackendCancellationIsReportedAsCancelled(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{hang: "30"})
	cfg.Binary = bin

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := NewAgentBackend(cfg).ChatCompletion(ctx, backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err == nil {
		t.Fatal("want an error when the turn is cancelled mid-flight")
	}
	// The relay distinguishes user cancels from failures with errors.Is; a bare
	// "signal: killed" never matches and gets persisted as a failed session.
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("cancellation took %s — the turn did not abort promptly", d)
	}
}

func TestAgentBackendTurnTimeoutIsBoundedAndDistinguishable(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{hang: "30"})
	cfg.Binary = bin
	cfg.TimeoutSecs = 1

	start := time.Now()
	_, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err == nil {
		t.Fatal("want an error when the turn exceeds TimeoutSecs")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to wrap context.DeadlineExceeded, distinct from a user cancel", err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("timeout took %s — the deadline did not bound the turn", d)
	}
}

// A queued caller must be able to abandon the wait; a sync.Mutex would pin it
// behind an unrelated turn on this shared, cached backend.
func TestAgentBackendQueuedCallerCanBeCancelled(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{hang: "30"})
	cfg.Binary = bin

	b := NewAgentBackend(cfg)
	hogCtx, stopHog := context.WithCancel(context.Background())
	running := make(chan struct{})
	hogDone := make(chan struct{})
	go func() {
		defer close(hogDone)
		close(running)
		_, _ = b.ChatCompletion(hogCtx, backend.ChatRequest{
			Messages: []backend.Message{{Role: "user", Content: "long"}},
		})
	}()
	// Join the hog before the test returns. A goroutine still inside
	// ChatCompletion after its test finishes races the next test's setup —
	// caught by -race against the agentScanMaxBytes seam.
	t.Cleanup(func() {
		stopHog()
		<-hogDone
	})
	<-running
	time.Sleep(150 * time.Millisecond) // let the hog take the slot

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := b.ChatCompletion(ctx, backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "queued"}},
	})
	if err == nil {
		t.Fatal("want an error for the queued caller")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want the queued caller's own deadline to release it", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("queued caller waited %s — its context could not reach the lock", d)
	}
}

// Property, not a copy of the literal slice: with the default configuration, a
// tool that can mutate state or reach the network actually gets a PreToolUse
// hook. That is what "gated" means; the slice is only how it is spelled.
func TestDefaultGatedToolsProduceAHookForEveryMutatingTool(t *testing.T) {
	if len(DefaultGatedTools) == 0 {
		t.Fatal("an empty gated list emits no hooks and gates nothing at all")
	}
	settings, err := BuildHookSettings(DefaultGatedTools, "huginn claude-approve")
	if err != nil {
		t.Fatalf("BuildHookSettings: %v", err)
	}
	if settings == "" {
		t.Fatal("the default gated set produced no --settings payload, so nothing is gated")
	}

	mutating := []string{"Bash", "Write", "Edit", "NotebookEdit", "WebFetch", "Task"}
	for _, tool := range mutating {
		if !strings.Contains(settings, `"matcher":"`+tool+`"`) {
			t.Errorf("%s can mutate state or reach the network but has no PreToolUse hook by default: %s", tool, settings)
		}
	}
	// Huginn's own tool names must never leak into Claude Code's namespace: a
	// matcher that names no real tool gates nothing while looking like it does.
	for _, never := range []string{"bash", "write_file", "read_file", "web_search"} {
		if strings.Contains(settings, `"matcher":"`+never+`"`) {
			t.Errorf("gated set contains Huginn tool name %q; Claude Code's namespace is Bash/Write/Read/WebFetch: %v", never, DefaultGatedTools)
		}
	}
}

// A scan error stops the reader. If the child is still writing, the pipe fills
// and Wait blocks until the turn deadline, holding the semaphore and stalling
// every session on this cached backend. TimeoutSecs is short so a regression
// fails fast rather than hanging the suite.
func TestAgentBackendOverlongLineDoesNotWedgeTheTurn(t *testing.T) {
	prev := agentScanMaxBytes
	agentScanMaxBytes = 4096
	t.Cleanup(func() { agentScanMaxBytes = prev })

	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{floodBytes: 16384})
	cfg.Binary = bin
	cfg.TimeoutSecs = 5

	start := time.Now()
	_, err := NewAgentBackend(cfg).ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want an error when a stream line exceeds the scanner limit")
	}
	if elapsed > 3*time.Second {
		t.Errorf("returned after %s: the turn waited on the deadline instead of killing the child, so the semaphore was held that whole time", elapsed)
	}
	// The scan failure must be reported as itself, not as a cancellation: the
	// relay treats context.Canceled as a user cancel and persists it as such.
	if errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want the scan failure, not a cancellation — our own cleanup cancel must not be mistaken for the caller's", err)
	}
	if !strings.Contains(err.Error(), "token too long") {
		t.Errorf("error = %v, want it to name the scan failure", err)
	}
}

// The semaphore must be free for the next turn after the wedge path unwinds.
func TestAgentBackendRecoversForTheNextTurnAfterAScanError(t *testing.T) {
	prev := agentScanMaxBytes
	agentScanMaxBytes = 4096
	t.Cleanup(func() { agentScanMaxBytes = prev })

	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{floodBytes: 16384})
	cfg.Binary = bin
	cfg.TimeoutSecs = 5

	b := NewAgentBackend(cfg)
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "one"}},
	}); err == nil {
		t.Fatal("want an error on the first turn")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = b.ChatCompletion(context.Background(), backend.ChatRequest{
			Messages: []backend.Message{{Role: "user", Content: "two"}},
		})
	}()
	// Registered after the agentScanMaxBytes restore, so LIFO cleanup joins the
	// goroutine BEFORE the seam is put back. Bounded so a genuine wedge fails
	// the test rather than hanging cleanup forever.
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
	})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the next turn never got the semaphore — the wedged turn never released it")
	}
}
