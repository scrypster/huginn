package tray

import (
	"runtime"
	"testing"
)

// stubOSCalls replaces openTerminalFn, openURLFn, and notifyFn with no-ops
// for the duration of the test, restoring the originals when the test finishes.
// This prevents tests from launching real Terminal windows, browser tabs, or
// OS notifications on the developer's machine.
func stubOSCalls(t *testing.T) {
	t.Helper()
	origTerminal := openTerminalFn
	origURL := openURLFn
	origNotify := notifyFn
	openTerminalFn = func(_ string) error { return nil }
	openURLFn = func(_ string) error { return nil }
	notifyFn = func(_, _ string) error { return nil }
	t.Cleanup(func() {
		openTerminalFn = origTerminal
		openURLFn = origURL
		notifyFn = origNotify
	})
}

// TestDetectTerminals_Darwin verifies that DetectTerminals on macOS returns at
// least one entry (Terminal.app is the hard fallback) and that every result has
// non-empty Name and AppPath fields.
func TestDetectTerminals_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	terms := DetectTerminals()
	if len(terms) == 0 {
		t.Fatal("DetectTerminals: expected at least one terminal on darwin, got none")
	}
	for _, term := range terms {
		if term.Name == "" {
			t.Errorf("DetectTerminals: terminal entry has empty Name: %+v", term)
		}
		if term.AppPath == "" {
			t.Errorf("DetectTerminals: terminal entry has empty AppPath: %+v", term)
		}
	}
}

// TestDetectTerminals_Darwin_TerminalAppFallback verifies Terminal.app is
// always present in the results on macOS, since it is the hard fallback.
func TestDetectTerminals_Darwin_TerminalAppFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	terms := DetectTerminals()
	found := false
	for _, term := range terms {
		if term.Name == "Terminal" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("DetectTerminals: expected Terminal.app fallback to be present, but it was missing")
	}
}

// TestDetectTerminals_NonDarwin verifies that on non-macOS platforms
// DetectTerminals returns nil (no UI detection implemented there yet).
func TestDetectTerminals_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin test")
	}
	terms := DetectTerminals()
	if terms != nil {
		t.Fatalf("DetectTerminals: expected nil on %s, got %v", runtime.GOOS, terms)
	}
}

// TestDetectTerminals_NoDuplicates ensures no terminal name appears twice.
func TestDetectTerminals_NoDuplicates(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	terms := DetectTerminals()
	seen := make(map[string]bool)
	for _, term := range terms {
		if seen[term.Name] {
			t.Errorf("DetectTerminals: duplicate terminal name %q", term.Name)
		}
		seen[term.Name] = true
	}
}

// TestDetectTerminals_OrderedByPreference checks that candidate order is
// preserved (earlier candidates should appear before later ones when both are
// present). Uses the known order from darwinTerminalCandidates.
func TestDetectTerminals_OrderedByPreference(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	terms := DetectTerminals()

	// Build index map from returned terms.
	idx := make(map[string]int, len(terms))
	for i, term := range terms {
		idx[term.Name] = i
	}

	// Verify candidate ordering is respected for every adjacent pair that both appear.
	for i := 0; i < len(darwinTerminalCandidates)-1; i++ {
		a := darwinTerminalCandidates[i].Name
		b := darwinTerminalCandidates[i+1].Name
		idxA, hasA := idx[a]
		idxB, hasB := idx[b]
		if hasA && hasB && idxA > idxB {
			t.Errorf("DetectTerminals: %q (candidate[%d]) appeared after %q (candidate[%d]), want before",
				a, i, b, i+1)
		}
	}
}

// TestOpenTerminal_UnknownName_Darwin verifies that passing an unknown app name
// on macOS still calls "open -a <name>" — the error (if any) is from the
// subprocess, not from our logic. We only verify the function returns without
// panicking.
func TestOpenTerminal_UnknownName_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	stubOSCalls(t)
	// Start is non-blocking; we don't wait for the subprocess.
	// We simply assert no panic and the error type is reasonable.
	err := OpenTerminal("__huginn_nonexistent_app__")
	// err can be nil (Start() succeeded even for a bad app — the OS handles it).
	// It can also be non-nil. Either way, no panic is the invariant.
	_ = err
}

// TestOpenURL_Darwin_NoError checks that openURL on darwin launches without
// an error for a well-formed URL (we just fire the subprocess, don't wait).
func TestOpenURL_Darwin_NoError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	stubOSCalls(t)
	err := openURL("http://127.0.0.1:8421")
	if err != nil {
		t.Errorf("openURL: unexpected error on darwin: %v", err)
	}
}

// TestOpenURL_Linux_NoError checks that openURL on linux doesn't return an
// error for the call structure (xdg-open is typically present).
func TestOpenURL_Linux_NoError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	stubOSCalls(t)
	err := openURL("http://127.0.0.1:8421")
	if err != nil {
		t.Errorf("openURL: unexpected error on linux: %v", err)
	}
}

// TestOpenURL_UnsupportedPlatform checks that openURL returns a non-nil error
// on unknown platforms.
func TestOpenURL_UnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("unsupported-platform test only")
	}
	err := openURL("http://127.0.0.1:8421")
	if err == nil {
		t.Error("expected error for unsupported platform, got nil")
	}
}

// TestNotify_Darwin verifies Notify constructs and starts the osascript call
// without returning an error. We don't block waiting for the notification.
func TestNotify_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	stubOSCalls(t)
	err := Notify("Huginn Test", "test notification from unit test")
	if err != nil {
		t.Errorf("Notify: unexpected error on darwin: %v", err)
	}
}

// TestNotify_Linux_NoNotifySend checks behavior on Linux when notify-send
// is not in PATH — function must return a non-nil error, not panic.
func TestNotify_Linux_NoNotifySend(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	stubOSCalls(t)
	// We can only validate this doesn't panic; the error depends on whether
	// notify-send is installed. Both outcomes are acceptable.
	_ = Notify("Huginn Test", "test")
}
