package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/storage"
)

// ---------------------------------------------------------------------------
// isBinaryContent — data > 512 bytes (truncation branch)
// ---------------------------------------------------------------------------

func TestIsBinaryContent_LargeTextFile(t *testing.T) {
	// Data > 512 bytes, no null byte → not binary.
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'A'
	}
	if isBinaryContent(data) {
		t.Error("expected large text data to not be binary")
	}
}

func TestIsBinaryContent_LargeWithNullInFirst512(t *testing.T) {
	// Null byte in first 512 bytes → binary.
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'A'
	}
	data[100] = 0x00
	if !isBinaryContent(data) {
		t.Error("expected binary when null byte is in first 512 bytes")
	}
}

func TestIsBinaryContent_LargeWithNullAfter512(t *testing.T) {
	// Null byte after byte 512 → should NOT be detected (only first 512 sniffed).
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'A'
	}
	data[600] = 0x00
	if isBinaryContent(data) {
		t.Error("expected not binary when null byte is after first 512 bytes")
	}
}

func TestIsBinaryContent_Exactly512Bytes(t *testing.T) {
	data := make([]byte, 512)
	for i := range data {
		data[i] = 'B'
	}
	if isBinaryContent(data) {
		t.Error("expected 512-byte text data to not be binary")
	}
}

func TestIsBinaryContent_Exactly513Bytes(t *testing.T) {
	// 513 bytes triggers the truncation branch.
	data := make([]byte, 513)
	for i := range data {
		data[i] = 'C'
	}
	if isBinaryContent(data) {
		t.Error("expected 513-byte text data to not be binary")
	}
}

// ---------------------------------------------------------------------------
// buildFromDir — skipped directories (node_modules, vendor)
// ---------------------------------------------------------------------------

func TestBuildFromDir_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nmDir, "index.js"), []byte("module.exports = {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "node_modules") {
			t.Errorf("expected node_modules to be excluded, got chunk path %q", ch.Path)
		}
	}
	found := false
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "app.js") {
			found = true
		}
	}
	if !found {
		t.Error("expected app.js to be indexed")
	}
}

func TestBuildFromDir_SkipsVendor(t *testing.T) {
	dir := t.TempDir()
	vendorDir := filepath.Join(dir, "vendor", "lib")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "lib.go"), []byte("package lib\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "vendor") {
			t.Errorf("expected vendor to be excluded, got chunk path %q", ch.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// buildFromDir — binary extension filtering
// ---------------------------------------------------------------------------

func TestBuildFromDir_SkipsBinaryExtensions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"img.png", "lib.so", "data.pdf", "code.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		ext := filepath.Ext(ch.Path)
		if ext == ".png" || ext == ".so" || ext == ".pdf" {
			t.Errorf("binary extension file should be skipped: %q", ch.Path)
		}
	}
	found := false
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "code.go") {
			found = true
		}
	}
	if !found {
		t.Error("expected code.go to be indexed")
	}
}

// ---------------------------------------------------------------------------
// Build on git repo — binary content and extension filtering
// ---------------------------------------------------------------------------

func TestBuild_GitRepo_BinaryContentSkipped(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"text.go": []byte("package main\n"),
		"binary":  append([]byte("header"), 0x00, 0x00),
	})

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		if ch.Path == "binary" {
			t.Error("expected binary content file to be excluded")
		}
	}
}

func TestBuild_GitRepo_BinaryExtSkipped(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"code.go":  []byte("package main\n"),
		"image.png": []byte("fake png content"),
	})

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		if ch.Path == "image.png" {
			t.Error("expected .png file to be excluded from git repo index")
		}
	}
}

// ---------------------------------------------------------------------------
// BuildIncremental on git repo — with store (cache hit/miss)
// ---------------------------------------------------------------------------

func TestBuildIncremental_GitRepo_WithStore_CacheHitMiss(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"main.go": []byte("package main\nfunc main() {}\n"),
	})

	storeDir := t.TempDir()
	store, err := storage.Open(storeDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	// First run: cache miss.
	idx1, err := BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("first BuildIncremental: %v", err)
	}
	if len(idx1.Chunks) == 0 {
		t.Fatal("expected chunks from first run")
	}

	// Second run: cache hit — file unchanged.
	idx2, err := BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("second BuildIncremental: %v", err)
	}
	if len(idx2.Chunks) == 0 {
		t.Error("expected chunks from cache hit")
	}

	// Modify the file and run again: cache miss.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() { println(\"changed\") }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Need to stage and commit for git-tracked mode.
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "commit", "-m", "update")

	idx3, err := BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("third BuildIncremental: %v", err)
	}
	if len(idx3.Chunks) == 0 {
		t.Error("expected chunks after file modification")
	}
}

