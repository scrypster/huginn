package relay_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

func TestSentinel_StartsAndStops(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("sentinel-token") //nolint:errcheck

	s := relay.NewSentinel(relay.SentinelConfig{
		MachineID:   "test-machine",
		TokenStorer: store,
		CloudURL:    "wss://invalid.example.com",
		SkipDial:    true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("sentinel did not stop after context cancellation")
	}
}

// TestSentinelFileTokenStore_SaveLoad verifies that tokens are saved with
// restricted permissions (0600) and can be loaded correctly.
func TestSentinelFileTokenStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".huginn", "sentinel-token")
	store := relay.NewSentinelFileTokenStore(path)

	// Save token
	if err := store.Save("my-secret-token"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file permissions are 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}

	// Load and verify token content
	token, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if token != "my-secret-token" {
		t.Errorf("expected token %q, got %q", "my-secret-token", token)
	}
}

// TestSentinelFileTokenStore_IsRegistered verifies that IsRegistered correctly
// reflects whether a token file exists.
func TestSentinelFileTokenStore_IsRegistered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	store := relay.NewSentinelFileTokenStore(path)

	// Before saving, should not be registered
	if store.IsRegistered() {
		t.Error("expected IsRegistered=false before save")
	}

	// After saving, should be registered
	if err := store.Save("tok"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !store.IsRegistered() {
		t.Error("expected IsRegistered=true after save")
	}
}

// TestSentinelFileTokenStore_Clear verifies that Clear removes the token file
// and subsequent IsRegistered returns false.
func TestSentinelFileTokenStore_Clear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	store := relay.NewSentinelFileTokenStore(path)

	// Save and verify
	if err := store.Save("tok"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !store.IsRegistered() {
		t.Error("expected IsRegistered=true after save")
	}

	// Clear and verify
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if store.IsRegistered() {
		t.Error("expected IsRegistered=false after clear")
	}
}

// TestSentinelFileTokenStore_LoadNonexistent verifies that Load returns an error
// when the token file does not exist.
func TestSentinelFileTokenStore_LoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")
	store := relay.NewSentinelFileTokenStore(path)

	_, err := store.Load()
	if err == nil {
		t.Error("expected Load to fail for nonexistent file")
	}
}

// TestSentinelFileTokenStore_ClearNonexistent verifies that Clear returns an error
// when the token file does not exist.
func TestSentinelFileTokenStore_ClearNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")
	store := relay.NewSentinelFileTokenStore(path)

	err := store.Clear()
	if err == nil {
		t.Error("expected Clear to fail for nonexistent file")
	}
}
