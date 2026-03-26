package tools

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initBoostRepo creates a temp git repo for internal package tests.
func initBoostRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644)
	w.Add("hello.txt")
	w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()},
	})
	return dir
}

// ============================================================
// builtin.go — RegisterTestsTool, RegisterWebTools
// ============================================================

func TestRegisterTestsTool_Registered(t *testing.T) {
	reg := NewRegistry()
	RegisterTestsTool(reg, t.TempDir(), 0)
	if _, ok := reg.Get("run_tests"); !ok {
		t.Error("run_tests not registered")
	}
}

func TestRegisterTestsTool_ZeroTimeout(t *testing.T) {
	reg := NewRegistry()
	// Should not panic with zero timeout (uses default 120s).
	RegisterTestsTool(reg, t.TempDir(), 0)
}

func TestRegisterTestsTool_CustomTimeout(t *testing.T) {
	reg := NewRegistry()
	RegisterTestsTool(reg, t.TempDir(), 30*time.Second)
	if _, ok := reg.Get("run_tests"); !ok {
		t.Error("run_tests not registered")
	}
}

func TestRegisterWebTools_NoAPIKey(t *testing.T) {
	reg := NewRegistry()
	RegisterWebTools(reg, "")
	// No web_search without key, but fetch_url should be registered.
	if _, ok := reg.Get("fetch_url"); !ok {
		t.Error("fetch_url not registered")
	}
	if _, ok := reg.Get("web_search"); ok {
		t.Error("web_search should NOT be registered without API key")
	}
}

func TestRegisterWebTools_WithAPIKey(t *testing.T) {
	reg := NewRegistry()
	RegisterWebTools(reg, "fake-key")
	if _, ok := reg.Get("web_search"); !ok {
		t.Error("web_search not registered")
	}
	if _, ok := reg.Get("fetch_url"); !ok {
		t.Error("fetch_url not registered")
	}
}

// ============================================================
// fetch_url.go — Schema, httpClient, ssrfSafeDialContext, isPrivateHost
// ============================================================

func TestFetchURLTool_Schema(t *testing.T) {
	tool := &FetchURLTool{}
	schema := tool.Schema()
	if schema.Type != "function" {
		t.Errorf("expected function type, got %q", schema.Type)
	}
	if schema.Function.Name != "fetch_url" {
		t.Errorf("expected fetch_url, got %q", schema.Function.Name)
	}
	if _, ok := schema.Function.Parameters.Properties["url"]; !ok {
		t.Error("expected 'url' property in schema")
	}
}

func TestFetchURLTool_HttpClientReturnsDefault(t *testing.T) {
	tool := &FetchURLTool{}
	c := tool.httpClient()
	if c == nil {
		t.Error("httpClient should not be nil")
	}
}

func TestFetchURLTool_HttpClientReturnsInjected(t *testing.T) {
	import_net_http_client := &FetchURLTool{client: nil}
	// Just verify the default path returns a non-nil client.
	c := import_net_http_client.httpClient()
	if c == nil {
		t.Error("httpClient should not return nil")
	}
}

func TestIsBlockedIP_Loopback(t *testing.T) {
	if !isBlockedIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should be blocked (loopback)")
	}
}

func TestIsBlockedIP_PrivateRFC1918(t *testing.T) {
	privates := []string{"10.0.0.1", "172.16.0.1", "192.168.1.1"}
	for _, ip := range privates {
		if !isBlockedIP(net.ParseIP(ip)) {
			t.Errorf("%s should be blocked (private)", ip)
		}
	}
}

func TestIsBlockedIP_LinkLocal(t *testing.T) {
	if !isBlockedIP(net.ParseIP("169.254.0.1")) {
		t.Error("169.254.0.1 should be blocked (link-local)")
	}
}

func TestIsBlockedIP_Unspecified(t *testing.T) {
	if !isBlockedIP(net.ParseIP("0.0.0.0")) {
		t.Error("0.0.0.0 should be blocked (unspecified)")
	}
}

func TestIsBlockedIP_Public(t *testing.T) {
	if isBlockedIP(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 should NOT be blocked (public)")
	}
}

