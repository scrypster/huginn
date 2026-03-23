package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/tools"
)

// ErrCircuitOpen is returned by CallToolGated when the circuit breaker for a
// server is in the open state and the call is rejected without reaching the wire.
var ErrCircuitOpen = errors.New("mcp: circuit open")

const (
	cbClosed   = 0
	cbOpen     = 1
	cbHalfOpen = 2

	cbFailureThreshold = 3
	cbOpenDuration     = 10 * time.Second

	// cbProbeTimeout is the maximum time allowed for a circuit-breaker health probe
	// when the circuit transitions from open → half-open. A dedicated, shorter
	// timeout prevents a slow server from holding up the half-open probe for the
	// full 2-minute tool call timeout.
	cbProbeTimeout = 10 * time.Second

	// MethodNotFoundCode is the JSON-RPC error code for "method not found",
	// returned by servers that do not implement the ping method.
	MethodNotFoundCode = -32601
)

type ClientFactory func(ctx context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error)

type ServerManager struct {
	configs         []MCPServerConfig
	factory         ClientFactory
	initBackoff     time.Duration
	maxBackoff      time.Duration
	healthInterval  time.Duration
	mu              sync.Mutex
	clients         []*managedServer
	registeredTools map[string][]string // server name → tool names
}

type managedServer struct {
	cfg    MCPServerConfig
	client *MCPClient
	cancel context.CancelFunc

	// circuit breaker state (guarded by ServerManager.mu)
	cbState    int
	cbFailures int
	cbOpenAt   time.Time

	// probedWithListTools is set when the server does not support ping and we
	// fall back to ListTools for health probing (guarded by ServerManager.mu).
	probedWithListTools bool

	// names of tools currently registered for this server (guarded by ServerManager.mu)
	registeredToolNames []string
}

// cbShouldAllow reports whether a call should be allowed through according to
// the circuit breaker state.  Must be called with m.mu held.
func (m *ServerManager) cbShouldAllow(ms *managedServer) bool {
	switch ms.cbState {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(ms.cbOpenAt) >= cbOpenDuration {
			ms.cbState = cbHalfOpen
			return true
		}
		return false
	case cbHalfOpen:
		// allow one probe through
		return true
	default:
		return true
	}
}

// cbRecordSuccess resets the circuit breaker to closed.
// Must be called with m.mu held.
func (m *ServerManager) cbRecordSuccess(ms *managedServer) {
	ms.cbState = cbClosed
	ms.cbFailures = 0
}

// cbRecordFailure increments the failure counter and may trip the breaker open.
// Must be called with m.mu held.
func (m *ServerManager) cbRecordFailure(ms *managedServer) {
	if ms.cbState == cbHalfOpen {
		// probe failed – stay/return to open
		ms.cbState = cbOpen
		ms.cbOpenAt = time.Now()
		return
	}
	ms.cbFailures++
	if ms.cbFailures >= cbFailureThreshold {
		ms.cbState = cbOpen
		ms.cbOpenAt = time.Now()
	}
}

type ManagerOption func(*ServerManager)

func WithClientFactory(f ClientFactory) ManagerOption {
	return func(m *ServerManager) { m.factory = f }
}

func WithRestartBackoff(initial, max time.Duration) ManagerOption {
	return func(m *ServerManager) {
		m.initBackoff = initial
		m.maxBackoff = max
	}
}

// WithHealthInterval sets the interval between health probes in watchServer.
func WithHealthInterval(d time.Duration) ManagerOption {
	return func(m *ServerManager) { m.healthInterval = d }
}

// registerServerTools unregisters any previously registered tools for the
// named server, then registers the new tool set and tracks the names.
// Must be called with m.mu held.
func (m *ServerManager) registerServerTools(serverName string, client *MCPClient, mcpTools []MCPTool, reg *tools.Registry) {
	// Unregister stale tools.
	if m.registeredTools == nil {
		m.registeredTools = make(map[string][]string)
	}
	for _, name := range m.registeredTools[serverName] {
		reg.Unregister(name)
	}
	// Register new tools.
	var names []string
	for _, t := range mcpTools {
		// Find the managedServer for this client (nil ms means no CB gating).
		reg.Register(NewMCPToolAdapterGated(client, t, m, nil))
		names = append(names, t.Name)
	}
	m.registeredTools[serverName] = names
}

// probeHealth sends a ping to ms.client and falls back to ListTools if the
// server does not support ping (MethodNotFound). It updates ms.probedWithListTools
// accordingly. The caller should hold no lock when calling this method.
func (m *ServerManager) probeHealth(ctx context.Context, ms *managedServer) error {
	m.mu.Lock()
	useFallback := ms.probedWithListTools
	client := ms.client
	m.mu.Unlock()

	if useFallback {
		_, err := client.ListTools(ctx)
		return err
	}

	err := client.Ping(ctx)
	if err == nil {
		return nil
	}

	// Check if the server returned MethodNotFound — fall back to ListTools.
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == MethodNotFoundCode {
		_, listErr := client.ListTools(ctx)
		if listErr == nil {
			m.mu.Lock()
			ms.probedWithListTools = true
			m.mu.Unlock()
		}
		return listErr
	}
	return err
}

