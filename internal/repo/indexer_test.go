package repo

import (
	"encoding/hex"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/storage"
)

func TestChunkFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")

	content := ""
	for i := 0; i < 60; i++ {
		content += "// block\nfunc foo() {\n\treturn\n}\n\n"
	}
	os.WriteFile(path, []byte(content), 0o644)

	chunks := chunkContent("main.go", []byte(content), 50*1024)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	for _, ch := range chunks {
		if ch.Path == "" {
			t.Error("chunk missing path")
		}
		if ch.Content == "" {
			t.Error("chunk missing content")
		}
	}
}

func TestChunkFileNoBlanks(t *testing.T) {
	// A file with no blank lines exceeding maxBytes should still be chunked
	line := strings.Repeat("x", 100) + "\n"
	content := strings.Repeat(line, 600) // 60600 bytes, well over 50KB
	chunks := chunkContent("dense.go", []byte(content), 50*1024)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for dense file, got %d", len(chunks))
	}
	for _, ch := range chunks {
		if len(ch.Content) > 55*1024 { // allow small overshoot
			t.Errorf("chunk too large: %d bytes", len(ch.Content))
		}
	}
}

func TestIsBinary(t *testing.T) {
	cases := []struct {
		name   string
		binary bool
	}{
		{"main.go", false},
		{"image.png", true},
		{"archive.zip", true},
		{"README.md", false},
		{"binary.exe", true},
	}
	for _, tc := range cases {
		got := isBinaryByExtension(tc.name)
		if got != tc.binary {
			t.Errorf("%q: want binary=%v got %v", tc.name, tc.binary, got)
		}
	}
}

// TestChunkContent_EmptyData verifies that empty content produces a single empty chunk.
func TestChunkContent_EmptyData(t *testing.T) {
	chunks := chunkContent("empty.txt", []byte(""), 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty data, got %d", len(chunks))
	}
	if chunks[0].Content != "" {
		t.Errorf("expected empty content, got %q", chunks[0].Content)
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("expected StartLine=1, got %d", chunks[0].StartLine)
	}
}

// TestChunkContent_SingleLine verifies that a single-line file produces one chunk.
func TestChunkContent_SingleLine(t *testing.T) {
	data := []byte("hello world\n")
	chunks := chunkContent("single.txt", data, 200*1024)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Path != "single.txt" {
		t.Errorf("expected path 'single.txt', got %q", chunks[0].Path)
	}
}

// TestChunkContent_StartLines verifies that StartLine is tracked correctly
// across chunk boundaries.
func TestChunkContent_StartLines(t *testing.T) {
	// Build content large enough to force chunking.
	line := strings.Repeat("a", 100) + "\n" // 101 bytes/line
	data := []byte(strings.Repeat(line, 600))
	chunks := chunkContent("big.txt", data, 50*1024)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// First chunk must start at line 1.
	if chunks[0].StartLine != 1 {
		t.Errorf("first chunk StartLine: want 1, got %d", chunks[0].StartLine)
	}
	// Second chunk must start after the first.
	if chunks[1].StartLine <= chunks[0].StartLine {
		t.Errorf("second chunk StartLine %d should be > first %d",
			chunks[1].StartLine, chunks[0].StartLine)
	}
}

// TestBuildFromDir_EmptyDir verifies that an empty directory produces an Index
// with no chunks.
func TestBuildFromDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
	if len(idx.Chunks) != 0 {
		t.Errorf("expected 0 chunks for empty dir, got %d", len(idx.Chunks))
	}
}

// TestBuildFromDir_SingleFile verifies that a directory with one text file
// produces exactly one chunk (assuming small file).
func TestBuildFromDir_SingleFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(idx.Chunks) == 0 {
		t.Error("expected at least 1 chunk for a file with content")
	}
}

// TestBuildFromDir_BinaryFileSkipped verifies that binary files (null byte) are excluded.
func TestBuildFromDir_BinaryFileSkipped(t *testing.T) {
	dir := t.TempDir()
	// Binary file with null byte.
	bin := make([]byte, 100)
	bin[50] = 0x00
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), bin, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "text.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		if ch.Path == "binary.bin" {
			t.Error("expected binary file to be excluded from index")
		}
	}
}

// TestBuildFromDir_ProgressCallback verifies that the progress callback is called.
func TestBuildFromDir_ProgressCallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	called := false
	_, err := Build(dir, func(done, total int, path string) {
		called = true
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !called {
		t.Error("expected progress callback to be called")
	}
}

// TestBuildFromDir_ExcludesGitDir verifies that .git directory is skipped.
func TestBuildFromDir_ExcludesGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.HasPrefix(ch.Path, ".git") {
			t.Errorf("expected .git directory to be excluded, but got chunk path %q", ch.Path)
		}
	}
}

