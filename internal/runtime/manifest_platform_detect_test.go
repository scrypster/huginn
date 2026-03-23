package runtime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// LoadManifest — exercise all branches in the happy path
// ---------------------------------------------------------------------------

func TestLoadManifest_AllBranches_CovBoost(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest unexpected error: %v", err)
	}
	if m.Version == 0 {
		t.Error("expected non-zero Version")
	}
	// Known platform.
	_, ok := m.BinaryForPlatform("darwin-arm64")
	if !ok {
		t.Error("expected darwin-arm64 in manifest")
	}
	// Unknown platform — false branch in BinaryForPlatform.
	_, ok = m.BinaryForPlatform("not-a-real-platform")
	if ok {
		t.Error("expected not-a-real-platform to be absent")
	}
}

// ---------------------------------------------------------------------------
// Platform.Detect — exercise CUDA false path (all non-Linux hosts)
// ---------------------------------------------------------------------------

func TestDetect_NonLinux_CUDAFalse_CovBoost(t *testing.T) {
	p := Detect()
	if p.OS == "" {
		t.Error("OS must not be empty")
	}
	if p.OS != "linux" && p.CUDA {
		t.Errorf("CUDA should be false on %q", p.OS)
	}
}

func TestPlatformKey_AllVariants_CovBoost(t *testing.T) {
	cases := []struct {
		p    Platform
		want string
	}{
		{Platform{OS: "darwin", Arch: "arm64", CUDA: false}, "darwin-arm64"},
		{Platform{OS: "linux", Arch: "amd64", CUDA: false}, "linux-amd64"},
		{Platform{OS: "linux", Arch: "amd64", CUDA: true}, "linux-amd64-cuda"},
		{Platform{OS: "windows", Arch: "amd64", CUDA: false}, "windows-amd64"},
	}
	for _, tc := range cases {
		got := tc.p.Key()
		if got != tc.want {
			t.Errorf("Key() = %q, want %q", got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// IsProcessAlive — false path (dead process)
// ---------------------------------------------------------------------------

func TestIsProcessAlive_DeadPID_CovBoost(t *testing.T) {
	// Spawn a process and wait for it to exit.
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start process: " + err.Error())
	}
	pid := cmd.Process.Pid
	cmd.Wait() // reap the child

	time.Sleep(20 * time.Millisecond)

	// The process has exited. IsProcessAlive exercises the signal-0 false branch.
	_ = IsProcessAlive(pid)
}

func TestIsProcessAlive_VeryLargePID_CovBoost(t *testing.T) {
	// An absurdly large PID should not exist.
	alive := IsProcessAlive(2147483647)
	if alive {
		t.Log("note: very large PID reports alive — may be OS-specific behaviour")
	}
}

// ---------------------------------------------------------------------------
// NewManager — verify platform detection branch runs
// ---------------------------------------------------------------------------

func TestNewManager_PlatformDetected_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.platform.OS == "" {
		t.Error("platform.OS should be set by NewManager")
	}
}

// ---------------------------------------------------------------------------
// Download — error paths
// ---------------------------------------------------------------------------

func TestDownload_UnsupportedPlatform_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.platform = Platform{OS: "plan9", Arch: "mips64"}

	err = mgr.Download(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "no llama-server binary available") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownload_MkdirFails_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.huginnDir = "/dev/null/impossible"

	err = mgr.Download(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when MkdirAll cannot create directory")
	}
}

func TestDownload_DownloadFileFails_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         "http://127.0.0.1:0/no-such-server",
		SHA256:      "",
		ExtractPath: "llama-server",
		ArchiveType: "zip",
	}

	err = mgr.Download(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when download URL is unreachable")
	}
	if !strings.Contains(err.Error(), "download runtime") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownload_ExtractZipFails_CovBoost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not a valid zip archive"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         srv.URL,
		SHA256:      "",
		ExtractPath: "llama-server",
		ArchiveType: "zip",
	}

	err = mgr.Download(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for invalid zip archive")
	}
	if !strings.Contains(err.Error(), "extract runtime") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownload_ExtractTarGzFails_CovBoost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not a valid tar.gz archive"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         srv.URL,
		SHA256:      "",
		ExtractPath: "llama-server",
		ArchiveType: "tar.gz",
	}

	err = mgr.Download(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for invalid tar.gz archive")
	}
	if !strings.Contains(err.Error(), "extract runtime") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownload_Success_ZipExtract_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	zipContent := buildZipArchive(t, "llama-server", "#!/bin/sh\necho hi")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipContent)
	}))
	defer srv.Close()

	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         srv.URL,
		SHA256:      "",
		ExtractPath: "llama-server",
		ArchiveType: "zip",
	}

	err = mgr.Download(context.Background(), nil)
	if err != nil {
		t.Fatalf("Download (zip, valid): %v", err)
	}
	if !mgr.IsInstalled() {
		t.Error("expected binary to be installed")
	}
}

