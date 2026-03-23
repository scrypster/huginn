package lsp

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

var ErrNotConfigured = errors.New("lsp: no server configured")

type ServerConfig struct {
	Command string
	Args    []string
}

type Manager struct {
	mu     sync.Mutex
	cfg    ServerConfig
	lang   string
	cmd    *exec.Cmd
	client *Client
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func NewManager(lang string, cfg ServerConfig) *Manager {
	return &Manager{lang: lang, cfg: cfg}
}

func (m *Manager) Start(rootDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Command == "" {
		return ErrNotConfigured
	}
	if m.client != nil && m.client.initDone {
		return nil
	}
	cmd := exec.Command(m.cfg.Command, m.cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	m.cmd = cmd
	m.stdin = stdin
	m.stdout = stdout
	rw := struct {
		io.Reader
		io.Writer
	}{stdout, stdin}
	m.client = NewClient(rw, m.lang)
	if err := m.client.Initialize("file://" + rootDir); err != nil {
		m.stop()
		return fmt.Errorf("initialize: %w", err)
	}
	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stop()
}

func (m *Manager) stop() error {
	if m.cmd == nil {
		return nil
	}
	if m.stdin != nil {
		m.stdin.Close()
	}
	if m.stdout != nil {
		m.stdout.Close()
	}
	if m.cmd.Process != nil {
		m.cmd.Process.Kill()
	}
	m.cmd.Wait()
	m.cmd = nil
	m.client = nil
	return nil
}

func (m *Manager) Definition(fileURI string, line, column int) ([]Location, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Command == "" {
		return nil, ErrNotConfigured
	}
	if m.client == nil {
		return nil, fmt.Errorf("lsp: not started")
	}
	return m.client.TextDocumentDefinition(fileURI, line, column)
}

func (m *Manager) Symbols(query string) ([]SymbolInformation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.Command == "" {
		return nil, ErrNotConfigured
	}
	if m.client == nil {
		return nil, fmt.Errorf("lsp: not started")
	}
	return m.client.WorkspaceSymbol(query)
}
