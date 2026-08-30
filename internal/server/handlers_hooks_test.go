package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newTestServerWithHooks builds a test server with a real workspace/global
// hooks.json root wired up (EnableUserHooks) so the hooks API endpoints have
// somewhere to read/write.
func newTestServerWithHooks(t *testing.T) (*Server, *httptest.Server, string, string) {
	t.Helper()
	srv, ts := newTestServer(t)
	huginnHome := t.TempDir()
	workspaceRoot := t.TempDir()
	srv.orch.SetHuginnHome(huginnHome)
	srv.orch.SetGitRoot(workspaceRoot)
	if _, err := srv.orch.EnableUserHooks(); err != nil {
		t.Fatalf("EnableUserHooks: %v", err)
	}
	return srv, ts, huginnHome, workspaceRoot
}

func doJSON(t *testing.T, ts *httptest.Server, method, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestHandleListHooks_EmptyByDefault(t *testing.T) {
	_, ts, _, _ := newTestServerWithHooks(t)
	resp, out := doJSON(t, ts, "GET", "/api/v1/hooks", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body = %+v", resp.StatusCode, out)
	}
	hooks, _ := out["hooks"].([]any)
	if len(hooks) != 0 {
		t.Fatalf("expected no hooks with no hooks.json, got %+v", hooks)
	}
}

func TestHandleCreateHook_AddEditDelete(t *testing.T) {
	_, ts, _, workspaceRoot := newTestServerWithHooks(t)

	create := map[string]any{
		"id":    "block-rm",
		"event": "PreToolUse",
		"match": map[string]any{"tools": []string{"bash"}},
		"action": map[string]any{
			"type":         "command",
			"command":      "exit 1",
			"timeout_secs": 5,
		},
		"enabled": true,
		"scope":   "workspace",
	}
	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks", create)
	if resp.StatusCode != 200 {
		t.Fatalf("create status = %d, body = %+v", resp.StatusCode, out)
	}

	// The file actually landed on disk in the workspace scope.
	raw, err := os.ReadFile(filepath.Join(workspaceRoot, ".huginn", "hooks.json"))
	if err != nil {
		t.Fatalf("expected workspace hooks.json to exist: %v", err)
	}
	if !bytes.Contains(raw, []byte("block-rm")) {
		t.Fatalf("hooks.json missing new hook: %s", raw)
	}

	// Listing reflects it.
	resp, out = doJSON(t, ts, "GET", "/api/v1/hooks", nil)
	hooks, _ := out["hooks"].([]any)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %+v", hooks)
	}

	// Duplicate id is rejected.
	resp, out = doJSON(t, ts, "POST", "/api/v1/hooks", create)
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409 for duplicate id, got %d: %+v", resp.StatusCode, out)
	}

	// Update: flip enabled off.
	update := map[string]any{
		"id":      "block-rm",
		"event":   "PreToolUse",
		"match":   map[string]any{"tools": []string{"bash"}},
		"action":  map[string]any{"type": "command", "command": "exit 1"},
		"enabled": false,
		"scope":   "workspace",
	}
	resp, out = doJSON(t, ts, "PUT", "/api/v1/hooks/block-rm", update)
	if resp.StatusCode != 200 {
		t.Fatalf("update status = %d, body = %+v", resp.StatusCode, out)
	}

	// Delete.
	resp, out = doJSON(t, ts, "DELETE", "/api/v1/hooks/block-rm", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("delete status = %d, body = %+v", resp.StatusCode, out)
	}
	resp, out = doJSON(t, ts, "GET", "/api/v1/hooks", nil)
	hooks, _ = out["hooks"].([]any)
	if len(hooks) != 0 {
		t.Fatalf("expected no hooks after delete, got %+v", hooks)
	}
}

