package memory

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalMCPEndpoint is the default local Muninn MCP daemon URL.
const LocalMCPEndpoint = "http://127.0.0.1:8750"

// ApplyLocalDaemonDiscovery fills Endpoint and MCPToken from the local Muninn
// daemon when the huginn muninn.json file is missing. Fail-closed if
// ~/.muninn/mcp.token is missing or empty. Does not invent a vault name.
func ApplyLocalDaemonDiscovery(cfg *GlobalConfig) bool {
	if cfg == nil || strings.TrimSpace(cfg.Endpoint) != "" {
		return false
	}
	tok := readLocalMCPToken()
	if tok == "" {
		return false
	}
	cfg.Endpoint = LocalMCPEndpoint
	cfg.MCPToken = tok
	return true
}

func maybeDiscoverLocalDaemon(cfg *GlobalConfig, cfgPath string) {
	if cfg == nil || cfgPath == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	defaultPath := filepath.Join(home, ".config", "huginn", "muninn.json")
	if cfgPath != defaultPath {
		return
	}
	if _, err := os.Stat(cfgPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return
	}
	ApplyLocalDaemonDiscovery(cfg)
}

func readLocalMCPToken() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".muninn", "mcp.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// DetectMuninnPresence reports whether Muninn is installed and/or the local
// MCP daemon is responding. A 401 from :8750 counts as running.
func DetectMuninnPresence() (installed, running bool) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if fi, err := os.Stat(filepath.Join(home, ".muninn")); err == nil && fi.IsDir() {
			installed = true
		}
	}
	running = ProbeMCPDaemon()
	if running {
		installed = true
	}
	return
}

// ProbeMCPDaemon returns true if the local Muninn MCP port answers at all,
// including HTTP 401 (daemon up, bearer required).
func ProbeMCPDaemon() bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(LocalMCPEndpoint + "/mcp")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// PersistLocalDaemonDiscovery writes muninn.json from the local daemon
// (mcp.token + default MCP endpoint) when the file is missing or has no
// endpoint/token. Does not invent a vault name. Returns the config and
// whether a file was written.
func PersistLocalDaemonDiscovery(cfgPath string) (*GlobalConfig, bool, error) {
	if strings.TrimSpace(cfgPath) == "" {
		return nil, false, errors.New("muninn config path not set")
	}
	cfg, err := LoadGlobalConfig(cfgPath)
	if err != nil {
		return nil, false, err
	}
	_, statErr := os.Stat(cfgPath)
	missing := errors.Is(statErr, os.ErrNotExist)
	filled := false
	if strings.TrimSpace(cfg.Endpoint) == "" {
		filled = ApplyLocalDaemonDiscovery(cfg)
	} else if strings.TrimSpace(cfg.MCPToken) == "" {
		if tok := readLocalMCPToken(); tok != "" {
			cfg.MCPToken = tok
			filled = true
		}
	}
	if !missing && !filled {
		return cfg, false, nil
	}
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.MCPToken) == "" {
		return cfg, false, nil
	}
	if err := SaveGlobalConfig(cfgPath, cfg); err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

// VaultNames returns configured vault names only (no tokens).
func VaultNames(cfg *GlobalConfig) []string {
	if cfg == nil || len(cfg.VaultTokens) == 0 {
		return []string{}
	}
	names := make([]string, 0, len(cfg.VaultTokens))
	for v := range cfg.VaultTokens {
		if strings.TrimSpace(v) != "" {
			names = append(names, v)
		}
	}
	return names
}

// PinDaemonAuth fills Endpoint and MCPToken from the same sources the rest of
// Huginn uses (muninn.json mcp_token, then ~/.muninn/mcp.token + :8750).
// Does not invent a vault name. Does not write reserved vaults.
func PinDaemonAuth(cfg *GlobalConfig) bool {
	if cfg == nil {
		return false
	}
	if strings.TrimSpace(cfg.MCPToken) == "" {
		if tok := readLocalMCPToken(); tok != "" {
			cfg.MCPToken = tok
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" && strings.TrimSpace(cfg.MCPToken) != "" {
		cfg.Endpoint = LocalMCPEndpoint
	}
	return strings.TrimSpace(cfg.Endpoint) != "" && strings.TrimSpace(cfg.MCPToken) != ""
}

// IsReservedVaultName reports vault names Huginn must never create or write.
func IsReservedVaultName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "muninndb", "shotgroup", "simplorium", "beacon", "aws", "personal", "default":
		return true
	default:
		return false
	}
}

