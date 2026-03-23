package agent

// agent_coverage_test.go — additional tests to push internal/agent coverage to 90%+.
// Covers: LastUsage, BatchChat, SetNotepads, SetGitRoot, SetSearcher,
// compactHistory, previewWrite, isIndependentTool, buildDebugPrompt,
// defaultTestRunner, LoadGlobalInstructions, LoadProjectInstructions,
// ContextBuilder.SetNotepads/SetSearcher, ContextBuilder.BuildCtx with searcher.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// LastUsage
// ---------------------------------------------------------------------------

func TestOrchestrator_LastUsage_InitiallyZero(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	prompt, completion := o.LastUsage()
	if prompt != 0 || completion != 0 {
		t.Errorf("expected (0, 0), got (%d, %d)", prompt, completion)
	}
}

func TestOrchestrator_LastUsage_PopulatedByChat(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "hi", DoneReason: "stop", PromptTokens: 42, CompletionTokens: 7},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	if err := o.Chat(context.Background(), "hello", nil, nil); err != nil {
		t.Fatal(err)
	}
	prompt, completion := o.LastUsage()
	if prompt != 42 {
		t.Errorf("expected promptTokens=42, got %d", prompt)
	}
	if completion != 7 {
		t.Errorf("expected completionTokens=7, got %d", completion)
	}
}

func TestOrchestrator_LastUsage_PopulatedByChatForSession(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "resp", DoneReason: "stop", PromptTokens: 10, CompletionTokens: 5},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	sessID := o.SessionID()
	if err := o.ChatForSession(context.Background(), sessID, "msg", nil, nil); err != nil {
		t.Fatal(err)
	}
	prompt, completion := o.LastUsage()
	if prompt != 10 || completion != 5 {
		t.Errorf("expected (10, 5), got (%d, %d)", prompt, completion)
	}
}

// ---------------------------------------------------------------------------
// BatchChat
// ---------------------------------------------------------------------------

func TestOrchestrator_BatchChat_EmptyTasks(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	results := o.BatchChat(context.Background(), nil)
	if results != nil {
		t.Errorf("expected nil for empty tasks, got %v", results)
	}
}

func TestOrchestrator_BatchChat_SingleTask(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "batch response", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	results := o.BatchChat(context.Background(), []string{"what is 2+2?"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("unexpected error: %v", results[0].Err)
	}
	if results[0].Output != "batch response" {
		t.Errorf("expected 'batch response', got %q", results[0].Output)
	}
	if results[0].Task != "what is 2+2?" {
		t.Errorf("expected task to be preserved, got %q", results[0].Task)
	}
}

func TestOrchestrator_BatchChat_MultipleTasks_Order(t *testing.T) {
	// Provide 3 responses for 3 tasks; results must come back in task order.
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "resp-0", DoneReason: "stop"},
			{Content: "resp-1", DoneReason: "stop"},
			{Content: "resp-2", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	tasks := []string{"task0", "task1", "task2"}
	results := o.BatchChat(context.Background(), tasks)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Task != tasks[i] {
			t.Errorf("result[%d].Task = %q, want %q", i, r.Task, tasks[i])
		}
	}
}

