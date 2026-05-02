package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/skills"
)

// TestHandleSkillsInstall_RejectsPathTraversalName verifies that handleSkillsInstall
// validates the skill name from the registry SKILL.md and rejects path-traversal
// attempts like "../../evil" with a 502 BadGateway error.
func TestHandleSkillsInstall_RejectsPathTraversalName(t *testing.T) {
	// Crafted SKILL.md with a malicious name that attempts path traversal
	evilSkillContent := "---\nname: ../../evil\nauthor: attacker\n---\n\nMalicious skill.\n"

	// Mock registry HTTP server that serves the crafted SKILL.md
	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(evilSkillContent))
	}))
	defer registrySrv.Close()

	// Prepare huginnDir with skills dir and cache
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	// Write skills-index.json cache so LoadIndex finds the skill without hitting network
	indexEntry := skills.IndexEntry{
		Name:      "evil-skill",
		SourceURL: registrySrv.URL + "/skills/evil/SKILL.md",
	}
	cachedIndex := map[string]any{
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"entries":    []skills.IndexEntry{indexEntry},
	}
	cacheBytes, _ := json.Marshal(cachedIndex)
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), cacheBytes, 0644)

	// Create Server (no orchestrator needed for this test)
	srv := &Server{
		huginnDir: dir,
	}

	// Call handleSkillsInstall via httptest
	body, _ := json.Marshal(map[string]string{"target": "evil-skill"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSkillsInstall(w, req)

	// Should reject with 502 BadGateway (not 201 Created)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("handleSkillsInstall: status = %d, want 502; body: %s", w.Code, w.Body.String())
	}

	// Verify the malicious file was NOT written to disk
	evilPath := filepath.Join(sdir, "..\\evil.md")
	evilPath2 := filepath.Join(sdir, "../../evil.md")
	if _, err := os.Stat(evilPath); !os.IsNotExist(err) {
		t.Errorf("malicious file should not exist at %s", evilPath)
	}
	if _, err := os.Stat(evilPath2); !os.IsNotExist(err) {
		t.Errorf("malicious file should not exist at %s", evilPath2)
	}

	// Ensure no .md file was written to the skills dir at all
	entries, _ := os.ReadDir(sdir)
	for _, entry := range entries {
		if entry.IsDir() || !bytes.HasSuffix([]byte(entry.Name()), []byte(".md")) {
			continue
		}
		// Should be no .md files (installed.json doesn't count)
		t.Errorf("unexpected .md file in skills dir: %s", entry.Name())
	}
}