// TestBuildIncrementalWithStats_EmptyDir verifies the stats struct for empty dir.
func TestBuildIncrementalWithStats_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	result, err := BuildIncrementalWithStats(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FilesScanned != 0 {
		t.Errorf("expected 0 files scanned, got %d", result.FilesScanned)
	}
	if result.FilesSkipped != 0 {
		t.Errorf("expected 0 files skipped, got %d", result.FilesSkipped)
	}
}

// TestIsBinaryContent_OnlyNullByte verifies single null byte is binary.
func TestIsBinaryContent_OnlyNullByte(t *testing.T) {
	if !isBinaryContent([]byte{0x00}) {
		t.Error("expected single null byte to be binary")
	}
}

// TestIsBinaryContent_Empty verifies empty slice is not binary.
func TestIsBinaryContent_Empty(t *testing.T) {
	if isBinaryContent([]byte{}) {
		t.Error("expected empty data to not be binary")
	}
}

// TestIsBinaryByExtension_CaseInsensitive verifies uppercase extensions work.
func TestIsBinaryByExtension_CaseInsensitive(t *testing.T) {
	cases := []struct {
		name   string
		binary bool
	}{
		{"IMAGE.PNG", true},
		{"ARCHIVE.ZIP", true},
		{"FILE.GO", false},
		{"IMAGE.Jpg", true},
	}
	for _, tc := range cases {
		got := isBinaryByExtension(tc.name)
		if got != tc.binary {
			t.Errorf("%q: want binary=%v, got %v", tc.name, tc.binary, got)
		}
	}
}

// TestBuildFromDir_VeryLongPath verifies a deeply nested path is handled.
func TestBuildFromDir_VeryLongPath(t *testing.T) {
	dir := t.TempDir()
	// Create a deeply nested directory structure.
	deepDir := filepath.Join(dir, "a", "b", "c", "d", "e")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deepDir, "deep.txt"), []byte("deep content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "deep.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected deeply nested file to be indexed")
	}
}

// --- sha256hex ---

// TestSha256hex_KnownInput verifies sha256hex against a well-known SHA-256 value.
func TestSha256hex_KnownInput(t *testing.T) {
	input := []byte("hello")
	sum := sha256.Sum256(input)
	expected := hex.EncodeToString(sum[:])
	got := sha256hex(input)
	if got != expected {
		t.Errorf("sha256hex(%q): want %q, got %q", input, expected, got)
	}
}

// TestSha256hex_Empty verifies sha256hex on empty input.
func TestSha256hex_Empty(t *testing.T) {
	got := sha256hex([]byte{})
	if len(got) != 64 {
		t.Errorf("expected 64-char hex string, got %d chars: %q", len(got), got)
	}
}

// TestSha256hex_Deterministic verifies two calls with same input return same hash.
func TestSha256hex_Deterministic(t *testing.T) {
	data := []byte("the quick brown fox")
	a := sha256hex(data)
	b := sha256hex(data)
	if a != b {
		t.Errorf("sha256hex not deterministic: %q vs %q", a, b)
	}
}

// TestSha256hex_DifferentInputs verifies two different inputs produce different hashes.
func TestSha256hex_DifferentInputs(t *testing.T) {
	a := sha256hex([]byte("foo"))
	b := sha256hex([]byte("bar"))
	if a == b {
		t.Error("expected different hashes for different inputs")
	}
}

// --- BuildIncremental (directory-walk path, no store) ---

// TestBuildIncremental_PlainDir verifies BuildIncremental on a plain directory
// returns chunks for text files.
func TestBuildIncremental_PlainDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
	if len(idx.Chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

// TestBuildIncremental_SkipsBinaryExtension verifies that binary extensions
// are skipped during incremental build.
func TestBuildIncremental_SkipsBinaryExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "img.png"), []byte("not really png but ext matters"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	for _, ch := range idx.Chunks {
		if ch.Path == "img.png" {
			t.Error("binary extension file should be skipped")
		}
	}
}

// TestBuildIncremental_WithProgress verifies that the progress callback fires.
func TestBuildIncremental_WithProgress(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data\n"), 0644); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := BuildIncremental(dir, nil, func(done, total int, path string) {
		called = true
	})
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	if !called {
		t.Error("expected progress callback to be called")
	}
}

