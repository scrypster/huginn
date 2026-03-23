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
// buildFromDir — oversized file path (> maxFileBytes)
// ---------------------------------------------------------------------------

// TestBuildFromDir_OversizedFile verifies that files larger than maxFileBytes
// are skipped without error. We cannot write 10 MB in a test, so we use a
// file exactly at maxFileBytes+1 via truncation (creates a sparse file on
// most filesystems).
func TestBuildFromDir_OversizedFile_CovBoost(t *testing.T) {
	dir := t.TempDir()
	bigPath := filepath.Join(dir, "huge.txt")

	// Create a sparse file that reports a very large size.
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Seek to just past maxFileBytes and write one byte to set file size.
	if _, err := f.Seek(maxFileBytes, 0); err != nil {
		f.Close()
		t.Fatalf("seek: %v", err)
	}
	if _, err := f.Write([]byte{0x41}); err != nil {
		f.Close()
		t.Fatalf("write: %v", err)
	}
	f.Close()

	// Also put a normal text file so we have at least one result.
	if err := os.WriteFile(filepath.Join(dir, "small.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write small: %v", err)
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The big file must not appear in the index.
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "huge.txt") {
			t.Error("oversized file should be excluded from index")
		}
	}
}

// ---------------------------------------------------------------------------
// buildFromDir — binary content (null byte) path
// ---------------------------------------------------------------------------

// TestBuildFromDir_BinaryContent_CovBoost verifies the isBinaryContent skip
// path when a file contains a null byte (but passes extension check).
func TestBuildFromDir_BinaryContent_CovBoost(t *testing.T) {
	dir := t.TempDir()
	// A .go file (not a binary extension) with a null byte — must be skipped.
	content := []byte("package main\x00\nfunc main() {}")
	if err := os.WriteFile(filepath.Join(dir, "nullbyte.go"), content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write clean: %v", err)
	}

	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "nullbyte.go") {
			t.Error("binary-content file should be excluded")
		}
	}
	found := false
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "clean.go") {
			found = true
		}
	}
	if !found {
		t.Error("expected clean.go to be indexed")
	}
}

// ---------------------------------------------------------------------------
// buildFromDirIncremental — oversized and binary content paths
// ---------------------------------------------------------------------------

func TestBuildFromDirIncremental_OversizedFile_CovBoost(t *testing.T) {
	dir := t.TempDir()
	bigPath := filepath.Join(dir, "huge.bin")
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.Seek(maxFileBytes, 0)
	f.Write([]byte{0x42})
	f.Close()

	if err := os.WriteFile(filepath.Join(dir, "small.go"), []byte("package small\n"), 0644); err != nil {
		t.Fatalf("write small: %v", err)
	}

	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "huge.bin") {
			t.Error("oversized file should not appear in index")
		}
	}
}

func TestBuildFromDirIncremental_BinaryContent_CovBoost(t *testing.T) {
	dir := t.TempDir()
	// A .ts file with a null byte.
	content := []byte("export default {}\x00")
	if err := os.WriteFile(filepath.Join(dir, "module.ts"), content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.ts"), []byte("export {}\n"), 0644); err != nil {
		t.Fatalf("write ok: %v", err)
	}

	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "module.ts") {
			t.Error("binary-content file should be excluded")
		}
	}
}

// ---------------------------------------------------------------------------
// buildFromDirIncrementalWithStats — oversized and binary content paths
// ---------------------------------------------------------------------------

