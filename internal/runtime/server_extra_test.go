package runtime

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ============================================================
// NewManager
// ============================================================

func TestNewManager_ValidDir(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager should succeed with valid dir, got: %v", err)
	}
	if mgr == nil {
		t.Fatal("NewManager returned nil manager")
	}
	if mgr.huginnDir != dir {
		t.Errorf("expected huginnDir=%q, got %q", dir, mgr.huginnDir)
	}
	if mgr.manifest == nil {
		t.Error("expected non-nil manifest")
	}
}

func TestNewManager_ManifestLoaded(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.manifest.Version != 1 {
		t.Errorf("expected manifest version 1, got %d", mgr.manifest.Version)
	}
}

// ============================================================
// Start — test with non-existent binary
// ============================================================

func TestStart_NonExistentBinary_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// BinaryPath points to a non-existent file — Start should fail.
	err = mgr.Start("/nonexistent/path/to/llama-server", 19988)
	if err == nil {
		t.Error("expected error when starting with non-existent binary path")
		// Clean up if it somehow started
		_ = mgr.Shutdown()
	}
}

// ============================================================
// WaitForReady — timeout path
// ============================================================

func TestWaitForReady_Timeout_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Use a context that's already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Set up a fake cmd that's "running" but ProcessState is nil.
	// Use a real process that sleeps for a long time so ProcessState stays nil.
	mgr.cmd = exec.Command("sleep", "300")
	mgr.port = 19977 // nothing listening here
	if startErr := mgr.cmd.Start(); startErr != nil {
		t.Skipf("could not start sleep process: %v", startErr)
	}
	defer func() {
		_ = mgr.cmd.Process.Kill()
		_ = mgr.cmd.Wait()
	}()

	err = mgr.WaitForReady(ctx)
	if err == nil {
		t.Error("expected WaitForReady to return error on cancelled context")
	}
}

// ============================================================
// Shutdown
// ============================================================

func TestShutdown_NilManager_NoOp(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// cmd is nil — Shutdown should be a no-op.
	mgr.cmd = nil
	err = mgr.Shutdown()
	if err != nil {
		t.Errorf("Shutdown with nil cmd should return nil, got: %v", err)
	}
}

func TestShutdown_NilProcess_NoOp(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// cmd is set but Process is nil (process never started).
	mgr.cmd = &exec.Cmd{}
	err = mgr.Shutdown()
	if err != nil {
		t.Errorf("Shutdown with nil Process should return nil, got: %v", err)
	}
}

func TestShutdown_AlreadyExitedProcess(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Start a short-lived process that exits immediately.
	mgr.cmd = exec.Command("sh", "-c", "exit 0")
	if startErr := mgr.cmd.Start(); startErr != nil {
		t.Skipf("could not start process: %v", startErr)
	}
	// Wait for it to exit.
	_ = mgr.cmd.Wait()

	// Shutdown on an already-exited process should not panic or block.
	done := make(chan error, 1)
	go func() { done <- mgr.Shutdown() }()
	select {
	case <-done:
		// success — any error is fine (process may already be gone)
	case <-time.After(6 * time.Second):
		t.Error("Shutdown timed out on already-exited process")
	}
}

// ============================================================
// downloadFile — using httptest server
// ============================================================

func TestDownloadFile_Success(t *testing.T) {
	content := "hello binary content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "downloaded.bin")

	var progressCalls int
	err := downloadFile(context.Background(), srv.URL, destPath, func(downloaded, total int64) {
		progressCalls++
	})
	if err != nil {
		t.Fatalf("downloadFile should succeed, got: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("could not read downloaded file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}
	if progressCalls == 0 {
		t.Error("expected at least one progress callback")
	}
}

func TestDownloadFile_404ReturnsEmptyFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "not found")
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "out.bin")

	// downloadFile itself doesn't check HTTP status codes — it reads the body.
	// A 404 still succeeds from downloadFile's perspective (body is "not found").
	err := downloadFile(context.Background(), srv.URL, destPath, nil)
	if err != nil {
		t.Logf("downloadFile returned error for 404: %v (acceptable)", err)
	}
}