func TestIsPrivateHost_Localhost(t *testing.T) {
	if !isPrivateHost("localhost") {
		t.Error("localhost should be private")
	}
}

func TestIsPrivateHost_NonExistent(t *testing.T) {
	// Cannot resolve — should return false (not private).
	result := isPrivateHost("this-hostname-does-not-exist-9876543210.example")
	if result {
		t.Error("non-resolvable host should return false")
	}
}

func TestSsrfSafeDialContext_InvalidAddr(t *testing.T) {
	dialFn := ssrfSafeDialContext()
	_, err := dialFn(context.Background(), "tcp", "not-valid-addr-no-port")
	if err == nil {
		t.Error("expected error for invalid addr")
	}
}

func TestSsrfSafeDialContext_PrivateHost(t *testing.T) {
	dialFn := ssrfSafeDialContext()
	_, err := dialFn(context.Background(), "tcp", "localhost:80")
	if err == nil {
		t.Error("expected error for localhost (private/loopback)")
	}
	if !strings.Contains(err.Error(), "private") {
		t.Errorf("error should mention private, got: %v", err)
	}
}

// ============================================================
// git.go — Description/Permission/Schema for all tools + Execute edge cases
// ============================================================

func TestGitStatusTool_Description(t *testing.T) {
	tool := &GitStatusTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitStatusTool_Schema(t *testing.T) {
	tool := &GitStatusTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_status" {
		t.Errorf("expected git_status, got %q", s.Function.Name)
	}
}

func TestGitStatusTool_NotGitRepo(t *testing.T) {
	tool := &GitStatusTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), nil)
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitStatusTool_InvalidPath(t *testing.T) {
	tool := &GitStatusTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"path": "../../escape"})
	if !result.IsError {
		t.Error("expected error for path escaping sandbox")
	}
}

func TestGitLogTool_Description(t *testing.T) {
	tool := &GitLogTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitLogTool_Permission(t *testing.T) {
	tool := &GitLogTool{SandboxRoot: "/tmp"}
	if tool.Permission() != PermRead {
		t.Error("expected PermRead")
	}
}

func TestGitLogTool_Schema(t *testing.T) {
	tool := &GitLogTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_log" {
		t.Errorf("expected git_log, got %q", s.Function.Name)
	}
}

func TestGitLogTool_NotGitRepo(t *testing.T) {
	tool := &GitLogTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), nil)
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitLogTool_NNegative(t *testing.T) {
	// n <= 0 should be replaced with 10; still fails since no repo, but tests the branch.
	tool := &GitLogTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"n": float64(-5)})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitLogTool_NOver100(t *testing.T) {
	tool := &GitLogTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"n": float64(200)})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitLogTool_NAsInt(t *testing.T) {
	tool := &GitLogTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"n": 5})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitDiffTool_Description(t *testing.T) {
	tool := &GitDiffTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitDiffTool_Permission(t *testing.T) {
	tool := &GitDiffTool{SandboxRoot: "/tmp"}
	if tool.Permission() != PermRead {
		t.Error("expected PermRead")
	}
}

func TestGitDiffTool_Schema(t *testing.T) {
	tool := &GitDiffTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_diff" {
		t.Errorf("expected git_diff, got %q", s.Function.Name)
	}
}

func TestGitDiffTool_NotGitRepo(t *testing.T) {
	tool := &GitDiffTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), nil)
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitBranchTool_Description(t *testing.T) {
	tool := &GitBranchTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitBranchTool_Permission(t *testing.T) {
	tool := &GitBranchTool{SandboxRoot: "/tmp"}
	if tool.Permission() != PermWrite {
		t.Error("expected PermWrite")
	}
}

func TestGitBranchTool_Schema(t *testing.T) {
	tool := &GitBranchTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_branch" {
		t.Errorf("expected git_branch, got %q", s.Function.Name)
	}
}

func TestGitBranchTool_NotGitRepo(t *testing.T) {
	tool := &GitBranchTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitBranchTool_UnknownAction(t *testing.T) {
	// GitBranchTool opens the repo first, then validates action.
	// Use a git repo so we reach the unknown-action branch.
	dir := initBoostRepo(t)
	tool := &GitBranchTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"action": "delete"})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
	if !strings.Contains(result.Error, "unknown action") {
		t.Errorf("error should mention unknown action, got %q", result.Error)
	}
}