// F2: a PUT that both changes scope AND makes the entry invalid must not
// lose the hook — it must fail with 400 and the hook must still exist,
// unchanged, in its original scope's file.
func TestHandleUpdateHook_ScopeChangeWithInvalidEntry_NoDataLoss(t *testing.T) {
	_, ts, _, workspaceRoot := newTestServerWithHooks(t)

	create := map[string]any{
		"id":      "no-lose-me",
		"event":   "PreToolUse",
		"match":   map[string]any{"tools": []string{"bash"}},
		"action":  map[string]any{"type": "command", "command": "exit 1"},
		"enabled": true,
		"scope":   "workspace",
	}
	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks", create)
	if resp.StatusCode != 200 {
		t.Fatalf("create status = %d, body = %+v", resp.StatusCode, out)
	}

	// Change scope to global AND blank out the required command.
	badUpdate := map[string]any{
		"id":      "no-lose-me",
		"event":   "PreToolUse",
		"match":   map[string]any{"tools": []string{"bash"}},
		"action":  map[string]any{"type": "command", "command": ""},
		"enabled": true,
		"scope":   "global",
	}
	resp, out = doJSON(t, ts, "PUT", "/api/v1/hooks/no-lose-me", badUpdate)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid entry on scope change, got %d: %+v", resp.StatusCode, out)
	}

	// The hook must still be exactly where it started: workspace file.
	raw, err := os.ReadFile(filepath.Join(workspaceRoot, ".huginn", "hooks.json"))
	if err != nil {
		t.Fatalf("expected workspace hooks.json to still exist: %v", err)
	}
	if !bytes.Contains(raw, []byte("no-lose-me")) {
		t.Fatalf("hook vanished from its original (workspace) file after a failed scope-change update: %s", raw)
	}

	resp, out = doJSON(t, ts, "GET", "/api/v1/hooks", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list status = %d, body = %+v", resp.StatusCode, out)
	}
	hooks, _ := out["hooks"].([]any)
	if len(hooks) != 1 {
		t.Fatalf("expected the hook to still be listed exactly once, got %+v", hooks)
	}
}

// F3: when the same id exists in both scopes, runtime precedence is
// workspace-wins (mergeUserHooks: global loaded first, workspace last,
// last-id-wins). DELETE must target that active workspace copy, not the
// inert global one, or the hook keeps firing after "deletion".
func TestHandleDeleteHook_SameIDBothScopes_RemovesActiveWorkspaceCopy(t *testing.T) {
	srv, ts, huginnHome, workspaceRoot := newTestServerWithHooks(t)

	globalPath := filepath.Join(huginnHome, "hooks.json")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	// Global's copy is disabled — inert on its own — so that if the API
	// deletes the wrong (global) copy, the workspace copy is left both
	// present AND still firing (the exact bug the vet reported).
	globalJSON := `{"hooks":[
		{"id":"dup","event":"PreToolUse","match":{"tools":["bash"]},"action":{"type":"command","command":"exit 1"},"enabled":false}
	]}`
	if err := os.WriteFile(globalPath, []byte(globalJSON), 0o644); err != nil {
		t.Fatalf("write global: %v", err)
	}

	workspacePath := filepath.Join(workspaceRoot, ".huginn", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	workspaceJSON := `{"hooks":[
		{"id":"dup","event":"PreToolUse","match":{"tools":["bash"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`
	if err := os.WriteFile(workspacePath, []byte(workspaceJSON), 0o644); err != nil {
		t.Fatalf("write workspace: %v", err)
	}

	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks/reload", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("reload status = %d, body = %+v", resp.StatusCode, out)
	}

	// Sanity: the active (workspace, last-wins) copy vetoes bash.
	allow, _ := srv.orch.UserHooks().Pre(context.Background(), "bash", map[string]any{})
	if allow {
		t.Fatalf("sanity: dup hook should veto bash before delete")
	}

	resp, out = doJSON(t, ts, "DELETE", "/api/v1/hooks/dup", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("delete status = %d, body = %+v", resp.StatusCode, out)
	}

	workspaceRaw, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace hooks.json: %v", err)
	}
	if bytes.Contains(workspaceRaw, []byte(`"dup"`)) {
		t.Fatalf("delete removed the wrong copy — workspace (active) hooks.json still has \"dup\": %s", workspaceRaw)
	}
	globalRaw, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read global hooks.json: %v", err)
	}
	if !bytes.Contains(globalRaw, []byte(`"dup"`)) {
		t.Fatalf("expected the global copy to be untouched (only the active workspace copy should be removed): %s", globalRaw)
	}

	// The hook must have actually stopped firing.
	allow2, _ := srv.orch.UserHooks().Pre(context.Background(), "bash", map[string]any{})
	if !allow2 {
		t.Fatalf("hook should no longer fire after deleting the active workspace copy")
	}
}

func TestHandleCreateHook_InvalidRejected(t *testing.T) {
	_, ts, _, _ := newTestServerWithHooks(t)
	bad := map[string]any{
		"id":      "no-tools",
		"event":   "PreToolUse",
		"match":   map[string]any{"tools": []string{}},
		"action":  map[string]any{"type": "command", "command": "exit 0"},
		"enabled": true,
	}
	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks", bad)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for empty match.tools, got %d: %+v", resp.StatusCode, out)
	}
}

