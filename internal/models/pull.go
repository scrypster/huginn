package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// PullProgress is called periodically during a download.
type PullProgress struct {
	Downloaded int64   // bytes downloaded so far
	Total      int64   // total bytes (-1 if unknown)
	Speed      float64 // bytes/sec (rolling average)
	Done       bool    // true when download is complete
}

// Pull downloads the model at url to destPath.
// If destPath already exists and is a partial download, it resumes via Range.
// onProgress is called approximately once per second (and once on completion).
// If expectedSHA256 is non-empty, the file is verified after download.
func Pull(ctx context.Context, url, destPath, expectedSHA256 string, onProgress func(PullProgress)) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create model directory: %w", err)
	}

	// Check for existing partial file
	var startByte int64
	if fi, err := os.Stat(destPath); err == nil {
		startByte = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if startByte > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	// If server doesn't support Range, restart from 0
	if resp.StatusCode == http.StatusOK && startByte > 0 {
		startByte = 0
	}

	// Determine total size
	total := resp.ContentLength
	if startByte > 0 && total > 0 {
		total += startByte
	}

	// Open file (append for resume, create/truncate for new)
	// When server returns 200 (ignoring Range), we must truncate so stale
	// bytes from the previous partial file are not left at the end.
	flag := os.O_CREATE | os.O_WRONLY
	if startByte > 0 && resp.StatusCode == http.StatusPartialContent {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(destPath, flag, 0644)
	if err != nil {
		return fmt.Errorf("open dest: %w", err)
	}
	defer f.Close()

	downloaded := startByte
	var lastReport time.Time
	windowStart := time.Now()
	var windowBytes int64

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return fmt.Errorf("write: %w", werr)
			}
			downloaded += int64(n)
			windowBytes += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		// Report progress ~once per second
		if onProgress != nil && time.Since(lastReport) >= time.Second {
			elapsed := time.Since(windowStart).Seconds()
			speed := 0.0
			if elapsed > 0 {
				speed = float64(windowBytes) / elapsed
			}
			onProgress(PullProgress{
				Downloaded: downloaded,
				Total:      total,
				Speed:      speed,
			})
			lastReport = time.Now()
			windowStart = time.Now()
			windowBytes = 0
		}
	}

	if onProgress != nil {
		onProgress(PullProgress{Downloaded: downloaded, Total: total, Done: true})
	}

	// SHA256 verification
	if expectedSHA256 != "" {
		if verifyErr := verifySHA256(destPath, expectedSHA256); verifyErr != nil {
			os.Remove(destPath) // best-effort cleanup; ignore remove error
			return verifyErr
		}
	}

	return nil
}

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
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, got)
	}
	return nil
}