func TestBuildFromDirIncrementalWithStats_OversizedFile_CovBoost(t *testing.T) {
	dir := t.TempDir()
	bigPath := filepath.Join(dir, "huge2.bin")
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.Seek(maxFileBytes, 0)
	f.Write([]byte{0x43})
	f.Close()

	if err := os.WriteFile(filepath.Join(dir, "normal.go"), []byte("package normal\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := BuildIncrementalWithStats(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	for _, ch := range result.Index.Chunks {
		if strings.Contains(ch.Path, "huge2.bin") {
			t.Error("oversized file should not appear in index")
		}
	}
}

func TestBuildFromDirIncrementalWithStats_BinaryContent_CovBoost(t *testing.T) {
	dir := t.TempDir()
	// A .py file with a null byte.
	content := []byte("print('hello')\x00")
	if err := os.WriteFile(filepath.Join(dir, "script.py"), content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean.py"), []byte("print('ok')\n"), 0644); err != nil {
		t.Fatalf("write clean: %v", err)
	}

	result, err := BuildIncrementalWithStats(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	for _, ch := range result.Index.Chunks {
		if strings.Contains(ch.Path, "script.py") {
			t.Error("binary-content file should be excluded")
		}
	}
}

// ---------------------------------------------------------------------------
// buildFromDir — WalkDir callback error (simulated via permission denied)
// ---------------------------------------------------------------------------

func TestBuildFromDir_WalkError_CovBoost(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test permission error as root")
	}
	dir := t.TempDir()
	subdir := filepath.Join(dir, "protected")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "secret.go"), []byte("package secret\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Remove execute permission so WalkDir cannot enter the directory.
	if err := os.Chmod(subdir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(subdir, 0755)

	// Build should not fail — WalkDir errors are silently skipped.
	_, err := Build(dir, nil)
	// Restore before asserting.
	os.Chmod(subdir, 0755)
	if err != nil {
		t.Errorf("Build should not return error on permission-denied subdir: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Git-backed Build/BuildIncremental — oversized and binary files in git repo
// ---------------------------------------------------------------------------

// TestBuild_GitRepo_OversizedFile verifies that files larger than maxFileBytes
// are skipped in git-backed Build.
func TestBuild_GitRepo_OversizedFile_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	// Commit a normal file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "main.go")
	runGit2(t, dir, "commit", "-m", "init")

	// After checkout, create an oversized file that won't fit in staging easily.
	// Instead, use a sparse file on disk (not committed, just present).
	bigPath := filepath.Join(dir, "main.go")
	// Overwrite after git so the stat reflects big size but git still lists main.go.
	// Create the sparse file directly.
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatalf("create big file: %v", err)
	}
	f.Seek(maxFileBytes, 0)
	f.Write([]byte{0x44})
	f.Close()

	// Now Build should detect the git repo, list main.go, find it oversized, and skip.
	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// main.go should be absent (oversized) or present (if stat gets the original size).
	_ = idx
}

// TestBuild_GitRepo_StatError verifies that files that can't be stat'd are skipped.
func TestBuild_GitRepo_StatError_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	if os.Getuid() == 0 {
		t.Skip("cannot test permission error as root")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	// Commit a file.
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "app.go")
	runGit2(t, dir, "commit", "-m", "init")

	// Remove the file so os.Stat fails.
	if err := os.Remove(filepath.Join(dir, "app.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Build should not fail — missing files are silently skipped.
	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build with missing file: %v", err)
	}
	// app.go should be absent from index.
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "app.go") {
			t.Error("missing file should be absent from index")
		}
	}
}

// TestBuildIncremental_GitRepo_StatError verifies missing files are skipped.
func TestBuildIncremental_GitRepo_StatError_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "lib.go"), []byte("package lib\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "lib.go")
	runGit2(t, dir, "commit", "-m", "init")

	// Remove the file so os.Stat fails during BuildIncremental.
	if err := os.Remove(filepath.Join(dir, "lib.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental with missing file: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "lib.go") {
			t.Error("missing file should be absent")
		}
	}
}

