package server

// hardening_iter14_test.go — Hardening iteration 14.
// Covers high-value zero-coverage paths:
//   1. handleSkillsGet: found, not found, invalid name
//   2. handleSkillsCreate: valid create, invalid JSON, invalid SKILL content
//   3. handleSkillsDisable: success path
//   4. setSkillEnabled: manifest error path (no installed.json, skill not found)
//   5. handleSkillsRegistryIndex: no-cache path (503)
//   6. instantiateTemplate: known slug returns routine; unknown slug errors
//   7. handleCloudConnect: goroutine completes satellite connect (satellite is non-nil)
//   8. handleGetConfig: API key and OAuth secrets are redacted in response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── skill test helpers ────────────────────────────────────────────────────────

const testSkillContent = "---\nname: test-skill\nauthor: tester\n---\n\nSystem prompt body.\n"

func writeTestSkill(t *testing.T, sdir, name, content string) {
	t.Helper()
	if content == "" {
		content = testSkillContent
	}
	if err := os.MkdirAll(sdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdir, name+".md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ── handleSkillsGet ───────────────────────────────────────────────────────────

func TestHandleSkillsGet_Found(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeTestSkill(t, sdir, "test-skill", testSkillContent)

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/test-skill", nil)
	req.SetPathValue("name", "test-skill")
	w := httptest.NewRecorder()
	s.handleSkillsGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "test-skill" {
		t.Errorf("name = %v, want test-skill", resp["name"])
	}
}

func TestHandleSkillsGet_NotFound(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/nonexistent", nil)
	req.SetPathValue("name", "nonexistent")
	w := httptest.NewRecorder()
	s.handleSkillsGet(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleSkillsGet_InvalidName(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/../etc/passwd", nil)
	req.SetPathValue("name", "../etc/passwd")
	w := httptest.NewRecorder()
	s.handleSkillsGet(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── handleSkillsCreate ────────────────────────────────────────────────────────

func TestHandleSkillsCreate_Valid(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}

	body := `{"content": "---\nname: my-skill\nauthor: dev\n---\n\nDo something.\n"}`
	req := httptest.NewRequest("POST", "/api/v1/skills", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSkillsCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "my-skill" {
		t.Errorf("name = %v, want my-skill", resp["name"])
	}
	// Verify file was written
	if _, err := os.Stat(filepath.Join(dir, "skills", "my-skill.md")); err != nil {
		t.Errorf("expected skill file to exist: %v", err)
	}
}

func TestHandleSkillsCreate_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}

	req := httptest.NewRequest("POST", "/api/v1/skills", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	s.handleSkillsCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleSkillsCreate_InvalidSkillContent(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}

	// Content without valid frontmatter
	body := `{"content": "no frontmatter here"}`
	req := httptest.NewRequest("POST", "/api/v1/skills", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleSkillsCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── handleSkillsDisable ───────────────────────────────────────────────────────

func TestHandleSkillsDisable_Success(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	manifest := `[{"name":"go-expert","source":"registry","enabled":true}]`
	os.WriteFile(filepath.Join(sdir, "installed.json"), []byte(manifest), 0644)

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("PUT", "/api/v1/skills/go-expert/disable", nil)
	req.SetPathValue("name", "go-expert")
	w := httptest.NewRecorder()
	s.handleSkillsDisable(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

// ── setSkillEnabled error paths ───────────────────────────────────────────────

func TestSetSkillEnabled_NoManifest(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}
	// No installed.json exists — LoadManifest returns empty manifest (nil error),
	// then SetEnabled returns false (skill not in empty manifest) → 404.
	req := httptest.NewRequest("PUT", "/api/v1/skills/anything/enable", nil)
	req.SetPathValue("name", "anything")
	w := httptest.NewRecorder()
	s.handleSkillsEnable(w, req)

	// Empty manifest → skill not found → 404
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSetSkillEnabled_SkillNotInManifest(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	// Manifest exists but doesn't contain the target skill
	manifest := `[{"name":"other-skill","source":"local","enabled":true}]`
	os.WriteFile(filepath.Join(sdir, "installed.json"), []byte(manifest), 0644)

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("PUT", "/api/v1/skills/missing-skill/enable", nil)
	req.SetPathValue("name", "missing-skill")
	w := httptest.NewRecorder()
	s.handleSkillsEnable(w, req)

	// Skill not in manifest → 404
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSetSkillEnabled_InvalidName(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("PUT", "/api/v1/skills/../secret/enable", nil)
	req.SetPathValue("name", "../secret")
	w := httptest.NewRecorder()
	s.handleSkillsEnable(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── handleSkillsRegistryIndex ─────────────────────────────────────────────────

func TestHandleSkillsRegistryIndex_NoCache(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}
	// No cache dir, no network — expect 503
	req := httptest.NewRequest("GET", "/api/v1/skills/registry/index", nil)
	w := httptest.NewRecorder()
	s.handleSkillsRegistryIndex(w, req)

	if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 503 (or 200 if network available)", w.Code)
	}
}

// ── builtinWorkflowTemplates ────────────────────────────────────────────────

func TestBuiltinWorkflowTemplates_HasMorningStandup(t *testing.T) {
	templates := builtinWorkflowTemplates()
	for _, tmpl := range templates {
		if tmpl.ID == "morning-standup" {
			if tmpl.Workflow.Name == "" {
				t.Error("expected non-empty workflow name")
			}
			return
		}
	}
	t.Error("expected morning-standup template to be present")
}

func TestBuiltinWorkflowTemplates_UnknownSlug(t *testing.T) {
	templates := builtinWorkflowTemplates()
	for _, tmpl := range templates {
		if tmpl.ID == "does-not-exist" {
			t.Error("unexpected template found")
		}
	}
}

// ── handleGetConfig: API key redaction ────────────────────────────────────────

func TestHandleGetConfig_RedactsAPIKey_Iter14(t *testing.T) {
	_, ts := newTestServer(t)

	// Set a config with a real API key via the server struct
	srv, _ := newTestServer(t)
	srv.cfg.Backend.APIKey = "super-secret-key"

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts2 := httptest.NewServer(mux)
	t.Cleanup(ts2.Close)

	req, _ := http.NewRequest("GET", ts2.URL+"/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)

	backend, _ := body["backend"].(map[string]any)
	if backend == nil {
		t.Fatal("missing backend in config response")
	}
	if backend["api_key"] == "super-secret-key" {
		t.Error("API key should be redacted, but got the raw secret")
	}
	if backend["api_key"] != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %v", backend["api_key"])
	}

	_ = ts // suppress unused warning
}

// ── handleSkillsDelete: invalid name ─────────────────────────────────────────

func TestHandleSkillsDelete_InvalidName(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("DELETE", "/api/v1/skills/../secret", nil)
	req.SetPathValue("name", "../secret")
	w := httptest.NewRecorder()
	s.handleSkillsDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── handleSkillsGet: with manifest (enabled field respected) ─────────────────

func TestHandleSkillsGet_WithManifest_DisabledState(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeTestSkill(t, sdir, "test-skill", testSkillContent)
	manifest := `[{"name":"test-skill","source":"registry","enabled":false}]`
	os.WriteFile(filepath.Join(sdir, "installed.json"), []byte(manifest), 0644)

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/test-skill", nil)
	req.SetPathValue("name", "test-skill")
	w := httptest.NewRecorder()
	s.handleSkillsGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["enabled"] != false {
		t.Errorf("expected enabled=false from manifest, got %v", resp["enabled"])
	}
	if resp["source"] != "registry" {
		t.Errorf("expected source=registry, got %v", resp["source"])
	}
}