// TestBuildIncremental_WithStore_CacheMiss verifies that a file not in the store
// is chunked and stored.
func TestBuildIncremental_WithStore_CacheMiss(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	storeDir := t.TempDir()
	store, err := storage.Open(storeDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	idx, err := BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	if len(idx.Chunks) == 0 {
		t.Error("expected chunks from cache miss")
	}
}

// TestBuildIncremental_WithStore_CacheHit verifies that on a second run,
// unchanged files are loaded from the store (cache hit path).
func TestBuildIncremental_WithStore_CacheHit(t *testing.T) {
	dir := t.TempDir()
	content := []byte("package main\nfunc main() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "code.go"), content, 0644); err != nil {
		t.Fatal(err)
	}
	storeDir := t.TempDir()
	store, err := storage.Open(storeDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	// First run: populates the store.
	_, err = BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("first BuildIncremental: %v", err)
	}

	// Second run: should hit cache.
	idx, err := BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("second BuildIncremental: %v", err)
	}
	if len(idx.Chunks) == 0 {
		t.Error("expected chunks from cache hit")
	}
}

// --- BuildIncrementalWithStats ---

// TestBuildIncrementalWithStats_PlainDir verifies stats are populated for a plain dir.
func TestBuildIncrementalWithStats_PlainDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := BuildIncrementalWithStats(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FilesScanned < 2 {
		t.Errorf("expected at least 2 files scanned, got %d", result.FilesScanned)
	}
	if result.Index == nil {
		t.Error("expected non-nil index in result")
	}
}

// TestBuildIncrementalWithStats_SkipCounts verifies that cached files
// increment FilesSkipped on subsequent runs.
func TestBuildIncrementalWithStats_SkipCounts(t *testing.T) {
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

	// First run: populates the store.
	_, err = BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run: file unchanged, so it should be skipped.
	result, err := BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if result.FilesSkipped == 0 {
		t.Error("expected at least 1 skipped file on second run with unchanged content")
	}
}

// TestBuildIncrementalWithStats_WithProgress verifies progress callback fires.
func TestBuildIncrementalWithStats_WithProgress(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := BuildIncrementalWithStats(dir, nil, func(done, total int, path string) {
		called = true
	})
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	if !called {
		t.Error("expected progress callback to be called")
	}
}

// --- listGitFiles (tested via Build on a real git repo) ---

// initGitRepo creates a temp git repo with one committed file.
// Returns the repo root.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "hello.go")
	run("commit", "-m", "initial")

	return dir
}

// TestListGitFiles_RealRepo verifies Build picks up committed files via listGitFiles.
func TestListGitFiles_RealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepo(t)

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "hello.go") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected hello.go to be indexed from git repo")
	}
}

// TestBuild_RealGitRepo_WithProgress verifies progress callback fires in git-backed Build.
func TestBuild_RealGitRepo_WithProgress(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepo(t)
	called := false
	_, err := Build(dir, func(done, total int, path string) {
		called = true
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !called {
		t.Error("expected progress callback from git-backed Build")
	}
}

// TestBuildIncremental_RealGitRepo verifies BuildIncremental works on a real git repo.
func TestBuildIncremental_RealGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepo(t)
	storeDir := t.TempDir()
	store, err := storage.Open(storeDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	idx, err := BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	if len(idx.Chunks) == 0 {
		t.Error("expected chunks from real git repo")
	}

	// Second run: should hit cache.
	idx2, err := BuildIncremental(dir, store, nil)
	if err != nil {
		t.Fatalf("second BuildIncremental: %v", err)
	}
	if len(idx2.Chunks) == 0 {
		t.Error("expected chunks from cache hit on real git repo")
	}
}

// TestBuildIncrementalWithStats_RealGitRepo verifies stats on a real git repo.
func TestBuildIncrementalWithStats_RealGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := initGitRepo(t)
	storeDir := t.TempDir()
	store, err := storage.Open(storeDir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	result, err := BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	if result.FilesScanned == 0 {
		t.Error("expected at least 1 file scanned in real git repo")
	}
	if result.Index == nil {
		t.Fatal("expected non-nil index")
	}

	// Second run: files unchanged, should skip.
	result2, err := BuildIncrementalWithStats(dir, store, nil)
	if err != nil {
		t.Fatalf("second BuildIncrementalWithStats: %v", err)
	}
	if result2.FilesSkipped == 0 {
		t.Error("expected skipped files on second run of real git repo")
	}
}