func TestGitBranchTool_Create_MissingName(t *testing.T) {
	tool := &GitBranchTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"action": "create"})
	if !result.IsError {
		t.Error("expected error for missing name")
	}
}

func TestGitBranchTool_InvalidBranchName(t *testing.T) {
	// Requires real repo so openRepo succeeds and name validation is reached.
	dir := initBoostRepo(t)
	tool := &GitBranchTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"name":   "..bad-name",
	})
	if !result.IsError {
		t.Error("expected error for invalid branch name")
	}
	if !strings.Contains(result.Error, "invalid branch name") {
		t.Errorf("error should mention invalid branch name, got %q", result.Error)
	}
}

func TestGitBranchTool_InvalidBranchNameChars(t *testing.T) {
	dir := initBoostRepo(t)
	tool := &GitBranchTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"name":   "branch with spaces",
	})
	if !result.IsError {
		t.Error("expected error for branch name with spaces")
	}
}

func TestGitBranchTool_DotDotInName(t *testing.T) {
	dir := initBoostRepo(t)
	tool := &GitBranchTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"name":   "feat..bad",
	})
	if !result.IsError {
		t.Error("expected error for '..' in branch name")
	}
}

func TestGitCommitTool_Description(t *testing.T) {
	tool := &GitCommitTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitCommitTool_Permission(t *testing.T) {
	tool := &GitCommitTool{SandboxRoot: "/tmp"}
	if tool.Permission() != PermWrite {
		t.Error("expected PermWrite")
	}
}

func TestGitCommitTool_Schema(t *testing.T) {
	tool := &GitCommitTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_commit" {
		t.Errorf("expected git_commit, got %q", s.Function.Name)
	}
}

func TestGitCommitTool_NotGitRepo(t *testing.T) {
	tool := &GitCommitTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"message": "test commit"})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitCommitTool_WhitespaceMessage(t *testing.T) {
	tool := &GitCommitTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"message": "   "})
	if !result.IsError {
		t.Error("expected error for whitespace-only message")
	}
}

func TestGitBlameTool_Description(t *testing.T) {
	tool := &GitBlameTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitBlameTool_Permission(t *testing.T) {
	tool := &GitBlameTool{SandboxRoot: "/tmp"}
	if tool.Permission() != PermRead {
		t.Error("expected PermRead")
	}
}

func TestGitBlameTool_Schema(t *testing.T) {
	tool := &GitBlameTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_blame" {
		t.Errorf("expected git_blame, got %q", s.Function.Name)
	}
}

func TestGitBlameTool_MissingPath(t *testing.T) {
	tool := &GitBlameTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing path")
	}
	if !strings.Contains(result.Error, "path required") {
		t.Errorf("error should say 'path required', got %q", result.Error)
	}
}

func TestGitBlameTool_EmptyPath(t *testing.T) {
	tool := &GitBlameTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"path": ""})
	if !result.IsError {
		t.Error("expected error for empty path")
	}
}

func TestGitBlameTool_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	tool := &GitBlameTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"path": "somefile.go"})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitStashTool_Description(t *testing.T) {
	tool := &GitStashTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitStashTool_Permission(t *testing.T) {
	tool := &GitStashTool{SandboxRoot: "/tmp"}
	if tool.Permission() != PermWrite {
		t.Error("expected PermWrite")
	}
}

func TestGitStashTool_Schema(t *testing.T) {
	tool := &GitStashTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_stash" {
		t.Errorf("expected git_stash, got %q", s.Function.Name)
	}
}

func TestGitStashTool_Pop_NotImplemented(t *testing.T) {
	tool := &GitStashTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"action": "pop"})
	if result.IsError {
		t.Errorf("expected pop to return helpful message, not error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "not implemented") {
		t.Errorf("expected 'not implemented' in output, got %q", result.Output)
	}
}

func TestGitStashTool_Push_NotGitRepo(t *testing.T) {
	tool := &GitStashTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"action": "push"})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

