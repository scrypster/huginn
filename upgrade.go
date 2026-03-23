package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

const (
	upgradeHTTPTimeout = 120 * time.Second
	// upgradeMaxBinarySize caps how many bytes are written from a tar entry before
	// the SHA256 check. This bounds disk use if the archive is corrupted or tampered.
	upgradeMaxBinarySize = 500 << 20 // 500 MB
)

// Upgrader holds injectable dependencies so every codepath is unit-testable.
type Upgrader struct {
	HTTPClient    *http.Client
	LatestRelease func(ctx context.Context) string   // defaults to latestRelease()
	HuginnDirFn   func() (string, error)             // defaults to huginnDir()
	PIDIsLiveFn   func(path string) (bool, int)      // defaults to pidFileIsLive()
	ExePath       string                              // if empty, resolved from os.Executable
	StopProcess   func(pid int, pidPath string) error // platform-specific
	DetachStart   func(cmd *exec.Cmd) error           // platform-specific
}

func defaultUpgrader() *Upgrader {
	return &Upgrader{
		HTTPClient:    &http.Client{Timeout: upgradeHTTPTimeout},
		LatestRelease: latestRelease,
		HuginnDirFn:   huginnDir,
		PIDIsLiveFn:   pidFileIsLive,
		StopProcess:   platformStopProcess,
		DetachStart:   platformDetachStart,
	}
}

// cmdUpgrade is the entry point for `huginn upgrade`.
func cmdUpgrade(args []string) error {
	return defaultUpgrader().Run(args)
}

