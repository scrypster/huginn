package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/logger"
	mcp "github.com/scrypster/huginn/internal/mcp"
	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/tools"
)

const (
	vaultMaxAttempts = 2
	vaultBaseDelay   = 500 * time.Millisecond
	vaultJitterMax   = 200 * time.Millisecond
)

// vaultHealthCacheTTL is how long a ProbeVaultConnectivity result is cached.
// Health probes are short-lived MCP connections; caching prevents hammering
// the vault server when the UI polls the vault-status endpoint.
const vaultHealthCacheTTL = 30 * time.Second

type vaultHealthEntry struct {
	toolsCount int
	warning    string
	fetchedAt  time.Time
}

var (
	vaultHealthCacheMu sync.Mutex
	vaultHealthCache   = make(map[string]vaultHealthEntry)
)

// isVaultConnectionError reports whether err indicates a lost transport connection
// to the MuninnDB vault (EOF, closed pipe, reset). Intentional teardowns such as
// context cancellation or deadline exceeded are NOT classified as connection errors.
func isVaultConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "EOF")
}

// connectVaultWithRetry retries Initialize+ListTools up to maxAttempts times.
// Each retry waits vaultBaseDelay + up to vaultJitterMax to reduce thundering-herd.
// Returns the connected client, its cancel func, the tool list, and any error.
func connectVaultWithRetry(ctx context.Context, buildFn func() (*mcp.MCPClient, func()), maxAttempts int) (*mcp.MCPClient, func(), []mcp.MCPTool, error) {
	if maxAttempts <= 0 {
		return nil, nil, nil, fmt.Errorf("connectVaultWithRetry: maxAttempts must be > 0, got %d", maxAttempts)
	}
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		// Bail immediately if the parent context is already cancelled.
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		if i > 0 {
			jitter := time.Duration(rand.Int63n(int64(vaultJitterMax)))
			select {
			case <-time.After(vaultBaseDelay + jitter):
			case <-ctx.Done():
				return nil, nil, nil, ctx.Err()
			}
		}
		client, cancelFn := buildFn()
		cCtx, cdone := context.WithTimeout(ctx, 10*time.Second)
		initErr := client.Initialize(cCtx)
		cdone()
		if initErr != nil {
			cancelFn()
			lastErr = initErr
			continue
		}
		lCtx, ldone := context.WithTimeout(ctx, 5*time.Second)
		ts, listErr := client.ListTools(lCtx)
		ldone()
		if listErr != nil {
			cancelFn()
			lastErr = listErr
			continue
		}
		return client, cancelFn, ts, nil
	}
	return nil, nil, nil, lastErr
}

// VaultReconnector coordinates mid-session MCP vault reconnects.
// It owns the vault tool lifecycle: connect → unregister/re-register on loss → close.
// Only one reconnect runs at a time (TryLock); concurrent callers return false immediately.
type VaultReconnector struct {
	mu            sync.Mutex
	buildFn       func() (*mcp.MCPClient, func()) // captures endpoint+muninnCfg; re-reads token each call
	sessionReg    *tools.Registry                  // the per-session fork (never the shared parent)
	toolNames     []string                         // names currently registered, for cleanup
	cancelCurrent func()                           // closes the current MCP transport

	// warnOnce gates the "vault lost" StreamWarning per connection lifetime.
	// Stored as atomic.Pointer so TryReconnect (under mu) can reset it without
	// racing concurrent EmitWarnOnce calls.
	warnOnce sync.Map // key: struct{}, value: *sync.Once — use Load/Store via warnOnceMu
}

// warnOnceKey is the key used in the sync.Map for the warn-once gate.
type warnOnceKey struct{}

// newVaultReconnector constructs a VaultReconnector for a successful vault connection.
func newVaultReconnector(
	buildFn func() (*mcp.MCPClient, func()),
	sessionReg *tools.Registry,
	toolNames []string,
	cancelFn func(),
) *VaultReconnector {
	vr := &VaultReconnector{
		buildFn:       buildFn,
		sessionReg:    sessionReg,
		toolNames:     append([]string(nil), toolNames...),
		cancelCurrent: cancelFn,
	}
	vr.warnOnce.Store(warnOnceKey{}, new(sync.Once))
	return vr
}

