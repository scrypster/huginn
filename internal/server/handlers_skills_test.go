package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/skills"
)

func TestHandleSkillsList_Empty(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}

	req := httptest.NewRequest("GET", "/api/v1/skills", nil)
	w := httptest.NewRecorder()
	s.handleSkillsList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 0 {
		t.Errorf("expected empty list, got %d items", len(resp))
	}
}

func TestHandleSkillsList_WithSkill(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	skillContent := "---\nname: go-expert\nauthor: official\n---\n\nGo body.\n"
	os.WriteFile(filepath.Join(sdir, "go-expert.md"), []byte(skillContent), 0644)
	// installed.json
	manifest := `[{"name":"go-expert","source":"registry","enabled":true}]`
	os.WriteFile(filepath.Join(sdir, "installed.json"), []byte(manifest), 0644)

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills", nil)
	w := httptest.NewRecorder()
	s.handleSkillsList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(resp))
	}
	if resp[0]["name"] != "go-expert" {
		t.Errorf("name = %v, want go-expert", resp[0]["name"])
	}
}

func TestHandleSkillsRegistrySearch_NoCache(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/registry/search?q=test", nil)
	w := httptest.NewRecorder()
	s.handleSkillsRegistrySearch(w, req)
	// Should return 503 or empty results when no cache and no network (CI)
	// Accept either 200 with empty data or 503
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("unexpected status %d", w.Code)
	}
}

