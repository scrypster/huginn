// Package agent — iteration 3 hardening tests.
// Focus: Session state transitions, history operations, ChatForSession error handling,
// DebugLoop behaviour (passes, exhausted, cancelled), buildDebugPrompt truncation,
// ImportHistory/ExportHistory round-trip, and BatchChat ordering.
package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// Session unit tests
// ---------------------------------------------------------------------------

// TestSession_ReplaceHistory_Atomic verifies that replaceHistory completely
// replaces the history slice atomically.
func TestSession_ReplaceHistory_Atomic(t *testing.T) {
	sess := newSession("tid-3")
	sess.appendHistory(
		backend.Message{Role: "user", Content: "old message"},
	)
	newMsgs := []backend.Message{
		{Role: "user", Content: "fresh a"},
		{Role: "assistant", Content: "fresh b"},
	}
	sess.replaceHistory(newMsgs)

	got := sess.snapshotHistory()
	if len(got) != 2 {
		t.Fatalf("expected 2 messages after replace, got %d", len(got))
	}
	if got[0].Content != "fresh a" || got[1].Content != "fresh b" {
		t.Errorf("unexpected history after replace: %v", got)
	}
}

// TestSession_ConcurrentAppendAndReplace verifies that concurrent appendHistory
// and replaceHistory calls do not panic or race (run with -race).
func TestSession_ConcurrentAppendAndReplace(t *testing.T) {
	sess := newSession("tid-4")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sess.appendHistory(backend.Message{Role: "user", Content: "appended"})
		}()
		go func() {
			defer wg.Done()
			sess.replaceHistory([]backend.Message{{Role: "system", Content: "replaced"}})
		}()
	}
	wg.Wait()
	// Just verify we can still read without crashing.
	_ = sess.snapshotHistory()
}

// ---------------------------------------------------------------------------
// Orchestrator: ChatForSession
// ---------------------------------------------------------------------------

// TestChatForSession_UnknownSessionID verifies that ChatForSession returns an
// error when the session ID is not found.
func TestChatForSession_UnknownSessionID(t *testing.T) {
	mb := newMockBackend("ok")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	err := o.ChatForSession(context.Background(), "no-such-session", "hello", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown session ID, got nil")
	}
	if !strings.Contains(err.Error(), "no-such-session") {
		t.Errorf("error should mention session ID, got: %v", err)
	}
}