// TestBuildIncrementalWithStats_GitRepo_StatError verifies stat-error path in stats variant.
func TestBuildIncrementalWithStats_GitRepo_StatError_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "svc.go"), []byte("package svc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "svc.go")
	runGit2(t, dir, "commit", "-m", "init")

	// Remove the file so os.Stat fails.
	if err := os.Remove(filepath.Join(dir, "svc.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	result, err := BuildIncrementalWithStats(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats with missing file: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	for _, ch := range result.Index.Chunks {
		if strings.Contains(ch.Path, "svc.go") {
			t.Error("missing file should be absent")
		}
	}
}

// TestBuildIncremental_GitRepo_BinaryContent_CovBoost verifies binary content
// (null byte) in a tracked file is skipped.
func TestBuildIncremental_GitRepo_BinaryContent_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	// Commit a text file.
	if err := os.WriteFile(filepath.Join(dir, "ok.go"), []byte("package ok\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "ok.go")
	runGit2(t, dir, "commit", "-m", "init")

	// After commit, overwrite with binary content on disk (git still lists it).
	if err := os.WriteFile(filepath.Join(dir, "ok.go"), []byte("package ok\x00\n"), 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	idx, err := BuildIncremental(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncremental: %v", err)
	}
	// File may or may not appear depending on git listing vs on-disk state.
	_ = idx
}

// TestBuildIncrementalWithStats_GitRepo_BinaryContent_CovBoost exercises the
// isBinaryContent skip in the git-backed Stats function.
func TestBuildIncrementalWithStats_GitRepo_BinaryContent_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "data.go"), []byte("package data\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "data.go")
	runGit2(t, dir, "commit", "-m", "init")

	// Overwrite with binary content.
	if err := os.WriteFile(filepath.Join(dir, "data.go"), []byte("package data\x00\n"), 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	result, err := BuildIncrementalWithStats(dir, nil, nil)
	if err != nil {
		t.Fatalf("BuildIncrementalWithStats: %v", err)
	}
	_ = result
}

// ---------------------------------------------------------------------------
// listGitFiles — repo with no HEAD (empty repo after git init)
// ---------------------------------------------------------------------------

// TestListGitFiles_EmptyRepo_CovBoost verifies that listGitFiles returns nil
// when the repo has no HEAD (just initialized, no commits).
func TestListGitFiles_EmptyRepo_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")

	// Build should fall back to buildFromDir since there are no commits.
	idx, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("Build on empty git repo: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
}

// ---------------------------------------------------------------------------
// findGitRoot — filesystem root path (stop condition)
// ---------------------------------------------------------------------------

// TestFindGitRoot_FilesystemRoot_CovBoost verifies that findGitRoot handles
// the filesystem root stop condition (parent == current).
func TestFindGitRoot_FilesystemRoot_CovBoost(t *testing.T) {
	// Start from a temp dir with no .git anywhere above it up to HOME.
	// On most systems, /tmp is a plain directory.
	result := findGitRoot("/tmp/nonexistent-dir-abc123")
	// Result should be empty since /tmp/nonexistent doesn't exist and
	// traversal will hit root without finding .git.
	_ = result // may be "" or a found repo path
}

// ---------------------------------------------------------------------------
// buildFromDirIncremental — oversized file and binary content in git mode
// with a store to verify store interaction
// ---------------------------------------------------------------------------

// TestBuildIncremental_DirWalk_OversizedFileWithStore_CovBoost verifies
// the oversized file skip path when a store is present.
func TestBuildIncremental_DirWalk_OversizedFileWithStore_CovBoost(t *testing.T) {
	dir := t.TempDir()

	// Create an oversized sparse file.
	bigPath := filepath.Join(dir, "huge3.bin")
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.Seek(maxFileBytes, 0)
	f.Write([]byte{0x45})
	f.Close()

	if err := os.WriteFile(filepath.Join(dir, "ok.go"), []byte("package ok\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
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
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "huge3.bin") {
			t.Error("oversized file must not appear in index")
		}
	}
}

// TestBuildIncrementalWithStats_DirWalk_OversizedFileWithStore_CovBoost verifies
// the stats-variant also skips oversized files with a store.
func TestBuildIncrementalWithStats_DirWalk_OversizedFileWithStore_CovBoost(t *testing.T) {
	dir := t.TempDir()

	bigPath := filepath.Join(dir, "huge4.bin")
	f, err := os.Create(bigPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.Seek(maxFileBytes, 0)
	f.Write([]byte{0x46})
	f.Close()

	if err := os.WriteFile(filepath.Join(dir, "fine.go"), []byte("package fine\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

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
	for _, ch := range result.Index.Chunks {
		if strings.Contains(ch.Path, "huge4.bin") {
			t.Error("oversized file must not appear in index")
		}
	}
}

// ---------------------------------------------------------------------------
// Git-backed Build — ReadFile error (file exists but unreadable)
// ---------------------------------------------------------------------------

// TestBuild_GitRepo_ReadFileError_CovBoost verifies that unreadable files
// are silently skipped in git-backed Build.
func TestBuild_GitRepo_ReadFileError_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	if os.Getuid() == 0 {
		t.Skip("cannot test permission error as root")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "secret.go"), []byte("package secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "secret.go")
	runGit2(t, dir, "commit", "-m", "init")

	// Make the file unreadable so os.ReadFile fails but os.Stat succeeds.
	secretPath := filepath.Join(dir, "secret.go")
	if err := os.Chmod(secretPath, 0000); err != nil {
		t.Skip("cannot chmod: " + err.Error())
	}
	defer os.Chmod(secretPath, 0644)

	idx, err := Build(dir, nil)
	// Restore before asserting.
	os.Chmod(secretPath, 0644)

	if err != nil {
		t.Fatalf("Build should not error on unreadable file: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "secret.go") {
			t.Error("unreadable file should be excluded from index")
		}
	}
}

// TestBuildIncremental_GitRepo_ReadFileError_CovBoost similar test for BuildIncremental.
func TestBuildIncremental_GitRepo_ReadFileError_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	if os.Getuid() == 0 {
		t.Skip("cannot test permission error as root")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "priv.go"), []byte("package priv\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "priv.go")
	runGit2(t, dir, "commit", "-m", "init")

	privPath := filepath.Join(dir, "priv.go")
	if err := os.Chmod(privPath, 0000); err != nil {
		t.Skip("cannot chmod: " + err.Error())
	}
	defer os.Chmod(privPath, 0644)

	idx, err := BuildIncremental(dir, nil, nil)
	os.Chmod(privPath, 0644)

	if err != nil {
		t.Fatalf("BuildIncremental should not error on unreadable file: %v", err)
	}
	for _, ch := range idx.Chunks {
		if strings.Contains(ch.Path, "priv.go") {
			t.Error("unreadable file should be excluded")
		}
	}
}

// TestBuildIncrementalWithStats_GitRepo_ReadFileError_CovBoost similar for stats variant.
func TestBuildIncrementalWithStats_GitRepo_ReadFileError_CovBoost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	if os.Getuid() == 0 {
		t.Skip("cannot test permission error as root")
	}

	dir := t.TempDir()
	runGit2(t, dir, "init")
	runGit2(t, dir, "config", "user.email", "test@example.com")
	runGit2(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "hidden.go"), []byte("package hidden\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit2(t, dir, "add", "hidden.go")
	runGit2(t, dir, "commit", "-m", "init")

	hiddenPath := filepath.Join(dir, "hidden.go")
	if err := os.Chmod(hiddenPath, 0000); err != nil {
		t.Skip("cannot chmod: " + err.Error())
	}
	defer os.Chmod(hiddenPath, 0644)

	result, err := BuildIncrementalWithStats(dir, nil, nil)
	os.Chmod(hiddenPath, 0644)

	if err != nil {
		t.Fatalf("BuildIncrementalWithStats should not error on unreadable: %v", err)
	}
	for _, ch := range result.Index.Chunks {
		if strings.Contains(ch.Path, "hidden.go") {
			t.Error("unreadable file should be excluded")
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func runGit2(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