// Run executes the upgrade workflow.
func (u *Upgrader) Run(args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	checkOnly     := fs.Bool("check",   false, "check for updates only; exit 1 if update available")
	yes           := fs.Bool("yes",     false, "skip confirmation prompt")
	targetVersion := fs.String("version", "", "upgrade to a specific version tag (e.g. v0.3.0)")
	force         := fs.Bool("force",   false, "reinstall even if already at the target version")
	fs.BoolVar(yes, "y", false, "shorthand for --yes")
	if err := fs.Parse(args); err != nil {
		return err
	}

	current := version
	fmt.Printf("\n  huginn upgrade\n")
	fmt.Printf("  current: %s\n\n", current)

	var latest string
	if *targetVersion != "" {
		latest = *targetVersion
		if !strings.HasPrefix(latest, "v") {
			latest = "v" + latest
		}
	} else {
		fmt.Printf("  Checking for updates...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		latest = u.LatestRelease(ctx)
		if latest == "" {
			fmt.Println(" ✗")
			return fmt.Errorf("could not reach GitHub releases API — check your network connection")
		}
		fmt.Println(" ✓")
	}

	if !*force && !newerVersionAvailable(current, latest) {
		fmt.Printf("\n  huginn is up to date (%s)\n\n", current)
		return nil
	}

	fmt.Printf("\n  Update available: %s → %s\n", current, latest)
	fmt.Printf("  Release notes: https://github.com/scrypster/huginn/releases/tag/%s\n\n", latest)

	if *checkOnly {
		os.Exit(1) // signals: update available (for CI/scripting)
	}

	// Windows: cannot self-replace a running executable; open browser instead.
	if goruntime.GOOS == "windows" {
		url := fmt.Sprintf("https://github.com/scrypster/huginn/releases/tag/%s", latest)
		fmt.Printf("  Opening release page: %s\n", url)
		_ = exec.Command("cmd", "/c", "start", url).Start()
		return nil
	}

	// Homebrew install: delegate to brew rather than self-updating.
	if isHomebrewInstall() {
		return u.upgradeViaHomebrew()
	}

	if !*yes {
		fmt.Printf("  Install %s? [y/N] ", latest)
		var resp string
		fmt.Scanln(&resp)
		if strings.ToLower(strings.TrimSpace(resp)) != "y" {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	return u.selfUpdate(latest)
}

// selfUpdate is a thin wrapper that resolves URLs for the current platform
// and delegates to selfUpdateWithURLs.
func (u *Upgrader) selfUpdate(latest string) error {
	return u.selfUpdateWithURLs(
		latest,
		releaseAssetURL(latest, goruntime.GOOS, goruntime.GOARCH),
		releaseChecksumURL(latest),
		goruntime.GOOS,
		goruntime.GOARCH,
	)
}

// selfUpdateWithURLs is the testable core of selfUpdate. It accepts explicit
// asset and checksum URLs so tests can point at a mock HTTP server.
func (u *Upgrader) selfUpdateWithURLs(latest, assetURL, checksumURL, goos, goarch string) error {
	huginnHome, err := u.HuginnDirFn()
	if err != nil {
		return fmt.Errorf("huginn home: %w", err)
	}

	servePIDPath := filepath.Join(huginnHome, "serve.pid")
	trayPIDPath  := filepath.Join(huginnHome, "tray.pid")
	serveRunning, servePID := u.PIDIsLiveFn(servePIDPath)
	trayRunning,  trayPID  := u.PIDIsLiveFn(trayPIDPath)

	// Stop daemons before touching the binary.
	if serveRunning {
		if err := upgradeStep("Stopping server...", func() error {
			return u.StopProcess(servePID, servePIDPath)
		}); err != nil {
			return err
		}
	}
	if trayRunning {
		if err := upgradeStep("Stopping tray...", func() error {
			return u.StopProcess(trayPID, trayPIDPath)
		}); err != nil {
			return err
		}
	}

	// Resolve current binary path.
	exePath := u.ExePath
	if exePath == "" {
		exePath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("locate binary: %w", err)
		}
		exePath, err = filepath.EvalSymlinks(exePath)
		if err != nil {
			return fmt.Errorf("resolve symlink: %w", err)
		}
	}

	// Fetch checksum file first (small, fast) so we can verify the archive.
	// NOTE: The checksums file itself is not GPG/cosign signed — it is fetched
	// over HTTPS (TLS). A compromised GitHub CDN or TLS MITM could serve a
	// matching checksum for a malicious binary. A future improvement is to add
	// cosign or GPG signing via goreleaser. Tracked as a follow-up.
	var expectedSHA string
	if err := upgradeStep("Fetching checksums...", func() error {
		var e error
		expectedSHA, e = u.fetchExpectedSHA(checksumURL, latest, goos, goarch)
		return e
	}); err != nil {
		return err
	}

	// Download archive with inline progress.
	var tmpPath string
	label := fmt.Sprintf("Downloading %s...", latest)
	fmt.Printf("  %-32s", label)
	tmpPath, err = u.downloadAndExtract(assetURL, exePath, func(dl, total int64) {
		if total > 0 {
			fmt.Printf("\r  %-32s%.1f / %.1f MB", label,
				float64(dl)/1024/1024, float64(total)/1024/1024)
		}
	})
	if err != nil {
		fmt.Println(" ✗")
		return err
	}
	fmt.Println(" ✓")

	// Ensure temp file is removed on any error after this point.
	tmpCleanup := tmpPath
	defer func() {
		if tmpCleanup != "" {
			os.Remove(tmpCleanup)
		}
	}()

	// SHA256 check BEFORE executing the binary (primary integrity gate).
	if err := upgradeStep("Verifying checksum...", func() error {
		return verifySHA256(tmpPath, expectedSHA)
	}); err != nil {
		return err
	}

	// Secondary sanity check: run `huginn version` to confirm it starts.
	if err := upgradeStep("Verifying binary...", func() error {
		if err := os.Chmod(tmpPath, 0755); err != nil {
			return err
		}
		return verifyHuginnBinary(tmpPath, latest)
	}); err != nil {
		return err
	}

	// Atomic install — rename with cross-device fallback.
	if err := upgradeStep("Installing...", func() error {
		return atomicReplace(tmpPath, exePath)
	}); err != nil {
		return err
	}
	tmpCleanup = "" // binary is installed; don't delete it

	// macOS: re-sign the new binary so the next launch doesn't need to re-exec.
	if goos == "darwin" {
		_ = upgradeStep("Signing binary (macOS)...", func() error {
			return exec.Command("codesign", "--force", "--sign", "-", exePath).Run()
		})
	}

	// Restart daemons — failures are warnings, not fatal.
	if serveRunning {
		if err := upgradeStep("Restarting server...", func() error {
			cmd := exec.Command(exePath, "serve")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
			return u.DetachStart(cmd)
		}); err != nil {
			fmt.Printf("\n  Warning: server did not restart: %v\n  Run: huginn serve\n\n", err)
		}
	}
	if trayRunning {
		if err := upgradeStep("Restarting tray...", func() error {
			cmd := exec.Command(exePath, "tray")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
			return u.DetachStart(cmd)
		}); err != nil {
			fmt.Printf("\n  Warning: tray did not restart: %v\n  Run: huginn tray\n\n", err)
		}
	}

	fmt.Printf("\n  huginn updated to %s\n\n", latest)
	return nil
}

// releaseAssetURL returns the GitHub download URL for the platform archive.
func releaseAssetURL(tag, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf(
		"https://github.com/scrypster/huginn/releases/download/%s/huginn_%s_%s_%s.%s",
		tag, tag, goos, goarch, ext,
	)
}

// releaseChecksumURL returns the URL for the SHA256 checksums file.
func releaseChecksumURL(tag string) string {
	return fmt.Sprintf(
		"https://github.com/scrypster/huginn/releases/download/%s/huginn_%s_checksums.txt",
		tag, tag,
	)
}

// fetchExpectedSHA downloads the checksums file and returns the SHA256 for the
// archive matching goos/goarch.
func (u *Upgrader) fetchExpectedSHA(checksumURL, tag, goos, goarch string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "huginn/"+version)
	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums HTTP %d for %s", resp.StatusCode, checksumURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	// Each line: "<sha256>  huginn_vX.Y.Z_GOOS_GOARCH.tar.gz"
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	target := fmt.Sprintf("huginn_%s_%s_%s.%s", tag, goos, goarch, ext)
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == target {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum found for %s in checksums file", target)
}

// verifySHA256 computes the SHA256 of the file at path and compares to expected.
func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("checksum mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
	return nil
}

// downloadAndExtract downloads the release tar.gz and extracts the huginn binary
// to a temp file. progressFn is called with (bytesDownloaded, totalBytes).
func (u *Upgrader) downloadAndExtract(assetURL, exePath string, progressFn func(dl, total int64)) (string, error) {
	req, err := http.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "huginn/"+version)

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, assetURL)
	}

	pr := &progressReader{r: resp.Body, total: resp.ContentLength, fn: progressFn}
	gz, err := gzip.NewReader(pr)
	if err != nil {
		return "", fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}
		if filepath.Base(hdr.Name) != "huginn" {
			continue
		}
		// Guard: reject entries that claim an unreasonable size.
		if hdr.Size > upgradeMaxBinarySize {
			return "", fmt.Errorf("archive entry size %d exceeds limit (%d bytes)", hdr.Size, upgradeMaxBinarySize)
		}

		// Prefer a temp file adjacent to the install dir for atomic rename later.
		dir := filepath.Dir(exePath)
		tmp, err := os.CreateTemp(dir, ".huginn-upgrade-*")
		if err != nil {
			// Fallback to system temp dir (atomicReplace handles cross-device).
			tmp, err = os.CreateTemp("", ".huginn-upgrade-*")
			if err != nil {
				return "", fmt.Errorf("temp file: %w", err)
			}
		}
		tmpPath := tmp.Name()
		// Copy with a hard cap — LimitReader caps bytes read, not just bytes declared.
		_, copyErr := io.Copy(tmp, io.LimitReader(tr, upgradeMaxBinarySize))
		tmp.Close()
		if copyErr != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("write binary: %w", copyErr)
		}
		return tmpPath, nil
	}
	return "", fmt.Errorf("binary 'huginn' not found in archive — wrong platform?")
}