// EmitWarnOnce fires fn at most once per connection lifetime.
// Resets automatically after a successful TryReconnect so a second disconnection
// still surfaces a warning to the user.
func (vr *VaultReconnector) EmitWarnOnce(fn func()) {
	if v, ok := vr.warnOnce.Load(warnOnceKey{}); ok {
		v.(*sync.Once).Do(fn)
	}
}

// TryReconnect attempts a single reconnect cycle.
// Returns true only when the vault is successfully reconnected and tools are re-registered.
// Concurrent callers that fail TryLock return false immediately — their tool error propagates
// to tryExecuteTool which then probes the registry for a fresh adapter (post-race retry).
func (vr *VaultReconnector) TryReconnect(ctx context.Context) bool {
	if !vr.mu.TryLock() {
		return false
	}
	defer vr.mu.Unlock()

	// Unregister stale tools before attempting reconnect.
	for _, name := range vr.toolNames {
		vr.sessionReg.Unregister(name)
	}
	vr.toolNames = nil

	// Close stale MCP client.
	if vr.cancelCurrent != nil {
		vr.cancelCurrent()
		vr.cancelCurrent = nil
	}

	client, cancelFn, mcpTools, err := connectVaultWithRetry(ctx, vr.buildFn, vaultMaxAttempts)
	if err != nil {
		slog.Warn("agent: vault reconnect failed", "err", err)
		return false
	}

	newNames := make([]string, 0, len(mcpTools))
	for _, t := range mcpTools {
		vr.sessionReg.Register(mcp.NewMCPToolAdapter(client, t))
		vr.sessionReg.TagTools([]string{t.Name}, "muninndb")
		newNames = append(newNames, t.Name)
	}
	vr.toolNames = newNames
	vr.cancelCurrent = cancelFn

	// Reset the warn gate so a second disconnection surfaces another warning to the user.
	vr.warnOnce.Store(warnOnceKey{}, new(sync.Once))

	slog.Info("agent: vault reconnected mid-session", "tools", len(mcpTools))
	return true
}

// Close unregisters all vault tools and tears down the MCP transport.
// Safe to call multiple times and from defer.
func (vr *VaultReconnector) Close() {
	vr.mu.Lock()
	defer vr.mu.Unlock()
	for _, name := range vr.toolNames {
		vr.sessionReg.Unregister(name)
	}
	vr.toolNames = nil
	if vr.cancelCurrent != nil {
		vr.cancelCurrent()
		vr.cancelCurrent = nil
	}
}

// vaultResult holds the outcome of a connectAgentVault call.
type vaultResult struct {
	// sessionReg is a forked registry with vault tools registered if the connection
	// succeeded. It is always non-nil and safe to use even when the vault is unavailable.
	sessionReg *tools.Registry
	// cancel closes the MCP client. Always non-nil; safe to call multiple times.
	// MUST be deferred by the caller to prevent connection leaks.
	// When reconnector is non-nil, cancel delegates to reconnector.Close().
	cancel func()
	// reconnector enables mid-session vault reconnect. Nil when the vault is not configured
	// or the initial connection failed (degraded-mode session).
	reconnector *VaultReconnector
	// memoryBlock is injected into the system prompt. Empty when vault is unavailable,
	// preventing the LLM from referencing tools that are not registered.
	memoryBlock string
	// warning is non-empty when the vault connection failed (degraded-mode session).
	warning string
}

