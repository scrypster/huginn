package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// makeTarGzBytes builds an in-memory tar.gz with a single "huginn" entry.
func makeTarGzBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "huginn",
		Size: int64(len(content)),
		Mode: 0755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// sha256Hex returns the lower-case hex SHA256 of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// noopDetach satisfies the Upgrader.DetachStart field without actually starting a process.
func noopDetach(cmd *exec.Cmd) error { return nil }

// noopStop satisfies the Upgrader.StopProcess field.
func noopStop(pid int, pidPath string) error { return nil }

// ─── parseSemver ──────────────────────────────────────────────────────────────

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input         string
		maj, min, pat int
		pre           string
		ok            bool
	}{
		{"v1.2.3", 1, 2, 3, "", true},
		{"1.2.3", 1, 2, 3, "", true},
		{"v0.0.0", 0, 0, 0, "", true},
		{"v1.2.3-rc.1", 1, 2, 3, "rc.1", true},
		{"v1.2.3+build.123", 1, 2, 3, "", true},
		{"v1.2.3-beta.1+build", 1, 2, 3, "beta.1", true},
		{"v10.20.30", 10, 20, 30, "", true},
		// invalid
		{"", 0, 0, 0, "", false},
		{"v1.2", 0, 0, 0, "", false},
		{"vX.Y.Z", 0, 0, 0, "", false},
		{"1.2.x", 0, 0, 0, "", false},
	}
	for _, tt := range tests {
		maj, min, pat, pre, ok := parseSemver(tt.input)
		if ok != tt.ok || maj != tt.maj || min != tt.min || pat != tt.pat || pre != tt.pre {
			t.Errorf("parseSemver(%q) = (%d,%d,%d,%q,%v), want (%d,%d,%d,%q,%v)",
				tt.input, maj, min, pat, pre, ok,
				tt.maj, tt.min, tt.pat, tt.pre, tt.ok)
		}
	}
}

// ─── newerVersionAvailable ────────────────────────────────────────────────────

func TestNewerVersionAvailable(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		// Normal upgrades
		{"v0.2.0", "v0.3.0", true},
		{"v0.2.0", "v0.2.1", true},
		{"v0.2.0", "v1.0.0", true},
		{"v0.9.99", "v1.0.0", true},
		// Already at or above latest
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.9", false},
		{"v1.0.0", "v0.9.9", false},
		// Pre-release: stable release is newer than same-number RC
		{"v0.3.0-rc.1", "v0.3.0", true},
		{"v0.3.0-beta.1", "v0.3.0", true},
		{"v0.3.0-alpha", "v0.3.0", true},
		// Stable to RC is NOT an upgrade
		{"v0.3.0", "v0.3.0-rc.1", false},
		// dev / empty: never upgrade
		{"dev", "v1.0.0", false},
		{"", "v1.0.0", false},
		{"v0.2.0", "", false},
	}
	for _, tt := range tests {
		got := newerVersionAvailable(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("newerVersionAvailable(%q, %q) = %v, want %v",
				tt.current, tt.latest, got, tt.want)
		}
	}
}

// ─── releaseAssetURL ──────────────────────────────────────────────────────────

func TestReleaseAssetURL(t *testing.T) {
	tests := []struct {
		tag, goos, goarch string
		want              string
	}{
		{"v0.2.0", "darwin", "arm64",
			"https://github.com/scrypster/huginn/releases/download/v0.2.0/huginn_v0.2.0_darwin_arm64.tar.gz"},
		{"v0.2.0", "darwin", "amd64",
			"https://github.com/scrypster/huginn/releases/download/v0.2.0/huginn_v0.2.0_darwin_amd64.tar.gz"},
		{"v0.2.0", "linux", "amd64",
			"https://github.com/scrypster/huginn/releases/download/v0.2.0/huginn_v0.2.0_linux_amd64.tar.gz"},
		{"v0.2.0", "linux", "arm64",
			"https://github.com/scrypster/huginn/releases/download/v0.2.0/huginn_v0.2.0_linux_arm64.tar.gz"},
		{"v0.2.0", "windows", "amd64",
			"https://github.com/scrypster/huginn/releases/download/v0.2.0/huginn_v0.2.0_windows_amd64.zip"},
	}
	for _, tt := range tests {
		got := releaseAssetURL(tt.tag, tt.goos, tt.goarch)
		if got != tt.want {
			t.Errorf("releaseAssetURL(%q,%q,%q):\n  got  %q\n  want %q",
				tt.tag, tt.goos, tt.goarch, got, tt.want)
		}
	}
}