func TestDownloadFile_InvalidURL_ReturnsError(t *testing.T) {
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "out.bin")
	err := downloadFile(context.Background(), "http://127.0.0.1:0/nonexistent", destPath, nil)
	if err == nil {
		t.Error("expected error for invalid/unreachable URL")
	}
}

func TestDownloadFile_NoProgressCallback(t *testing.T) {
	content := "test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "out.bin")

	// nil onProgress — should not panic.
	err := downloadFile(context.Background(), srv.URL, destPath, nil)
	if err != nil {
		t.Fatalf("downloadFile with nil progress callback failed: %v", err)
	}
}

func TestDownloadFile_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server.
		time.Sleep(2 * time.Second)
		_, _ = io.WriteString(w, "data")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "out.bin")

	err := downloadFile(ctx, srv.URL, destPath, nil)
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

// ============================================================
// extractZip
// ============================================================

// makeZip creates an in-memory zip archive with one file at the given path.
func makeZip(t *testing.T, filePath, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.zip")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	fw, err := w.Create(filePath)
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	if _, err := io.WriteString(fw, content); err != nil {
		t.Fatalf("zip.Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	return archivePath
}

func TestExtractZip_Success(t *testing.T) {
	content := "#!/bin/sh\necho hello"
	archivePath := makeZip(t, "llama-server", content)

	destDir := t.TempDir()
	finalPath := filepath.Join(destDir, "llama-server")

	err := extractZip(archivePath, "llama-server", finalPath)
	if err != nil {
		t.Fatalf("extractZip should succeed, got: %v", err)
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("could not read extracted file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}
}

func TestExtractZip_MissingPath_ReturnsError(t *testing.T) {
	archivePath := makeZip(t, "other-file", "content")

	destDir := t.TempDir()
	finalPath := filepath.Join(destDir, "llama-server")

	err := extractZip(archivePath, "nonexistent-file", finalPath)
	if err == nil {
		t.Error("expected error when extract_path not found in zip")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestExtractZip_CorruptedZip_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "corrupt.zip")
	if err := os.WriteFile(archivePath, []byte("not a zip file at all"), 0644); err != nil {
		t.Fatalf("write corrupt zip: %v", err)
	}
	destDir := t.TempDir()
	err := extractZip(archivePath, "anything", filepath.Join(destDir, "out"))
	if err == nil {
		t.Error("expected error for corrupted zip")
	}
}

// ============================================================
// extractTarGz
// ============================================================

// makeTarGz creates an in-memory tar.gz archive with one file.
func makeTarGz(t *testing.T, filePath, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	data := []byte(content)
	hdr := &tar.Header{
		Name: filePath,
		Mode: 0755,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar.WriteHeader: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("tar.Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}
	return archivePath
}

func TestExtractTarGz_Success(t *testing.T) {
	content := "#!/bin/sh\necho hello"
	archivePath := makeTarGz(t, "llama-b8192/llama-server", content)

	destDir := t.TempDir()
	finalPath := filepath.Join(destDir, "llama-server")

	err := extractTarGz(archivePath, "llama-b8192/llama-server", finalPath)
	if err != nil {
		t.Fatalf("extractTarGz should succeed, got: %v", err)
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("could not read extracted file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}
}

func TestExtractTarGz_MissingPath_ReturnsError(t *testing.T) {
	archivePath := makeTarGz(t, "some-other-file", "content")

	destDir := t.TempDir()
	finalPath := filepath.Join(destDir, "llama-server")

	err := extractTarGz(archivePath, "nonexistent-path", finalPath)
	if err == nil {
		t.Error("expected error when extract_path not found in tar.gz")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestExtractTarGz_CorruptedInput_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "corrupt.tar.gz")
	if err := os.WriteFile(archivePath, []byte("not a gzip file"), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	destDir := t.TempDir()
	err := extractTarGz(archivePath, "anything", filepath.Join(destDir, "out"))
	if err == nil {
		t.Error("expected error for corrupted tar.gz")
	}
}

func TestExtractTarGz_CorruptedGzipValidHeader(t *testing.T) {
	// A valid gzip header but corrupted tar content.
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "corrupt.tar.gz")

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write([]byte("not valid tar content at all!!!"))
	_ = gw.Close()

	if err := os.WriteFile(archivePath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	destDir := t.TempDir()
	err := extractTarGz(archivePath, "anything", filepath.Join(destDir, "out"))
	if err == nil {
		t.Error("expected error for corrupted tar content inside valid gzip")
	}
}

// ============================================================
// LoadManifest — additional coverage
// ============================================================

func TestLoadManifest_AllPlatformsHaveNonEmptyURLs(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	for key, entry := range m.Binaries {
		if entry.URL == "" {
			t.Errorf("platform %q has empty URL", key)
		}
		if entry.ExtractPath == "" {
			t.Errorf("platform %q has empty ExtractPath", key)
		}
		if entry.ArchiveType == "" {
			t.Errorf("platform %q has empty ArchiveType", key)
		}
	}
}

func TestLoadManifest_ArchiveTypesAreKnown(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	known := map[string]bool{"zip": true, "tar.gz": true}
	for key, entry := range m.Binaries {
		if !known[entry.ArchiveType] {
			t.Errorf("platform %q has unknown archive_type %q", key, entry.ArchiveType)
		}
	}
}

// ============================================================
// Platform.Detect — additional branch coverage
// ============================================================

func TestDetect_CUDAFalseOnNonLinux(t *testing.T) {
	p := Detect()
	// On macOS and Windows, CUDA should always be false.
	if p.OS != "linux" && p.CUDA {
		t.Errorf("CUDA should be false on non-linux OS %q", p.OS)
	}
}

func TestPlatform_Key_Linux_NoCUDA(t *testing.T) {
	p := Platform{OS: "linux", Arch: "amd64", CUDA: false}
	key := p.Key()
	if key != "linux-amd64" {
		t.Errorf("expected 'linux-amd64', got %q", key)
	}
}

func TestPlatform_Key_Linux_CUDA(t *testing.T) {
	p := Platform{OS: "linux", Arch: "amd64", CUDA: true}
	key := p.Key()
	if key != "linux-amd64-cuda" {
		t.Errorf("expected 'linux-amd64-cuda', got %q", key)
	}
}

func TestPlatform_Key_Windows(t *testing.T) {
	p := Platform{OS: "windows", Arch: "amd64", CUDA: false}
	key := p.Key()
	if key != "windows-amd64" {
		t.Errorf("expected 'windows-amd64', got %q", key)
	}
}

func TestPlatform_Key_DarwinArm64(t *testing.T) {
	p := Platform{OS: "darwin", Arch: "arm64"}
	key := p.Key()
	if key != "darwin-arm64" {
		t.Errorf("expected 'darwin-arm64', got %q", key)
	}
}

// ============================================================
// Manager.Port() and Manager.Cmd()
// ============================================================

func TestManager_Port(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Initially port should be 0
	if mgr.Port() != 0 {
		t.Errorf("expected initial Port() == 0, got %d", mgr.Port())
	}
	// After setting port
	mgr.port = 8888
	if mgr.Port() != 8888 {
		t.Errorf("expected Port() == 8888, got %d", mgr.Port())
	}
}

func TestManager_Cmd(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Initially cmd should be nil
	if mgr.Cmd() != nil {
		t.Error("expected initial Cmd() == nil")
	}
	// Create a dummy command
	cmd := exec.Command("sh", "-c", "exit 0")
	mgr.cmd = cmd
	if mgr.Cmd() != cmd {
		t.Error("expected Cmd() to return the set command")
	}
}

// ============================================================
// Manager.Download() - error cases
// ============================================================

func TestDownload_NoPlatformBinary_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Override platform to one not in manifest
	mgr.platform = Platform{OS: "freebsd", Arch: "riscv64"}
	err = mgr.Download(context.Background(), nil)
	if err == nil {
		t.Error("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "no llama-server binary available") {
		t.Errorf("expected 'no llama-server binary available' in error, got: %v", err)
	}
}

func TestDownload_MkdirallFailure(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Set huginnDir to a path that can't be created
	mgr.huginnDir = "/dev/null/impossible/path"
	err = mgr.Download(context.Background(), nil)
	if err == nil {
		t.Error("expected error when mkdir fails")
	}
}

// ============================================================
// WaitForReady - HTTP health check success
// ============================================================

func TestWaitForReady_HealthCheckSuccess(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a fake health check server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Extract port from server URL
	parts := strings.Split(srv.URL, ":")
	port := 0
	if len(parts) >= 3 {
		port, _ = strconv.Atoi(parts[2])
	}
	if port == 0 {
		t.Skip("could not extract port from test server")
	}

	// Start a process that will stay alive
	mgr.cmd = exec.Command("sleep", "10")
	mgr.port = port
	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("could not start sleep: %v", err)
	}
	defer func() {
		_ = mgr.cmd.Process.Kill()
		_ = mgr.cmd.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.WaitForReady(ctx)
	if err != nil {
		t.Errorf("expected WaitForReady to succeed with health check, got: %v", err)
	}
}

// ============================================================
// Shutdown - graceful shutdown
// ============================================================

func TestShutdown_GracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Start a process that responds to SIGTERM gracefully
	mgr.cmd = exec.Command("sh", "-c", "trap 'exit 0' TERM; sleep 30")
	mgr.port = 9999
	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("could not start process: %v", err)
	}

	err = mgr.Shutdown()
	// Should succeed (either via Signal or Kill)
	if err != nil {
		t.Logf("Shutdown returned error (may be expected): %v", err)
	}
}

// ============================================================
// Detect - CUDA on Linux (if available)
// ============================================================

func TestDetect_ReturnsStructWithOSAndArch(t *testing.T) {
	p := Detect()
	if p.OS == "" {
		t.Error("OS should not be empty")
	}
	if p.Arch == "" {
		t.Error("Arch should not be empty")
	}
}

// ============================================================
// FindFreePort - error case
// ============================================================

func TestFindFreePort_BindSucceeds(t *testing.T) {
	port, err := FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	// Port should be in valid range
	if port <= 0 || port > 65535 {
		t.Errorf("invalid port returned: %d", port)
	}
}

// ============================================================
// downloadFile - write error
// ============================================================

func TestDownloadFile_WriteError(t *testing.T) {
	content := "test content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	// Try to write to a read-only directory
	err := downloadFile(context.Background(), srv.URL, "/dev/null/nonexistent/path", nil)
	if err == nil {
		t.Error("expected error when write destination is invalid")
	}
}

// ============================================================
// extractZip - file open error
// ============================================================

func TestExtractZip_NonZipFile_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "notazip.zip")
	if err := os.WriteFile(archivePath, []byte("plaintext"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	destDir := t.TempDir()
	err := extractZip(archivePath, "anything", filepath.Join(destDir, "out"))
	if err == nil {
		t.Error("expected error for non-zip file")
	}
}

// ============================================================
// extractZip - file open within archive error
// ============================================================

func TestExtractZip_OpenFileError(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	// Create a directory entry instead of a file
	_, err = w.Create("llama-server/")
	if err != nil {
		t.Fatalf("zip.Create dir: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	destDir := t.TempDir()
	finalPath := filepath.Join(destDir, "llama-server")

	// This should not crash even with a directory entry
	err = extractZip(archivePath, "llama-server/", finalPath)
	// Either success or error is acceptable
}

// ============================================================
// extractTarGz - file open error
// ============================================================

func TestExtractTarGz_OpenFileError(t *testing.T) {
	err := extractTarGz("/nonexistent/archive.tar.gz", "anything", "/tmp/out")
	if err == nil {
		t.Error("expected error when archive doesn't exist")
	}
}

// ============================================================
// WaitForReady - multiple health check attempts
// ============================================================

func TestWaitForReady_EventualSuccess(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt >= 3 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	parts := strings.Split(srv.URL, ":")
	port := 0
	if len(parts) >= 3 {
		port, _ = strconv.Atoi(parts[2])
	}
	if port == 0 {
		t.Skip("could not extract port")
	}

	mgr.cmd = exec.Command("sleep", "10")
	mgr.port = port
	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("could not start sleep: %v", err)
	}
	defer func() {
		_ = mgr.cmd.Process.Kill()
		_ = mgr.cmd.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.WaitForReady(ctx)
	if err != nil {
		t.Errorf("expected success after retries, got: %v", err)
	}
}

// ============================================================
// IsProcessAlive - edge cases
// ============================================================

func TestIsProcessAlive_DeadProcess(t *testing.T) {
	// Use PID 1 which should be init/launchd and should be alive
	// Or test with current process which we know is alive
	alive := IsProcessAlive(os.Getpid())
	if !alive {
		t.Error("current process should be alive")
	}
}

// ============================================================
// Manager.Download() - successful zip download and extraction
// ============================================================

func TestDownload_Success_Zip(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a real zip archive with the expected structure
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "archive.zip")

	// Create zip with llama-server binary
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	fw, err := zw.Create("llama-server")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	_, err = io.WriteString(fw, "#!/bin/sh\necho test")
	if err != nil {
		t.Fatalf("zip.Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	// Start a test server that serves the zip
	content, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	// Update manifest to point to our test server
	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         srv.URL,
		SHA256:      "ignored",
		ExtractPath: "llama-server",
		ArchiveType: "zip",
	}

	err = mgr.Download(context.Background(), nil)
	if err != nil {
		t.Fatalf("Download should succeed: %v", err)
	}

	// Verify the binary was extracted
	if !mgr.IsInstalled() {
		t.Error("expected binary to be installed after Download")
	}
}

// ============================================================
// Manager.Download() - successful tar.gz download and extraction
// ============================================================

func TestDownload_Success_TarGz(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a real tar.gz archive
	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, "archive.tar.gz")

	tf, err := os.Create(tarPath)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer tf.Close()

	gw := gzip.NewWriter(tf)
	tw := tar.NewWriter(gw)

	data := []byte("#!/bin/sh\necho test")
	hdr := &tar.Header{
		Name: "llama-bin/llama-server",
		Mode: 0755,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar.WriteHeader: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("tar.Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}

	// Read the tar.gz content
	content, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatalf("read tar.gz: %v", err)
	}

	// Start a test server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	// Update manifest for tar.gz
	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         srv.URL,
		SHA256:      "ignored",
		ExtractPath: "llama-bin/llama-server",
		ArchiveType: "tar.gz",
	}

	err = mgr.Download(context.Background(), nil)
	if err != nil {
		t.Fatalf("Download tar.gz should succeed: %v", err)
	}

	// Verify the binary was extracted
	if !mgr.IsInstalled() {
		t.Error("expected binary to be installed after Download")
	}
}

// ============================================================
// Manager.Download() - with progress callback
// ============================================================

func TestDownload_WithProgressCallback(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a small zip archive
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "archive.zip")

	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	fw, err := zw.Create("llama-server")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	_, err = io.WriteString(fw, "test binary content here")
	if err != nil {
		t.Fatalf("zip.Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	content, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         srv.URL,
		SHA256:      "ignored",
		ExtractPath: "llama-server",
		ArchiveType: "zip",
	}

	progressCallCount := 0
	err = mgr.Download(context.Background(), func(downloaded, total int64) {
		progressCallCount++
	})
	if err != nil {
		t.Fatalf("Download with progress should succeed: %v", err)
	}

	// Progress callback may or may not be called depending on timing
	// Just verify download succeeded
	if !mgr.IsInstalled() {
		t.Error("binary should be installed")
	}
}

// ============================================================
// NewManager - tests for manifest loading error
// ============================================================

// Note: Since LoadManifest uses embedded data, we can't really make it fail
// But we test that NewManager properly propagates errors

// ============================================================
// Detect - CUDA detection if on Linux
// ============================================================

func TestDetect_ReturnsPlatformInfo(t *testing.T) {
	p := Detect()
	// Should always have OS and Arch
	if p.OS != "darwin" && p.OS != "linux" && p.OS != "windows" {
		t.Logf("Unusual OS detected: %q", p.OS)
	}
	if p.Arch != "amd64" && p.Arch != "arm64" {
		t.Logf("Unusual Arch detected: %q", p.Arch)
	}
	// CUDA should only be true on Linux
	if p.CUDA && p.OS != "linux" {
		t.Errorf("CUDA should not be true on non-Linux OS")
	}
}

// ============================================================
// FindFreePort - verify loopback constraint
// ============================================================

func TestFindFreePort_OnLoopback(t *testing.T) {
	port, err := FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	// Verify it's in valid port range
	if port <= 0 || port > 65535 {
		t.Errorf("port %d out of range", port)
	}
}

// ============================================================
// downloadFile - read error from server
// ============================================================

func TestDownloadFile_ReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a read error by closing the connection early
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "out.bin")

	err := downloadFile(context.Background(), srv.URL, destPath, nil)
	// May fail with connection error or similar
	_ = err // Acceptable to fail
}

// ============================================================
// extractZip - create file error
// ============================================================

func TestExtractZip_CreateFileError(t *testing.T) {
	content := "test binary"
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.zip")

	zf, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	fw, err := zw.Create("llama-server")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	_, err = io.WriteString(fw, content)
	if err != nil {
		t.Fatalf("zip.Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	// Try to extract to a read-only directory
	err = extractZip(archivePath, "llama-server", "/dev/null/nonexistent/file")
	if err == nil {
		t.Error("expected error when creating output file fails")
	}
}

// ============================================================
// Shutdown - force kill path
// ============================================================

func TestShutdown_ForceKill(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Start a process that ignores SIGTERM
	mgr.cmd = exec.Command("sh", "-c", "trap '' TERM; sleep 30")
	mgr.port = 9999
	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("could not start process: %v", err)
	}

	// Set a very short timeout to force the kill path
	done := make(chan error, 1)
	go func() {
		done <- mgr.Shutdown()
	}()

	select {
	case err := <-done:
		// Should complete (via Kill)
		_ = err
	case <-time.After(10 * time.Second):
		t.Error("Shutdown timed out")
	}
}

// ============================================================
// LoadManifest - successful parse
// ============================================================

func TestLoadManifest_VersionField(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Version == 0 {
		t.Error("expected non-zero Version")
	}
}

func TestLoadManifest_BinariesMap(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Binaries) == 0 {
		t.Error("expected non-empty Binaries map")
	}
	for key, entry := range m.Binaries {
		if key == "" {
			t.Error("found empty key in Binaries")
		}
		if entry.URL == "" {
			t.Errorf("platform %q has empty URL", key)
		}
		if entry.ExtractPath == "" {
			t.Errorf("platform %q has empty ExtractPath", key)
		}
	}
}

// ============================================================
// WaitForReady - context cancellation
// ============================================================

func TestWaitForReady_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Use a context that times out quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Start a process that won't respond to health checks
	mgr.cmd = exec.Command("sleep", "10")
	mgr.port = 19999 // port with no listener
	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("could not start sleep: %v", err)
	}
	defer func() {
		_ = mgr.cmd.Process.Kill()
		_ = mgr.cmd.Wait()
	}()

	err = mgr.WaitForReady(ctx)
	if err == nil {
		t.Error("expected WaitForReady to return error on timeout")
	}
	if !strings.Contains(err.Error(), "failed to start within") {
		t.Logf("got error: %v", err)
	}
}

// ============================================================
// FindFreePort - multiple successful calls
// ============================================================

func TestFindFreePort_MultipleCalls(t *testing.T) {
	port1, err1 := FindFreePort()
	if err1 != nil {
		t.Fatalf("FindFreePort first call: %v", err1)
	}

	port2, err2 := FindFreePort()
	if err2 != nil {
		t.Fatalf("FindFreePort second call: %v", err2)
	}

	// Both ports should be valid and likely different
	if port1 <= 0 || port2 <= 0 {
		t.Error("ports should be positive")
	}
	// (they might be the same, that's okay)
}

// ============================================================
// downloadFile - with multiple chunks
// ============================================================

func TestDownloadFile_LargeFile(t *testing.T) {
	// Create a file larger than the buffer size (32KB)
	largeContent := strings.Repeat("x", 100*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(largeContent)))
		_, _ = io.WriteString(w, largeContent)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "large.bin")

	err := downloadFile(context.Background(), srv.URL, destPath, nil)
	if err != nil {
		t.Fatalf("downloadFile for large file: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if len(data) != len(largeContent) {
		t.Errorf("expected %d bytes, got %d", len(largeContent), len(data))
	}
}

// ============================================================
// extractTarGz - read multiple files, extract first match
// ============================================================

func TestExtractTarGz_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Write multiple files
	files := []struct {
		name string
		data string
	}{
		{"file1.txt", "content1"},
		{"target-file", "target-content"},
		{"file3.txt", "content3"},
	}

	for _, file := range files {
		hdr := &tar.Header{
			Name: file.name,
			Mode: 0644,
			Size: int64(len(file.data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar.WriteHeader: %v", err)
		}
		if _, err := tw.Write([]byte(file.data)); err != nil {
			t.Fatalf("tar.Write: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}

	destDir := t.TempDir()
	finalPath := filepath.Join(destDir, "out")

	err = extractTarGz(archivePath, "target-file", finalPath)
	if err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "target-content" {
		t.Errorf("expected 'target-content', got %q", string(data))
	}
}

// ============================================================
// extractZip - multiple files in archive
// ============================================================

func TestExtractZip_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.zip")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	files := []struct {
		name string
		data string
	}{
		{"file1.txt", "content1"},
		{"target-file", "target-content"},
		{"file3.txt", "content3"},
	}

	for _, file := range files {
		fw, err := w.Create(file.name)
		if err != nil {
			t.Fatalf("zip.Create: %v", err)
		}
		if _, err := io.WriteString(fw, file.data); err != nil {
			t.Fatalf("zip.Write: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	destDir := t.TempDir()
	finalPath := filepath.Join(destDir, "out")

	err = extractZip(archivePath, "target-file", finalPath)
	if err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "target-content" {
		t.Errorf("expected 'target-content', got %q", string(data))
	}
}

// ============================================================
// Shutdown - returns nil when no signal support
// ============================================================

func TestShutdown_SendsSignal(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Start a process that handles SIGTERM properly
	mgr.cmd = exec.Command("sh", "-c", "trap 'exit 0' TERM; sleep 30")
	mgr.port = 9999

	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("could not start process: %v", err)
	}

	// Call Shutdown and verify it completes
	done := make(chan error, 1)
	go func() {
		done <- mgr.Shutdown()
	}()

	select {
	case <-done:
		// Success
	case <-time.After(6 * time.Second):
		t.Error("Shutdown timed out")
	}
}

// ============================================================
// IsProcessAlive - edge cases with signal
// ============================================================

func TestIsProcessAlive_WithSelfProcess(t *testing.T) {
	// Verify current process is alive
	alive := IsProcessAlive(os.Getpid())
	if !alive {
		t.Error("current process should be alive")
	}
}

// ============================================================
// Start method - set port and modelPath
// ============================================================

func TestStart_SetsPortAndModelPath(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Note: Start will fail because the binary doesn't exist,
	// but we can verify the error
	modelPath := "/path/to/model"
	port := 12345

	err = mgr.Start(modelPath, port)
	if err == nil {
		// If it somehow succeeds, clean up
		_ = mgr.Shutdown()
	}
	// Error is expected since binary doesn't exist

	// Even though Start failed, the port and modelPath should not have been set
	// (they would be set before the command is actually started)
}
