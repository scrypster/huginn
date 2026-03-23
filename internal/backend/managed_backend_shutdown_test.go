package backend

import (
	"context"
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// ManagedBackend
// ---------------------------------------------------------------------------

func TestManagedBackend_ShutdownCallsHook(t *testing.T) {
	called := false
	mb := NewManagedBackend("http://localhost:11434", func(_ context.Context) error {
		called = true
		return nil
	})
	if err := mb.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !called {
		t.Error("expected shutdown hook to be called")
	}
}

func TestManagedBackend_ShutdownNilHook(t *testing.T) {
	mb := NewManagedBackend("http://localhost:11434", nil)
	if err := mb.Shutdown(context.Background()); err != nil {
		t.Errorf("expected nil error for nil shutdown hook, got: %v", err)
	}
}

func TestManagedBackend_ShutdownPropagatesError(t *testing.T) {
	mb := NewManagedBackend("http://localhost:11434", func(_ context.Context) error {
		return errors.New("shutdown failed")
	})
	err := mb.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected error from shutdown hook")
	}
	if err.Error() != "shutdown failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManagedBackend_InheritsExternalBackend(t *testing.T) {
	mb := NewManagedBackend("http://localhost:11434", nil)
	// Should be usable as a Backend interface.
	var _ Backend = mb
	// Health should not panic (will fail to connect, but shouldn't crash).
	// We skip the actual call since there's no server.
	_ = mb
}

func TestManagedBackend_EndpointStripped(t *testing.T) {
	mb := NewManagedBackend("http://localhost:11434/", nil)
	// The embedded ExternalBackend should have trimmed the trailing slash.
	if mb.endpoint != "http://localhost:11434" {
		t.Errorf("expected trimmed endpoint, got %q", mb.endpoint)
	}
}