// atomicReplace replaces dst with src atomically. Falls back to copy+rename
// when src and dst are on different filesystems (EXDEV). Original permissions
// of dst are preserved in both code paths.
func atomicReplace(src, dst string) error {
	// Capture dst permissions before we replace it.
	var origMode os.FileMode = 0755
	if info, err := os.Stat(dst); err == nil {
		origMode = info.Mode().Perm()
	}

	if err := os.Rename(src, dst); err == nil {
		// Apply original permissions to the newly installed binary.
		_ = os.Chmod(dst, origMode)
		return nil
	}

	// Cross-device fallback: write to a temp file in the same directory as dst,
	// then rename within the same filesystem.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".huginn-install-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmp.Name(), origMode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	os.Remove(src)
	return nil
}

// isHomebrewInstall reports whether the current binary is managed by Homebrew.
func isHomebrewInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	for _, marker := range []string{"/Cellar/", "/opt/homebrew/", "/usr/local/opt/"} {
		if strings.Contains(exe, marker) {
			return true
		}
	}
	return false
}

func (u *Upgrader) upgradeViaHomebrew() error {
	fmt.Println("  Homebrew install detected — running brew upgrade...")
	cmd := exec.Command("brew", "upgrade", "scrypster/tap/huginn")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// verifyHuginnBinary runs `<path> version` and confirms it contains the expected
// version string. This is a secondary sanity check; SHA256 is the primary gate.
func verifyHuginnBinary(path, expectedTag string) error {
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return fmt.Errorf("version check failed: %w", err)
	}
	want := strings.TrimPrefix(expectedTag, "v")
	if !strings.Contains(string(out), want) {
		return fmt.Errorf("version mismatch: output %q does not contain %q",
			strings.TrimSpace(string(out)), want)
	}
	return nil
}