// ─── releaseChecksumURL ───────────────────────────────────────────────────────

func TestReleaseChecksumURL(t *testing.T) {
	got := releaseChecksumURL("v0.2.0")
	want := "https://github.com/scrypster/huginn/releases/download/v0.2.0/huginn_v0.2.0_checksums.txt"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ─── verifySHA256 ─────────────────────────────────────────────────────────────

func TestVerifySHA256(t *testing.T) {
	content := []byte("hello huginn upgrade test")
	expected := sha256Hex(content)

	tmp, err := os.CreateTemp(t.TempDir(), "sha256-*")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Write(content)
	tmp.Close()

	if err := verifySHA256(tmp.Name(), expected); err != nil {
		t.Fatalf("correct SHA: unexpected error: %v", err)
	}
	if err := verifySHA256(tmp.Name(), "deadbeef"); err == nil {
		t.Fatal("wrong SHA: expected mismatch error, got nil")
	}
	if err := verifySHA256("/nonexistent/path", expected); err == nil {
		t.Fatal("missing file: expected error, got nil")
	}
}

// ─── atomicReplace ────────────────────────────────────────────────────────────

func TestAtomicReplace_SameDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.WriteFile(src, []byte("new-content"), 0755)
	os.WriteFile(dst, []byte("old-content"), 0755)

	if err := atomicReplace(src, dst); err != nil {
		t.Fatalf("atomicReplace: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new-content" {
		t.Errorf("dst = %q, want %q", got, "new-content")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("src should be removed after successful replace")
	}
}

func TestAtomicReplace_PreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.WriteFile(src, []byte("new"), 0644)
	os.WriteFile(dst, []byte("old"), 0750)

	if err := atomicReplace(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	// Mode bits only (mask off type bits)
	if info.Mode().Perm() != 0750 {
		t.Errorf("mode = %o, want %o", info.Mode().Perm(), 0750)
	}
}

// ─── isHomebrewInstall path logic ─────────────────────────────────────────────

func TestHomebrewPathDetection(t *testing.T) {
	markers := []string{"/Cellar/", "/opt/homebrew/", "/usr/local/opt/"}
	check := func(path string) bool {
		for _, m := range markers {
			if strings.Contains(path, m) {
				return true
			}
		}
		return false
	}

	homebrewPaths := []string{
		"/opt/homebrew/bin/huginn",
		"/usr/local/opt/huginn/bin/huginn",
		"/usr/local/Cellar/huginn/0.2.0/bin/huginn",
	}
	for _, p := range homebrewPaths {
		if !check(p) {
			t.Errorf("expected Homebrew detection for %q", p)
		}
	}

	nonHomebrewPaths := []string{
		"/usr/local/bin/huginn",
		"/home/user/.local/bin/huginn",
		"/tmp/huginn",
		"/usr/bin/huginn",
	}
	for _, p := range nonHomebrewPaths {
		if check(p) {
			t.Errorf("unexpected Homebrew detection for %q", p)
		}
	}
}

// ─── fetchExpectedSHA ─────────────────────────────────────────────────────────

func TestFetchExpectedSHA(t *testing.T) {
	checksumsBody := "" +
		"abc123def456  huginn_v0.2.1_darwin_arm64.tar.gz\n" +
		"def456abc123  huginn_v0.2.1_linux_amd64.tar.gz\n" +
		"111222333444  huginn_v0.2.1_darwin_amd64.tar.gz\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksumsBody)
	}))
	defer srv.Close()

	u := &Upgrader{HTTPClient: srv.Client()}

	sha, err := u.fetchExpectedSHA(srv.URL, "v0.2.1", "darwin", "arm64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != "abc123def456" {
		t.Errorf("got %q, want %q", sha, "abc123def456")
	}

	sha, err = u.fetchExpectedSHA(srv.URL, "v0.2.1", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "def456abc123" {
		t.Errorf("got %q, want %q", sha, "def456abc123")
	}

	// Missing platform returns error
	_, err = u.fetchExpectedSHA(srv.URL, "v0.2.1", "freebsd", "amd64")
	if err == nil {
		t.Error("expected error for missing platform, got nil")
	}
}