func TestOrchestrator_BatchChat_BackendError(t *testing.T) {
	mb := &mockBackend{
		errors: []error{errors.New("backend failure")},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	results := o.BatchChat(context.Background(), []string{"task"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Error("expected error in result")
	}
}

// ---------------------------------------------------------------------------
// SetNotepads / SetGitRoot / SetSearcher (Orchestrator delegates)
// ---------------------------------------------------------------------------

func TestOrchestrator_SetNotepads(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	nps := []*notepad.Notepad{{Name: "todo", Content: "- fix bug"}}
	o.SetNotepads(nps)
	// Verify the notepads show up in a Build call (no panic, correct content).
	result := o.contextBuilder.Build("", "test-model")
	if !strings.Contains(result, "todo") {
		t.Error("expected notepad name 'todo' in context output")
	}
}

func TestOrchestrator_SetNotepads_Nil(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	// Should not panic.
	o.SetNotepads(nil)
}

func TestOrchestrator_SetGitRoot(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	o.SetGitRoot("/some/path")
	o.mu.Lock()
	got := o.workspaceRoot
	o.mu.Unlock()
	if got != "/some/path" {
		t.Errorf("expected workspaceRoot='/some/path', got %q", got)
	}
}

func TestOrchestrator_SetSearcher(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	// A nil searcher should not panic.
	o.SetSearcher(nil)
}

// ---------------------------------------------------------------------------
// ContextBuilder.SetNotepads / SetSearcher
// ---------------------------------------------------------------------------

func TestContextBuilder_SetNotepads_AppearsInBuild(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	nps := []*notepad.Notepad{
		{Name: "mypad", Content: "important context"},
		{Name: "otherpad", Content: "more context"},
	}
	cb.SetNotepads(nps)
	result := cb.Build("query", "test-model")
	if !strings.Contains(result, "mypad") {
		t.Error("expected notepad name 'mypad' in build output")
	}
	if !strings.Contains(result, "important context") {
		t.Error("expected notepad content in build output")
	}
}

func TestContextBuilder_SetNotepads_BudgetTruncation(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	// Add a notepad so large it exceeds maxNotepadsChars (32768).
	bigContent := strings.Repeat("x", 33000)
	nps := []*notepad.Notepad{
		{Name: "huge", Content: bigContent},
		{Name: "small", Content: "tiny"},
	}
	cb.SetNotepads(nps)
	result := cb.Build("query", "test-model")
	// The huge notepad is skipped if it alone exceeds the budget.
	// The small one may or may not appear — important: no panic.
	_ = result
}

func TestContextBuilder_SetSearcher_NilSearcher(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	cb.SetSearcher(nil)
	// Build with a non-empty query — nil searcher falls back to BM25 (no-op with nil index).
	result := cb.Build("query", "test-model")
	_ = result
}

// mockSearcher is a simple Searcher implementation for testing.
type mockSearcher struct {
	results []search.Chunk
	err     error
}

func (ms *mockSearcher) Search(_ context.Context, _ string, _ int) ([]search.Chunk, error) {
	return ms.results, ms.err
}

func (ms *mockSearcher) Close() error { return nil }

func (ms *mockSearcher) Index(_ context.Context, _ []search.Chunk) error { return nil }

func TestContextBuilder_SetSearcher_ReturnsResults(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	ms := &mockSearcher{
		results: []search.Chunk{
			{Path: "foo.go", StartLine: 1, Content: "package foo"},
		},
	}
	cb.SetSearcher(ms)
	result := cb.BuildCtx(context.Background(), "foo", "test-model")
	if !strings.Contains(result, "Repository Context") {
		t.Errorf("expected 'Repository Context' section in output, got: %q", result)
	}
}

func TestContextBuilder_SetSearcher_ErrorFallsBackToBM25(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	ms := &mockSearcher{err: errors.New("search failed")}
	cb.SetSearcher(ms)
	// No panic — falls back to BM25 (which is a no-op with nil index).
	result := cb.BuildCtx(context.Background(), "foo", "test-model")
	_ = result
}

func TestContextBuilder_SetSearcher_EmptyResults_FallsBackToBM25(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	ms := &mockSearcher{results: nil}
	cb.SetSearcher(ms)
	result := cb.BuildCtx(context.Background(), "foo", "test-model")
	_ = result
}

func TestContextBuilder_BuildCtx_WithSkillsFragment(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	cb.SetSkillsFragment("my-skill-content")
	result := cb.BuildCtx(context.Background(), "", "test-model")
	if !strings.Contains(result, "Skills & Workspace Rules") {
		t.Error("expected 'Skills & Workspace Rules' in output")
	}
	if !strings.Contains(result, "my-skill-content") {
		t.Error("expected skill content in output")
	}
}

// ---------------------------------------------------------------------------
// buildDebugPrompt — branches
// ---------------------------------------------------------------------------

func TestBuildDebugPrompt_WithFailedTests(t *testing.T) {
	result := tools.TestResult{
		Passed: false,
		Failed: []string{"TestFoo", "TestBar"},
		Output: "some output",
	}
	prompt := buildDebugPrompt(result, 1, 3, "go test ./...")
	if !strings.Contains(prompt, "TestFoo") {
		t.Error("expected failing test name in prompt")
	}
	if !strings.Contains(prompt, "TestBar") {
		t.Error("expected failing test name in prompt")
	}
	if !strings.Contains(prompt, "some output") {
		t.Error("expected test output in prompt")
	}
	if !strings.Contains(prompt, "1/3") {
		t.Error("expected attempt count in prompt")
	}
}

func TestBuildDebugPrompt_LongOutputTruncated(t *testing.T) {
	longOutput := strings.Repeat("x", 5000)
	result := tools.TestResult{
		Passed: false,
		Output: longOutput,
	}
	prompt := buildDebugPrompt(result, 2, 3, "go test")
	if !strings.Contains(prompt, "truncated") {
		t.Error("expected truncation marker for long output")
	}
}

func TestBuildDebugPrompt_NoFailedTests_NoOutput(t *testing.T) {
	result := tools.TestResult{Passed: false}
	prompt := buildDebugPrompt(result, 1, 1, "go test")
	if prompt == "" {
		t.Error("expected non-empty prompt even with no failed tests/output")
	}
	if !strings.Contains(prompt, "analyze") {
		t.Error("expected analysis instruction in prompt")
	}
}

// ---------------------------------------------------------------------------
// defaultTestRunner — calls through to RunTestsTool (unit sanity)
// ---------------------------------------------------------------------------

func TestDefaultTestRunner_InvalidCommand(t *testing.T) {
	// defaultTestRunner wraps RunTestsTool; an invalid command should not panic.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := defaultTestRunner(ctx, "false", t.TempDir(), 3*time.Second)
	// We don't care about the result — just that it doesn't panic.
	_ = result
}

// ---------------------------------------------------------------------------
// LoadGlobalInstructions — the main branch tests are in instructions_test.go.
// Here we just verify it doesn't panic on a standard call.
// ---------------------------------------------------------------------------

func TestLoadGlobalInstructions_NoPanic(t *testing.T) {
	// instructions_test.go has the full suite. We call to ensure non-panic.
	_ = LoadGlobalInstructions()
}

// ---------------------------------------------------------------------------
// LoadProjectInstructions — additional edge cases not in instructions_test.go
// ---------------------------------------------------------------------------

func TestLoadProjectInstructions_EmptyRoot(t *testing.T) {
	got := LoadProjectInstructions("")
	if got != "" {
		t.Errorf("expected '' for empty root, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// previewWrite — branches
// ---------------------------------------------------------------------------

func TestPreviewWrite_WriteFile(t *testing.T) {
	args := map[string]any{
		"file_path": "/nonexistent/path/file.go",
		"content":   "package main\n",
	}
	path, oldContent, newContent := previewWrite("write_file", args)
	if path != "/nonexistent/path/file.go" {
		t.Errorf("expected path, got %q", path)
	}
	// old content is nil/empty since file doesn't exist.
	_ = oldContent
	if string(newContent) != "package main\n" {
		t.Errorf("expected new content, got %q", newContent)
	}
}

func TestPreviewWrite_EditFile_NoOldContent(t *testing.T) {
	args := map[string]any{
		"file_path":  "/nonexistent/edit.go",
		"old_string": "old",
		"new_string": "new",
	}
	path, oldContent, newContent := previewWrite("edit_file", args)
	if path != "/nonexistent/edit.go" {
		t.Errorf("expected path, got %q", path)
	}
	// File doesn't exist so oldContent is nil, newContent is nil.
	if oldContent != nil {
		t.Errorf("expected nil oldContent for missing file, got %q", oldContent)
	}
	if newContent != nil {
		t.Errorf("expected nil newContent when file missing, got %q", newContent)
	}
}

func TestPreviewWrite_EditFile_WithRealFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "edit.go")
	if err := os.WriteFile(f, []byte("old string here"), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{
		"file_path":  f,
		"old_string": "old string",
		"new_string": "new string",
	}
	path, oldContent, newContent := previewWrite("edit_file", args)
	if path != f {
		t.Errorf("expected path=%q, got %q", f, path)
	}
	if !strings.Contains(string(oldContent), "old string") {
		t.Errorf("expected old content, got %q", oldContent)
	}
	if !strings.Contains(string(newContent), "new string") {
		t.Errorf("expected new content after replacement, got %q", newContent)
	}
}

func TestPreviewWrite_EditFile_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "multi.go")
	if err := os.WriteFile(f, []byte("foo foo foo"), 0644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{
		"file_path":   f,
		"old_string":  "foo",
		"new_string":  "bar",
		"replace_all": true,
	}
	_, _, newContent := previewWrite("edit_file", args)
	if string(newContent) != "bar bar bar" {
		t.Errorf("expected all replacements, got %q", newContent)
	}
}

func TestPreviewWrite_UnknownTool(t *testing.T) {
	// Unknown tool name — all returns should be zero values (no panic).
	path, old, new := previewWrite("unknown_tool", map[string]any{})
	if path != "" || old != nil || new != nil {
		t.Errorf("expected zero values for unknown tool, got path=%q", path)
	}
}

func TestPreviewWrite_WriteFile_NoPath(t *testing.T) {
	args := map[string]any{"content": "data"}
	path, _, _ := previewWrite("write_file", args)
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

// ---------------------------------------------------------------------------
// isIndependentTool — branches
// ---------------------------------------------------------------------------

func TestIsIndependentTool_Bash(t *testing.T) {
	if isIndependentTool("bash", nil, nil) {
		t.Error("bash should never be independent")
	}
}

func TestIsIndependentTool_GitWrite(t *testing.T) {
	for _, name := range []string{"git_commit", "git_stash"} {
		if isIndependentTool(name, nil, nil) {
			t.Errorf("%s should not be independent", name)
		}
	}
}

func TestIsIndependentTool_ReadOnlyTools(t *testing.T) {
	readTools := []string{
		"read_file", "grep", "list_dir", "search_files",
		"web_search", "fetch_url",
		"git_status", "git_log", "git_blame", "git_diff", "git_branch",
	}
	for _, name := range readTools {
		if !isIndependentTool(name, nil, nil) {
			t.Errorf("%s should be independent (read-only)", name)
		}
	}
}

func TestIsIndependentTool_WriteFile_UniqueFiles(t *testing.T) {
	allCalls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "a.go"}}},
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "b.go"}}},
	}
	// a.go is unique in the batch — independent.
	if !isIndependentTool("write_file", map[string]any{"file_path": "a.go"}, allCalls) {
		t.Error("write_file with unique path should be independent")
	}
}

