package tray

import (
	"runtime"
	"testing"
)

func TestOpenTerminal_UnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("skipping unsupported-platform test on supported OS")
	}
	err := OpenTerminal("Terminal")
	if err == nil {
		t.Error("expected error on unsupported platform, got nil")
	}
}

func TestOpenURL_Compiles(t *testing.T) {
	// Verify the function signature exists and compiles.
	_ = openURL
}

func TestNotify_UnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("skipping unsupported-platform test on supported OS")
	}
	err := Notify("test", "message")
	if err == nil {
		t.Error("expected error on unsupported platform, got nil")
	}
}
