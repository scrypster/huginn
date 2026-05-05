package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStaleWatcher_NotStaleInitially(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	w, err := newStaleWatcher(exe)
	if err != nil {
		t.Fatal(err)
	}
	if w.IsStale() {
		t.Error("expected IsStale()=false immediately after construction")
	}
}

func TestStaleWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "huginn")
	if err := os.WriteFile(f, []byte("v1"), 0755); err != nil {
		t.Fatal(err)
	}

	staleCheckInterval = 50 * time.Millisecond
	t.Cleanup(func() { staleCheckInterval = 60 * time.Second })

	w, err := newStaleWatcher(f)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	w.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	now := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(f, now, now); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.IsStale() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("expected IsStale()=true after binary mtime changed")
}

func TestStaleWatcher_NoChangeNoStale(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "huginn")
	if err := os.WriteFile(f, []byte("v1"), 0755); err != nil {
		t.Fatal(err)
	}

	staleCheckInterval = 50 * time.Millisecond
	t.Cleanup(func() { staleCheckInterval = 60 * time.Second })

	w, err := newStaleWatcher(f)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	time.Sleep(250 * time.Millisecond)
	if w.IsStale() {
		t.Error("expected IsStale()=false when file is not modified")
	}
}