func TestHandleSkillsEnable(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	manifest := `[{"name":"go-expert","source":"registry","enabled":false}]`
	os.WriteFile(filepath.Join(sdir, "installed.json"), []byte(manifest), 0644)

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("PUT", "/api/v1/skills/go-expert/enable", nil)
	// Set path value for Go 1.22 pattern matching
	req.SetPathValue("name", "go-expert")
	w := httptest.NewRecorder()
	s.handleSkillsEnable(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestHandleSkillsDelete(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	os.WriteFile(filepath.Join(sdir, "go-expert.md"), []byte("---\nname: go-expert\n---\n\nbody\n"), 0644)
	manifest := `[{"name":"go-expert","source":"registry","enabled":true}]`
	os.WriteFile(filepath.Join(sdir, "installed.json"), []byte(manifest), 0644)

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("DELETE", "/api/v1/skills/go-expert", nil)
	req.SetPathValue("name", "go-expert")
	w := httptest.NewRecorder()
	s.handleSkillsDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if _, err := os.Stat(filepath.Join(sdir, "go-expert.md")); !os.IsNotExist(err) {
		t.Error("skill file should have been deleted")
	}
}

func TestReloadSkillsUpdatesFragment(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)

	// Write a minimal SKILL.md and manifest (deny-by-default requires it).
	skillContent := "---\nname: hot-test\n---\n\nYou are a hot-reload test skill.\n"
	os.WriteFile(filepath.Join(sdir, "hot-test.md"), []byte(skillContent), 0644)
	os.WriteFile(filepath.Join(sdir, "installed.json"),
		[]byte(`[{"name":"hot-test","source":"local","enabled":true}]`), 0644)

	orch, err := agent.NewOrchestrator(&stubBackend{}, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	srv := &Server{huginnDir: dir, orch: orch}

	srv.reloadSkills()

	// Skills now flow through the registry, NOT the fragment.
	// The fragment carries workspace rules only; skills arrive via agentSkillsFragment.
	reg := orch.SkillsRegistry()
	if reg == nil {
		t.Fatal("expected non-nil SkillsRegistry after reloadSkills")
	}
	found := false
	for _, sk := range reg.All() {
		if strings.Contains(sk.SystemPromptFragment(), "hot-reload test skill") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("registry missing hot-test skill content; skills in registry: %v", reg.All())
	}
}

// TestReloadSkills_NilOrch verifies reloadSkills returns early without panic
// when no orchestrator is set.
func TestReloadSkills_NilOrch(t *testing.T) {
	s := &Server{huginnDir: t.TempDir()}
	// nil orch — should return early without panic
	s.reloadSkills()
}

// TestReloadSkills_SetsRegistry verifies that after reloadSkills the orchestrator's
// registry is populated and the fragment carries workspace rules only (not skills).
func TestReloadSkills_SetsRegistry(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)

	skillContent := "---\nname: registry-test\n---\n\nRegistry test skill body.\n"
	os.WriteFile(filepath.Join(sdir, "registry-test.md"), []byte(skillContent), 0644)
	os.WriteFile(filepath.Join(sdir, "installed.json"),
		[]byte(`[{"name":"registry-test","source":"local","enabled":true}]`), 0644)

	orch, err := agent.NewOrchestrator(&stubBackend{}, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	srv := &Server{huginnDir: dir, orch: orch}

	srv.reloadSkills()

	// Registry must be populated.
	reg := orch.SkillsRegistry()
	if reg == nil {
		t.Fatal("SkillsRegistry is nil after reloadSkills")
	}

	// Skills must be in the registry, not in the fragment.
	found := false
	for _, sk := range reg.All() {
		if strings.Contains(sk.SystemPromptFragment(), "Registry test skill body") {
			found = true
			break
		}
	}
	if !found {
		t.Error("skill not found in registry after reloadSkills")
	}

	// Fragment must NOT contain skill content (workspace rules only).
	fragment := orch.SkillsFragment()
	if strings.Contains(fragment, "Registry test skill body") {
		t.Errorf("SkillsFragment should not contain skill content; got:\n%s", fragment)
	}
}

func TestHandleSkillsList_HasToolCountField(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	content := "---\nname: test-skill\n---\n\nTest prompt.\n"
	os.WriteFile(filepath.Join(sdir, "test-skill.md"), []byte(content), 0644)

	srv := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills", nil)
	w := httptest.NewRecorder()
	srv.handleSkillsList(w, req)

	var result []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected at least one skill")
	}
	if _, ok := result[0]["tool_count"]; !ok {
		t.Errorf("response missing tool_count field; got keys: %v", result[0])
	}
}

// TestInstallSkillReloadsOrchestrator verifies that handleSkillsInstall calls
// reloadSkills() so the orchestrator's skills fragment is updated immediately.
func TestInstallSkillReloadsOrchestrator(t *testing.T) {
	const skillPromptText = "You are an install-reload integration test skill."
	const skillContent = "---\nname: install-reload-test\nauthor: test\n---\n\n" + skillPromptText + "\n"

	// Mock registry HTTP server that serves the SKILL.md bytes.
	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(skillContent))
	}))
	defer registrySrv.Close()

	// Prepare huginnDir with skills dir and cache.
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	// Write skills-index.json cache so LoadIndex finds the skill without hitting network.
	// SourceURL must be a full URL pointing to our mock server.
	indexEntry := skills.IndexEntry{
		Name:      "install-reload-test",
		SourceURL: registrySrv.URL + "/skills/install-reload-test/SKILL.md",
	}
	cachedIndex := map[string]any{
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"entries":    []skills.IndexEntry{indexEntry},
	}
	cacheBytes, _ := json.Marshal(cachedIndex)
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), cacheBytes, 0644)

	// Create a real Orchestrator and Server.
	orch, err := agent.NewOrchestrator(&stubBackend{}, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	srv := &Server{
		huginnDir:     dir,
		orch:          orch,
		skillsBaseURL: registrySrv.URL,
	}

	// Call handleSkillsInstall via httptest.
	body, _ := json.Marshal(map[string]string{"target": "install-reload-test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSkillsInstall(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("handleSkillsInstall: status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	// Skills now flow through the registry, NOT the fragment (workspace rules only).
	// Verify the orchestrator's registry contains the installed skill's prompt text.
	reg := orch.SkillsRegistry()
	if reg == nil {
		t.Fatal("expected non-nil SkillsRegistry after install")
	}
	found := false
	for _, sk := range reg.All() {
		if strings.Contains(sk.SystemPromptFragment(), skillPromptText) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SkillsRegistry missing installed skill prompt text;\ngot skills: %v", reg.All())
	}
}