// CircuitState returns a human-readable state string ("closed", "open", "half-open")
// for the named server, or "closed" if the server is not found.
func (m *ServerManager) CircuitState(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ms := range m.clients {
		if ms.cfg.Name == name {
			switch ms.cbState {
			case cbOpen:
				return "open"
			case cbHalfOpen:
				return "half-open"
			default:
				return "closed"
			}
		}
	}
	return "closed"
}

func NewServerManager(cfgs []MCPServerConfig, opts ...ManagerOption) *ServerManager {
	m := &ServerManager{
		configs:     cfgs,
		factory:     defaultClientFactory,
		initBackoff: 1 * time.Second,
		maxBackoff:  30 * time.Second,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *ServerManager) StartAll(ctx context.Context, reg *tools.Registry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cfg := range m.configs {
		cfg := cfg
		client, mcpTools, err := m.factory(ctx, cfg)
		if err != nil {
			logger.Warn("mcp: server unavailable", "server", cfg.Name, "err", err)
			continue
		}
		svrCtx, cancel := context.WithCancel(ctx)
		ms := &managedServer{cfg: cfg, client: client, cancel: cancel}
		var names []string
		for _, t := range mcpTools {
			reg.Register(NewMCPToolAdapterGated(client, t, m, ms))
			names = append(names, t.Name)
		}
		ms.registeredToolNames = names
		m.clients = append(m.clients, ms)
		go m.watchServer(svrCtx, ms, reg)
	}
}

func (m *ServerManager) watchServer(ctx context.Context, ms *managedServer, reg *tools.Registry) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("mcp: watchServer panic recovered", "server", ms.cfg.Name, "panic", r)
		}
	}()
	backoff := m.initBackoff
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		hCtx, hCancel := context.WithTimeout(ctx, cbProbeTimeout)
		err := m.probeHealth(hCtx, ms)
		hCancel()
		if err == nil {
			interval := m.healthInterval
			if interval <= 0 {
				interval = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
				continue
			}
		}
		logger.Warn("mcp: server unhealthy, restarting", "server", ms.cfg.Name, "backoff", backoff)

		// Deregister this server's tools from the shared registry before restart.
		m.mu.Lock()
		m.cbRecordFailure(ms)
		for _, name := range ms.registeredToolNames {
			reg.Unregister(name)
		}
		ms.registeredToolNames = nil
		m.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		newClient, mcpTools, err := m.factory(ctx, ms.cfg)
		if err != nil {
			if backoff < m.maxBackoff {
				backoff *= 2
			}
			if backoff > m.maxBackoff {
				backoff = m.maxBackoff
			}
			continue
		}

		// Re-register tools cleanly (slot is clear after deregister above).
		m.mu.Lock()
		ms.client = newClient
		var names []string
		for _, t := range mcpTools {
			reg.Register(NewMCPToolAdapterGated(newClient, t, m, ms))
			names = append(names, t.Name)
		}
		ms.registeredToolNames = names
		m.cbRecordSuccess(ms)
		m.mu.Unlock()

		backoff = m.initBackoff
	}
}

func (m *ServerManager) StopAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ms := range m.clients {
		ms.cancel()
		ms.client.Close()
	}
	m.clients = nil
}

// CallToolGated executes a tool call through the circuit breaker for ms.
// It returns ErrCircuitOpen if the circuit is open and the call should not
// be attempted.  On success it records a success; on transport error it
// records a failure.
func (m *ServerManager) CallToolGated(ctx context.Context, ms *managedServer, name string, args map[string]any) (*MCPToolCallResult, error) {
	m.mu.Lock()
	allow := m.cbShouldAllow(ms)
	client := ms.client
	m.mu.Unlock()

	if !allow {
		return nil, ErrCircuitOpen
	}

	result, err := client.CallTool(ctx, name, args)
	if err != nil {
		m.mu.Lock()
		m.cbRecordFailure(ms)
		m.mu.Unlock()
		return nil, err
	}

	m.mu.Lock()
	m.cbRecordSuccess(ms)
	m.mu.Unlock()
	return result, nil
}

func defaultClientFactory(ctx context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error) {
	transport := cfg.Transport
	if transport == "" {
		transport = "stdio"
	}
	var tr Transport
	var err error
	switch transport {
	case "stdio":
		if cfg.Command == "" {
			return nil, nil, fmt.Errorf("stdio requires command")
		}
		tr, err = NewStdioTransport(ctx, cfg.Command, cfg.Args, cfg.Env)
	case "http":
		if cfg.URL == "" {
			return nil, nil, fmt.Errorf("http transport requires url")
		}
		token := ""
		for _, e := range cfg.Env {
			if strings.HasPrefix(e, "BEARER_TOKEN=") {
				token = strings.TrimPrefix(e, "BEARER_TOKEN=")
			}
		}
		tr = NewHTTPTransport(cfg.URL, token)
		err = nil
	default:
		return nil, nil, fmt.Errorf("unknown transport %q", transport)
	}
	if err != nil {
		return nil, nil, err
	}
	client := NewMCPClient(tr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		tr.Close()
		return nil, nil, err
	}
	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		tr.Close()
		return nil, nil, err
	}
	return client, mcpTools, nil
}