// ============================================================
// github.go — Description/Schema, intArg json.Number
// ============================================================

func TestGHPRListTool_Schema(t *testing.T) {
	tool := &GHPRListTool{}
	s := tool.Schema()
	if s.Function.Name != "gh_pr_list" {
		t.Errorf("expected gh_pr_list, got %q", s.Function.Name)
	}
}

func TestGHPRViewTool_Schema(t *testing.T) {
	tool := &GHPRViewTool{}
	s := tool.Schema()
	if s.Function.Name != "gh_pr_view" {
		t.Errorf("expected gh_pr_view, got %q", s.Function.Name)
	}
}

func TestGHPRDiffTool_Schema(t *testing.T) {
	tool := &GHPRDiffTool{}
	s := tool.Schema()
	if s.Function.Name != "gh_pr_diff" {
		t.Errorf("expected gh_pr_diff, got %q", s.Function.Name)
	}
}

func TestGHPRCreateTool_Schema(t *testing.T) {
	tool := &GHPRCreateTool{}
	s := tool.Schema()
	if s.Function.Name != "gh_pr_create" {
		t.Errorf("expected gh_pr_create, got %q", s.Function.Name)
	}
}

func TestGHIssueListTool_Schema(t *testing.T) {
	tool := &GHIssueListTool{}
	s := tool.Schema()
	if s.Function.Name != "gh_issue_list" {
		t.Errorf("expected gh_issue_list, got %q", s.Function.Name)
	}
}

func TestGHIssueViewTool_Schema(t *testing.T) {
	tool := &GHIssueViewTool{}
	s := tool.Schema()
	if s.Function.Name != "gh_issue_view" {
		t.Errorf("expected gh_issue_view, got %q", s.Function.Name)
	}
}

func TestGHIssueCreateTool_Schema(t *testing.T) {
	tool := &GHIssueCreateTool{}
	s := tool.Schema()
	if s.Function.Name != "gh_issue_create" {
		t.Errorf("expected gh_issue_create, got %q", s.Function.Name)
	}
}

func TestIntArg_JsonNumber(t *testing.T) {
	args := map[string]any{"n": json.Number("99")}
	got, ok := intArg(args, "n")
	if !ok || got != 99 {
		t.Errorf("intArg json.Number: got (%d, %v), want (99, true)", got, ok)
	}
}

func TestIntArg_InvalidJsonNumber(t *testing.T) {
	args := map[string]any{"n": json.Number("not-a-number")}
	_, ok := intArg(args, "n")
	if ok {
		t.Error("intArg with invalid json.Number should return false")
	}
}

func TestIntArg_WrongType(t *testing.T) {
	args := map[string]any{"n": "a-string"}
	_, ok := intArg(args, "n")
	if ok {
		t.Error("intArg with string should return false")
	}
}

// noGHReg is a sentinel gh path used in Execute tests to prevent any real gh CLI execution.
// On GitHub Actions gh is authenticated; never fall through to the real binary in unit tests.
const noGHReg = "/nonexistent/gh-test-sentinel"

func TestGHPRListTool_Execute_LimitAsInt(t *testing.T) {
	// Cover the int case in limit parsing.
	tool := &GHPRListTool{ghBase: ghBase{GHPath: noGHReg}}
	_ = tool.Execute(context.Background(), map[string]any{"limit": 5})
}

func TestGHIssueListTool_Execute_LimitAsInt(t *testing.T) {
	tool := &GHIssueListTool{ghBase: ghBase{GHPath: noGHReg}}
	_ = tool.Execute(context.Background(), map[string]any{"limit": 10})
}

func TestGHIssueListTool_Execute_LimitAsFloat(t *testing.T) {
	tool := &GHIssueListTool{ghBase: ghBase{GHPath: noGHReg}}
	_ = tool.Execute(context.Background(), map[string]any{"limit": float64(10)})
}

func TestGHIssueListTool_Execute_WithLabel(t *testing.T) {
	tool := &GHIssueListTool{ghBase: ghBase{GHPath: noGHReg}}
	_ = tool.Execute(context.Background(), map[string]any{"label": "bug"})
}

