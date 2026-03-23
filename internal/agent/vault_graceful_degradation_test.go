package agent

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/tools"
)

// writeMuninnConfig writes a minimal muninn.json to dir and returns the path.
// It also writes a vault token for the given vaultName so the code path reaches
// the MCP Initialize call.
func writeMuninnConfig(t *testing.T, dir string, endpoint string, vaultName string) string {
	t.Helper()
	type muninnCfg struct {
		Endpoint    string            `json:"endpoint"`
		VaultTokens map[string]string `json:"vault_tokens"`
	}
	cfg := muninnCfg{
		Endpoint:    endpoint,
		VaultTokens: map[string]string{vaultName: "test-token"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal muninn config: %v", err)
	}
	cfgPath := filepath.Join(dir, "muninn.json")
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatalf("write muninn config: %v", err)
	}
	return cfgPath
}

// findFreePort returns a TCP port that is free at call time. Because the port is
// released immediately, there is a small TOCTOU window, but it is good enough for
// tests that only need "connection refused".
func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// TestVaultUnavailable_SessionStillWorks verifies that when the MCP vault server
// is unreachable (connection refused on Initialize), connectAgentVault returns a
// valid, non-nil sessionReg so the agent can still run in degraded mode.
func TestVaultUnavailable_SessionStillWorks(t *testing.T) {
	// Use a port that is not listening — Initialize will fail immediately with
	// "connection refused" rather than hanging.
	port := findFreePort(t)
	endpoint := "http://127.0.0.1:" + itoa(port)

	dir := t.TempDir()
	vaultName := "huginn:agent:default:test-agent"
	cfgPath := writeMuninnConfig(t, dir, endpoint, vaultName)

	o := newTestOrchestrator()
	o.muninnCfgPath = cfgPath

	parent := tools.NewRegistry()
	parent.Register(&vaultStubTool{name: "bash"})

	ag := &agents.Agent{
		Name:          "test-agent",
		MemoryEnabled: true,
		VaultName:     vaultName,
	}

	vr := o.connectAgentVault(context.Background(), ag, parent)
	defer vr.cancel()

	// The session registry must always be non-nil — the agent must be runnable.
	if vr.sessionReg == nil {
		t.Fatal("connectAgentVault must return a non-nil sessionReg even when vault is unavailable")
	}

	// Vault failure must produce a warning, not a panic.
	if vr.warning == "" {
		t.Error("expected a warning when the vault MCP endpoint is unreachable")
	}

	// No memory block injected on failure — the LLM must not reference tools that
	// are not registered.
	if vr.memoryBlock != "" {
		t.Errorf("memoryBlock must be empty on vault failure, got %q", vr.memoryBlock)
	}

	// The fork must still inherit parent tools.
	if _, ok := vr.sessionReg.Get("bash"); !ok {
		t.Error("sessionReg must inherit parent tools even when vault is unavailable")
	}

	// No vault tools must have been registered on failure.
	names := registeredToolNames(vr.sessionReg)
	for _, n := range names {
		if strings.HasPrefix(n, "muninn_") {
			t.Errorf("vault tool %q must not be registered on connection failure", n)
		}
	}
}

// TestVaultUnavailable_NoPanic verifies that connectAgentVault never panics,
// even when the vault endpoint is unreachable and the context has a very short deadline.
func TestVaultUnavailable_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("connectAgentVault panicked: %v", r)
		}
	}()

	port := findFreePort(t)
	endpoint := "http://127.0.0.1:" + itoa(port)

	dir := t.TempDir()
	vaultName := "huginn:agent:default:panic-agent"
	cfgPath := writeMuninnConfig(t, dir, endpoint, vaultName)

	o := newTestOrchestrator()
	o.muninnCfgPath = cfgPath

	parent := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "panic-agent",
		MemoryEnabled: true,
		VaultName:     vaultName,
	}

	// Very short deadline — exercises the timeout/cancel path.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	vr := o.connectAgentVault(ctx, ag, parent)
	defer vr.cancel()

	if vr.sessionReg == nil {
		t.Fatal("sessionReg must never be nil")
	}
}

