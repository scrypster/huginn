package models

import (
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path atomically using a temp file + rename.
// This guarantees that readers never see a partial write — the file either
// contains the old content or the new content, never a torn half-written state.
// The temp file is created in the same directory as path so that os.Rename
// is always an atomic same-filesystem move (avoids cross-device rename errors).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	// Flush OS write buffers to storage before rename so that a crash after
	// rename still gives readers a fully-written file.
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
