package notepad

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Manager handles loading, creating, and deleting notepads.
type Manager struct {
	globalDir  string
	projectDir string

	cacheMu    sync.RWMutex
	cacheKey   string      // FNV hash of all .md file names+sizes+mtimes
	cacheValue []*Notepad  // cached Load() result for cacheKey
}

// NewManager creates a Manager with explicit directories.
func NewManager(globalDir, projectDir string) *Manager {
	return &Manager{globalDir: globalDir, projectDir: projectDir}
}

// DefaultManager creates a Manager using standard home directory paths.
func DefaultManager(projectRoot string) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	globalDir := filepath.Join(home, ".huginn", "notepads")
	projectDir := ""
	if projectRoot != "" {
		projectDir = filepath.Join(projectRoot, ".huginn", "notepads")
	}
	return NewManager(globalDir, projectDir), nil
}

// Load reads all notepads from global and project directories.
// Project notepads override global ones with the same name.
// Results are cached by an FNV hash of all .md file names, sizes, and mtimes;
// repeated calls with no file-system changes return the cached slice instantly.
func (m *Manager) Load() ([]*Notepad, error) {
	key := m.dirCacheKey()

	// Fast path: cache hit.
	m.cacheMu.RLock()
	if key != "" && key == m.cacheKey {
		cached := m.cacheValue
		m.cacheMu.RUnlock()
		return cached, nil
	}
	m.cacheMu.RUnlock()

	// Slow path: reload from disk.
	global, err := m.loadDir(m.globalDir, "global")
	if err != nil {
		return nil, err
	}
	project, err := m.loadDir(m.projectDir, "project")
	if err != nil {
		return nil, err
	}

	// Merge: project overrides global
	merged := make(map[string]*Notepad)
	for _, np := range global {
		merged[strings.ToLower(np.Name)] = np
	}
	for _, np := range project {
		merged[strings.ToLower(np.Name)] = np
	}

	result := make([]*Notepad, 0, len(merged))
	for _, np := range merged {
		result = append(result, np)
	}

	// Sort by priority (high first), then by name
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].Name < result[j].Name
	})

	// Store in cache.
	m.cacheMu.Lock()
	m.cacheKey = key
	m.cacheValue = result
	m.cacheMu.Unlock()

	return result, nil
}

// dirCacheKey returns an FNV-64a hash string covering all *.md files in both
// the global and project directories (file path relative to its root, size,
// mtime). Returns "" for empty directories so callers can skip caching.
// The hash is seeded with both directory paths to prevent cross-manager
// collision when two managers point to different (both empty) directories.
func (m *Manager) dirCacheKey() string {
	h := fnv.New64a()
	// Seed with both directory paths so empty dirs produce unique hashes.
	_, _ = h.Write([]byte(m.globalDir))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write([]byte(m.projectDir))

	hasEntries := false
	for _, dir := range []string{m.globalDir, m.projectDir} {
		if dir == "" {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			_, _ = h.Write([]byte(rel))
			size := info.Size()
			mtime := info.ModTime().UnixNano()
			buf := [16]byte{
				byte(size >> 56), byte(size >> 48), byte(size >> 40), byte(size >> 32),
				byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size),
				byte(mtime >> 56), byte(mtime >> 48), byte(mtime >> 40), byte(mtime >> 32),
				byte(mtime >> 24), byte(mtime >> 16), byte(mtime >> 8), byte(mtime),
			}
			_, _ = h.Write(buf[:])
			hasEntries = true
			return nil
		})
	}

	if !hasEntries {
		return "" // empty dirs — skip caching to avoid stale empty-result hits
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// InvalidateCache clears the mtime cache so the next Load() re-reads from disk.
// Call this after any Create, Update, or Delete operation to ensure callers
// immediately see the updated state.
func (m *Manager) InvalidateCache() {
	m.cacheMu.Lock()
	m.cacheKey = ""
	m.cacheValue = nil
	m.cacheMu.Unlock()
}

// loadDir reads all .md files from a directory and parses them as notepads.
func (m *Manager) loadDir(dir, defaultScope string) ([]*Notepad, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("notepad: read dir %q: %w", dir, err)
	}

	var result []*Notepad
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		if !validName.MatchString(name) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		np, err := ParseNotepad(name, defaultScope, path, data)
		if err != nil {
			continue
		}
		result = append(result, np)
	}
	return result, nil
}

const maxNotepadContentBytes = 1024 * 1024 // 1 MB

// Create writes a new notepad to the appropriate directory using an atomic
// write-then-rename to protect against partial writes on disk-full conditions.
func (m *Manager) Create(name, content string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("notepad: invalid name %q", name)
	}
	if len(content) > maxNotepadContentBytes {
		return fmt.Errorf("notepad: content too large (%d bytes, max %d)", len(content), maxNotepadContentBytes)
	}
	dir := m.globalDir
	if m.projectDir != "" {
		dir = m.projectDir
	}
	os.MkdirAll(dir, 0o750)
	path := filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("notepad: %q already exists", name)
	}
	// Atomic write: write to .tmp then rename to avoid partial-write corruption.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

// Delete removes a notepad from global or project directory (project checked first).
func (m *Manager) Delete(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("notepad: invalid name %q", name)
	}
	for _, dir := range []string{m.projectDir, m.globalDir} {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name+".md")
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err != nil {
				return err
			}
			m.InvalidateCache()
			return nil
		}
	}
	return fmt.Errorf("notepad: %q not found", name)
}

// Update atomically replaces the content of an existing notepad.
// Returns an error if the notepad does not exist or content exceeds the size limit.
// Uses a write-then-rename pattern to protect against partial-write corruption.
func (m *Manager) Update(name, content string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("notepad: invalid name %q", name)
	}
	if len(content) > maxNotepadContentBytes {
		return fmt.Errorf("notepad: content too large (%d bytes, max %d)", len(content), maxNotepadContentBytes)
	}
	// Resolve which directory owns this notepad (project takes precedence).
	var path string
	for _, dir := range []string{m.projectDir, m.globalDir} {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name+".md")
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
	}
	if path == "" {
		return fmt.Errorf("notepad: %q not found", name)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	m.InvalidateCache()
	return nil
}

// Get retrieves a single notepad by name (case-insensitive).
func (m *Manager) Get(name string) (*Notepad, error) {
	all, err := m.Load()
	if err != nil {
		return nil, err
	}
	for _, np := range all {
		if strings.EqualFold(np.Name, name) {
			return np, nil
		}
	}
	return nil, fmt.Errorf("notepad: %q not found", name)
}

// List returns all loaded notepads (same as Load).
func (m *Manager) List() ([]*Notepad, error) {
	return m.Load()
}