func TestHandleCreateHook_TakesEffectOnLiveRunner(t *testing.T) {
	srv, ts, _, _ := newTestServerWithHooks(t)
	create := map[string]any{
		"id":      "block-bash",
		"event":   "PreToolUse",
		"match":   map[string]any{"tools": []string{"bash"}},
		"action":  map[string]any{"type": "command", "command": "exit 1"},
		"enabled": true,
	}
	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks", create)
	if resp.StatusCode != 200 {
		t.Fatalf("create status = %d, body = %+v", resp.StatusCode, out)
	}
	allow, reason := srv.orch.UserHooks().Pre(context.Background(), "bash", map[string]any{})
	if allow {
		t.Fatalf("expected the newly-created hook to veto bash immediately, no restart")
	}
	if reason == "" {
		t.Fatalf("expected a deny reason")
	}
}

func TestHandleReloadHooks(t *testing.T) {
	_, ts, _, workspaceRoot := newTestServerWithHooks(t)
	// Hand-write hooks.json directly (bypassing the API) to simulate a human
	// editing the file, then hit the reload endpoint.
	dir := filepath.Join(workspaceRoot, ".huginn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{"hooks":[{"id":"h1","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 1"},"enabled":true}]}`
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks/reload", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("reload status = %d, body = %+v", resp.StatusCode, out)
	}
	resp, out = doJSON(t, ts, "GET", "/api/v1/hooks", nil)
	hooks, _ := out["hooks"].([]any)
	if len(hooks) != 1 {
		t.Fatalf("expected reload to pick up hand-edited hooks.json, got %+v", hooks)
	}
}

func TestHandleReloadHooks_MalformedFileSurfacesError(t *testing.T) {
	_, ts, _, workspaceRoot := newTestServerWithHooks(t)
	dir := filepath.Join(workspaceRoot, ".huginn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{not json`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks/reload", nil)
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422 for malformed hooks.json, got %d: %+v", resp.StatusCode, out)
	}
}

func TestHandleTestHook_Inline(t *testing.T) {
	_, ts, _, _ := newTestServerWithHooks(t)
	req := map[string]any{
		"hook": map[string]any{
			"id":      "preview",
			"event":   "PreToolUse",
			"match":   map[string]any{"tools": []string{"*"}},
			"action":  map[string]any{"type": "command", "command": "echo hi; exit 1"},
			"enabled": true,
		},
		"tool": "bash",
		"args": map[string]any{"command": "ls"},
	}
	resp, out := doJSON(t, ts, "POST", "/api/v1/hooks/test", req)
	if resp.StatusCode != 200 {
		t.Fatalf("test status = %d, body = %+v", resp.StatusCode, out)
	}
	if allowed, _ := out["allowed"].(bool); allowed {
		t.Fatalf("expected allowed=false for exit 1, got %+v", out)
	}
	if out["output"] != "hi" {
		t.Fatalf("expected hook stdout in output, got %+v", out)
	}
}

// F5: a test-run must still be recorded in the audit trail (the feature's
// trust story is "every run is audited") but tagged test_run=true so it's
// distinguishable from a real execution.
func TestHandleTestHook_RecordedWithTestRunTrue(t *testing.T) {
	_, ts, _, _ := newTestServerWithHooks(t)
	req := map[string]any{
		"hook": map[string]any{
			"id": "preview2", "event": "PreToolUse",
			"match":   map[string]any{"tools": []string{"*"}},
			"action":  map[string]any{"type": "command", "command": "exit 0"},
			"enabled": true,
		},
		"tool": "bash",
	}
	doJSON(t, ts, "POST", "/api/v1/hooks/test", req)
	resp, out := doJSON(t, ts, "GET", "/api/v1/hooks/audit", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("audit status = %d, body = %+v", resp.StatusCode, out)
	}
	entries, _ := out["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected the test-run to be recorded in the audit trail, got %+v", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if testRun, _ := entry["test_run"].(bool); !testRun {
		t.Fatalf("expected test_run=true on a /hooks/test entry, got %+v", entry)
	}
}

func TestHandleHooksAudit_RecordsRealExecutions(t *testing.T) {
	srv, ts, _, _ := newTestServerWithHooks(t)
	create := map[string]any{
		"id": "audited", "event": "PreToolUse",
		"match":   map[string]any{"tools": []string{"bash"}},
		"action":  map[string]any{"type": "command", "command": "exit 0"},
		"enabled": true,
	}
	doJSON(t, ts, "POST", "/api/v1/hooks", create)
	srv.orch.UserHooks().Pre(context.Background(), "bash", map[string]any{})

	resp, out := doJSON(t, ts, "GET", "/api/v1/hooks/audit", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("audit status = %d, body = %+v", resp.StatusCode, out)
	}
	entries, _ := out["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry for the real Pre() call, got %+v", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if testRun, _ := entry["test_run"].(bool); testRun {
		t.Fatalf("expected test_run=false on a real Pre() entry, got %+v", entry)
	}
}