// TestVaultUnavailable_CancelIsAlwaysSafe verifies that vr.cancel() can be
// called multiple times without panicking, even after a failed vault connection.
func TestVaultUnavailable_CancelIsAlwaysSafe(t *testing.T) {
	port := findFreePort(t)
	endpoint := "http://127.0.0.1:" + itoa(port)

	dir := t.TempDir()
	vaultName := "huginn:agent:default:cancel-agent"
	cfgPath := writeMuninnConfig(t, dir, endpoint, vaultName)

	o := newTestOrchestrator()
	o.muninnCfgPath = cfgPath

	parent := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "cancel-agent",
		MemoryEnabled: true,
		VaultName:     vaultName,
	}

	vr := o.connectAgentVault(context.Background(), ag, parent)

	// Calling cancel multiple times must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("vr.cancel() panicked: %v", r)
		}
	}()
	vr.cancel()
	vr.cancel()
	vr.cancel()
}

// TestVaultUnavailable_DoesNotBlockLong verifies that connectAgentVault returns
// within a reasonable time when the vault is unreachable. Because the transport
// connects to a port with nothing listening, the dial should fail quickly
// (connection refused), well within 5 seconds.
func TestVaultUnavailable_DoesNotBlockLong(t *testing.T) {
	port := findFreePort(t)
	endpoint := "http://127.0.0.1:" + itoa(port)

	dir := t.TempDir()
	vaultName := "huginn:agent:default:timing-agent"
	cfgPath := writeMuninnConfig(t, dir, endpoint, vaultName)

	o := newTestOrchestrator()
	o.muninnCfgPath = cfgPath

	parent := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "timing-agent",
		MemoryEnabled: true,
		VaultName:     vaultName,
	}

	done := make(chan struct{})
	go func() {
		vr := o.connectAgentVault(context.Background(), ag, parent)
		vr.cancel()
		close(done)
	}()

	select {
	case <-done:
		// Fast path: connection refused returned immediately — good.
	case <-time.After(5 * time.Second):
		t.Fatal("connectAgentVault blocked for more than 5 seconds with an unreachable vault")
	}
}

// TestVaultConfigMissing_SessionStillWorks verifies graceful degradation when
// the muninn config file does not exist (common on first run / unconfigured system).
func TestVaultConfigMissing_SessionStillWorks(t *testing.T) {
	o := newTestOrchestrator()
	o.muninnCfgPath = "/nonexistent/path/to/muninn.json"

	parent := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "no-config-agent",
		MemoryEnabled: true,
		VaultName:     "huginn:agent:default:no-config-agent",
	}

	vr := o.connectAgentVault(context.Background(), ag, parent)
	defer vr.cancel()

	if vr.sessionReg == nil {
		t.Fatal("sessionReg must be non-nil even when config file is missing")
	}
	// A warning should be produced so operators can diagnose the issue.
	if vr.warning == "" {
		t.Error("expected warning when muninn config file does not exist")
	}
	if vr.memoryBlock != "" {
		t.Errorf("memoryBlock must be empty when vault config is missing, got %q", vr.memoryBlock)
	}
}

// TestVaultEmptyEndpoint_SessionStillWorks verifies graceful degradation when
// the muninn config exists but has an empty endpoint (vault not yet pointed at a server).
func TestVaultEmptyEndpoint_SessionStillWorks(t *testing.T) {
	dir := t.TempDir()
	vaultName := "huginn:agent:default:empty-ep-agent"

	// Config with empty endpoint.
	type muninnCfg struct {
		Endpoint    string            `json:"endpoint"`
		VaultTokens map[string]string `json:"vault_tokens"`
	}
	cfg := muninnCfg{
		Endpoint:    "", // deliberately empty
		VaultTokens: map[string]string{vaultName: "tok"},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "muninn.json")
	_ = os.WriteFile(cfgPath, data, 0600)

	o := newTestOrchestrator()
	o.muninnCfgPath = cfgPath

	parent := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "empty-ep-agent",
		MemoryEnabled: true,
		VaultName:     vaultName,
	}

	vr := o.connectAgentVault(context.Background(), ag, parent)
	defer vr.cancel()

	if vr.sessionReg == nil {
		t.Fatal("sessionReg must be non-nil when vault endpoint is empty")
	}
	if vr.warning == "" {
		t.Error("expected warning when vault endpoint is empty")
	}
}

// registeredToolNames returns all tool names currently in reg.
// It uses Get() by trying known name patterns; simpler than full enumeration.
// Here we just return the names we know are potentially registered.
func registeredToolNames(reg *tools.Registry) []string {
	candidates := []string{
		"muninn_remember", "muninn_recall", "muninn_read", "muninn_link",
		"muninn_guide", "muninn_remember_batch", "muninn_where_left_off",
	}
	var found []string
	for _, name := range candidates {
		if _, ok := reg.Get(name); ok {
			found = append(found, name)
		}
	}
	return found
}

// itoa is a minimal int-to-string helper to avoid importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