// TestHandleSkillsInstall_AcceptsValidName verifies that a valid skill name
// from the registry is accepted and written to disk (sanity check that the
// validation doesn't block legitimate skills).
func TestHandleSkillsInstall_AcceptsValidName(t *testing.T) {
	const skillContent = "---\nname: valid-skill\nauthor: official\n---\n\nA valid skill.\n"

	// Mock registry HTTP server
	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(skillContent))
	}))
	defer registrySrv.Close()

	// Prepare huginnDir
	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	// Write skills-index.json cache
	indexEntry := skills.IndexEntry{
		Name:      "valid-skill",
		SourceURL: registrySrv.URL + "/skills/valid-skill/SKILL.md",
	}
	cachedIndex := map[string]any{
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"entries":    []skills.IndexEntry{indexEntry},
	}
	cacheBytes, _ := json.Marshal(cachedIndex)
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), cacheBytes, 0644)

	// Create Server
	srv := &Server{
		huginnDir: dir,
	}

	// Call handleSkillsInstall
	body, _ := json.Marshal(map[string]string{"target": "valid-skill"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSkillsInstall(w, req)

	// Should succeed with 201 Created
	if w.Code != http.StatusCreated {
		t.Fatalf("handleSkillsInstall: status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	// Verify file was written to disk at correct location
	expectedPath := filepath.Join(sdir, "valid-skill.md")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected skill file not found at %s", expectedPath)
	}
}

// TestHandleSkillsInstall_WithCorrectSHA256 verifies that when the registry index
// entry has a SHA-256 that matches the downloaded content, the skill is installed.
func TestHandleSkillsInstall_WithCorrectSHA256(t *testing.T) {
	const skillContent = "---\nname: hashed-skill\nauthor: official\n---\n\nA verified skill.\n"

	sum := sha256.Sum256([]byte(skillContent))
	correctHash := hex.EncodeToString(sum[:])

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(skillContent))
	}))
	defer registrySrv.Close()

	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	indexEntry := skills.IndexEntry{
		Name:      "hashed-skill",
		SourceURL: registrySrv.URL + "/skills/hashed-skill/SKILL.md",
		SHA256:    correctHash,
	}
	cachedIndex := map[string]any{
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"entries":    []skills.IndexEntry{indexEntry},
	}
	cacheBytes, _ := json.Marshal(cachedIndex)
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), cacheBytes, 0644)

	srv := &Server{huginnDir: dir}

	body, _ := json.Marshal(map[string]string{"target": "hashed-skill"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSkillsInstall(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("handleSkillsInstall: status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	expectedPath := filepath.Join(sdir, "hashed-skill.md")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected skill file not found at %s", expectedPath)
	}
}

// TestHandleSkillsInstall_WithWrongSHA256 verifies that when the registry index
// entry has a SHA-256 that does NOT match the downloaded content, the install
// is rejected with a 502 BadGateway and nothing is written to disk.
func TestHandleSkillsInstall_WithWrongSHA256(t *testing.T) {
	const skillContent = "---\nname: tampered-skill\nauthor: attacker\n---\n\nTampered content.\n"
	const wrongHash = "0000000000000000000000000000000000000000000000000000000000000000"

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(skillContent))
	}))
	defer registrySrv.Close()

	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	indexEntry := skills.IndexEntry{
		Name:      "tampered-skill",
		SourceURL: registrySrv.URL + "/skills/tampered-skill/SKILL.md",
		SHA256:    wrongHash,
	}
	cachedIndex := map[string]any{
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"entries":    []skills.IndexEntry{indexEntry},
	}
	cacheBytes, _ := json.Marshal(cachedIndex)
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), cacheBytes, 0644)

	srv := &Server{huginnDir: dir}

	body, _ := json.Marshal(map[string]string{"target": "tampered-skill"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSkillsInstall(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("handleSkillsInstall: status = %d, want 502; body: %s", w.Code, w.Body.String())
	}
	// Verify nothing was written to disk
	entries, _ := os.ReadDir(sdir)
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
			t.Errorf("unexpected .md file written after integrity failure: %s", entry.Name())
		}
	}
}

// TestHandleSkillsInstall_WithoutSHA256 verifies that when the registry index
// entry has no SHA-256 field, the skill installs without any hash check (existing behavior).
func TestHandleSkillsInstall_WithoutSHA256(t *testing.T) {
	const skillContent = "---\nname: unverified-skill\nauthor: author\n---\n\nNo hash provided.\n"

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(skillContent))
	}))
	defer registrySrv.Close()

	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	// SHA256 is intentionally omitted (empty string / zero value)
	indexEntry := skills.IndexEntry{
		Name:      "unverified-skill",
		SourceURL: registrySrv.URL + "/skills/unverified-skill/SKILL.md",
	}
	cachedIndex := map[string]any{
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"entries":    []skills.IndexEntry{indexEntry},
	}
	cacheBytes, _ := json.Marshal(cachedIndex)
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), cacheBytes, 0644)

	srv := &Server{huginnDir: dir}

	body, _ := json.Marshal(map[string]string{"target": "unverified-skill"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSkillsInstall(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("handleSkillsInstall: status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	expectedPath := filepath.Join(sdir, "unverified-skill.md")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected skill file not found at %s", expectedPath)
	}
}

// TestHandleSkillsInstall_RejectsSlashInName verifies that "/" in name is rejected
func TestHandleSkillsInstall_RejectsSlashInName(t *testing.T) {
	slashSkillContent := "---\nname: evil/skill\nauthor: attacker\n---\n\nMalicious.\n"

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(slashSkillContent))
	}))
	defer registrySrv.Close()

	dir := t.TempDir()
	sdir := filepath.Join(dir, "skills")
	os.MkdirAll(sdir, 0755)
	cacheDir := filepath.Join(dir, "cache")
	os.MkdirAll(cacheDir, 0755)

	indexEntry := skills.IndexEntry{
		Name:      "slash-skill",
		SourceURL: registrySrv.URL + "/skills/slash/SKILL.md",
	}
	cachedIndex := map[string]any{
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"entries":    []skills.IndexEntry{indexEntry},
	}
	cacheBytes, _ := json.Marshal(cachedIndex)
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), cacheBytes, 0644)

	srv := &Server{huginnDir: dir}

	body, _ := json.Marshal(map[string]string{"target": "slash-skill"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSkillsInstall(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}
