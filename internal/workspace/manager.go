package workspace

import (
	"sync"
)

// Manager wraps workspace discovery and provides a stable Root() for the
// rest of the system. It is safe for concurrent use.
type Manager struct {
	mu       sync.RWMutex
	root     string
	method   string // how root was discovered: "config|git|gomod|packagejson|cwd"
	cfg      *WorkspaceConfig
	startDir string // the directory passed to NewManager (used by Refresh)
}

// NewManager creates a Manager by running DiscoverRoot from startDir.
func NewManager(startDir string) (*Manager, error) {
	root, method, err := DiscoverRoot(startDir)
	if err != nil {
		return nil, err
	}

	// Attempt to load .huginn/workspace.json from the discovered root.
	// LoadConfig accepts the workspace root and appends .huginn/workspace.json internally.
	// Errors are non-fatal (file may not exist); cfg will be nil if missing or on error.
	cfg, _ := LoadConfig(root)

	return &Manager{
		root:     root,
		method:   method,
		cfg:      cfg,
		startDir: startDir,
	}, nil
}

// Root returns the discovered workspace root. Safe for concurrent use.
func (m *Manager) Root() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.root
}

// Method returns the discovery method used: "config", "git", "gomod",
// "packagejson", or "cwd".
func (m *Manager) Method() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.method
}

// Config returns the parsed WorkspaceConfig, or nil if no .huginn/workspace.json
// was found.
func (m *Manager) Config() *WorkspaceConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Refresh re-runs discovery from the original startDir. Useful when the
// workspace changes (e.g. user cd'd or created a .huginn/workspace.json).
func (m *Manager) Refresh() error {
	root, method, err := DiscoverRoot(m.startDir)
	if err != nil {
		return err
	}
	cfg, _ := LoadConfig(root)

	m.mu.Lock()
	m.root = root
	m.method = method
	m.cfg = cfg
	m.mu.Unlock()
	return nil
}