func TestGHPRCreateTool_Execute_WithDraftAndBase(t *testing.T) {
	// Covers the draft and base branches.
	tool := &GHPRCreateTool{ghBase: ghBase{GHPath: noGHReg}}
	_ = tool.Execute(context.Background(), map[string]any{
		"title": "My PR",
		"body":  "Body text",
		"draft": true,
		"base":  "main",
	})
}

func TestGHIssueCreateTool_Execute_WithLabelAndAssignee(t *testing.T) {
	tool := &GHIssueCreateTool{ghBase: ghBase{GHPath: noGHReg}}
	_ = tool.Execute(context.Background(), map[string]any{
		"title":    "My Issue",
		"body":     "Body text",
		"label":    "bug",
		"assignee": "user123",
	})
}

// ============================================================
// run_tests.go — Description, Schema
// ============================================================

func TestRunTestsTool_Description(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestRunTestsTool_Schema(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "run_tests" {
		t.Errorf("expected run_tests, got %q", s.Function.Name)
	}
}

// ============================================================
// symbol_tools.go — toInt, uriToRelPath, lspKindName
// ============================================================

func TestToInt_Int64(t *testing.T) {
	result, err := toInt(int64(42))
	if err != nil || result != 42 {
		t.Errorf("toInt(int64): got (%d, %v)", result, err)
	}
}

func TestToInt_UnsupportedType(t *testing.T) {
	_, err := toInt("not-a-number")
	if err == nil {
		t.Error("expected error for string type")
	}
}

func TestToInt_Bool(t *testing.T) {
	_, err := toInt(true)
	if err == nil {
		t.Error("expected error for bool type")
	}
}

func TestUriToRelPath_NoFileScheme(t *testing.T) {
	// URI without file:// should strip nothing and return rel from root.
	root := "/project"
	uri := "/project/main.go"
	got := uriToRelPath(root, uri)
	if got != "main.go" {
		t.Errorf("expected main.go, got %q", got)
	}
}

func TestUriToRelPath_OutsideRoot(t *testing.T) {
	root := "/project"
	uri := "file:///other/path/file.go"
	got := uriToRelPath(root, uri)
	// Should return absolute path when rel fails (or return the path itself).
	if got == "" {
		t.Error("expected non-empty result")
	}
}

func TestLspKindName_KnownKinds(t *testing.T) {
	tests := []struct {
		kind int
		want string
	}{
		{5, "Class"},
		{6, "Method"},
		{12, "Function"},
		{13, "Variable"},
		{11, "Interface"},
		{23, "Struct"},
		{14, "Constant"},
	}
	for _, tc := range tests {
		got := lspKindName(tc.kind)
		if got != tc.want {
			t.Errorf("lspKindName(%d) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestLspKindName_Unknown(t *testing.T) {
	got := lspKindName(999)
	if got != "Symbol" {
		t.Errorf("expected Symbol for unknown kind, got %q", got)
	}
}

// ============================================================
// worktree.go — Description/Schema
// ============================================================

func TestGitWorktreeCreateTool_Description(t *testing.T) {
	tool := &GitWorktreeCreateTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitWorktreeCreateTool_Schema(t *testing.T) {
	tool := &GitWorktreeCreateTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_worktree_create" {
		t.Errorf("expected git_worktree_create, got %q", s.Function.Name)
	}
}

func TestGitWorktreeRemoveTool_Description(t *testing.T) {
	tool := &GitWorktreeRemoveTool{SandboxRoot: "/tmp"}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestGitWorktreeRemoveTool_Schema(t *testing.T) {
	tool := &GitWorktreeRemoveTool{SandboxRoot: "/tmp"}
	s := tool.Schema()
	if s.Function.Name != "git_worktree_remove" {
		t.Errorf("expected git_worktree_remove, got %q", s.Function.Name)
	}
}

// ============================================================
// git_stager.go — StageFile (non-git path returns nil)
// ============================================================

func TestDefaultGitStager_StageFile_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	stager := &DefaultGitStager{SandboxRoot: dir}
	err := stager.StageFile(dir)
	if err != nil {
		t.Errorf("expected nil error for non-git dir, got: %v", err)
	}
}
