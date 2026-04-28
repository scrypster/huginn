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
	Stdout        io.Writer                           // if nil, defaults to os.Stdout
	Stdin         io.Reader                           // if nil, defaults to os.Stdin
	IsHomebrewFn  func() bool                        // if nil, defaults to isHomebrewInstall
	BrewUpgrade   func(pkg string) error             // if nil, runs exec.Command("brew", "upgrade", pkg)
	VerifyBinary  func(path, tag string) error       // if nil, defaults to verifyHuginnBinary
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

// runningState captures which huginn daemons were live at the start of Run().
// Detected once to avoid a TOCTOU gap between the prompt and the stop calls.
type runningState struct {
	serveRunning bool
	servePID     int
	trayRunning  bool
	trayPID      int
}

func (u *Upgrader) out() io.Writer {
	if u.Stdout != nil {
		return u.Stdout
	}
	return os.Stdout
}

func (u *Upgrader) in() io.Reader {
	if u.Stdin != nil {
		return u.Stdin
	}
	return os.Stdin
}

// step prints a labeled upgrade step to u.out(), runs f(), and prints ✓ or ✗.
func (u *Upgrader) step(label string, f func() error) error {
	fmt.Fprintf(u.out(), "  %-32s", label)
	if err := f(); err != nil {
		fmt.Fprintf(u.out(), " ✗\n  Error: %v\n", err)
		return err
	}
	fmt.Fprintln(u.out(), " ✓")
	return nil
}