func TestFetchExpectedSHA_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	u := &Upgrader{HTTPClient: srv.Client()}
	_, err := u.fetchExpectedSHA(srv.URL, "v0.2.1", "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got: %v", err)
	}
}

// ─── downloadArchive ──────────────────────────────────────────────────────────

func TestDownloadArchive_Success(t *testing.T) {
	archive := makeTarGzBytes(t, []byte("fake-huginn-binary-content"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
		w.Write(archive)
	}))
	defer srv.Close()

	u := &Upgrader{HTTPClient: srv.Client()}
	archivePath, err := u.downloadArchive(srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(archivePath)

	got, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, archive) {
		t.Errorf("archive content mismatch: got %d bytes, want %d bytes", len(got), len(archive))
	}
}

func TestDownloadArchive_WithProgress(t *testing.T) {
	archive := makeTarGzBytes(t, bytes.Repeat([]byte("x"), 1024))
	var progressCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
		w.Write(archive)
	}))
	defer srv.Close()

	u := &Upgrader{HTTPClient: srv.Client()}
	archivePath, err := u.downloadArchive(srv.URL, func(dl, total int64) {
		progressCalls++
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archivePath)

	if progressCalls == 0 {
		t.Error("expected progress callbacks, got none")
	}
}

func TestDownloadArchive_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	u := &Upgrader{HTTPClient: srv.Client()}
	_, err := u.downloadArchive(srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got: %v", err)
	}
}

// ─── extractBinary ────────────────────────────────────────────────────────────