// ---------------------------------------------------------------------------
// BuildIncrementalWithStats on git repo — stats tracking
// ---------------------------------------------------------------------------

func TestBuildIncrementalWithStats_GitRepo_StatsAccurate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"a.go": []byte("package a\n"),
		"b.go": []byte("package b\n"),
	})

	storeDir := t.TempDir()
	store, err := storage.Open(storeDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	// First run: all files scanned, none skipped.
	result1, err := BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if result1.FilesScanned < 2 {
		t.Errorf("expected at least 2 files scanned, got %d", result1.FilesScanned)
	}
	if result1.FilesSkipped != 0 {
		t.Errorf("expected 0 skipped on first run, got %d", result1.FilesSkipped)
	}

	// Second run: files unchanged, should be skipped.
	result2, err := BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if result2.FilesSkipped < 2 {
		t.Errorf("expected at least 2 files skipped, got %d", result2.FilesSkipped)
	}
}

// ---------------------------------------------------------------------------
// BuildIncrementalWithStats on git repo — binary extension excluded from scan count
// ---------------------------------------------------------------------------

func TestBuildIncrementalWithStats_GitRepo_BinaryNotCounted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"code.go":   []byte("package main\n"),
		"image.png": []byte("fake png data"),
	})

	result, err := BuildIncrementalWithStats(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	// Only code.go should be counted as scanned; image.png is skipped by extension.
	if result.FilesScanned != 1 {
		t.Errorf("expected 1 file scanned (binary ext excluded), got %d", result.FilesScanned)
	}
}

// ---------------------------------------------------------------------------
// BuildIncremental on git repo — nil store (no caching)
// ---------------------------------------------------------------------------

func TestBuildIncremental_GitRepo_NilStore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"main.go": []byte("package main\n"),
	})

	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental with nil store: %v", err)
	}
	if len(idx.Chunks) == 0 {
		t.Error("expected chunks even with nil store")
	}
}

// ---------------------------------------------------------------------------
// BuildIncremental on git repo — with progress callback
// ---------------------------------------------------------------------------

func TestBuildIncremental_GitRepo_WithProgress(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"main.go": []byte("package main\n"),
	})

	var calls int
	_, err := BuildIncremental(dir, nil, func(done, total int, path string) {
		calls++
		if total == 0 {
			t.Error("expected non-zero total in git mode")
		}
	})
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	if calls == 0 {
		t.Error("expected progress callback to be called in git mode")
	}
}

// ---------------------------------------------------------------------------
// BuildIncrementalWithStats — progress callback in git mode
// ---------------------------------------------------------------------------

func TestBuildIncrementalWithStats_GitRepo_Progress(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepoWithFiles(t, map[string][]byte{
		"main.go": []byte("package main\n"),
	})

	var calls int
	_, err := BuildIncrementalWithStats(dir, nil, func(done, total int, path string) {
		calls++
	})
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	if calls == 0 {
		t.Error("expected progress callback in git mode")
	}
}

// ---------------------------------------------------------------------------
// buildFromDirIncremental — skip directories (dist, .next)
// ---------------------------------------------------------------------------

func TestBuildFromDirIncremental_SkipsDist(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "bundle.js"), []byte("var x = 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src.js"), []byte("var y = 2;\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "dist") {
			t.Errorf("expected dist to be excluded, got chunk path %q", ch.Path)
		}
	}
}

func TestBuildFromDirIncremental_SkipsDotNext(t *testing.T) {
	dir := t.TempDir()
	nextDir := filepath.Join(dir, ".next")
	if err := os.MkdirAll(nextDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nextDir, "cache.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.tsx"), []byte("export default function() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, ".next") {
			t.Errorf("expected .next to be excluded, got chunk path %q", ch.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// BuildIncrementalWithStats dir walk — with store cache hit
// ---------------------------------------------------------------------------

func TestBuildIncrementalWithStats_DirWalk_CacheHit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	storeDir := t.TempDir()
	store, err := storage.Open(storeDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	// First run populates.
	r1, err := BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if r1.FilesScanned == 0 {
		t.Error("expected at least 1 scanned")
	}

	// Second run should skip.
	r2, err := BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if r2.FilesSkipped == 0 {
		t.Error("expected at least 1 skipped on second run")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// initGitRepoWithFiles creates a temp git repo with the given files committed.
func initGitRepoWithFiles(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", name)
	}
	runGit(t, dir, "commit", "-m", "initial")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