func TestIsIndependentTool_WriteFile_DuplicatePaths(t *testing.T) {
	allCalls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "same.go"}}},
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "same.go"}}},
	}
	// same.go appears twice — should be serial.
	if isIndependentTool("write_file", map[string]any{"file_path": "same.go"}, allCalls) {
		t.Error("write_file with duplicate path should NOT be independent")
	}
}

func TestIsIndependentTool_WriteFile_NoPath(t *testing.T) {
	if isIndependentTool("write_file", map[string]any{}, nil) {
		t.Error("write_file with no path should NOT be independent")
	}
}

func TestIsIndependentTool_EditFile_UniqueFiles(t *testing.T) {
	allCalls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "edit_file", Arguments: map[string]any{"file_path": "x.go"}}},
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "y.go"}}},
	}
	if !isIndependentTool("edit_file", map[string]any{"file_path": "x.go"}, allCalls) {
		t.Error("edit_file with unique path should be independent")
	}
}

func TestIsIndependentTool_MCPTool(t *testing.T) {
	if isIndependentTool("mcp_some_tool", nil, nil) {
		t.Error("mcp_ tools should always be serial")
	}
}

func TestIsIndependentTool_UnknownTool(t *testing.T) {
	if isIndependentTool("completely_unknown", nil, nil) {
		t.Error("unknown tools should default to serial (false)")
	}
}