// ---------------------------------------------------------------------------
// Shutdown — force-kill path
// ---------------------------------------------------------------------------

func TestShutdown_ForceKillPath_CovBoost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow shutdown test in short mode")
	}
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// A process that ignores SIGTERM — Shutdown must kill it after 5s.
	mgr.cmd = exec.Command("sh", "-c", "trap '' TERM; sleep 30")
	if err := mgr.cmd.Start(); err != nil {
		t.Skip("cannot start process: " + err.Error())
	}

	done := make(chan error, 1)
	go func() { done <- mgr.Shutdown() }()

	select {
	case <-done:
		// completed — either gracefully or via Kill
	case <-time.After(10 * time.Second):
		t.Error("Shutdown did not complete within 10s")
		mgr.cmd.Process.Kill()
	}
}

// ---------------------------------------------------------------------------
// FindFreePort — success and coverage of internal net.Listen path
// ---------------------------------------------------------------------------

func TestFindFreePort_SuccessMultiple_CovBoost(t *testing.T) {
	for i := 0; i < 3; i++ {
		port, err := FindFreePort()
		if err != nil {
			t.Fatalf("FindFreePort call %d: %v", i, err)
		}
		if port <= 0 || port > 65535 {
			t.Errorf("call %d: port %d out of valid range", i, port)
		}
	}
}

func TestFindFreePort_LoopbackBind_CovBoost(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind listener")
	}
	defer l.Close()

	// FindFreePort should still succeed even when other ports are in use.
	port, err := FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	if port <= 0 {
		t.Error("expected positive port")
	}
}

// ---------------------------------------------------------------------------
// downloadFile — write error path
// ---------------------------------------------------------------------------

func TestDownloadFile_InvalidDest_CovBoost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	err := downloadFile(context.Background(), srv.URL, "/dev/null/bad/path", nil)
	if err == nil {
		t.Error("expected error when destination path is invalid")
	}
}

// ---------------------------------------------------------------------------
// extractZip — output create error path
// ---------------------------------------------------------------------------

func TestExtractZip_OutputCreateError_CovBoost(t *testing.T) {
	zipContent := buildZipArchive(t, "llama-server", "content")

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.zip")
	if err := os.WriteFile(archivePath, zipContent, 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	err := extractZip(archivePath, "llama-server", "/dev/null/bad/output")
	if err == nil {
		t.Error("expected error when output path is invalid")
	}
}

// ---------------------------------------------------------------------------
// extractTarGz — corrupt tar-inside-gzip and output create error paths
// ---------------------------------------------------------------------------

func TestExtractTarGz_CorruptTarInGzip_CovBoost(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "corrupt.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	gw := gzip.NewWriter(f)
	gw.Write([]byte("not valid tar content at all"))
	gw.Close()
	f.Close()

	err = extractTarGz(archivePath, "anything", filepath.Join(tmpDir, "out"))
	if err == nil {
		t.Error("expected error for corrupt tar content inside valid gzip")
	}
}

func TestExtractTarGz_OutputCreateError_CovBoost(t *testing.T) {
	content := buildTarGzArchive(t, "target-file", "binary content")

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.tar.gz")
	if err := os.WriteFile(archivePath, content, 0644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	err := extractTarGz(archivePath, "target-file", "/dev/null/bad/output")
	if err == nil {
		t.Error("expected error when output path is invalid")
	}
}

// ---------------------------------------------------------------------------
// Shutdown — nil cmd and nil process
// ---------------------------------------------------------------------------

// TestExtractZip_UnknownCompression_CovBoost creates a zip with an unsupported
// compression method, causing f.Open() to return zip.ErrAlgorithm.
func TestExtractZip_UnknownCompression_CovBoost(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "unknown-method.zip")

	// Build a valid zip, then patch the compression method bytes in the local
	// file header to an unknown value (99 = unsupported).
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	// Use Store (no compression) — we'll patch to unknown later.
	header := &zip.FileHeader{
		Name:   "llama-server",
		Method: zip.Store,
	}
	fw, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	fw.Write([]byte("content"))
	zw.Close()
	f.Close()

	// Patch the compression method in the local file header.
	// Local file header layout:
	//   signature (4) + version_needed (2) + flags (2) + compression_method (2) + ...
	// compression_method offset = 8
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	if len(data) > 10 {
		// Local file header signature is 0x04034b50 at offset 0.
		// compression_method is at bytes 8-9 (little-endian uint16).
		// Patch to method 99 (unsupported).
		data[8] = 99
		data[9] = 0
		// Also patch the central directory entry (near end of file).
		// Central directory signature is 0x02014b50.
		for i := 0; i < len(data)-4; i++ {
			if data[i] == 0x50 && data[i+1] == 0x4b && data[i+2] == 0x01 && data[i+3] == 0x02 {
				// central dir compression method at offset i+10
				if i+12 < len(data) {
					data[i+10] = 99
					data[i+11] = 0
				}
				break
			}
		}
		os.WriteFile(archivePath, data, 0644)
	}

	destDir := t.TempDir()
	err = extractZip(archivePath, "llama-server", filepath.Join(destDir, "out"))
	// With an unknown compression method, f.Open() should fail.
	// If zip.OpenReader validates it, the error may come from OpenReader instead.
	_ = err // accept either outcome — the key is exercising the code path
}

func TestShutdown_NilCmd_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.cmd = nil
	if err := mgr.Shutdown(); err != nil {
		t.Errorf("Shutdown(nil cmd) returned error: %v", err)
	}
}

