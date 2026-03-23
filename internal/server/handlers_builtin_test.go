package server

// handlers_builtin_test.go — Coverage for builtin model handler endpoints (Iteration 2)
// Covers behavior not already exercised in builtin_and_tools_test.go:
//   - handleBuiltinCatalog: marks installed models correctly
//   - handleBuiltinListModels: returns entries when models are installed
//   - handleBuiltinStatus: 503 when runtimeMgr nil (re-confirmed), proper fields when configured
//   - handleBuiltinActivate: rejects unknown model with modelStore, accepts installed model
//   - handleBuiltinPullModel: 404 for unknown model name (when modelStore is set)

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/models"
)

// ─── handleBuiltinCatalog — with installed model store ────────────────────────

// TestHandleBuiltinCatalog_MarksInstalledModels verifies that catalog entries
// for models recorded in the model store have Installed=true, while others are false.
func TestHandleBuiltinCatalog_MarksInstalledModels(t *testing.T) {
	srv, _ := newTestServer(t)

	// Load the real catalog to get a valid model name.
	catalog, err := models.LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if len(catalog) == 0 {
		t.Skip("no curated catalog entries to test with")
	}

	// Pick the first entry name.
	var firstModelName string
	for name := range catalog {
		firstModelName = name
		break
	}

	// Create a store and record that model as installed.
	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	err = store.Record(firstModelName, models.LockEntry{
		Name:        firstModelName,
		Filename:    "test.gguf",
		Path:        "/tmp/test.gguf",
		InstalledAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	srv.modelStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/catalog", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinCatalog(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var entries []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one catalog entry")
	}

	// Find our installed model in the response.
	found := false
	for _, e := range entries {
		name, _ := e["name"].(string)
		if name == firstModelName {
			installed, _ := e["installed"].(bool)
			if !installed {
				t.Errorf("expected model %q to be marked installed=true", firstModelName)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("installed model %q not found in catalog response", firstModelName)
	}
}

// TestHandleBuiltinCatalog_UninstalledModelsMarkedFalse verifies catalog entries
// for uninstalled models have Installed=false.
func TestHandleBuiltinCatalog_UninstalledModelsMarkedFalse(t *testing.T) {
	srv, _ := newTestServer(t)

	// Empty store — nothing installed.
	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srv.modelStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/catalog", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinCatalog(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var entries []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, e := range entries {
		if installed, _ := e["installed"].(bool); installed {
			name, _ := e["name"].(string)
			t.Errorf("expected model %q to be installed=false with empty store", name)
		}
	}
}

// ─── handleBuiltinListModels — with installed entries ─────────────────────────

// TestHandleBuiltinListModels_ReturnsInstalledEntries verifies that when models
// are recorded in the store, they appear in the list response.
func TestHandleBuiltinListModels_ReturnsInstalledEntries(t *testing.T) {
	srv, _ := newTestServer(t)

	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	now := time.Now()
	if err := store.Record("my-model", models.LockEntry{
		Name:        "my-model",
		Filename:    "my-model.gguf",
		Path:        "/tmp/my-model.gguf",
		SizeBytes:   500000000,
		InstalledAt: now,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	srv.modelStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/models", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinListModels(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
	if result[0]["name"] != "my-model" {
		t.Errorf("expected name=my-model, got %v", result[0]["name"])
	}
	if result[0]["filename"] != "my-model.gguf" {
		t.Errorf("expected filename=my-model.gguf, got %v", result[0]["filename"])
	}
}

// TestHandleBuiltinListModels_EmptyStoreReturnsEmptyArray verifies that an
// existing but empty model store returns an empty JSON array (not null).
func TestHandleBuiltinListModels_EmptyStoreReturnsEmptyArray(t *testing.T) {
	srv, _ := newTestServer(t)

	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srv.modelStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/models", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinListModels(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %d entries", len(result))
	}
}

// ─── handleBuiltinStatus — nil runtimeMgr returns 503 ────────────────────────

// TestHandleBuiltinStatus_NilRuntimeMgr_Returns503 is a focused confirmation
// that the nil-runtimeMgr guard returns 503 with an error body.
func TestHandleBuiltinStatus_NilRuntimeMgr_Returns503(t *testing.T) {
	srv, _ := newTestServer(t)
	// runtimeMgr is nil by default from newTestServer.

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builtin/status", nil)
	w := httptest.NewRecorder()
	srv.handleBuiltinStatus(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected non-empty error message in 503 response")
	}
}

// ─── handleBuiltinPullModel — 404 for unknown model name ─────────────────────

// TestHandleBuiltinPullModel_UnknownModelName verifies that requesting a pull
// for a model name not in the catalog returns 404.
func TestHandleBuiltinPullModel_UnknownModelName(t *testing.T) {
	srv, _ := newTestServer(t)

	// Wire a valid model store so we pass the nil-store check.
	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srv.modelStore = store

	// Use a name that definitely won't be in the curated catalog.
	body := `{"name":"__nonexistent-model-xyz-12345__"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/models/pull", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinPullModel(w, req)

	if w.Code != 404 {
		t.Fatalf("expected 404 for unknown model, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error message in 404 response")
	}
}

// ─── handleBuiltinActivate — with installed model in store ────────────────────

// TestHandleBuiltinActivate_InstalledModel_Succeeds verifies that activating a
// model that is recorded in the store returns 200 with activated=true.
func TestHandleBuiltinActivate_InstalledModel_Succeeds(t *testing.T) {
	srv, _ := newTestServer(t)

	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Record("installed-model", models.LockEntry{
		Name:        "installed-model",
		Filename:    "installed-model.gguf",
		InstalledAt: time.Now(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	srv.modelStore = store

	body := `{"model":"installed-model"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/activate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinActivate(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["activated"] != true {
		t.Errorf("expected activated=true, got %v", result["activated"])
	}
	if result["model"] != "installed-model" {
		t.Errorf("expected model=installed-model, got %v", result["model"])
	}
	if result["requires_restart"] != true {
		t.Errorf("expected requires_restart=true, got %v", result["requires_restart"])
	}
}

// TestHandleBuiltinActivate_UnknownModel_Returns400 verifies that attempting to
// activate a model not in the store returns 400 with an error.
func TestHandleBuiltinActivate_UnknownModel_Returns400(t *testing.T) {
	srv, _ := newTestServer(t)

	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srv.modelStore = store

	body := `{"model":"not-installed-anywhere"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/activate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinActivate(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected non-empty error in 400 response")
	}
}
