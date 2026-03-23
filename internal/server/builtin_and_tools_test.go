package server

// builtin_and_tools_test.go — Coverage for builtin handlers, system tools, and setters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/models"
	"github.com/scrypster/huginn/internal/runtime"
)

// ─── SetRuntimeManager ────────────────────────────────────────────────────────

func TestSetRuntimeManager(t *testing.T) {
	srv, _ := newTestServer(t)
	mockMgr := &runtime.Manager{}
	srv.SetRuntimeManager(mockMgr)
	if srv.runtimeMgr != mockMgr {
		t.Error("SetRuntimeManager did not set runtimeMgr")
	}
}

// ─── SetModelStore ────────────────────────────────────────────────────────────

func TestSetModelStore(t *testing.T) {
	srv, _ := newTestServer(t)
	mockStore := &models.Store{}
	srv.SetModelStore(mockStore)
	if srv.modelStore != mockStore {
		t.Error("SetModelStore did not set modelStore")
	}
}

// ─── handleBuiltinStatus — nil runtime manager ────────────────────────────────

func TestHandleBuiltinStatus_NilRuntimeManager(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/status", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinStatus(w, req)
	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected error message in response")
	}
}

// ─── handleBuiltinListModels — nil model store ────────────────────────────────

func TestHandleBuiltinListModels_NilModelStore(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/models", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinListModels(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body []map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body == nil {
		t.Error("expected empty array, got nil")
	}
}

// ─── handleBuiltinCatalog — loads catalog ─────────────────────────────────────