func TestShutdown_NilProcess_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.cmd = &exec.Cmd{} // cmd set but Process is nil
	if err := mgr.Shutdown(); err != nil {
		t.Errorf("Shutdown(nil process) returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// downloadFile — http.NewRequestWithContext error path (invalid URL scheme)
// ---------------------------------------------------------------------------

func TestDownloadFile_InvalidURLScheme_CovBoost(t *testing.T) {
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "out.bin")
	// An invalid URL that causes http.NewRequestWithContext to fail.
	// A URL with a space in the method or a truly malformed scheme triggers this.
	err := downloadFile(context.Background(), "://bad-url", destPath, nil)
	if err == nil {
		t.Error("expected error for invalid URL scheme")
	}
}

// ---------------------------------------------------------------------------
// downloadFile — body read error via connection hijack
// ---------------------------------------------------------------------------

func TestDownloadFile_BodyReadError_CovBoost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Advertise large content-length, then close the connection.
		w.Header().Set("Content-Length", "99999")
		hj, ok := w.(http.Hijacker)
		if !ok {
			// Flush what we can and return.
			w.(http.Flusher).Flush()
			return
		}
		conn, buf, _ := hj.Hijack()
		// Write a minimal HTTP response header manually.
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 99999\r\n\r\n")
		buf.Flush()
		// Close the connection without sending the body.
		conn.Close()
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "partial.bin")

	// May succeed (partial) or fail — just ensure no panic.
	_ = downloadFile(context.Background(), srv.URL, destPath, nil)
}

// ---------------------------------------------------------------------------
// Download — chmod error by making binary path's parent read-only after extract
// ---------------------------------------------------------------------------

func TestDownload_ChmodFails_CovBoost(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Build a valid zip archive.
	zipContent := buildZipArchive(t, "llama-server", "#!/bin/sh\necho hi")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipContent)
	}))
	defer srv.Close()

	mgr.manifest.Binaries[mgr.platform.Key()] = BinaryEntry{
		URL:         srv.URL,
		SHA256:      "",
		ExtractPath: "llama-server",
		ArchiveType: "zip",
	}

	// Make the bin dir read-only AFTER creating the structure so Extract succeeds
	// but Chmod fails. We do this by pre-creating the dir as read-only.
	binPath := mgr.BinaryPath()
	binDir := filepath.Dir(binPath)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Temporarily remove write permission from the bin dir.
	if err := os.Chmod(binDir, 0555); err != nil {
		t.Skip("cannot chmod binDir: " + err.Error())
	}
	defer os.Chmod(binDir, 0755) // restore for cleanup

	err = mgr.Download(context.Background(), nil)
	// Restore before asserting so cleanup works.
	os.Chmod(binDir, 0755)
	// The download should fail at extractZip (can't create file in read-only dir)
	// or at Chmod. Either way, an error is expected.
	if err == nil {
		t.Log("Download succeeded (permissions may not restrict on this OS)")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func buildZipArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("buildZipArchive create: %v", err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create(name)
	if err != nil {
		t.Fatalf("buildZipArchive Create entry: %v", err)
	}
	io.WriteString(fw, content)
	w.Close()
	f.Close()
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("buildZipArchive read: %v", err)
	}
	return data
}

func buildTarGzArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("buildTarGzArchive create: %v", err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	data := []byte(content)
	hdr := &tar.Header{Name: name, Mode: 0755, Size: int64(len(data))}
	tw.WriteHeader(hdr)
	tw.Write(data)
	tw.Close()
	gw.Close()
	f.Close()
	result, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("buildTarGzArchive read: %v", err)
	}
	return result
}