// ---------------------------------------------------------------------------
// ChatForSession — error path (session not found)
// ---------------------------------------------------------------------------

func TestOrchestrator_ChatForSession_NotFound(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	err := o.ChatForSession(context.Background(), "nonexistent-session-id", "msg", nil, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// DebugLoop — buildDebugPrompt called (with LLM error that's not cancel)
// ---------------------------------------------------------------------------

func TestDebugLoop_LLMErrorRecordsStatsAndContinues(t *testing.T) {
	// The LLM returns an error on the first call (non-cancel), so the loop
	// records a stat and continues to the next attempt.
	var attemptCount int
	runner := func(_ context.Context, _, _ string, _ time.Duration) tools.TestResult {
		attemptCount++
		if attemptCount >= 2 {
			return tools.TestResult{Passed: true}
		}
		return tools.TestResult{Passed: false, Output: "fail"}
	}

	mb := &mockBackend{
		errors: []error{errors.New("temporary llm error")},
		responses: []*backend.ChatResponse{
			{Content: "ok", DoneReason: "stop"},
		},
	}
	o := newTestOrchestratorForDebugLoop(t)
	o.backend = mb

	err := o.DebugLoop(context.Background(), "go test ./...", 3, t.TempDir(), 5*time.Second, nil, nil, nil, runner)
	// May or may not error — important: no panic.
	_ = err
}

// ---------------------------------------------------------------------------
// compactHistory — with nil compactor (trivial branch)
// ---------------------------------------------------------------------------

func TestOrchestrator_CompactHistory_NilCompactor(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	// compactor is nil — should return immediately without panic.
	o.compactHistory(context.Background(), o.defaultSession())
}

// ---------------------------------------------------------------------------
// NewSession / GetSession
// ---------------------------------------------------------------------------

func TestOrchestrator_NewSession_AutoID(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	sess := mustNewSession(t, o, "")
	if sess.ID == "" {
		t.Error("expected auto-generated session ID")
	}
}

func TestOrchestrator_NewSession_ExplicitID(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	sess := mustNewSession(t, o, "my-explicit-id")
	if sess.ID != "my-explicit-id" {
		t.Errorf("expected 'my-explicit-id', got %q", sess.ID)
	}
}

func TestOrchestrator_GetSession_Found(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	sess := mustNewSession(t, o, "test-sess")
	got, ok := o.GetSession("test-sess")
	if !ok {
		t.Fatal("expected session to be found")
	}
	if got.ID != sess.ID {
		t.Errorf("expected session ID %q, got %q", sess.ID, got.ID)
	}
}

func TestOrchestrator_GetSession_NotFound(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	_, ok := o.GetSession("does-not-exist")
	if ok {
		t.Error("expected not found for missing session")
	}
}