// daemonDesc returns a human-readable description of running daemons.
func daemonDesc(state runningState) string {
	switch {
	case state.serveRunning && state.trayRunning:
		return "server + tray"
	case state.serveRunning:
		return "server"
	case state.trayRunning:
		return "tray"
	default:
		return ""
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
	fmt.Fprintf(u.out(), "\n  huginn upgrade\n")
	fmt.Fprintf(u.out(), "  current: %s\n\n", current)

	var latest string
	if *targetVersion != "" {
		latest = *targetVersion
		if !strings.HasPrefix(latest, "v") {
			latest = "v" + latest
		}
	} else {
		fmt.Fprintf(u.out(), "  Checking for updates...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		latest = u.LatestRelease(ctx)
		if latest == "" {
			fmt.Fprintln(u.out(), " ✗")
			return fmt.Errorf("could not reach GitHub releases API — check your network connection")
		}
		fmt.Fprintln(u.out(), " ✓")
	}

	if !*force && !newerVersionAvailable(current, latest) {
		fmt.Fprintf(u.out(), "\n  huginn is up to date (%s)\n\n", current)
		return nil
	}

	fmt.Fprintf(u.out(), "\n  Update available: %s → %s\n", current, latest)
	fmt.Fprintf(u.out(), "  Release notes: https://github.com/scrypster/huginn/releases/tag/%s\n\n", latest)

	if *checkOnly {
		os.Exit(1) // signals: update available (for CI/scripting)
	}

	// Windows: cannot self-replace a running executable; open browser instead.
	if goruntime.GOOS == "windows" {
		url := fmt.Sprintf("https://github.com/scrypster/huginn/releases/tag/%s", latest)
		fmt.Fprintf(u.out(), "  Opening release page: %s\n", url)
		_ = exec.Command("cmd", "/c", "start", url).Start()
		return nil
	}

	// Detect running daemons once to avoid a TOCTOU gap between the prompt and
	// the stop calls. Both self-update and Homebrew paths use this state.
	huginnHome, err := u.HuginnDirFn()
	if err != nil {
		return fmt.Errorf("huginn home: %w", err)
	}
	servePIDPath := filepath.Join(huginnHome, "serve.pid")
	trayPIDPath  := filepath.Join(huginnHome, "tray.pid")
	serveRunning, servePID := u.PIDIsLiveFn(servePIDPath)
	trayRunning,  trayPID  := u.PIDIsLiveFn(trayPIDPath)
	state := runningState{
		serveRunning: serveRunning,
		servePID:     servePID,
		trayRunning:  trayRunning,
		trayPID:      trayPID,
	}

	// Homebrew install: delegate to brew rather than self-updating.
	isHB := u.IsHomebrewFn
	if isHB == nil {
		isHB = isHomebrewInstall
	}
	if isHB() {
		if *targetVersion != "" || *force {
			fmt.Fprintf(u.out(), "  --version and --force are not supported with Homebrew installs.\n  Run: brew upgrade scrypster/tap/huginn\n")
			return fmt.Errorf("--version and --force are not supported with Homebrew installs")
		}
		return u.upgradeViaHomebrew(latest, *yes, state)
	}

	// Self-update path: show prompt with daemon context.
	desc := daemonDesc(state)
	if !*yes {
		if desc != "" {
			fmt.Fprintf(u.out(), "  huginn %s are running and will be stopped and\n  restarted automatically during the upgrade.\n\n", desc)
		}
		fmt.Fprintf(u.out(), "  Upgrade to %s? [y/N] ", latest)
		var resp string
		fmt.Fscanln(u.in(), &resp)
		if strings.ToLower(strings.TrimSpace(resp)) != "y" {
			fmt.Fprintln(u.out(), "  Aborted.")
			return nil
		}
	} else if desc != "" {
		fmt.Fprintf(u.out(), "  Note: huginn %s are running and will be stopped and restarted.\n", desc)
	}

	return u.selfUpdate(latest, state)
}

// selfUpdate is a thin wrapper that resolves URLs for the current platform
// and delegates to selfUpdateWithURLs.
func (u *Upgrader) selfUpdate(latest string, state runningState) error {
	return u.selfUpdateWithURLs(
		latest,
		releaseAssetURL(latest, goruntime.GOOS, goruntime.GOARCH),
		releaseChecksumURL(latest),
		goruntime.GOOS,
		goruntime.GOARCH,
		state,
	)
}

// selfUpdateWithURLs is the testable core of selfUpdate. It accepts explicit
// asset and checksum URLs so tests can point at a mock HTTP server.
func (u *Upgrader) selfUpdateWithURLs(latest, assetURL, checksumURL, goos, goarch string, state runningState) error {
	huginnHome, err := u.HuginnDirFn()
	if err != nil {
		return fmt.Errorf("huginn home: %w", err)
	}

	servePIDPath := filepath.Join(huginnHome, "serve.pid")
	trayPIDPath  := filepath.Join(huginnHome, "tray.pid")

	// Stop daemons before touching the binary.
	if state.serveRunning {
		if err := u.step("Stopping server...", func() error {
			return u.StopProcess(state.servePID, servePIDPath)
		}); err != nil {
			return err
		}
	}
	if state.trayRunning {
		if err := u.step("Stopping tray...", func() error {
			return u.StopProcess(state.trayPID, trayPIDPath)
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
	if err := u.step("Fetching checksums...", func() error {
		var e error
		expectedSHA, e = u.fetchExpectedSHA(checksumURL, latest, goos, goarch)
		return e
	}); err != nil {
		return err
	}

	// Download archive with inline progress.
	var archivePath string
	label := fmt.Sprintf("Downloading %s...", latest)
	fmt.Fprintf(u.out(), "  %-32s", label)
	archivePath, err = u.downloadArchive(assetURL, func(dl, total int64) {
		if total > 0 {
			fmt.Fprintf(u.out(), "\r  %-32s%.1f / %.1f MB", label,
				float64(dl)/1024/1024, float64(total)/1024/1024)
		}
	})
	if err != nil {
		fmt.Fprintln(u.out(), " ✗")
		return err
	}
	fmt.Fprintln(u.out(), " ✓")
	defer os.Remove(archivePath) // always remove the archive temp file

	// SHA256 check on the archive BEFORE extracting (primary integrity gate).
	// The checksums file contains hashes of the compressed archives, not the
	// extracted binaries — so we must verify the archive, not the binary.
	if err := u.step("Verifying checksum...", func() error {
		return verifySHA256(archivePath, expectedSHA)
	}); err != nil {
		return err
	}

	// Extract the binary from the verified archive.
	var tmpPath string
	if err := u.step("Extracting binary...", func() error {
		var e error
		tmpPath, e = extractBinary(archivePath, exePath)
		return e
	}); err != nil {
		return err
	}

	// Ensure extracted binary is removed on any error after this point.
	tmpCleanup := tmpPath
	defer func() {
		if tmpCleanup != "" {
			os.Remove(tmpCleanup)
		}
	}()

	// Secondary sanity check: run `huginn version` to confirm it starts.
	if err := u.step("Verifying binary...", func() error {
		if err := os.Chmod(tmpPath, 0755); err != nil {
			return err
		}
		return verifyHuginnBinary(tmpPath, latest)
	}); err != nil {
		return err
	}

	// Back up the current binary before replacing it, so we can restore on failure.
	hDir, _ := u.HuginnDirFn()
	backupPath := filepath.Join(hDir, "huginn.backup")
	_ = copyFile(exePath, backupPath) // best-effort; ignore error if exePath doesn't exist yet

	// Atomic install — rename with cross-device fallback.
	if err := u.step("Installing...", func() error {
		return atomicReplace(tmpPath, exePath)
	}); err != nil {
		return err
	}
	tmpCleanup = "" // binary is installed; don't delete it

	// Verify the installed binary starts correctly; restore backup on failure.
	if err := verifyHuginnBinary(exePath, latest); err != nil {
		if backupPath != "" {
			_ = atomicReplace(backupPath, exePath)
		}
		return fmt.Errorf("installed binary failed verification, restored backup: %w", err)
	}
	_ = os.Remove(backupPath) // clean up backup on success

	// macOS: re-sign the new binary so the next launch doesn't need to re-exec.
	if goos == "darwin" {
		_ = u.step("Signing binary (macOS)...", func() error {
			return exec.Command("codesign", "--force", "--sign", "-", exePath).Run()
		})
	}

	// Restart daemons — failures are warnings, not fatal.
	if state.serveRunning {
		if err := u.step("Restarting server...", func() error {
			cmd := exec.Command(exePath, "serve")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
			return u.DetachStart(cmd)
		}); err != nil {
			fmt.Fprintf(u.out(), "\n  Warning: server did not restart: %v\n  Run: huginn serve\n\n", err)
		}
	}
	if state.trayRunning {
		if err := u.step("Restarting tray...", func() error {
			cmd := exec.Command(exePath, "tray")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
			return u.DetachStart(cmd)
		}); err != nil {
			fmt.Fprintf(u.out(), "\n  Warning: tray did not restart: %v\n  Run: huginn tray\n\n", err)
		}
	}

	fmt.Fprintf(u.out(), "\n  huginn updated to %s\n\n", latest)
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

// downloadArchive downloads the release archive to a temp file.
// progressFn is called with (bytesDownloaded, totalBytes).
// The caller is responsible for removing the returned temp file.
func (u *Upgrader) downloadArchive(assetURL string, progressFn func(dl, total int64)) (string, error) {
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

	tmp, err := os.CreateTemp("", ".huginn-archive-*")
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()

	pr := &progressReader{r: resp.Body, total: resp.ContentLength, fn: progressFn}
	_, copyErr := io.Copy(tmp, pr)
	tmp.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("write archive: %w", copyErr)
	}
	return tmpPath, nil
}

// extractBinary extracts the huginn binary from a .tar.gz archive file.
// The extracted binary is written to a temp file adjacent to exePath (for
// atomic rename). The caller is responsible for removing the returned temp file.
func extractBinary(archivePath, exePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
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

func (u *Upgrader) upgradeViaHomebrew(latest string, yes bool, state runningState) error {
	fmt.Fprintf(u.out(), "  Homebrew install detected.\n")

	desc := daemonDesc(state)
	if !yes {
		if desc != "" {
			fmt.Fprintf(u.out(), "  huginn %s are running and will be stopped\n  and restarted automatically during the upgrade.\n\n", desc)
		}
		fmt.Fprintf(u.out(), "  Upgrade to %s? [y/N] ", latest)
		var resp string
		fmt.Fscanln(u.in(), &resp)
		if strings.ToLower(strings.TrimSpace(resp)) != "y" {
			fmt.Fprintln(u.out(), "  Aborted.")
			return nil
		}
	} else if desc != "" {
		fmt.Fprintf(u.out(), "  Note: huginn %s are running and will be stopped and restarted.\n", desc)
	}

	// Resolve PID paths for stop/restart calls.
	huginnHome, _ := u.HuginnDirFn()
	servePIDPath := filepath.Join(huginnHome, "serve.pid")
	trayPIDPath  := filepath.Join(huginnHome, "tray.pid")

	if state.serveRunning {
		if err := u.step("Stopping server...", func() error {
			return u.StopProcess(state.servePID, servePIDPath)
		}); err != nil {
			return err
		}
	}
	if state.trayRunning {
		if err := u.step("Stopping tray...", func() error {
			return u.StopProcess(state.trayPID, trayPIDPath)
		}); err != nil {
			return err
		}
	}

	// Run brew upgrade.
	brew := u.BrewUpgrade
	if brew == nil {
		brew = func(pkg string) error {
			cmd := exec.Command("brew", "upgrade", pkg)
			cmd.Stdout = u.out()
			cmd.Stderr = u.out()
			return cmd.Run()
		}
	}
	brewErr := brew("scrypster/tap/huginn")

	// Resolve exe path for verification and restart.
	exePath := u.ExePath
	if exePath == "" {
		if exe, err := os.Executable(); err == nil {
			exePath, _ = filepath.EvalSymlinks(exe)
		}
	}

	// Verify the new binary only when brew succeeded.
	if brewErr == nil && exePath != "" {
		verify := u.VerifyBinary
		if verify == nil {
			verify = verifyHuginnBinary
		}
		if err := verify(exePath, latest); err != nil {
			fmt.Fprintf(u.out(), "  Warning: new binary failed verification.\n  Run: brew reinstall scrypster/tap/huginn\n")
		}
	}

	// Restart daemons — best-effort even when brew failed.
	if state.serveRunning {
		if err := u.step("Restarting server...", func() error {
			cmd := exec.Command(exePath, "serve")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
			return u.DetachStart(cmd)
		}); err != nil {
			fmt.Fprintf(u.out(), "\n  Warning: server did not restart: %v\n  Run: huginn serve\n\n", err)
		}
	}
	if state.trayRunning {
		if err := u.step("Restarting tray...", func() error {
			cmd := exec.Command(exePath, "tray")
			cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
			return u.DetachStart(cmd)
		}); err != nil {
			fmt.Fprintf(u.out(), "\n  Warning: tray did not restart: %v\n  Run: huginn tray\n\n", err)
		}
	}

	if brewErr != nil {
		return brewErr
	}

	fmt.Fprintf(u.out(), "\n  huginn updated to %s\n\n", latest)
	return nil
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

// copyFile copies src to dst, creating dst if necessary.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(dst), ".huginn-backup-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(out.Name())
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return err
	}
	return os.Rename(out.Name(), dst)
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