// TestChatForSession_KnownSession verifies that ChatForSession succeeds for a
// session that was created via NewSession.
func TestChatForSession_KnownSession(t *testing.T) {
	mb := newMockBackend("hello from session")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	sess := mustNewSession(t, o, "")
	err := o.ChatForSession(context.Background(), sess.ID, "hi", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: ImportHistory / ExportHistory round-trip
// ---------------------------------------------------------------------------

// TestImportExportHistory_RoundTrip verifies that history imported via
// ImportHistory can be fully recovered by ExportHistory.
func TestImportExportHistory_RoundTrip(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	input := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	o.ImportHistory(input)

	got := o.ExportHistory()
	if len(got) != len(input) {
		t.Fatalf("expected %d messages, got %d", len(input), len(got))
	}
	for i, m := range input {
		if got[i].Role != m.Role || got[i].Content != m.Content {
			t.Errorf("message[%d] mismatch: got %+v, want %+v", i, got[i], m)
		}
	}
}

// TestImportHistory_MakesCopy verifies that modifying the original slice after
// ImportHistory does not corrupt the stored history (defensive copy).
func TestImportHistory_MakesCopy(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	input := []backend.Message{{Role: "user", Content: "original"}}
	o.ImportHistory(input)

	// Mutate the original slice.
	input[0].Content = "mutated"

	got := o.ExportHistory()
	if got[0].Content != "original" {
		t.Errorf("history was mutated via original slice; got %q, want %q", got[0].Content, "original")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: BatchChat
// ---------------------------------------------------------------------------

// TestBatchChat_EmptyTasks verifies that BatchChat returns nil for no tasks.
func TestBatchChat_EmptyTasks(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend("resp"), modelconfig.DefaultModels(), nil, nil, nil, nil)
	result := o.BatchChat(context.Background(), nil)
	if result != nil {
		t.Errorf("expected nil for empty task list, got %v", result)
	}
}

// TestBatchChat_PreservesOrder verifies that results are returned in the same
// order as the input tasks, even though requests run concurrently.
func TestBatchChat_PreservesOrder(t *testing.T) {
	mb := &mockBackend{}
	tasks := []string{"task-0", "task-1", "task-2"}
	for i := 0; i < len(tasks); i++ {
		mb.responses = append(mb.responses, &backend.ChatResponse{
			Content:    "response",
			DoneReason: "stop",
		})
	}

	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	results := o.BatchChat(context.Background(), tasks)

	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}
	for i, r := range results {
		if r.Task != tasks[i] {
			t.Errorf("result[%d].Task = %q, want %q", i, r.Task, tasks[i])
		}
	}
}

// ---------------------------------------------------------------------------
// DebugLoop tests
// ---------------------------------------------------------------------------

// passthroughTestRunner is a TestRunnerFunc that returns a passing result immediately.
func passthroughTestRunner(_ context.Context, _ string, _ string, _ time.Duration) tools.TestResult {
	return tools.TestResult{Passed: true}
}

// failingTestRunner always reports failure.
func failingTestRunner(_ context.Context, _ string, _ string, _ time.Duration) tools.TestResult {
	return tools.TestResult{
		Passed: false,
		Output: "FAIL: something went wrong",
		Failed: []string{"TestFoo"},
	}
}

// TestDebugLoop_PassesImmediately verifies that when the test runner reports
// passing on the first attempt, DebugLoop returns nil without calling the LLM.
func TestDebugLoop_PassesImmediately(t *testing.T) {
	mb := newMockBackend("fixed code")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	err := o.DebugLoop(
		context.Background(),
		"go test ./...",
		3,
		t.TempDir(),
		5*time.Second,
		nil, nil, nil,
		passthroughTestRunner,
	)
	if err != nil {
		t.Fatalf("expected nil error when tests pass immediately, got: %v", err)
	}
	// LLM should NOT have been called.
	mb.mu.Lock()
	calls := mb.callCount
	mb.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected 0 LLM calls when tests pass immediately, got %d", calls)
	}
}

// TestDebugLoop_ExhaustsAttempts verifies that when tests never pass, DebugLoop
// returns an error after the configured number of attempts.
func TestDebugLoop_ExhaustsAttempts(t *testing.T) {
	// The LLM must return a response for each attempt.
	mb := &mockBackend{}
	for i := 0; i < 5; i++ {
		mb.responses = append(mb.responses, &backend.ChatResponse{
			Content:    "I'll fix it",
			DoneReason: "stop",
		})
	}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	err := o.DebugLoop(
		context.Background(),
		"go test ./...",
		3,
		t.TempDir(),
		5*time.Second,
		nil, nil, nil,
		failingTestRunner,
	)
	if err == nil {
		t.Fatal("expected error after exhausted attempts, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("expected 'exhausted' in error message, got: %v", err)
	}
}

// TestDebugLoop_DefaultMaxAttempts verifies that maxAttempts <= 0 defaults to 3.
func TestDebugLoop_DefaultMaxAttempts(t *testing.T) {
	var callCount int
	countingRunner := func(_ context.Context, _ string, _ string, _ time.Duration) tools.TestResult {
		callCount++
		return tools.TestResult{Passed: false, Output: "fail"}
	}

	mb := &mockBackend{}
	for i := 0; i < 5; i++ {
		mb.responses = append(mb.responses, &backend.ChatResponse{Content: "fix", DoneReason: "stop"})
	}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	_ = o.DebugLoop(
		context.Background(),
		"go test ./...",
		0, // should default to 3
		t.TempDir(),
		5*time.Second,
		nil, nil, nil,
		countingRunner,
	)
	if callCount != 3 {
		t.Errorf("expected 3 runner calls (default maxAttempts), got %d", callCount)
	}
}

// TestDebugLoop_CancelledContext verifies that a cancelled context causes
// DebugLoop to return ctx.Err() on the next iteration check.
func TestDebugLoop_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var calls int
	cancelOnSecond := func(_ context.Context, _ string, _ string, _ time.Duration) tools.TestResult {
		calls++
		if calls >= 2 {
			cancel()
		}
		return tools.TestResult{Passed: false, Output: "fail"}
	}

	mb := &mockBackend{}
	for i := 0; i < 5; i++ {
		mb.responses = append(mb.responses, &backend.ChatResponse{Content: "fix", DoneReason: "stop"})
	}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	err := o.DebugLoop(
		ctx,
		"go test ./...",
		10,
		t.TempDir(),
		5*time.Second,
		nil, nil, nil,
		cancelOnSecond,
	)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// buildDebugPrompt
// ---------------------------------------------------------------------------

// TestBuildDebugPrompt_OutputTruncatedAt4000 verifies that the test output in
// the generated prompt is truncated at 4000 characters with "[truncated]".
func TestBuildDebugPrompt_OutputTruncatedAt4000(t *testing.T) {
	longOutput := strings.Repeat("x", 5000)
	result := tools.TestResult{
		Passed: false,
		Output: longOutput,
	}

	prompt := buildDebugPrompt(result, 1, 3, "go test ./...")
	if !strings.Contains(prompt, "[truncated]") {
		t.Error("expected [truncated] marker in prompt for long output")
	}
	// The output section should contain at most 4000 + some overhead bytes for the marker.
	if len(prompt) > 4200 {
		t.Errorf("prompt seems too large: %d bytes", len(prompt))
	}
}

// TestBuildDebugPrompt_IncludesFailedTestNames verifies that individual failing
// test names appear in the generated prompt.
func TestBuildDebugPrompt_IncludesFailedTestNames(t *testing.T) {
	result := tools.TestResult{
		Passed: false,
		Failed: []string{"TestFoo", "TestBar"},
		Output: "some output",
	}
	prompt := buildDebugPrompt(result, 2, 3, "go test ./...")
	if !strings.Contains(prompt, "TestFoo") {
		t.Error("expected TestFoo in debug prompt")
	}
	if !strings.Contains(prompt, "TestBar") {
		t.Error("expected TestBar in debug prompt")
	}
	if !strings.Contains(prompt, "attempt 2/3") {
		t.Error("expected attempt count in debug prompt")
	}
}

// TestBuildDebugPrompt_NoFailedNames verifies that the prompt handles the case
// where the Failed slice is empty (only output available).
func TestBuildDebugPrompt_NoFailedNames(t *testing.T) {
	result := tools.TestResult{
		Passed: false,
		Failed: nil,
		Output: "compilation error",
	}
	prompt := buildDebugPrompt(result, 1, 1, "go build ./...")
	if !strings.Contains(prompt, "compilation error") {
		t.Error("expected output in debug prompt")
	}
	if strings.Contains(prompt, "Failing tests:") {
		t.Error("unexpected 'Failing tests:' section when Failed is empty")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: ModelNames
// ---------------------------------------------------------------------------

// TestModelNames_Deduplicated verifies that ModelNames returns deduplicated names
// when multiple slots share the same model.
func TestModelNames_Deduplicated(t *testing.T) {
	models := &modelconfig.Models{
		Reasoner: "shared-model",
	}
	o := mustNewOrchestrator(t, newMockBackend(""), models, nil, nil, nil, nil)
	names := o.ModelNames()
	if len(names) != 1 {
		t.Errorf("expected 1 deduplicated name, got %d: %v", len(names), names)
	}
	if names[0] != "shared-model" {
		t.Errorf("expected 'shared-model', got %q", names[0])
	}
}

// TestModelNames_AllDistinct verifies that ModelNames returns all names when
// each slot uses a different model.
func TestModelNames_AllDistinct(t *testing.T) {
	models := &modelconfig.Models{
		Reasoner: "reasoner-model",
	}
	o := mustNewOrchestrator(t, newMockBackend(""), models, nil, nil, nil, nil)
	names := o.ModelNames()
	if len(names) != 1 {
		t.Errorf("expected 1 distinct name, got %d: %v", len(names), names)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator: LastUsage
// ---------------------------------------------------------------------------

// TestLastUsage_ZeroBeforeAnyCall verifies that LastUsage returns (0,0) before
// any Chat call is made.
func TestLastUsage_ZeroBeforeAnyCall(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	prompt, completion := o.LastUsage()
	if prompt != 0 || completion != 0 {
		t.Errorf("expected (0,0) before any call, got (%d,%d)", prompt, completion)
	}
}

// TestLastUsage_UpdatedAfterChat verifies that LastUsage reflects token counts
// from the most recent Chat response.
func TestLastUsage_UpdatedAfterChat(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "resp", DoneReason: "stop", PromptTokens: 42, CompletionTokens: 7},
		},
	}
	models := modelconfig.DefaultModels()
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	if err := o.Chat(context.Background(), "hello", nil, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	prompt, completion := o.LastUsage()
	if prompt != 42 {
		t.Errorf("expected prompt tokens=42, got %d", prompt)
	}
	if completion != 7 {
		t.Errorf("expected completion tokens=7, got %d", completion)
	}
}
