package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunLoaderNeverReturnsNil guards against the regression where Init() fired
// an empty indexDoneMsg{} immediately, causing RunLoader to return a nil *repo.Index
// before the background goroutine finished building the real index.
// A nil return caused a panic in main.go when ranging over idx.Chunks.
func TestRunLoaderNeverReturnsNil(t *testing.T) {
	// Use a temp dir with a couple of files so indexing has something to do.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx := RunLoader(dir)
	if idx == nil {
		t.Fatal("RunLoader returned nil — Init() may have fired indexDoneMsg{} early")
	}
	if idx.Root == "" {
		t.Error("RunLoader returned an Index with empty Root")
	}
}

// TestLoaderInitReturnsNil ensures loaderModel.Init() returns nil (not a Cmd
// that fires an empty indexDoneMsg and quits before real indexing completes).
func TestLoaderInitReturnsNil(t *testing.T) {
	m := newLoaderModel("/tmp")
	cmd := m.Init()
	if cmd != nil {
		t.Error("loaderModel.Init() must return nil; a non-nil Cmd fires an empty indexDoneMsg and causes RunLoader to return nil")
	}
}
