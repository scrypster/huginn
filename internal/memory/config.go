package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// VaultStrategy controls which vaults are read from and written to.
type VaultStrategy string

const (
	StrategyTwoTier      VaultStrategy = "two-tier"      // default: personal + project
	StrategySingle       VaultStrategy = "single"        // everything in personal vault
	StrategyProjectOnly  VaultStrategy = "project-only"  // skip personal vault
	StrategyPersonalOnly VaultStrategy = "personal-only" // skip project vault
)

// GlobalConfig is loaded from ~/.config/huginn/muninn.json.
type GlobalConfig struct {
	Endpoint        string            `json:"endpoint"`               // MuninnDB server URL
	Username        string            `json:"username"`               // MuninnDB username; default "root"
	UserVault       string            `json:"user_vault"`             // overrides username resolution
	Strategy        VaultStrategy     `json:"strategy"`               // default: two-tier
	ActivationLimit int               `json:"activation_limit"`       // max results per activation call; default 10
	VaultTokens     map[string]string `json:"vault_tokens,omitempty"` // per-vault REST API tokens (mk_...)
	// MCPToken is the daemon token for authenticating to the MuninnDB MCP server (mdb_...).
	// This is distinct from per-vault API keys. If set, it takes precedence over VaultTokens
	// for all MCP transport connections. Run `muninn --mcp-token <value>` to find the value.
	MCPToken string `json:"mcp_token,omitempty"`
}

// ProjectConfig is loaded from .huginn/muninn.json (nearest, walking up to git root).
// user_vault is intentionally absent — project config cannot override it.
type ProjectConfig struct {
	ProjectVault     string        `json:"project_vault"`
	Strategy         VaultStrategy `json:"strategy"`          // overrides global if set
	AdditionalVaults []string      `json:"additional_vaults"` // extra read-only vaults
}

// LoadGlobalConfig reads the global muninn config from path.
// Returns defaults if the file does not exist.
func LoadGlobalConfig(path string) (*GlobalConfig, error) {
	cfg := &GlobalConfig{
		Endpoint:        "",
		Username:        "root",
		Strategy:        StrategyTwoTier,
		ActivationLimit: 10,
		VaultTokens:     make(map[string]string),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		maybeDiscoverLocalDaemon(cfg, path)
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("muninn config: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("muninn config: parse %s: %w", path, err)
	}
	if cfg.Strategy == "" {
		cfg.Strategy = StrategyTwoTier
	}
	if cfg.ActivationLimit == 0 {
		cfg.ActivationLimit = 10
	}
	if cfg.Username == "" {
		cfg.Username = "root"
	}
	if cfg.VaultTokens == nil {
		cfg.VaultTokens = make(map[string]string)
	}
	return cfg, nil
}

// ErrEmptyMuninnEndpoint is returned when a valid muninn.json exists but
// Endpoint is still empty after pin. Callers must not log this as
// "config unavailable" with a null error and empty endpoint.
var ErrEmptyMuninnEndpoint = errors.New("muninn config: empty endpoint in valid file")

// DefaultGlobalConfigPaths is ~/.config/huginn/muninn.json then ~/.huginn/muninn.json.
func DefaultGlobalConfigPaths(home string) []string {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".config", "huginn", "muninn.json"),
		filepath.Join(home, ".huginn", "muninn.json"),
	}
}

// ResolveGlobalConfigPath prefers an existing explicit path, then the two
// default locations. Missing files fall back to explicit (or the XDG path).
func ResolveGlobalConfigPath(explicit string) string {
	explicit = strings.TrimSpace(explicit)
	home, _ := os.UserHomeDir()
	defaults := DefaultGlobalConfigPaths(home)
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit
		}
		for _, d := range defaults {
			if explicit == d {
				for _, alt := range defaults {
					if _, err := os.Stat(alt); err == nil {
						return alt
					}
				}
				return explicit
			}
		}
		return explicit
	}
	for _, d := range defaults {
		if _, err := os.Stat(d); err == nil {
			return d
		}
	}
	if len(defaults) > 0 {
		return defaults[0]
	}
	return ""
}

