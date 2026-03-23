package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// minimalSkillMD returns a minimal valid SKILL.md with the given name.
func minimalSkillMD(name string) string {
	return "---\nname: " + name + "\nauthor: test\n---\n\nTest skill body for " + name + ".\n"
}

// writeSkill writes a skill file and (optionally) an installed.json manifest
// into the server's skills directory.
func writeSkillFile(t *testing.T, sdir, name string) {
	t.Helper()
	if err := os.MkdirAll(sdir, 0755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	path := filepath.Join(sdir, name+".md")
	if err := os.WriteFile(path, []byte(minimalSkillMD(name)), 0644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	// Write a minimal installed.json so the manifest exists for the handler.
	manifest := `[{"name":"` + name + `","source":"local","enabled":true}]`
	if err := os.WriteFile(filepath.Join(sdir, "installed.json"), []byte(manifest), 0644); err != nil {
		t.Fatalf("write installed.json: %v", err)
	}
}

// doSkillPUT issues PUT /api/v1/skills/{name} using httptest directly (no
// HTTP server needed — the handler is called directly so path values must be
// set manually).
func doSkillPUTDirect(s *Server, urlName, content string) *httptest.ResponseRecorder {
	body := `{"content":` + mustJSON(content) + `}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/skills/"+urlName, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", urlName)
	w := httptest.NewRecorder()
	s.handleSkillsUpdate(w, req)
	return w
}

// mustJSON marshals v to JSON string; panics on error (test helper only).
func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// 200: same-name overwrite (content + manifest updated)
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_SameNameOverwrite(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeSkillFile(t, sdir, "my-skill")

	s := &Server{huginnDir: dir}

	updatedContent := minimalSkillMD("my-skill")
	updatedContent += "\nExtra line added.\n"

	w := doSkillPUTDirect(s, "my-skill", updatedContent)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "my-skill" {
		t.Errorf("name = %q, want my-skill", resp["name"])
	}

	// Verify file was updated on disk.
	raw, err := os.ReadFile(filepath.Join(sdir, "my-skill.md"))
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}
	if !strings.Contains(string(raw), "Extra line added") {
		t.Errorf("updated file missing new content; got:\n%s", string(raw))
	}

	// Verify Content-Type header.
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ---------------------------------------------------------------------------
// 200: rename (old name gone, new name exists, manifest consistent)
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_Rename(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeSkillFile(t, sdir, "old-skill")

	s := &Server{huginnDir: dir}

	// PUT with URL name=old-skill but markdown name=new-skill (rename).
	newContent := minimalSkillMD("new-skill")
	w := doSkillPUTDirect(s, "old-skill", newContent)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "new-skill" {
		t.Errorf("name = %q, want new-skill", resp["name"])
	}

	// Old file must be gone.
	if _, err := os.Stat(filepath.Join(sdir, "old-skill.md")); !os.IsNotExist(err) {
		t.Error("old skill file should have been removed after rename")
	}

	// New file must exist.
	if _, err := os.Stat(filepath.Join(sdir, "new-skill.md")); err != nil {
		t.Errorf("new skill file not found: %v", err)
	}

	// Manifest must reflect the rename: new name present, old name absent.
	raw, err := os.ReadFile(filepath.Join(sdir, "installed.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestStr := string(raw)
	if strings.Contains(manifestStr, "old-skill") {
		t.Errorf("manifest still contains old-skill after rename:\n%s", manifestStr)
	}
	if !strings.Contains(manifestStr, "new-skill") {
		t.Errorf("manifest missing new-skill after rename:\n%s", manifestStr)
	}
}

// ---------------------------------------------------------------------------
// 400: missing skill name param (empty path value)
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_MissingSkillName(t *testing.T) {
	dir := t.TempDir()
	s := &Server{huginnDir: dir}

	body := `{"content":"` + minimalSkillMD("some-skill") + `"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/skills/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Intentionally do not set path value — simulates missing param.
	req.SetPathValue("name", "")
	w := httptest.NewRecorder()
	s.handleSkillsUpdate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 400: empty body / whitespace-only content
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeSkillFile(t, sdir, "my-skill")

	s := &Server{huginnDir: dir}

	body := `{"content":"   "}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/skills/my-skill", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "my-skill")
	w := httptest.NewRecorder()
	s.handleSkillsUpdate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty content, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 400: invalid JSON body
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeSkillFile(t, sdir, "my-skill")

	s := &Server{huginnDir: dir}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/skills/my-skill", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "my-skill")
	w := httptest.NewRecorder()
	s.handleSkillsUpdate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid JSON, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 404: skill not found (source skill does not exist)
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_NotFound(t *testing.T) {
	dir := t.TempDir()
	// Skills dir exists but "ghost-skill.md" does not.
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)

	s := &Server{huginnDir: dir}

	w := doSkillPUTDirect(s, "ghost-skill", minimalSkillMD("ghost-skill"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 415: wrong Content-Type (handler does not check it, but wrong body format
// should result in 400 from JSON decode failure)
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_WrongContentType_NonJSONBody(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeSkillFile(t, sdir, "my-skill")

	s := &Server{huginnDir: dir}

	// Send plain text body without JSON wrapping — this will fail JSON decode.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/skills/my-skill",
		strings.NewReader("just plain text, not json"))
	req.Header.Set("Content-Type", "text/plain")
	req.SetPathValue("name", "my-skill")
	w := httptest.NewRecorder()
	s.handleSkillsUpdate(w, req)

	// The handler decodes JSON; a non-JSON body must produce 400.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for non-JSON body, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Concurrent update safety: two goroutines updating same skill simultaneously
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_ConcurrentSafety(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeSkillFile(t, sdir, "concurrent-skill")

	s := &Server{huginnDir: dir}

	const goroutines = 8
	var wg sync.WaitGroup
	errors := make(chan int, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			content := minimalSkillMD("concurrent-skill")
			w := doSkillPUTDirect(s, "concurrent-skill", content)
			if w.Code != http.StatusOK {
				errors <- w.Code
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for code := range errors {
		t.Errorf("concurrent update returned unexpected status %d (want 200)", code)
	}

	// After all concurrent writes, the skill file must still exist and be valid.
	if _, err := os.Stat(filepath.Join(sdir, "concurrent-skill.md")); err != nil {
		t.Errorf("skill file missing after concurrent updates: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Large payload handling
// ---------------------------------------------------------------------------

func TestHandleSkillsUpdate_LargePayload(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	writeSkillFile(t, sdir, "big-skill")

	s := &Server{huginnDir: dir}

	// Build a large content block (well within OS limits, ~200 KB).
	const repeat = 2000
	filler := strings.Repeat("This is a filler line of approximately 100 bytes for the large payload test.\n", repeat)
	largeContent := "---\nname: big-skill\nauthor: test\n---\n\n" + filler

	w := doSkillPUTDirect(s, "big-skill", largeContent)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for large payload, got %d; body: %s", w.Code, w.Body.String())
	}

	// Confirm the file is the correct size.
	info, err := os.Stat(filepath.Join(sdir, "big-skill.md"))
	if err != nil {
		t.Fatalf("stat large skill file: %v", err)
	}
	if info.Size() == 0 {
		t.Error("large skill file is empty after write")
	}
}