// ProbeVaultConnectivity is a lightweight connectivity check for a named vault.
// It resolves the vault token from the muninn global config at cfgPath, opens a
// short-lived MCP connection (5 s timeout), runs Initialize + ListTools, and
// returns (toolsCount, warning, nil) on success or (0, "", err) on any failure.
//
// warning is non-empty when the probe succeeded but the vault token is about to
// expire (within 5 minutes). The caller should surface this to the user so they
// can rotate the token before the session fails mid-conversation.
//
// This is used by the server's vault-test endpoint so it returns a truthful
// "connected" status rather than merely confirming that the vault is configured.
func ProbeVaultConnectivity(ctx context.Context, cfgPath, vaultName string) (int, string, error) {
	if cfgPath == "" {
		return 0, "", fmt.Errorf("muninn config path not set")
	}
	if vaultName == "" {
		return 0, "", fmt.Errorf("vault name is empty")
	}

	// Check cache before opening a connection.
	cacheKey := cfgPath + "\x00" + vaultName
	vaultHealthCacheMu.Lock()
	if entry, ok := vaultHealthCache[cacheKey]; ok && time.Since(entry.fetchedAt) < vaultHealthCacheTTL {
		vaultHealthCacheMu.Unlock()
		return entry.toolsCount, entry.warning, nil
	}
	vaultHealthCacheMu.Unlock()

	muninnCfg, err := mem.LoadGlobalConfig(cfgPath)
	if err != nil || muninnCfg.Endpoint == "" {
		if err != nil {
			return 0, "", fmt.Errorf("muninn config load: %w", err)
		}
		return 0, "", fmt.Errorf("muninn endpoint not configured")
	}

	token, err := mem.VaultTokenFor(muninnCfg, vaultName)
	if err != nil {
		return 0, "", fmt.Errorf("no token for vault %q: %w", vaultName, err)
	}

	// Best-effort token expiry check: try to decode the token as a JWT and
	// inspect the "exp" claim. If the token is not a JWT or has no "exp" claim,
	// this check is silently skipped and no warning is emitted.
	var tokenWarning string
	if tokenExpiresAt := jwtExpiry(token); tokenExpiresAt != nil {
		remaining := time.Until(*tokenExpiresAt)
		if remaining < 5*time.Minute {
			slog.Warn("vault: token expires soon",
				"vault", vaultName,
				"expires_in", remaining.Round(time.Second))
			tokenWarning = fmt.Sprintf("token expires in %s", remaining.Round(time.Second))
		}
	}

	mcpURL, err := mem.MCPURLFromEndpoint(muninnCfg.Endpoint)
	if err != nil {
		return 0, "", fmt.Errorf("invalid muninn endpoint: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	buildFn := func() (*mcp.MCPClient, func()) {
		tr := mcp.NewHTTPTransport(mcpURL, token)
		c := mcp.NewMCPClient(tr)
		return c, func() { c.Close() }
	}

	client, cancelFn, mcpTools, err := connectVaultWithRetry(probeCtx, buildFn, 1)
	if err != nil {
		return 0, "", fmt.Errorf("connect: %w", err)
	}
	cancelFn()
	_ = client

	// Cache the successful result.
	vaultHealthCacheMu.Lock()
	vaultHealthCache[cacheKey] = vaultHealthEntry{
		toolsCount: len(mcpTools),
		warning:    tokenWarning,
		fetchedAt:  time.Now(),
	}
	vaultHealthCacheMu.Unlock()

	return len(mcpTools), tokenWarning, nil
}

// jwtExpiry attempts to decode a JWT token's "exp" claim without verifying the
// signature. Returns nil if the token is not a valid JWT or has no "exp" claim.
// This is intentionally best-effort — a malformed token simply produces no warning.
func jwtExpiry(token string) *time.Time {
	// A JWT consists of exactly three base64url-encoded segments separated by dots.
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	// Decode the payload (second segment). base64.RawURLEncoding handles the
	// missing padding that is standard in JWT base64url encoding.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return nil
	}
	t := time.Unix(claims.Exp, 0)
	return &t
}