func TestExtractBinary_Success(t *testing.T) {
	binaryContent := []byte("fake-huginn-binary-content")
	archive := makeTarGzBytes(t, binaryContent)

	// Write archive to a temp file.
	archiveFile, err := os.CreateTemp(t.TempDir(), "huginn-archive-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	archiveFile.Write(archive)
	archiveFile.Close()

	exePath := filepath.Join(t.TempDir(), "huginn")
	tmpPath, err := extractBinary(archiveFile.Name(), exePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(tmpPath)

	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binaryContent) {
		t.Errorf("extracted content mismatch: got %q, want %q", got, binaryContent)
	}
}

func TestExtractBinary_BinaryNotInArchive(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "other-tool", Size: 3, Mode: 0755})
	tw.Write([]byte("xyz"))
	tw.Close()
	gz.Close()

	archiveFile, err := os.CreateTemp(t.TempDir(), "huginn-archive-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	archiveFile.Write(buf.Bytes())
	archiveFile.Close()

	_, err = extractBinary(archiveFile.Name(), t.TempDir()+"/huginn")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestExtractBinary_OversizedEntry(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// Header claiming more than the limit
	tw.WriteHeader(&tar.Header{
		Name: "huginn",
		Size: upgradeMaxBinarySize + 1,
		Mode: 0755,
	})
	tw.Write([]byte("small")) // actual data is tiny
	tw.Close()
	gz.Close()

	archiveFile, err := os.CreateTemp(t.TempDir(), "huginn-archive-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	archiveFile.Write(buf.Bytes())
	archiveFile.Close()

	_, err = extractBinary(archiveFile.Name(), t.TempDir()+"/huginn")
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("expected size limit error, got: %v", err)
	}
}

func TestExtractBinary_CorruptGzip(t *testing.T) {
	archiveFile, err := os.CreateTemp(t.TempDir(), "huginn-archive-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	archiveFile.Write([]byte("this is not gzip data at all"))
	archiveFile.Close()

	_, err = extractBinary(archiveFile.Name(), t.TempDir()+"/huginn")
	if err == nil {
		t.Error("expected error for corrupt gzip, got nil")
	}
}

// TestChecksumVerifiesArchive verifies the checksum is verified against the
// archive (not the extracted binary), matching how the checksums file is generated.
func TestChecksumVerifiesArchive(t *testing.T) {
	binaryContent := []byte("fake-huginn-binary-content")
	archive := makeTarGzBytes(t, binaryContent)

	// SHA256 of the archive (what checksums.txt contains).
	archiveSHA := sha256Hex(archive)

	// Confirm the binary's SHA256 is different from the archive's SHA256.
	binarySHA := sha256Hex(binaryContent)
	if archiveSHA == binarySHA {
		t.Fatal("test setup error: archive and binary have same SHA256")
	}

	// Write archive to file and verify against archive SHA — must succeed.
	archiveFile, err := os.CreateTemp(t.TempDir(), "*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	archiveFile.Write(archive)
	archiveFile.Close()

	if err := verifySHA256(archiveFile.Name(), archiveSHA); err != nil {
		t.Errorf("expected archive checksum to pass, got: %v", err)
	}

	// Verifying with the binary's SHA must fail (the old bug).
	if err := verifySHA256(archiveFile.Name(), binarySHA); err == nil {
		t.Error("expected checksum mismatch when comparing archive against binary SHA, got nil")
	}
}

// ─── progressReader ───────────────────────────────────────────────────────────

func TestProgressReader(t *testing.T) {
	data := []byte("hello world progress test")
	var calls []int64
	pr := &progressReader{
		r:     bytes.NewReader(data),
		total: int64(len(data)),
		fn:    func(dl, total int64) { calls = append(calls, dl) },
	}
	out := make([]byte, len(data))
	n, _ := pr.Read(out)
	if n != len(data) {
		t.Errorf("read %d bytes, want %d", n, len(data))
	}
	if len(calls) == 0 {
		t.Error("expected progress callbacks, got none")
	}
	if calls[len(calls)-1] != int64(len(data)) {
		t.Errorf("last progress value = %d, want %d", calls[len(calls)-1], len(data))
	}
}

// ─── Upgrader.Run integration ─────────────────────────────────────────────────

func TestUpgraderRun_AlreadyUpToDate(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	u := &Upgrader{
		HTTPClient:    http.DefaultClient,
		LatestRelease: func(ctx context.Context) string { return "v0.3.0" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return false, 0 },
	}
	if err := u.Run([]string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpgraderRun_NetworkFailure(t *testing.T) {
	savedVersion := version
	version = "0.2.0"
	defer func() { version = savedVersion }()

	u := &Upgrader{
		HTTPClient:    http.DefaultClient,
		LatestRelease: func(ctx context.Context) string { return "" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return false, 0 },
	}
	err := u.Run([]string{})
	if err == nil {
		t.Error("expected error on network failure, got nil")
	}
}

func TestUpgraderRun_CheckOnlyFlag_UpToDate(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	u := &Upgrader{
		HTTPClient:    http.DefaultClient,
		LatestRelease: func(ctx context.Context) string { return "v0.3.0" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return false, 0 },
	}
	// --check on an already-up-to-date install should return nil (no os.Exit(1))
	if err := u.Run([]string{"--check"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpgraderRun_TargetVersionSkipsAPICall(t *testing.T) {
	savedVersion := version
	version = "0.2.0"
	defer func() { version = savedVersion }()

	apiCalled := false
	u := &Upgrader{
		HTTPClient: http.DefaultClient,
		LatestRelease: func(ctx context.Context) string {
			apiCalled = true
			return "v0.3.0"
		},
		HuginnDirFn:  func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:  func(path string) (bool, int) { return false, 0 },
		StopProcess:  noopStop,
		IsHomebrewFn: func() bool { return false },
	}
	// Expect failure (no real release server) but the API must NOT be called.
	_ = u.Run([]string{"--version", "v0.3.0", "--yes"})
	if apiCalled {
		t.Error("LatestRelease should not be called when --version is specified")
	}
}

// ─── selfUpdateWithURLs full pipeline (mock HTTP) ─────────────────────────────

// TestSelfUpdateWithURLs_DownloadAndChecksumOK drives the entire pipeline through
// a mock server. The "verify binary" step will fail (fake content), but every
// step before it — stop, checksum fetch, download, SHA256 — must succeed.
func TestSelfUpdateWithURLs_DownloadAndChecksumOK(t *testing.T) {
	savedVersion := version
	version = "0.2.0"
	defer func() { version = savedVersion }()

	binaryContent := []byte("fake-huginn-binary")
	archive := makeTarGzBytes(t, binaryContent)
	archiveSHA := sha256Hex(archive) // checksum of the archive, matching how checksums.txt is generated

	checksumLine := fmt.Sprintf("%s  huginn_v0.3.0_linux_amd64.tar.gz\n", archiveSHA)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums") {
			fmt.Fprint(w, checksumLine)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
		w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "huginn")
	os.WriteFile(exePath, []byte("old-binary"), 0755)

	u := &Upgrader{
		HTTPClient:  srv.Client(),
		HuginnDirFn: func() (string, error) { return dir, nil },
		PIDIsLiveFn: func(path string) (bool, int) { return false, 0 },
		StopProcess: noopStop,
		DetachStart: noopDetach,
		ExePath:     exePath,
	}

	err := u.selfUpdateWithURLs(
		"v0.3.0",
		srv.URL+"/archive.tar.gz",
		srv.URL+"/checksums.txt",
		"linux", "amd64",
		runningState{},
	)
	// Expected to fail at verifyHuginnBinary (fake binary can't run `huginn version`).
	// But must NOT fail with a checksum error.
	if err != nil && strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("checksum step failed — test setup error: %v", err)
	}
}

func TestSelfUpdateWithURLs_ChecksumMismatch(t *testing.T) {
	savedVersion := version
	version = "0.2.0"
	defer func() { version = savedVersion }()

	archive := makeTarGzBytes(t, []byte("fake-binary"))
	// Checksum that does NOT match the archive content
	wrongChecksum := "0000000000000000000000000000000000000000000000000000000000000000"
	checksumLine := fmt.Sprintf("%s  huginn_v0.3.0_linux_amd64.tar.gz\n", wrongChecksum)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums") {
			fmt.Fprint(w, checksumLine)
			return
		}
		w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "huginn")
	os.WriteFile(exePath, []byte("old"), 0755)

	u := &Upgrader{
		HTTPClient:  srv.Client(),
		HuginnDirFn: func() (string, error) { return dir, nil },
		PIDIsLiveFn: func(path string) (bool, int) { return false, 0 },
		StopProcess: noopStop,
		ExePath:     exePath,
	}

	err := u.selfUpdateWithURLs("v0.3.0", srv.URL+"/archive.tar.gz", srv.URL+"/checksums.txt", "linux", "amd64", runningState{})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected checksum mismatch error, got: %v", err)
	}

	// Original binary must be unchanged
	got, _ := os.ReadFile(exePath)
	if string(got) != "old" {
		t.Error("original binary was modified despite checksum failure")
	}
}

func TestSelfUpdateWithURLs_ChecksumsFetch404(t *testing.T) {
	savedVersion := version
	version = "0.2.0"
	defer func() { version = savedVersion }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "huginn")
	os.WriteFile(exePath, []byte("old"), 0755)

	u := &Upgrader{
		HTTPClient:  srv.Client(),
		HuginnDirFn: func() (string, error) { return dir, nil },
		PIDIsLiveFn: func(path string) (bool, int) { return false, 0 },
		StopProcess: noopStop,
		ExePath:     exePath,
	}

	err := u.selfUpdateWithURLs("v0.3.0", srv.URL+"/archive.tar.gz", srv.URL+"/checksums.txt", "linux", "amd64", runningState{})
	if err == nil {
		t.Error("expected error on 404 checksums, got nil")
	}
}

// ─── Run prompt: daemon awareness ─────────────────────────────────────────────

func TestRun_PromptMentionsRunningDaemons(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	var out bytes.Buffer
	u := &Upgrader{
		LatestRelease: func(ctx context.Context) string { return "v0.3.1" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		// Both daemons live — PIDIsLiveFn is called twice (serve, tray)
		PIDIsLiveFn:  func(path string) (bool, int) { return true, 42 },
		StopProcess:  noopStop,
		DetachStart:  noopDetach,
		IsHomebrewFn: func() bool { return false },
		Stdout:       &out,
		Stdin:        strings.NewReader("n\n"), // user aborts
	}
	_ = u.Run([]string{})
	got := out.String()
	if !strings.Contains(got, "server + tray") {
		t.Errorf("prompt missing 'server + tray':\n%s", got)
	}
	if !strings.Contains(got, "will be stopped") {
		t.Errorf("prompt missing restart warning:\n%s", got)
	}
}

func TestRun_PromptOmitsRestartWhenNotRunning(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	var out bytes.Buffer
	u := &Upgrader{
		LatestRelease: func(ctx context.Context) string { return "v0.3.1" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return false, 0 },
		StopProcess:   noopStop,
		DetachStart:   noopDetach,
		IsHomebrewFn:  func() bool { return false },
		Stdout:        &out,
		Stdin:         strings.NewReader("n\n"),
	}
	_ = u.Run([]string{})
	got := out.String()
	if strings.Contains(got, "running") {
		t.Errorf("prompt should not mention 'running' when no daemons:\n%s", got)
	}
}

func TestRun_YesFlagPrintsNoticeWhenRunning(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	var out bytes.Buffer
	// StopProcess returns error so selfUpdateWithURLs bails before network calls.
	u := &Upgrader{
		LatestRelease: func(ctx context.Context) string { return "v0.3.1" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return true, 42 },
		StopProcess:   func(pid int, path string) error { return fmt.Errorf("stop aborted") },
		IsHomebrewFn:  func() bool { return false },
		Stdout:        &out,
		Stdin:         strings.NewReader(""), // must NOT be read
	}
	_ = u.Run([]string{"--yes"})
	got := out.String()
	if !strings.Contains(got, "Note:") {
		t.Errorf("expected 'Note:' line with --yes + running daemons:\n%s", got)
	}
	if !strings.Contains(got, "server + tray") {
		t.Errorf("notice should mention 'server + tray':\n%s", got)
	}
	if strings.Contains(got, "[y/N]") {
		t.Errorf("should not show interactive prompt with --yes:\n%s", got)
	}
}

func TestRun_YesFlagSilentWhenNotRunning(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	var out bytes.Buffer
	// Use a client that can't reach github.com to avoid real network calls.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	u := &Upgrader{
		HTTPClient:    srv.Client(),
		LatestRelease: func(ctx context.Context) string { return "v0.3.1" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return false, 0 },
		StopProcess:   noopStop,
		DetachStart:   noopDetach,
		IsHomebrewFn:  func() bool { return false },
		Stdout:        &out,
		Stdin:         strings.NewReader(""),
	}
	_ = u.Run([]string{"--yes"})
	got := out.String()
	if strings.Contains(got, "Note:") {
		t.Errorf("should not print Note: when no daemons running:\n%s", got)
	}
}
