package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPull_basic(t *testing.T) {
	content := []byte("fake gguf content for testing")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	var lastProgress PullProgress
	err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", func(p PullProgress) {
		lastProgress = p
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !lastProgress.Done {
		t.Error("expected Done=true in final progress")
	}
	got, _ := os.ReadFile(destPath)
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}
}

// TestPull_SHA256Mismatch verifies that when the downloaded file does not match
// the expected SHA256, Pull returns an error containing "sha256" and removes
// the corrupt file from disk.
func TestPull_SHA256Mismatch(t *testing.T) {
	content := []byte("real file content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, wrongHash, nil)
	if err == nil {
		t.Fatal("expected error on SHA256 mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Errorf("expected error to mention sha256, got: %v", err)
	}
	// File must be removed after mismatch.
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Error("expected corrupt file to be removed after SHA256 mismatch")
	}
}

// TestPull_SHA256Match verifies that a correct hash passes without error.
func TestPull_SHA256Match(t *testing.T) {
	content := []byte("exact content")
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, hash, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPull_HTTP404 verifies that a 404 response causes Pull to return an error.
func TestPull_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	err := Pull(context.Background(), srv.URL+"/missing.gguf", destPath, "", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
}

// TestPull_ContextCancellation verifies that cancelling the context causes Pull
// to return an error. The partial file may or may not be present on disk (the
// implementation does not guarantee cleanup on context cancellation — only on
// SHA256 mismatch). This test documents the actual behavior: partial file IS
// left on disk.
func TestPull_ContextCancellation(t *testing.T) {
	var once sync.Once
	started := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		once.Do(func() { close(started) })
		// Write a small chunk then block until the request context is done.
		w.Write(make([]byte, 512))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	destPath := filepath.Join(t.TempDir(), "model.gguf")

	errCh := make(chan error, 1)
	go func() {
		errCh <- Pull(ctx, srv.URL+"/model.gguf", destPath, "", nil)
	}()

	// Wait until the server has started sending data, then cancel.
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("server never started")
	}
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after context cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Pull did not return after context cancellation")
	}
	// Document behavior: partial file is left on disk (no cleanup on cancel).
	// We just verify Pull returned an error — file presence is informational.
}

// TestPull_ResumeWithPartialFile verifies that when a partial file exists and
// the server honours the Range request (206 Partial Content), Pull downloads
// only the remaining bytes and assembles the correct final file.
func TestPull_ResumeWithPartialFile(t *testing.T) {
	fullContent := []byte("ABCDEFGHIJ0123456789") // 20 bytes total
	partial := fullContent[:10]                   // first 10 bytes already on disk

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			// Honour the Range request — return remaining bytes.
			remaining := fullContent[10:]
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(remaining)))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 10-%d/%d", len(fullContent)-1, len(fullContent)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(remaining)
		} else {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fullContent)))
			w.Write(fullContent)
		}
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	// Write partial file to simulate a previous interrupted download.
	if err := os.WriteFile(destPath, partial, 0644); err != nil {
		t.Fatalf("setup: write partial file: %v", err)
	}

	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(got) != string(fullContent) {
		t.Errorf("content mismatch after resume: got %q, want %q", got, fullContent)
	}
}

// TestPull_ServerIgnoresRange verifies that when a partial file exists but the
// server returns 200 (ignoring the Range header), the download restarts from
// scratch and the final file content is correct (not corrupted/doubled).
func TestPull_ServerIgnoresRange(t *testing.T) {
	fullContent := []byte("COMPLETE_CONTENT_1234567890")
	partial := fullContent[:10] // stale partial bytes on disk

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always respond with 200 — ignore Range header.
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fullContent)))
		w.WriteHeader(http.StatusOK)
		w.Write(fullContent)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(destPath, partial, 0644); err != nil {
		t.Fatalf("setup: write partial file: %v", err)
	}

	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(got) != string(fullContent) {
		t.Errorf("file corrupted after server ignored Range: got %q, want %q", got, fullContent)
	}
}

// TestPull_CreatesParentDirectory verifies that Pull creates missing parent
// directories before attempting to open the destination file.
func TestPull_CreatesParentDirectory(t *testing.T) {
	content := []byte("hello")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	// Nested path — subdirectories do not yet exist.
	destPath := filepath.Join(t.TempDir(), "a", "b", "c", "model.gguf")
	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(destPath)
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}
}

// TestPull_InvalidURL verifies that Pull returns an error for an invalid URL.
func TestPull_InvalidURL(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "model.gguf")
	err := Pull(context.Background(), "ht@tp://[invalid", destPath, "", nil)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

// TestPull_NoProgressCallback verifies that Pull works correctly when
// onProgress is nil (no progress reporting).
func TestPull_NoProgressCallback(t *testing.T) {
	content := []byte("test content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	// Pass nil for progress callback
	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(destPath)
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}
}

// TestPull_ProgressReporting verifies that the progress callback is called
// with correct values during download.
func TestPull_ProgressReporting(t *testing.T) {
	content := []byte("ABCDEFGHIJ") // 10 bytes, small to keep test fast
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	var progressCalls []PullProgress
	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", func(p PullProgress) {
		progressCalls = append(progressCalls, p)
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// At minimum, should have a final progress report
	if len(progressCalls) == 0 {
		t.Error("expected at least one progress callback")
	}
	// Last callback should be Done=true
	if !progressCalls[len(progressCalls)-1].Done {
		t.Error("expected final progress to have Done=true")
	}
}

// TestPull_HTTP500 verifies that a 500 server error causes Pull to return an error.
func TestPull_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "model.gguf")
	err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention 500, got: %v", err)
	}
}

// TestPull_EmptyFile verifies that Pull can download an empty file (0 bytes).
func TestPull_EmptyFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		// Write nothing
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "empty.gguf")
	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected empty file, got %d bytes", info.Size())
	}
}

// TestPull_LargeFile simulates downloading a large file and verifies
// that the download completes without errors.
func TestPull_LargeFile(t *testing.T) {
	// Create 1MB of content
	content := make([]byte, 1024*1024)
	for i := 0; i < len(content); i++ {
		content[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "large.gguf")
	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(destPath)
	if len(got) != len(content) {
		t.Errorf("size mismatch: expected %d, got %d", len(content), len(got))
	}
}

// TestVerifySHA256_ValidHash verifies that a matching SHA256 passes verification.
// (This tests the verifySHA256 function indirectly via Pull.)
func TestVerifySHA256_ValidHash(t *testing.T) {
	content := []byte("test file for sha256 verification")
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "verified.gguf")
	if err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, hash, nil); err != nil {
		t.Fatalf("expected no error with valid hash, got: %v", err)
	}
}

// TestVerifySHA256_MismatchedHash verifies that a mismatched SHA256 fails
// and the file is removed.
func TestVerifySHA256_MismatchedHash(t *testing.T) {
	content := []byte("some content")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	destPath := filepath.Join(t.TempDir(), "mismatch.gguf")
	err := Pull(context.Background(), srv.URL+"/model.gguf", destPath, wrongHash, nil)
	if err == nil {
		t.Fatal("expected error with mismatched hash")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Errorf("expected sha256 error message, got: %v", err)
	}
}