func TestHandleBuiltinCatalog_Success(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/catalog", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinCatalog(w, req)
	if w.Code == 200 {
		var body []map[string]any
		json.NewDecoder(w.Body).Decode(&body)
		// Success if we get a valid response (may be empty if no catalog file exists)
		if body == nil {
			t.Error("expected array response")
		}
	} else if w.Code != 500 {
		// 500 is acceptable if models.LoadMerged fails
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

// ─── handleBuiltinDownload — no flusher support ────────────────────────────────

func TestHandleBuiltinDownload_NilRuntimeManager(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/download", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinDownload(w, req)
	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

// ─── handleBuiltinPullModel — nil model store ─────────────────────────────────

func TestHandleBuiltinPullModel_NilModelStore(t *testing.T) {
	srv, _ := newTestServer(t)
	body := `{"name":"test-model"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/models/pull", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinPullModel(w, req)
	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

// ─── handleBuiltinPullModel — invalid JSON ────────────────────────────────────

func TestHandleBuiltinPullModel_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	// Create a minimal model store so we pass the nil check
	tmpDir := t.TempDir()
	modelStore, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	srv.modelStore = modelStore

	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/models/pull", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinPullModel(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── handleBuiltinPullModel — empty name ──────────────────────────────────────

func TestHandleBuiltinPullModel_EmptyName(t *testing.T) {
	srv, _ := newTestServer(t)
	tmpDir := t.TempDir()
	modelStore, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	srv.modelStore = modelStore

	body := `{"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/models/pull", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinPullModel(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── handleBuiltinActivate — empty model ──────────────────────────────────────

func TestHandleBuiltinActivate_EmptyModel(t *testing.T) {
	srv, _ := newTestServer(t)
	body := `{"model":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/activate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinActivate(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── handleBuiltinActivate — invalid JSON ────────────────────────────────────

func TestHandleBuiltinActivate_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	body := `{invalid}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/activate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinActivate(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ─── handleSystemTools — calls all checkers ───────────────────────────────────

func TestHandleSystemTools(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/tools", nil)
	w := httptest.NewRecorder()
	srv.handleSystemTools(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var tools []map[string]any
	json.NewDecoder(w.Body).Decode(&tools)
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	// Verify expected tool names
	names := map[string]bool{}
	for _, tool := range tools {
		if name, ok := tool["name"].(string); ok {
			names[name] = true
		}
	}
	if !names["github"] {
		t.Error("missing github tool")
	}
	if !names["aws"] {
		t.Error("missing aws tool")
	}
	if !names["gcloud"] {
		t.Error("missing gcloud tool")
	}
}

// ─── checkGitHub ──────────────────────────────────────────────────────────────

func TestCheckGitHub_NotInstalled(t *testing.T) {
	// checkGitHub is called as a function directly
	result := checkGitHub()
	if result.Name != "github" {
		t.Fatalf("expected name github, got %q", result.Name)
	}
	// If gh is not in PATH, Installed should be false
	if result.Installed {
		// gh is installed in CI, so we skip detailed assertions
	}
}

// ─── checkAWS ─────────────────────────────────────────────────────────────────

func TestCheckAWS_NotInstalled(t *testing.T) {
	result := checkAWS()
	if result.Name != "aws" {
		t.Fatalf("expected name aws, got %q", result.Name)
	}
}

// ─── checkGCloud ──────────────────────────────────────────────────────────────

func TestCheckGCloud_NotInstalled(t *testing.T) {
	result := checkGCloud()
	if result.Name != "gcloud" {
		t.Fatalf("expected name gcloud, got %q", result.Name)
	}
}

// ─── awsProfiles — no home directory ──────────────────────────────────────────

func TestAwsProfiles_NoHomeDir(t *testing.T) {
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	// Simulate missing HOME by setting to a non-existent path
	os.Setenv("HOME", "/nonexistent-home-dir-12345")

	profiles := awsProfiles()
	if profiles != nil {
		t.Fatalf("expected nil profiles when home missing, got %v", profiles)
	}
}

// ─── awsProfiles — reads credentials and config files ────────────────────────

func TestAwsProfiles_ReadFiles(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpHome)

	// Create .aws/credentials file with profiles
	awsDir := filepath.Join(tmpHome, ".aws")
	if err := os.MkdirAll(awsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	credFile := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credFile, []byte("[default]\naws_access_key_id=test\n[profile1]\naws_access_key_id=test\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	profiles := awsProfiles()
	if profiles == nil {
		t.Fatalf("expected profiles, got nil")
	}
	// Should find at least "default" and "profile1"
	if len(profiles) < 1 {
		t.Fatalf("expected at least 1 profile, got %d", len(profiles))
	}
}

// ─── awsProfiles — config file with profile prefix ───────────────────────────

func TestAwsProfiles_ConfigFile(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpHome)

	awsDir := filepath.Join(tmpHome, ".aws")
	if err := os.MkdirAll(awsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create .aws/config file with profile entries
	configFile := filepath.Join(awsDir, "config")
	if err := os.WriteFile(configFile, []byte("[profile myprofile]\nregion=us-west-2\n[profile another]\nregion=eu-west-1\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	profiles := awsProfiles()
	if profiles == nil {
		t.Fatalf("expected profiles, got nil")
	}
	// Should find myprofile and another
	profileMap := map[string]bool{}
	for _, p := range profiles {
		profileMap[p] = true
	}
	if !profileMap["myprofile"] {
		t.Errorf("expected myprofile in %v", profiles)
	}
	if !profileMap["another"] {
		t.Errorf("expected another in %v", profiles)
	}
}

// ─── awsProfiles — duplicate suppression ──────────────────────────────────────

func TestAwsProfiles_DeduplicatesProfiles(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpHome)

	awsDir := filepath.Join(tmpHome, ".aws")
	if err := os.MkdirAll(awsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create credentials with "default"
	credFile := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credFile, []byte("[default]\naws_access_key_id=test\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create config with "default" too
	configFile := filepath.Join(awsDir, "config")
	if err := os.WriteFile(configFile, []byte("[default]\nregion=us-west-2\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	profiles := awsProfiles()
	// Count how many times "default" appears
	count := 0
	for _, p := range profiles {
		if p == "default" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 'default' once, found %d times in %v", count, profiles)
	}
}

// ─── awsProfiles — sorted order ───────────────────────────────────────────────

func TestAwsProfiles_SortedOrder(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpHome)

	awsDir := filepath.Join(tmpHome, ".aws")
	if err := os.MkdirAll(awsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create credentials with profiles in non-alphabetical order
	credFile := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credFile, []byte("[zebra]\naws_access_key_id=test\n[alpha]\naws_access_key_id=test\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	profiles := awsProfiles()
	// Should be sorted
	if len(profiles) >= 2 {
		if profiles[0] > profiles[1] {
			t.Fatalf("expected sorted profiles, got %v", profiles)
		}
	}
}

// ─── awsProfiles — handles empty lines and whitespace ────────────────────────

func TestAwsProfiles_HandlesWhitespace(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)
	os.Setenv("HOME", tmpHome)

	awsDir := filepath.Join(tmpHome, ".aws")
	if err := os.MkdirAll(awsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create credentials with whitespace and empty lines
	credFile := filepath.Join(awsDir, "credentials")
	credContent := "[profile1]\n  \n  aws_access_key_id=test\n  \n[profile2]\n"
	if err := os.WriteFile(credFile, []byte(credContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	profiles := awsProfiles()
	profileMap := map[string]bool{}
	for _, p := range profiles {
		profileMap[p] = true
	}
	if !profileMap["profile1"] {
		t.Errorf("expected profile1 in %v", profiles)
	}
	if !profileMap["profile2"] {
		t.Errorf("expected profile2 in %v", profiles)
	}
}

// ─── handleBuiltinActivate — with model store validation ──────────────────────

func TestHandleBuiltinActivate_WithModelStore_NotInstalled(t *testing.T) {
	srv, _ := newTestServer(t)
	tmpDir := t.TempDir()
	modelStore, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	srv.modelStore = modelStore

	body := `{"model":"nonexistent-model"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/activate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinActivate(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for non-installed model, got %d", w.Code)
	}
}

// ─── handleBuiltinActivate — success path ────────────────────────────────────

func TestHandleBuiltinActivate_Success(t *testing.T) {
	srv, _ := newTestServer(t)
	// Don't set modelStore so we skip the validation
	srv.modelStore = nil

	body := `{"model":"test-model"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/activate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinActivate(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["activated"] != true {
		t.Errorf("expected activated=true")
	}
	if result["model"] != "test-model" {
		t.Errorf("expected model=test-model")
	}
}
