package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// LoadIndex — public entrypoint (0% coverage)
// ---------------------------------------------------------------------------

func TestLoadIndex_FreshCacheHit(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")

	// Write a fresh cache (5 minutes old — well within 24h TTL)
	cached := cachedIndex{
		FetchedAt: time.Now().Add(-5 * time.Minute),
		Entries: []IndexEntry{
			{Name: "cached-skill"},
		},
	}
	data, _ := json.Marshal(cached)
	os.WriteFile(cachePath, data, 0644)

	entries, _, err := LoadIndex(cachePath)
	if err != nil {
		t.Fatalf("LoadIndex with fresh cache: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "cached-skill" {
		t.Errorf("unexpected entries: %+v", entries)
	}
}

func TestLoadIndex_StaleCacheFallsBackToNetwork(t *testing.T) {
	// Serve fresh data from a local test server in remoteRegistry format
	served := remoteRegistry{Skills: []IndexEntry{{Name: "fresh-skill"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(served)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")

	// Write stale cache (30 hours old)
	stale := cachedIndex{
		FetchedAt: time.Now().Add(-30 * time.Hour),
		Entries:   []IndexEntry{{Name: "stale-skill"}},
	}
	data, _ := json.Marshal(stale)
	os.WriteFile(cachePath, data, 0644)

	// fetchAndCacheIndex uses the URL arg — we can't redirect LoadIndex's constant without
	// a test server, so instead exercise via loadIndexFromFile (stale) + fetchAndCacheIndex directly.
	// Verify stale returns error, then fresh fetch works.
	_, _, err := loadIndexFromFile(cachePath)
	if err == nil {
		t.Fatal("stale cache should return error")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected 'stale' in error, got: %v", err)
	}

	// Now verify fetchAndCacheIndex (used by LoadIndex internally) works with our server
	entries, _, err := fetchAndCacheIndex(srv.URL, cachePath)
	if err != nil {
		t.Fatalf("fetchAndCacheIndex: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "fresh-skill" {
		t.Errorf("unexpected entries: %+v", entries)
	}
}

// ---------------------------------------------------------------------------
// FetchAndCacheIndex — public entrypoint (0% coverage)
// ---------------------------------------------------------------------------

func TestFetchAndCacheIndex_Public(t *testing.T) {
	payload := remoteRegistry{
		Version: "1",
		Skills:  []IndexEntry{{Name: "public-skill", Author: "test"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	// FetchAndCacheIndex hardcodes RegistryIndexURL — we can't redirect it without
	// monkey-patching. Instead, verify that fetchAndCacheIndex (which it delegates to)
	// round-trips correctly via the test server.
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "index.json")

	entries, _, err := fetchAndCacheIndex(srv.URL, cachePath)
	if err != nil {
		t.Fatalf("fetchAndCacheIndex: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one entry")
	}
	// Cache file should now exist and be fresh enough for LoadIndex to use it
	freshEntries, _, err := loadIndexFromFile(cachePath)
	if err != nil {
		t.Fatalf("loadIndexFromFile after fetch: %v", err)
	}
	if len(freshEntries) != len(entries) {
		t.Errorf("cache entry count mismatch: got %d, want %d", len(freshEntries), len(entries))
	}
}

// ---------------------------------------------------------------------------
// fetchAndCacheIndex — non-200 status error path
// ---------------------------------------------------------------------------

func TestFetchAndCacheIndex_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, _, err := fetchAndCacheIndex(srv.URL, filepath.Join(dir, "cache.json"))
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// fetchAndCacheIndex — malformed JSON response
// ---------------------------------------------------------------------------

func TestFetchAndCacheIndex_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is not json {{{"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, _, err := fetchAndCacheIndex(srv.URL, filepath.Join(dir, "cache.json"))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// ---------------------------------------------------------------------------
// fetchAndCacheIndex — network failure (server not reachable)
// ---------------------------------------------------------------------------

func TestFetchAndCacheIndex_NetworkFailure(t *testing.T) {
	// Use a localhost port that's not listening
	dir := t.TempDir()
	_, _, err := fetchAndCacheIndex("http://127.0.0.1:1", filepath.Join(dir, "cache.json"))
	if err == nil {
		t.Fatal("expected error when server is unreachable")
	}
}

// ---------------------------------------------------------------------------
// loadIndexFromFile — malformed JSON in cache file
// ---------------------------------------------------------------------------

func TestLoadIndexFromFile_CorruptedCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")
	os.WriteFile(cachePath, []byte("not valid json"), 0644)

	_, _, err := loadIndexFromFile(cachePath)
	if err == nil {
		t.Fatal("expected error for corrupted cache file")
	}
}

// ---------------------------------------------------------------------------
// PromptTool.Permission — shell and agent modes (50% coverage)
// ---------------------------------------------------------------------------

func TestPromptTool_Permission_AllModes(t *testing.T) {
	cases := []struct {
		mode string
		want tools.PermissionLevel
	}{
		{"template", tools.PermRead},
		{"shell", tools.PermExec},
		{"agent", tools.PermWrite},
		{"unknown", tools.PermRead}, // defaults to template path
	}

	for _, tc := range cases {
		pt := &PromptTool{mode: tc.mode}
		got := pt.Permission()
		if got != tc.want {
			t.Errorf("mode %q: Permission() = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// validateArgs — pattern mismatch returns error
// ---------------------------------------------------------------------------

func TestPromptTool_ValidateArgs_PatternMismatch(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"email": {"type": "string", "pattern": "^[a-z]+@[a-z]+\\.com$"}
		}
	}`
	pt := NewPromptTool("t", "d", schema, "body")

	err := pt.validateArgs(map[string]any{"email": "NOT_AN_EMAIL"})
	if err == nil {
		t.Fatal("expected error for pattern mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "does not match pattern") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPromptTool_ValidateArgs_PatternMatch(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"code": {"type": "string", "pattern": "^[A-Z]{3}$"}
		}
	}`
	pt := NewPromptTool("t", "d", schema, "body")

	err := pt.validateArgs(map[string]any{"code": "ABC"})
	if err != nil {
		t.Errorf("expected no error for valid pattern match, got: %v", err)
	}
}

func TestPromptTool_ValidateArgs_MalformedPattern_Skipped(t *testing.T) {
	// Malformed regex pattern should be silently skipped (not block execution)
	schema := `{
		"type": "object",
		"properties": {
			"val": {"type": "string", "pattern": "[invalid("}
		}
	}`
	pt := NewPromptTool("t", "d", schema, "body")

	err := pt.validateArgs(map[string]any{"val": "anything"})
	if err != nil {
		t.Errorf("malformed pattern should be skipped, got: %v", err)
	}
}

func TestPromptTool_ValidateArgs_MissingRequired(t *testing.T) {
	schema := `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`
	pt := NewPromptTool("t", "d", schema, "body")

	err := pt.validateArgs(map[string]any{}) // name is missing
	if err == nil {
		t.Fatal("expected error for missing required field, got nil")
	}
	if !strings.Contains(err.Error(), "missing required arg") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPromptTool_ValidateArgs_MalformedSchemaJSON_Allowed(t *testing.T) {
	pt := NewPromptTool("t", "d", "not json", "body")
	// Should not error — malformed schema is treated permissively
	if err := pt.validateArgs(map[string]any{"x": "y"}); err != nil {
		t.Errorf("malformed schema should be permissive, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// renderAgentPrompt — parse error path (83.3% coverage)
// ---------------------------------------------------------------------------

func TestPromptTool_RenderAgentPrompt_ParseError(t *testing.T) {
	pt := &PromptTool{
		name: "bad_agent",
		body: "{{badfunction .x}}", // unknown function → parse error
	}
	_, err := pt.renderAgentPrompt(map[string]any{"x": "val"})
	if err == nil {
		t.Fatal("expected parse error for unknown template function")
	}
}

func TestPromptTool_RenderAgentPrompt_Success(t *testing.T) {
	pt := &PromptTool{
		name: "ok_agent",
		body: "Analyze {{.topic}} using {{upper .method}}",
	}
	result, err := pt.renderAgentPrompt(map[string]any{"topic": "go", "method": "profiling"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "go") || !strings.Contains(result, "PROFILING") {
		t.Errorf("unexpected prompt: %q", result)
	}
}

// ---------------------------------------------------------------------------
// InjectAgentExecutor — nil guard paths (83.3% coverage)
// ---------------------------------------------------------------------------

func TestInjectAgentExecutor_NilRegistry_NoOp(t *testing.T) {
	executor := &mockAgentExecutor{returnValue: "ok"}
	// Should not panic
	InjectAgentExecutor(nil, executor)
}

func TestInjectAgentExecutor_NilExecutor_NoOp(t *testing.T) {
	reg := tools.NewRegistry()
	// Should not panic
	InjectAgentExecutor(reg, nil)
}

// ---------------------------------------------------------------------------
// Manifest.Save — MkdirAll error path: use an unwritable parent
// ---------------------------------------------------------------------------

func TestManifest_Save_UnwritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — permission test not meaningful")
	}

	// Create a temp dir, make it read-only so MkdirAll inside it will fail
	parent := t.TempDir()
	roDir := filepath.Join(parent, "readonly")
	os.MkdirAll(roDir, 0555) // read+execute only, no write

	m := &Manifest{
		Entries: []InstalledEntry{{Name: "x"}},
		path:    filepath.Join(roDir, "subdir", "installed.json"),
	}

	err := m.Save()
	if err == nil {
		t.Error("expected error when writing to unwritable directory, got nil")
	}
}

// ---------------------------------------------------------------------------
// executeTemplate — execute error path (missingkey=zero makes this hard to trigger,
// but we can confirm success path with funcmap: join, replace
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteTemplate_JoinAndReplace(t *testing.T) {
	// join takes (sep, slice) — but strArgs is map[string]string so we test replace
	pt := NewPromptTool("t", "d", "", `{{replace .msg "world" "Go"}}`)
	result := pt.Execute(context.Background(), map[string]any{"msg": "hello world"})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output != "hello Go" {
		t.Errorf("replace func: got %q, want %q", result.Output, "hello Go")
	}
}

// ---------------------------------------------------------------------------
// parseMarkdownSkill — unclosed frontmatter (closing --- missing)
// ---------------------------------------------------------------------------

func TestParseMarkdownSkill_UnclosedFrontmatter(t *testing.T) {
	content := "---\nname: test\n"
	// No closing ---
	_, err := parseMarkdownSkill(content)
	if err == nil {
		t.Fatal("expected error for unclosed frontmatter, got nil")
	}
	if !strings.Contains(err.Error(), "not closed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// LoadFromDir — missing skill.json
// ---------------------------------------------------------------------------

func TestLoadFromDir_MissingSkillJSON(t *testing.T) {
	dir := t.TempDir()
	// No skill.json written
	_, err := LoadFromDir(dir)
	if err == nil {
		t.Fatal("expected error when skill.json is missing, got nil")
	}
}

// ---------------------------------------------------------------------------
// LoadFromDir — custom prompt_file specified but file is missing (should error)
// ---------------------------------------------------------------------------

func TestLoadFromDir_CustomPromptFileMissing(t *testing.T) {
	dir := t.TempDir()
	def := `{"name":"x","prompt_file":"missing_prompt.md"}`
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(def), 0644)
	// missing_prompt.md not created — but os.IsNotExist is ignored, so this succeeds with empty prompt

	s, err := LoadFromDir(dir)
	// prompt_file missing → os.IsNotExist → no error, just empty prompt
	if err != nil {
		t.Fatalf("expected no error for missing optional prompt file, got: %v", err)
	}
	if s.SystemPromptFragment() != "" {
		t.Errorf("expected empty prompt for missing file, got: %q", s.SystemPromptFragment())
	}
}

// ---------------------------------------------------------------------------
// SkillRegistry.AllTools — with skills that have tools vs nil
// ---------------------------------------------------------------------------

func TestSkillRegistry_AllTools_Mixed(t *testing.T) {
	reg := NewSkillRegistry()

	// Skill with no tools
	reg.Register(&stubSkill{name: "no-tools"})

	// Skill with tools (use FilesystemSkill via temp dir)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"name":"has-tools"}`), 0644)
	toolsDir := filepath.Join(dir, "tools")
	os.MkdirAll(toolsDir, 0755)
	os.WriteFile(filepath.Join(toolsDir, "my_tool.md"), []byte("---\ntool: my_tool\ndescription: Test\n---\nbody"), 0644)

	fs, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	reg.Register(fs)

	allTools := reg.AllTools()
	if len(allTools) != 1 {
		t.Errorf("AllTools: expected 1 tool (from FilesystemSkill), got %d", len(allTools))
	}
	if allTools[0].Name() != "my_tool" {
		t.Errorf("unexpected tool name: %s", allTools[0].Name())
	}
}

func TestSkillRegistry_AllTools_EmptyRegistry(t *testing.T) {
	reg := NewSkillRegistry()
	tools := reg.AllTools()
	// Should return nil (no append happened) or empty — just not panic
	_ = tools
}

// ---------------------------------------------------------------------------
// parseToolMD — timeout field is parsed correctly
// ---------------------------------------------------------------------------

func TestParseToolMD_TimeoutField(t *testing.T) {
	content := "---\ntool: timed_tool\ndescription: Timed\nmode: shell\nshell: echo\ntimeout: 30\n---\nbody"
	pt, err := parseToolMD([]byte(content))
	if err != nil {
		t.Fatalf("parseToolMD: %v", err)
	}
	if pt.shellTimeoutSecs != 30 {
		t.Errorf("shellTimeoutSecs: got %d, want 30", pt.shellTimeoutSecs)
	}
}

// ---------------------------------------------------------------------------
// parseToolMD — agent mode fields parsed correctly
// ---------------------------------------------------------------------------

func TestParseToolMD_AgentModeFields(t *testing.T) {
	content := "---\ntool: agent_tool\ndescription: Agent\nmode: agent\nagent_model: claude-3\nbudget_tokens: 2000\n---\nbody"
	pt, err := parseToolMD([]byte(content))
	if err != nil {
		t.Fatalf("parseToolMD: %v", err)
	}
	if pt.mode != "agent" {
		t.Errorf("mode: got %q, want agent", pt.mode)
	}
	if pt.agentModel != "claude-3" {
		t.Errorf("agentModel: got %q, want claude-3", pt.agentModel)
	}
	if pt.budgetTokens != 2000 {
		t.Errorf("budgetTokens: got %d, want 2000", pt.budgetTokens)
	}
}

// ---------------------------------------------------------------------------
// executeAgent — executor returns error
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteAgent_ExecutorError(t *testing.T) {
	pt := &PromptTool{
		name:         "err_agent",
		mode:         "agent",
		body:         "Do {{task}}",
		agentModel:   "gpt-4",
		budgetTokens: 100,
	}

	executor := &mockAgentExecutor{
		returnError: fmt.Errorf("LLM quota exceeded"),
	}
	pt.SetAgentExecutor(executor)

	result := pt.Execute(context.Background(), map[string]any{"task": "work"})
	if !result.IsError {
		t.Fatal("expected error when executor returns error")
	}
	if !strings.Contains(result.Error, "execution failed") {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// executeShell — shell timeout via per-tool shellTimeoutSecs
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteShell_PerToolTimeout(t *testing.T) {
	pt := &PromptTool{
		name:             "timeout_tool",
		mode:             "shell",
		shellBin:         "sleep",
		shellArgs:        []string{"10"},
		shellTimeoutSecs: 1, // 1 second — but context already very short
		maxOutputBytes:   maxShellOutputBytes,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := pt.Execute(ctx, map[string]any{})
	if !result.IsError {
		t.Error("expected timeout error")
	}
}

// ---------------------------------------------------------------------------
// executeShell — empty argv after template render
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteShell_EmptyArgvAfterRender(t *testing.T) {
	// Body renders to only whitespace → empty argv
	pt := &PromptTool{
		name:           "empty_argv",
		mode:           "shell",
		body:           "   ", // all whitespace → Fields() returns []
		maxOutputBytes: maxShellOutputBytes,
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for empty argv after template render")
	}
	if !strings.Contains(result.Error, "empty command") {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// limitedWriter — partial write when remaining < len(p)
// ---------------------------------------------------------------------------

func TestLimitedWriter_PartialWrite(t *testing.T) {
	lw := &limitedWriter{max: 5}
	n, err := lw.Write([]byte("hello world")) // 11 bytes, cap at 5
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Returns len(p) even when truncated (contract)
	if n != 11 {
		t.Errorf("n = %d, want 11", n)
	}
	if !lw.truncated {
		t.Error("should be truncated")
	}
	if lw.buf.String() != "hello" {
		t.Errorf("buf = %q, want %q", lw.buf.String(), "hello")
	}
}

// ---------------------------------------------------------------------------
// NewPromptTool — defaults maxOutputBytes to maxShellOutputBytes
// ---------------------------------------------------------------------------

func TestNewPromptTool_DefaultMaxOutputBytes(t *testing.T) {
	pt := NewPromptTool("name", "desc", "{}", "body")
	if pt.maxOutputBytes != maxShellOutputBytes {
		t.Errorf("maxOutputBytes: got %d, want %d", pt.maxOutputBytes, maxShellOutputBytes)
	}
	if pt.mode != "template" {
		t.Errorf("mode: got %q, want template", pt.mode)
	}
}

// ---------------------------------------------------------------------------
// LoadManifest — non-IsNotExist read error (unreadable file)
// ---------------------------------------------------------------------------

func TestLoadManifest_ReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — permission test not meaningful")
	}

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "installed.json")
	// Create the file, then remove read permission
	os.WriteFile(manifestPath, []byte("[]"), 0644)
	os.Chmod(manifestPath, 0000) // no read permission

	_, err := LoadManifest(manifestPath)
	if err == nil {
		t.Error("expected error for unreadable manifest, got nil")
	}
}

// ---------------------------------------------------------------------------
// DefaultManifestPath — error fallback when HOME is unset
// ---------------------------------------------------------------------------

func TestDefaultManifestPath_NoHOME_Fallback(t *testing.T) {
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })
	os.Unsetenv("HOME")
	os.Unsetenv("USERPROFILE")
	os.Unsetenv("HOMEPATH")

	path := DefaultManifestPath()
	if path == "" {
		t.Error("DefaultManifestPath returned empty string even with no HOME")
	}
	// Should contain .huginn and installed.json
	if !strings.Contains(path, ".huginn") {
		t.Errorf("fallback path missing .huginn: %q", path)
	}
}

// ---------------------------------------------------------------------------
// LoadManifest — invalid JSON in existing file
// ---------------------------------------------------------------------------

func TestLoadManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "installed.json")
	os.WriteFile(manifestPath, []byte("not json"), 0644)

	_, err := LoadManifest(manifestPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON manifest, got nil")
	}
	if !strings.Contains(err.Error(), "JSON parse") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// fetchAndCacheIndex — cache dir creation fails (write to read-only subpath)
// Exercises the mkdir-error branch (non-fatal, still returns entries)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Manifest.Save — WriteFile fails because path is a directory
// ---------------------------------------------------------------------------

func TestManifest_Save_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	// Make the manifest path itself a directory (not a file)
	manifestDir := filepath.Join(dir, "installed.json")
	os.MkdirAll(manifestDir, 0755)

	m := &Manifest{
		Entries: []InstalledEntry{{Name: "x"}},
		path:    manifestDir, // path is a dir, not a file
	}

	err := m.Save()
	if err == nil {
		t.Error("expected error when manifest path is a directory, got nil")
	}
}

func TestFetchAndCacheIndex_CacheDirUnwritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — permission test not meaningful")
	}

	payload := remoteRegistry{
		Version: "1",
		Skills:  []IndexEntry{{Name: "skill-x"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	parent := t.TempDir()
	roDir := filepath.Join(parent, "readonly")
	os.MkdirAll(roDir, 0555) // no write permission

	// cachePath inside a subdirectory of the read-only dir — MkdirAll will fail
	cachePath := filepath.Join(roDir, "subdir", "cache.json")

	// Should still return entries even if caching fails
	entries, _, err := fetchAndCacheIndex(srv.URL, cachePath)
	if err != nil {
		t.Fatalf("fetchAndCacheIndex: unexpected error: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected entries even when cache write fails")
	}
}