func isDefaultGlobalConfigPath(path string) bool {
	home, _ := os.UserHomeDir()
	for _, d := range DefaultGlobalConfigPaths(home) {
		if path == d {
			return true
		}
	}
	return false
}

// LoadAndPinGlobalConfig resolves the path (both default locations), loads
// the file, and pins daemon auth for default paths only. Empty endpoint in a
// valid file is illegal. Does not invent a vault name. Does not write
// winston-huginn.
func LoadAndPinGlobalConfig(explicit string) (*GlobalConfig, string, error) {
	path := ResolveGlobalConfigPath(explicit)
	cfg, err := LoadGlobalConfig(path)
	if err != nil {
		return nil, path, err
	}
	_, statErr := os.Stat(path)
	fileExists := statErr == nil
	if fileExists && isDefaultGlobalConfigPath(path) {
		PinDaemonAuth(cfg)
	}
	if fileExists && strings.TrimSpace(cfg.Endpoint) == "" {
		return cfg, path, ErrEmptyMuninnEndpoint
	}
	return cfg, path, nil
}

// SaveGlobalConfig atomically writes cfg to path, creating parent directories as needed.
// Permissions: 0600.
func SaveGlobalConfig(path string, cfg *GlobalConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("muninn config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("muninn config: marshal: %w", err)
	}
	tmp := path + ".tmp"
	defer os.Remove(tmp) // best-effort cleanup; no-op after successful rename
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("muninn config: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("muninn config: rename: %w", err)
	}
	return nil
}

// LoadProjectConfig reads a project muninn config from an explicit path.
// Returns nil (not an error) if the file does not exist.
func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("muninn config: read %s: %w", path, err)
	}
	var cfg ProjectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("muninn config: parse %s: %w", path, err)
	}
	return &cfg, nil
}

// ResolveUsername determines the username from the global config, environment, or git.
// Priority: user_vault field in global config > $HUGINN_USER > git config user.name > $USER > os user.
// dir is the working directory used for git config lookups (may be "").
// Exported so callers that need only a username do not need to instantiate a VaultResolver.
func ResolveUsername(dir string) string {
	home, _ := os.UserHomeDir()
	globalPath := filepath.Join(home, ".config", "huginn", "muninn.json")
	gcfg, err := LoadGlobalConfig(globalPath)
	if err == nil {
		if name := resolveUsername(gcfg, dir); name != "" {
			return name
		}
	}
	return resolveUsername(nil, dir)
}

// resolveUsername determines the username in priority order:
// 1. user_vault field in global config (strip "huginn:user:" prefix if present)
// 2. $HUGINN_USER env var
// 3. git config user.name (lowercased, spaces→hyphens)
// 4. $USER / os/user.Current().Username
//
// dir is the working directory for the git command; "" uses the process working directory.
func resolveUsername(gcfg *GlobalConfig, dir string) string {
	if gcfg != nil && gcfg.UserVault != "" {
		v := gcfg.UserVault
		v = strings.TrimPrefix(v, "huginn:user:")
		if v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(os.Getenv("HUGINN_USER")); v != "" {
		return v
	}
	cmd := exec.Command("git", "config", "user.name")
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.Output(); err == nil {
		name := strings.TrimSpace(string(out))
		if name != "" {
			name = strings.ToLower(name)
			name = strings.ReplaceAll(name, " ", "-")
			return name
		}
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "huginn"
}

// LoadProjectConfigFromDir walks upward from dir looking for .huginn/muninn.json.
// Stops at filesystem root. Returns nil if not found.
func LoadProjectConfigFromDir(dir string) (*ProjectConfig, error) {
	current := dir
	for {
		candidate := filepath.Join(current, ".huginn", "muninn.json")
		cfg, err := LoadProjectConfig(candidate)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			return cfg, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break // filesystem root
		}
		current = parent
	}
	return nil, nil
}
