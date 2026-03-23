//go:build !embed_frontend

package server

import (
	"net/http"
	"os"
	"path/filepath"
)

// staticFS returns a file system for serving the frontend.
// Without the embed_frontend build tag, this serves files from disk.
// Resolves relative to the binary location so it works regardless of cwd.
func staticFS() http.FileSystem {
	// Try resolving relative to the executable first (works when installed).
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "internal", "server", "dist")
		if _, err := os.Stat(candidate); err == nil {
			return http.Dir(candidate)
		}
	}
	// Fall back to cwd-relative path (works when run from project root in dev).
	return http.Dir("internal/server/dist")
}
