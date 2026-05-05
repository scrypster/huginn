# Upgrade UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Warn users about running daemons before stopping them during `huginn upgrade`, and fix the Homebrew path to stop/restart/verify automatically.

**Architecture:** Single file change (`upgrade.go`). Add `Stdout`/`Stdin`/`IsHomebrewFn`/`BrewUpgrade`/`VerifyBinary` injectable fields to `Upgrader` for testability. Introduce `runningState` struct detected once in `Run()` and passed to both `selfUpdateWithURLs` and `upgradeViaHomebrew`. Add `daemonDesc()` helper and `(u *Upgrader) step()` method.

**Tech Stack:** Go standard library only. No new dependencies.

---

## File Changes

| File | Change |
|------|--------|
| `upgrade.go` | Add `runningState`, `daemonDesc`, `out()`, `in()`, `step()` methods; move PID detection to `Run()`; update prompt; restructure `upgradeViaHomebrew`; update `selfUpdate`/`selfUpdateWithURLs` signatures |
| `upgrade_test.go` | Add 9 new tests covering prompt text, `--yes` notice, Homebrew stop/restart, brew failure, flag errors, binary verification |

---

### Task 1: `runningState` struct + move PID detection + updated self-update prompt

**Files:**
- Modify: `upgrade.go`
- Modify: `upgrade_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `upgrade_test.go`:

```go
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
		PIDIsLiveFn: func(path string) (bool, int) { return true, 42 },
		StopProcess: noopStop,
		DetachStart: noopDetach,
		Stdout:      &out,
		Stdin:       strings.NewReader("n\n"), // user aborts
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
		Stdout:        &out,
		Stdin:         strings.NewReader(""),
	}
	_ = u.Run([]string{"--yes"})
	got := out.String()
	if strings.Contains(got, "Note:") {
		t.Errorf("should not print Note: when no daemons running:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test -run 'TestRun_Prompt|TestRun_YesFlag' . 2>&1 | head -40
```

Expected: FAIL — `Stdout`, `Stdin` fields not defined on `Upgrader`.

- [ ] **Step 3: Implement `runningState`, helpers, and updated `Run()`**

In `upgrade.go`, make the following changes:

**3a. Add new fields to `Upgrader` struct** (after the existing `DetachStart` field):

```go
// New injectable fields for testability
Stdout       io.Writer   // if nil, defaults to os.Stdout
Stdin        io.Reader   // if nil, defaults to os.Stdin
IsHomebrewFn func() bool // if nil, defaults to isHomebrewInstall
```

**3b. Add `runningState` struct** (after the `Upgrader` struct):

```go
// runningState captures which huginn daemons were live at the start of Run().
// Detected once to avoid a TOCTOU gap between the prompt and the stop calls.
type runningState struct {
	serveRunning bool
	servePID     int
	trayRunning  bool
	trayPID      int
}
```

**3c. Add helper methods** (after `defaultUpgrader`):

```go
// out returns the writer for user-facing output.
func (u *Upgrader) out() io.Writer {
	if u.Stdout != nil {
		return u.Stdout
	}
	return os.Stdout
}

// in returns the reader for interactive input.
func (u *Upgrader) in() io.Reader {
	if u.Stdin != nil {
		return u.Stdin
	}
	return os.Stdin
}

// step prints a labeled upgrade step, runs f(), and prints ✓ or ✗.
// Used by upgradeViaHomebrew; selfUpdateWithURLs uses the package-level upgradeStep.
func (u *Upgrader) step(label string, f func() error) error {
	fmt.Fprintf(u.out(), "  %-32s", label)
	if err := f(); err != nil {
		fmt.Fprintf(u.out(), " ✗\n  Error: %v\n", err)
		return err
	}
	fmt.Fprintln(u.out(), " ✓")
	return nil
}

// daemonDesc returns a human-readable description of running daemons
// ("server + tray", "server", "tray", or "" when none are running).
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
```

**3d. Replace the body of `Run()`** with the updated version:

```go
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
```

- [ ] **Step 4: Update `selfUpdate` and `selfUpdateWithURLs` to accept `state runningState`**

Replace `selfUpdate`:

```go
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
```

Replace the opening of `selfUpdateWithURLs` — change signature and remove PID re-detection:

```go
// selfUpdateWithURLs is the testable core of selfUpdate. It accepts explicit
// asset and checksum URLs so tests can point at a mock HTTP server.
// state contains the daemon liveness snapshot taken in Run() — no re-detection.
func (u *Upgrader) selfUpdateWithURLs(latest, assetURL, checksumURL, goos, goarch string, state runningState) error {
	huginnHome, err := u.HuginnDirFn()
	if err != nil {
		return fmt.Errorf("huginn home: %w", err)
	}

	servePIDPath := filepath.Join(huginnHome, "serve.pid")
	trayPIDPath  := filepath.Join(huginnHome, "tray.pid")

	// Stop daemons before touching the binary.
	// (rest of function body unchanged from line 154 onward)
```

Remove these four lines that previously re-detected PIDs (they are now deleted — use `state` fields instead):

```go
// DELETE these four lines:
serveRunning, servePID := u.PIDIsLiveFn(servePIDPath)
trayRunning,  trayPID  := u.PIDIsLiveFn(trayPIDPath)
```

Replace every reference to the deleted local variables with the struct fields:
- `serveRunning` → `state.serveRunning`
- `servePID` → `state.servePID`
- `trayRunning` → `state.trayRunning`
- `trayPID` → `state.trayPID`

Also update the existing test call to `selfUpdateWithURLs` (line ~668 of `upgrade_test.go`) to pass an empty `runningState{}` as the last argument:

```go
err := u.selfUpdateWithURLs(
    "v0.3.0",
    srv.URL+"/archive.tar.gz",
    srv.URL+"/checksums.txt",
    "linux", "amd64",
    runningState{}, // added
)
```

Do the same for `TestSelfUpdateWithURLs_ChecksumMismatch` and `TestSelfUpdateWithURLs_ChecksumsFetch404`.

- [ ] **Step 5: Run new and existing tests**

```bash
go test -run 'TestRun_Prompt|TestRun_YesFlag|TestSelfUpdateWithURLs|TestUpgraderRun' . -v 2>&1 | tail -30
```

Expected: all PASS.

- [ ] **Step 6: Build check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add upgrade.go upgrade_test.go
git commit -m "feat(upgrade): detect running daemons once in Run(), update self-update prompt"
```

---

### Task 2: Restructure `upgradeViaHomebrew`

**Files:**
- Modify: `upgrade.go`
- Modify: `upgrade_test.go`

- [ ] **Step 1: Write failing tests**

Append to `upgrade_test.go`:

```go
// ─── upgradeViaHomebrew ───────────────────────────────────────────────────────

func TestUpgradeViaHomebrew_StopsAndRestartsDaemons(t *testing.T) {
	stopCalls, restartCalls := 0, 0
	var out bytes.Buffer
	dir := t.TempDir()
	exePath := filepath.Join(dir, "huginn")
	os.WriteFile(exePath, []byte("fake"), 0755)

	u := &Upgrader{
		HuginnDirFn:  func() (string, error) { return dir, nil },
		ExePath:      exePath,
		StopProcess:  func(pid int, path string) error { stopCalls++; return nil },
		DetachStart:  func(cmd *exec.Cmd) error { restartCalls++; return nil },
		BrewUpgrade:  func(pkg string) error { return nil },
		VerifyBinary: func(path, tag string) error { return nil },
		Stdout:       &out,
		Stdin:        strings.NewReader("y\n"),
	}

	state := runningState{serveRunning: true, servePID: 42, trayRunning: true, trayPID: 43}
	if err := u.upgradeViaHomebrew("v0.3.1", false, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopCalls != 2 {
		t.Errorf("expected 2 stop calls, got %d", stopCalls)
	}
	if restartCalls != 2 {
		t.Errorf("expected 2 restart calls, got %d", restartCalls)
	}
}

func TestUpgradeViaHomebrew_RestartsOnBrewFailure(t *testing.T) {
	stopCalls, restartCalls := 0, 0
	var out bytes.Buffer
	dir := t.TempDir()
	exePath := filepath.Join(dir, "huginn")
	os.WriteFile(exePath, []byte("fake"), 0755)

	u := &Upgrader{
		HuginnDirFn:  func() (string, error) { return dir, nil },
		ExePath:      exePath,
		StopProcess:  func(pid int, path string) error { stopCalls++; return nil },
		DetachStart:  func(cmd *exec.Cmd) error { restartCalls++; return nil },
		BrewUpgrade:  func(pkg string) error { return fmt.Errorf("brew exploded") },
		VerifyBinary: func(path, tag string) error { return nil },
		Stdout:       &out,
		Stdin:        strings.NewReader("y\n"),
	}

	state := runningState{serveRunning: true, servePID: 42, trayRunning: true, trayPID: 43}
	err := u.upgradeViaHomebrew("v0.3.1", false, state)
	if err == nil || !strings.Contains(err.Error(), "brew exploded") {
		t.Errorf("expected brew error, got: %v", err)
	}
	if stopCalls != 2 {
		t.Errorf("expected 2 stop calls, got %d", stopCalls)
	}
	if restartCalls != 2 {
		t.Errorf("expected 2 restart calls after brew failure, got %d", restartCalls)
	}
}

func TestUpgradeViaHomebrew_VersionFlagErrors(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	brewCalled := false
	var out bytes.Buffer
	u := &Upgrader{
		LatestRelease: func(ctx context.Context) string { return "v0.3.1" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return false, 0 },
		IsHomebrewFn:  func() bool { return true },
		BrewUpgrade:   func(pkg string) error { brewCalled = true; return nil },
		Stdout:        &out,
	}
	err := u.Run([]string{"--version", "v0.3.1"})
	if err == nil {
		t.Error("expected error for --version with Homebrew, got nil")
	}
	if brewCalled {
		t.Error("brew must not be called when --version is set for Homebrew install")
	}
}

func TestUpgradeViaHomebrew_ForceFlagErrors(t *testing.T) {
	savedVersion := version
	version = "0.3.0"
	defer func() { version = savedVersion }()

	brewCalled := false
	var out bytes.Buffer
	u := &Upgrader{
		LatestRelease: func(ctx context.Context) string { return "v0.3.1" },
		HuginnDirFn:   func() (string, error) { return t.TempDir(), nil },
		PIDIsLiveFn:   func(path string) (bool, int) { return false, 0 },
		IsHomebrewFn:  func() bool { return true },
		BrewUpgrade:   func(pkg string) error { brewCalled = true; return nil },
		Stdout:        &out,
	}
	err := u.Run([]string{"--force"})
	if err == nil {
		t.Error("expected error for --force with Homebrew, got nil")
	}
	if brewCalled {
		t.Error("brew must not be called when --force is set for Homebrew install")
	}
}

func TestUpgradeViaHomebrew_VerifiesNewBinary(t *testing.T) {
	var out bytes.Buffer
	dir := t.TempDir()
	exePath := filepath.Join(dir, "huginn")
	os.WriteFile(exePath, []byte("fake"), 0755)

	u := &Upgrader{
		HuginnDirFn:  func() (string, error) { return dir, nil },
		ExePath:      exePath,
		StopProcess:  noopStop,
		DetachStart:  noopDetach,
		BrewUpgrade:  func(pkg string) error { return nil },
		VerifyBinary: func(path, tag string) error { return fmt.Errorf("binary verify failed") },
		Stdout:       &out,
		Stdin:        strings.NewReader("y\n"),
	}

	state := runningState{}
	err := u.upgradeViaHomebrew("v0.3.1", false, state)
	// verify failure prints a warning but does NOT return an error
	if err != nil {
		t.Errorf("verify failure should not cause error return, got: %v", err)
	}
	if !strings.Contains(out.String(), "Warning") {
		t.Errorf("expected Warning about verify failure:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run 'TestUpgradeViaHomebrew' . 2>&1 | head -40
```

Expected: FAIL — `BrewUpgrade`, `VerifyBinary`, `IsHomebrewFn` fields not defined.

- [ ] **Step 3: Add new fields to `Upgrader` and replace `upgradeViaHomebrew`**

**3a. Add to `Upgrader` struct** (after `IsHomebrewFn`):

```go
BrewUpgrade  func(pkg string) error          // if nil, runs exec.Command("brew", "upgrade", pkg)
VerifyBinary func(path, tag string) error    // if nil, defaults to verifyHuginnBinary
```

**3b. Replace `upgradeViaHomebrew` entirely:**

```go
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
```

- [ ] **Step 4: Run all new and existing upgrade tests**

```bash
go test -run 'TestUpgradeViaHomebrew|TestRun_Prompt|TestRun_YesFlag|TestUpgraderRun|TestSelfUpdateWithURLs' . -v 2>&1 | tail -40
```

Expected: all PASS.

- [ ] **Step 5: Run full test suite**

```bash
go test . -v 2>&1 | grep -E 'PASS|FAIL|---'
```

Expected: all PASS. No regressions.

- [ ] **Step 6: Build check**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add upgrade.go upgrade_test.go
git commit -m "feat(upgrade): stop/restart daemons on Homebrew path, add --version/--force guard"
```

---

## Self-Review

**Spec coverage:**

| Spec requirement | Task |
|-----------------|------|
| `runningState` struct, detect once in `Run()` | Task 1, Step 3 |
| Self-update prompt shows daemon names | Task 1, Step 3d |
| `--yes` prints notice when daemons running | Task 1, Step 3d |
| User says N → clean exit, nothing touched | Task 1, Step 3d |
| Homebrew: `--version`/`--force` error | Task 1, Step 3d (guard in `Run()`) |
| Homebrew: prompt + stop/restart/verify | Task 2, Step 3 |
| Homebrew: restart on brew failure | Task 2, Step 3 |
| Homebrew: verify warning (not error) | Task 2, Step 3 |
| All 9 spec tests | Tasks 1–2, Steps 1 |

**Type consistency:** `runningState` defined in Task 1 Step 3b, used identically in both tasks. `daemonDesc(state runningState) string` defined in Task 1 Step 3c, used in Task 1 Step 3d and Task 2 Step 3b.

**No placeholders:** All steps contain complete code.