// connectAgentVault forks the shared registry, connects to the agent's MuninnDB vault,
// and registers the vault's tools into the fork. The shared registry is never mutated.
//
// The caller MUST defer result.cancel() to close the MCP client at session end.
// When the WebSocket session drops, the relay cancels conn.ctx, which propagates to
// all in-flight MCP calls via the session context, and the deferred cancel() closes
// the client. No connection leaks occur on unexpected disconnect.
//
// All failure paths return a valid sessionReg (without vault tools) so the session
// degrades gracefully — the agent runs without memory rather than failing hard.
func (o *Orchestrator) connectAgentVault(ctx context.Context, ag *agents.Agent, sharedReg *tools.Registry) vaultResult {
	// Always fork the shared registry — per-session tools go into the fork only.
	sessionReg := sharedReg.Fork()

	if ag == nil || !ag.MemoryEnabled || ag.VaultName == "" {
		logger.Warn("muninn mcp: skipping vault connect", "agent_nil", ag == nil,
			"memory_enabled", ag != nil && ag.MemoryEnabled, "vault_name", func() string {
				if ag != nil {
					return ag.VaultName
				}
				return ""
			}())
		return vaultResult{sessionReg: sessionReg, cancel: func() {}}
	}

	logger.Warn("muninn mcp: connecting vault", "agent", ag.Name, "vault", ag.VaultName)

	o.mu.Lock()
	cfgPath := o.muninnCfgPath
	o.mu.Unlock()

	if cfgPath == "" {
		logger.Warn("muninn mcp: config path not set", "agent", ag.Name)
		return vaultResult{sessionReg: sessionReg, cancel: func() {}, warning: "muninn config path not set"}
	}

	muninnCfg, err := mem.LoadGlobalConfig(cfgPath)
	if err != nil || muninnCfg.Endpoint == "" {
		warn := "muninn config unavailable"
		if err != nil {
			warn = fmt.Sprintf("muninn config load: %v", err)
		}
		logger.Warn("muninn mcp: config unavailable", "agent", ag.Name, "cfg_path", cfgPath, "err", err, "endpoint", muninnCfg.Endpoint)
		return vaultResult{sessionReg: sessionReg, cancel: func() {}, warning: warn}
	}

	token, err := mem.VaultTokenFor(muninnCfg, ag.VaultName)
	if err != nil {
		warn := fmt.Sprintf("no token for vault %q", ag.VaultName)
		logger.Warn("muninn mcp: no token for vault", "agent", ag.Name, "vault", ag.VaultName)
		return vaultResult{sessionReg: sessionReg, cancel: func() {}, warning: warn}
	}

	mcpURL, err := mem.MCPURLFromEndpoint(muninnCfg.Endpoint)
	if err != nil {
		warn := fmt.Sprintf("invalid muninn endpoint: %v", err)
		logger.Warn("muninn mcp: invalid endpoint", "agent", ag.Name, "err", err)
		return vaultResult{sessionReg: sessionReg, cancel: func() {}, warning: warn}
	}

	// buildFn re-reads the vault token on every invocation so that a rotated JWT
	// is picked up automatically on mid-session reconnect. Falls back to the
	// session-start token if the re-read fails (avoids hard failure on transient errors).
	buildFn := func() (*mcp.MCPClient, func()) {
		freshToken, err := mem.VaultTokenFor(muninnCfg, ag.VaultName)
		if err != nil {
			slog.Warn("vault reconnect: token re-read failed, using session token", "err", err)
			freshToken = token
		}
		tr := mcp.NewHTTPTransport(mcpURL, freshToken)
		c := mcp.NewMCPClient(tr)
		return c, func() { c.Close() }
	}

	client, cancelFn, mcpTools, err := connectVaultWithRetry(ctx, buildFn, vaultMaxAttempts)
	if err != nil {
		warn := fmt.Sprintf("connect: %v", err)
		logger.Warn("muninn mcp: connect failed", "agent", ag.Name, "vault", ag.VaultName, "err", err)
		return vaultResult{sessionReg: sessionReg, cancel: func() {}, warning: warn}
	}

	// Register vault tools into the FORK only — never the shared parent registry.
	toolNames := make([]string, 0, len(mcpTools))
	for _, t := range mcpTools {
		sessionReg.Register(mcp.NewMCPToolAdapter(client, t))
		sessionReg.TagTools([]string{t.Name}, "muninndb")
		toolNames = append(toolNames, t.Name)
	}

	// Construct a reconnector that owns the vault MCP client lifecycle.
	// cancel delegates to reconnector.Close() so existing defer vr.cancel() callers
	// continue to work without modification.
	reconnector := newVaultReconnector(buildFn, sessionReg, toolNames, cancelFn)

	// memoryBlock is only populated on successful connection.
	block := agents.BuildMemoryBlock(ag)
	logger.Info("muninn mcp: vault connected", "agent", ag.Name, "vault", ag.VaultName, "tools", len(mcpTools))
	return vaultResult{
		sessionReg:  sessionReg,
		cancel:      reconnector.Close,
		reconnector: reconnector,
		memoryBlock: block,
	}
}