// upgradeStep prints a labeled step, runs f(), and prints ✓ or ✗.
func upgradeStep(label string, f func() error) error {
	fmt.Printf("  %-32s", label)
	if err := f(); err != nil {
		fmt.Printf(" ✗\n  Error: %v\n", err)
		return err
	}
	fmt.Println(" ✓")
	return nil
}

// newerVersionAvailable returns true when latest is newer than current.
// A stable release is considered newer than the same version with a pre-release suffix
// (e.g. v0.3.0 is "newer" than v0.3.0-rc.1).
func newerVersionAvailable(current, latest string) bool {
	if current == "" || latest == "" || current == "dev" {
		return false
	}
	cMaj, cMin, cPat, cPre, ok1 := parseSemver(current)
	lMaj, lMin, lPat, lPre, ok2 := parseSemver(latest)
	if !ok1 || !ok2 {
		return false
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	if lPat != cPat {
		return lPat > cPat
	}
	// Same numeric version: stable (empty pre) is "newer" than a pre-release.
	return cPre != "" && lPre == ""
}

// parseSemver parses "vX.Y.Z[-prerelease][+build]" into its components.
func parseSemver(s string) (maj, min, pat int, pre string, ok bool) {
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // strip build metadata
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, "", false
	}
	var err error
	if maj, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, "", false
	}
	if min, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, "", false
	}
	if pat, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, "", false
	}
	return maj, min, pat, pre, true
}

// latestRelease queries the GitHub Releases API and returns the latest tag name
// (e.g. "v0.4.0"), or "" on any error or timeout.
func latestRelease(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/scrypster/huginn/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "huginn/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	// Parse minimal JSON: just extract "tag_name".
	tag := extractJSONString(string(body), "tag_name")
	return tag
}

// extractJSONString is a minimal JSON string extractor for a single named key.
// Avoids importing encoding/json for a 2-field response.
func extractJSONString(body, key string) string {
	needle := `"` + key + `":"`
	idx := strings.Index(body, needle)
	if idx < 0 {
		return ""
	}
	start := idx + len(needle)
	end := strings.IndexByte(body[start:], '"')
	if end < 0 {
		return ""
	}
	return body[start : start+end]
}

// pidFileIsLive reads a PID file at path and reports whether the contained
// process is still running. Returns (alive bool, pid int).
func pidFileIsLive(path string) (bool, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, 0
	}
	pidStr := strings.TrimSpace(string(data))
	// PID files may contain "PID PORT" — only the first field is the PID.
	if i := strings.IndexByte(pidStr, ' '); i >= 0 {
		pidStr = pidStr[:i]
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false, 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, 0
	}
	// On Unix, FindProcess always succeeds; we must send signal 0 to check liveness.
	if err := proc.Signal(os.Signal(nil)); err != nil {
		return false, 0
	}
	return true, pid
}

type progressReader struct {
	r     io.Reader
	read  int64
	total int64
	fn    func(dl, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.fn != nil {
		p.fn(p.read, p.total)
	}
	return n, err
}
