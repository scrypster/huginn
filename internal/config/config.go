package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/scrypster/huginn/internal/mcp"
)

// warnedEnvVars ensures each unset environment variable is warned about at most once
// per process lifetime, preventing log spam when ResolvedAPIKey is called frequently.
var warnedEnvVars sync.Map

// WebUIConfig configures the local web server and browser UI.
type WebUIConfig struct {
	Enabled        bool     `json:"enabled"`
	Port           int      `json:"port"`            // 0 = dynamic allocation
	AutoOpen       bool     `json:"auto_open"`       // open browser on start
	Bind           string   `json:"bind"`            // default "127.0.0.1"
	AllowedOrigins []string `json:"allowed_origins"` // WebSocket origin allowlist; nil/empty = allow all (backwards-compat)
	// TrustedProxies is the list of CIDR ranges from which X-Forwarded-For
	// headers are trusted when determining the real client IP. If empty,
	// the server defaults to trusting loopback (127.0.0.0/8, ::1) and the
	// three RFC 1918 private ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16).
	// Set to a non-empty list to restrict trust to specific proxy addresses.
	TrustedProxies []string `json:"trusted_proxies,omitempty"`
}

// OAuthAppConfig holds OAuth client credentials for a provider.
type OAuthAppConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` // may be empty (use env var)
}

// IntegrationsConfig holds OAuth app credentials for all providers.
type IntegrationsConfig struct {
	Google    OAuthAppConfig `json:"google"`
	GitHub    OAuthAppConfig `json:"github"`
	Slack     OAuthAppConfig `json:"slack"`
	Jira      OAuthAppConfig `json:"jira"`
	Bitbucket OAuthAppConfig `json:"bitbucket"`
}

// CloudConfig configures the HuginnCloud satellite connection.
type CloudConfig struct {
	URL string `json:"url"` // default "https://huginncloud.com"
}

// BackendConfig holds configuration for the LLM backend.
type BackendConfig struct {
	Type         string `json:"type"`               // "external" (Phase 1 default) or "managed" (Phase 3)
	Endpoint     string `json:"endpoint"`            // used when type="external"
	Provider     string `json:"provider,omitempty"` // "anthropic", "openai", "openrouter", "ollama"
	APIKey       string `json:"api_key,omitempty"`  // literal key or "$ENV_VAR_NAME"
	BuiltinModel string `json:"builtin_model,omitempty"` // active model when type="managed" (builtin llama.cpp)
}

// ResolvedAPIKey returns the API key, resolving environment variables if needed.
// If APIKey starts with "$", it's treated as an environment variable name.
// If the env var is unset, emits a WARN log once per variable name and returns "".
func (bc *BackendConfig) ResolvedAPIKey() string {
	if bc.APIKey == "" {
		return ""
	}
	if strings.HasPrefix(bc.APIKey, "$") {
		varName := strings.TrimPrefix(bc.APIKey, "$")
		val := os.Getenv(varName)
		if val == "" {
			if _, loaded := warnedEnvVars.LoadOrStore(varName, struct{}{}); !loaded {
				slog.Warn("config: API key environment variable is unset or empty", "var", varName)
			}
		}
		return val
	}
	return bc.APIKey
}

// Config holds all Huginn configuration.
type Config struct {
	DefaultModel      string        `json:"default_model,omitempty"`       // default model for the primary agent
	ReasonerModel     string        `json:"reasoner_model"`
	OllamaBaseURL     string        `json:"ollama_base_url"`
	Backend           BackendConfig `json:"backend"`
	Theme             string        `json:"theme"`
	ContextLimitKB    int           `json:"context_limit_kb"`
	GitStageOnWrite   bool          `json:"git_stage_on_write"`
	WorkspacePath     string        `json:"workspace_path,omitempty"`
	MaxTurns          int           `json:"max_turns,omitempty"`         // default 50; max agentic loop iterations
	ToolsEnabled      bool          `json:"tools_enabled"`               // default true
	AllowedTools      []string      `json:"allowed_tools,omitempty"`     // whitelist; empty = all allowed
	DisallowedTools   []string      `json:"disallowed_tools,omitempty"`  // blacklist
	BashTimeoutSecs   int           `json:"bash_timeout_secs,omitempty"` // default 120
	MachineID         string        `json:"machine_id,omitempty"`
	DiffReviewMode    string        `json:"diff_review_mode,omitempty"` // "always", "never", "auto"
	MCPServers        []mcp.MCPServerConfig `json:"mcp_servers,omitempty"`     // MCP server configurations
	NotepadsEnabled   bool          `json:"notepads_enabled"`
	NotepadsMaxTokens int           `json:"notepads_max_tokens,omitempty"`
	CompactMode       string        `json:"compact_mode,omitempty"`    // "auto", "never", "always"
	CompactTrigger    float64       `json:"compact_trigger,omitempty"` // 0.0-1.0
	VisionEnabled     bool          `json:"vision_enabled"`
	MaxImageSizeKB    int           `json:"max_image_size_kb,omitempty"`
	EmbeddingModel    string        `json:"embedding_model,omitempty"`     // default "nomic-embed-text"
	SemanticSearch    bool          `json:"semantic_search,omitempty"`     // default false
	BraveAPIKey       string        `json:"brave_api_key,omitempty"`       // web_search disabled if empty
	WebUI        WebUIConfig        `json:"web_ui"`
	Integrations IntegrationsConfig `json:"integrations"`
	Cloud        CloudConfig        `json:"cloud"`
	ActiveAgent       string        `json:"active_agent,omitempty"`
	ActiveSessionID   string        `json:"active_session_id,omitempty"`
	SchedulerEnabled  bool          `json:"scheduler_enabled"` // default true; set false to pause all routines
	Version           int           `json:"version,omitempty"`
}

// Default returns a Config with all production defaults.
func Default() *Config {
	return &Config{
		ReasonerModel:   "deepseek-r1:14b",
		OllamaBaseURL:   "http://localhost:11434",
		Backend: BackendConfig{
			Type:     "external",
			Endpoint: "http://localhost:11434",
			Provider: "ollama",
		},
		Theme:             "dark",
		ContextLimitKB:    128,
		GitStageOnWrite:   false,
		MaxTurns:          50,
		ToolsEnabled:      true,
		BashTimeoutSecs:   120,
		DiffReviewMode:    "always",
		NotepadsEnabled:   true,
		NotepadsMaxTokens: 8192,
		CompactMode:       "auto",
		CompactTrigger:    0.70,
		VisionEnabled:     true,
		MaxImageSizeKB:    2048,
		EmbeddingModel:    "nomic-embed-text",
		SemanticSearch:    false,
		WebUI: WebUIConfig{
			Enabled:  true,
			Port:     8421,
			AutoOpen: true,
			Bind:     "127.0.0.1",
		},
		Cloud: CloudConfig{
			URL: "https://huginncloud.com",
		},
		SchedulerEnabled: true,
	}
}

// Load reads config from ~/.huginn/config.json, returning defaults if missing.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Default(), nil
	}
	return LoadFrom(filepath.Join(home, ".huginn", "config.json"))
}

// generateMachineID produces a unique identifier in the form <hostname>-<4hexbytes>.
func generateMachineID() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%x", hostname, b)
}

// currentConfigVersion is incremented whenever a breaking schema change is made.
// Version 0 is treated as legacy (no version field present).
const currentConfigVersion = 13

// configMigrations is a forward-only chain. Index i migrates version i → i+1.
var configMigrations = []func(*Config){
	migrateV0toV1,
	migrateV1toV2,
	migrateV2toV3,
	migrateV3toV4,
	migrateV4toV5,
	migrateV5toV6,
	migrateV6toV7,
	migrateV7toV8,
	migrateV8toV9,
	migrateV9toV10,
	migrateV10toV11,
	migrateV11toV12,
	migrateV12toV13,
}

// migrateV0toV1 is the baseline migration.
// Default() already fills in sensible values via json.Unmarshal merging.
func migrateV0toV1(cfg *Config) {
	// No field renames in v1. Future migrations go here.
}

// migrateV1toV2 adds DiffReviewMode field.
func migrateV1toV2(cfg *Config) {
	if cfg.DiffReviewMode == "" {
		cfg.DiffReviewMode = "always"
	}
}

// migrateV2toV3 adds MCPServers field.
func migrateV2toV3(cfg *Config) {
	// MCPServers is optional; default is empty
	if cfg.MCPServers == nil {
		cfg.MCPServers = []mcp.MCPServerConfig{}
	}
}

// migrateV3toV4 adds notepad configuration fields.
func migrateV3toV4(cfg *Config) {
	if cfg.NotepadsMaxTokens == 0 {
		cfg.NotepadsMaxTokens = 8192
	}
	if !cfg.NotepadsEnabled {
		cfg.NotepadsEnabled = true
	}
}

// migrateV4toV5 adds context compaction configuration fields.
func migrateV4toV5(cfg *Config) {
	if cfg.CompactMode == "" {
		cfg.CompactMode = "auto"
	}
	if cfg.CompactTrigger == 0 {
		cfg.CompactTrigger = 0.70
	}
}

// migrateV5toV6 adds Provider field to BackendConfig.
// Sets Provider based on endpoint URL pattern or defaults to "ollama".
func migrateV5toV6(cfg *Config) {
	if cfg.Backend.Provider == "" {
		endpoint := strings.ToLower(cfg.Backend.Endpoint)
		if strings.Contains(endpoint, "anthropic.com") {
			cfg.Backend.Provider = "anthropic"
		} else if strings.Contains(endpoint, "openrouter.ai") {
			cfg.Backend.Provider = "openrouter"
		} else if strings.Contains(endpoint, "openai.com") {
			cfg.Backend.Provider = "openai"
		} else {
			cfg.Backend.Provider = "ollama"
		}
	}
}

// migrateV6toV7 adds BraveAPIKey field.
// BraveAPIKey is optional; default is empty string (web_search disabled).
func migrateV6toV7(cfg *Config) {
	// BraveAPIKey defaults to empty string. No action needed.
}

// migrateV8toV9 adds ActiveAgent field.
// ActiveAgent is optional; default is empty string (no active agent selected).
func migrateV8toV9(cfg *Config) {
	// ActiveAgent defaults to empty string. No action needed.
}

// migrateV9toV10 adds SchedulerEnabled field (default true).
func migrateV9toV10(cfg *Config) {
	cfg.SchedulerEnabled = true
}

// migrateV10toV11 pins the web UI to a fixed port (8421) so the dashboard
// URL is stable across server restarts. Dynamic port 0 caused "Failed to fetch"
// in the browser whenever the server restarted and picked a different port.
func migrateV10toV11(cfg *Config) {
	if cfg.WebUI.Port == 0 {
		cfg.WebUI.Port = 8421
	}
}

// migrateV11toV12 adds ActiveSessionID field.
// ActiveSessionID is optional; default is empty string (no active session selected).
func migrateV11toV12(_ *Config) {
	// No-op: ActiveSessionID defaults to "" which is correct for existing configs.
}

// migrateV12toV13 drops planner_model and coder_model from the persisted config.
// These fields have been removed from the Config struct as part of removing the
// Plan→Approve→Implement workflow. The SaveTo() call that follows all migrations
// rewrites the file using json.Marshal(cfg), which excludes removed struct fields,
// completing the active cleanup on first launch after upgrade.
func migrateV12toV13(_ *Config) {
	// No struct mutation needed: fields removed from Config struct.
	// Active stripping is handled by the SaveTo() in LoadFrom() post-migration.
}

// migrateV7toV8 adds WebUI, Integrations, and Cloud config fields.
func migrateV7toV8(cfg *Config) {
	if !cfg.WebUI.Enabled {
		cfg.WebUI.Enabled = true
	}
	if cfg.WebUI.Bind == "" {
		cfg.WebUI.Bind = "127.0.0.1"
	}
	if !cfg.WebUI.AutoOpen {
		cfg.WebUI.AutoOpen = true
	}
	if cfg.Cloud.URL == "" {
		cfg.Cloud.URL = "https://huginncloud.com"
	}
}

// ValidationWarning describes a single field that was clamped to a safe default.
type ValidationWarning struct {
	Field    string
	OldValue any
	NewValue any
}

// Validate checks all field values, clamps invalid ones to safe defaults, and
// returns a warning for each field that was clamped. The returned error is
// always nil (clamping never fails); it exists for interface consistency.
// Called automatically by LoadFrom after migration.
func (c *Config) Validate() ([]ValidationWarning, error) {
	var warnings []ValidationWarning

	// DiffReviewMode: must be "always", "never", or "auto"
	switch c.DiffReviewMode {
	case "always", "never", "auto":
		// valid
	default:
		warnings = append(warnings, ValidationWarning{Field: "DiffReviewMode", OldValue: c.DiffReviewMode, NewValue: "always"})
		c.DiffReviewMode = "always"
	}

	// CompactMode: must be "auto", "never", or "always"
	switch c.CompactMode {
	case "auto", "never", "always":
		// valid
	default:
		warnings = append(warnings, ValidationWarning{Field: "CompactMode", OldValue: c.CompactMode, NewValue: "auto"})
		c.CompactMode = "auto"
	}

	// CompactTrigger: must be 0.0-1.0
	if c.CompactTrigger < 0 || c.CompactTrigger > 1.0 {
		warnings = append(warnings, ValidationWarning{Field: "CompactTrigger", OldValue: c.CompactTrigger, NewValue: 0.70})
		c.CompactTrigger = 0.70
	}

	// MaxImageSizeKB: must be positive
	if c.MaxImageSizeKB <= 0 {
		warnings = append(warnings, ValidationWarning{Field: "MaxImageSizeKB", OldValue: c.MaxImageSizeKB, NewValue: 2048})
		c.MaxImageSizeKB = 2048
	}

	// MaxTurns: must be in [1, 1000]
	if c.MaxTurns <= 0 {
		warnings = append(warnings, ValidationWarning{Field: "MaxTurns", OldValue: c.MaxTurns, NewValue: 50})
		c.MaxTurns = 50
	} else if c.MaxTurns > 1000 {
		warnings = append(warnings, ValidationWarning{Field: "MaxTurns", OldValue: c.MaxTurns, NewValue: 1000})
		c.MaxTurns = 1000
	}

	// BashTimeoutSecs: must be positive
	if c.BashTimeoutSecs <= 0 {
		warnings = append(warnings, ValidationWarning{Field: "BashTimeoutSecs", OldValue: c.BashTimeoutSecs, NewValue: 120})
		c.BashTimeoutSecs = 120
	}

	return warnings, nil
}

// secretEnvSuffixes is the set of key suffixes whose values are treated as secrets
// and redacted in GET /api/v1/config responses. Uses HasSuffix matching on the
// uppercased key so PUBLIC_KEY is also redacted (acceptable for operator-only UI).
var secretEnvSuffixes = []string{
	"_TOKEN", "_KEY", "_SECRET", "_PASSWORD", "_API_KEY", "_PASS", "_CREDENTIAL",
}

// IsSecretEnvKey reports whether the environment variable key (before "=") should be redacted.
func IsSecretEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, suffix := range secretEnvSuffixes {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}

// mcpCmdForbidden is the set of characters that must not appear in stdio MCP command strings.
// Newline, carriage-return, and null byte prevent control-character injection into exec paths.
const mcpCmdForbidden = ";&|><$(){}` \t\n\r\x00"

// Validate checks config values that need semantic validation.
// Called before writing config changes via PUT /api/v1/config.
func Validate(cfg Config) error {
	if cfg.WebUI.Port != 0 && (cfg.WebUI.Port < 1024 || cfg.WebUI.Port > 65535) {
		return fmt.Errorf("web_ui.port must be 0 (dynamic) or 1024-65535")
	}
	if cfg.WebUI.Bind != "" && cfg.WebUI.Bind != "127.0.0.1" && cfg.WebUI.Bind != "localhost" {
		return fmt.Errorf("web_ui.bind must be 127.0.0.1 or localhost")
	}
	// When using managed backend, an API key is required.
	if cfg.Backend.Type == "managed" && cfg.Backend.ResolvedAPIKey() == "" {
		return fmt.Errorf("backend.api_key is required when backend.type is \"managed\"")
	}
	// BashTimeoutSecs has a hard upper limit.
	if cfg.BashTimeoutSecs > 3600 {
		return fmt.Errorf("bash_timeout_secs must be ≤ 3600")
	}
	// ContextLimitKB must be non-negative (0 = use model default).
	if cfg.ContextLimitKB < 0 {
		return fmt.Errorf("context_limit_kb must be ≥ 0")
	}
	// MCP server validation.
	seenMCPNames := make(map[string]bool, len(cfg.MCPServers))
	for i, srv := range cfg.MCPServers {
		if strings.TrimSpace(srv.Name) == "" {
			return fmt.Errorf("mcp_servers[%d]: name is required", i)
		}
		if seenMCPNames[srv.Name] {
			return fmt.Errorf("mcp_servers: duplicate name %q", srv.Name)
		}
		seenMCPNames[srv.Name] = true
		switch srv.Transport {
		case "", "stdio":
			if srv.Command == "" {
				return fmt.Errorf("mcp_servers[%d] %q: command is required for stdio transport", i, srv.Name)
			}
			if strings.ContainsAny(srv.Command, mcpCmdForbidden) {
				return fmt.Errorf("mcp_servers[%d] %q: command contains disallowed characters", i, srv.Name)
			}
		case "sse", "http":
			if srv.URL == "" {
				return fmt.Errorf("mcp_servers[%d] %q: url is required for %s transport", i, srv.Name, srv.Transport)
			}
			u, err := url.ParseRequestURI(srv.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("mcp_servers[%d] %q: url must be a valid http or https URL", i, srv.Name)
			}
			// SSRF note: this endpoint is operator-only and localhost-bound, so private
			// IP ranges are intentionally allowed (operators use LAN MCP servers).
		default:
			return fmt.Errorf("mcp_servers[%d] %q: transport must be stdio, sse, or http (got %q)", i, srv.Name, srv.Transport)
		}
	}
	return nil
}

// ValidateConfig is an alias for Validate, provided for API compatibility.
func ValidateConfig(cfg Config) error {
	return Validate(cfg)
}

// LoadFrom reads config from an explicit path, returning defaults if missing.
// If the loaded (or default) config has no MachineID, one is generated and
// written back to disk so subsequent loads return the same value.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	isNew := errors.Is(err, os.ErrNotExist)
	if isNew {
		// File doesn't exist yet — start from defaults.
		cfg := Default()
		cfg.MachineID = generateMachineID()
		cfg.Version = currentConfigVersion
		// Best-effort save; ignore errors (e.g. read-only path).
		_ = cfg.SaveTo(path)
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config.LoadFrom: read file %s: %w", path, err)
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config.LoadFrom: unmarshal %s: %w", path, err)
	}
	// Run forward-only migrations if config is behind current version.
	migrated := false
	for cfg.Version < currentConfigVersion {
		if cfg.Version < len(configMigrations) {
			configMigrations[cfg.Version](cfg)
		}
		cfg.Version++
		migrated = true
	}
	_, _ = cfg.Validate()
	if migrated {
		_ = cfg.SaveTo(path) // best-effort write-back
	}
	if cfg.MachineID == "" {
		cfg.MachineID = generateMachineID()
		// Best-effort save so the ID persists on next load.
		_ = cfg.SaveTo(path)
	}
	return cfg, nil
}

// Save writes config to ~/.huginn/config.json.
func (c *Config) Save() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("config.Save: get home dir: %w", err)
	}
	return c.SaveTo(filepath.Join(home, ".huginn", "config.json"))
}

// SaveTo writes config to an explicit path using an atomic rename so the
// original file is never left in a partially-written state if the process
// crashes mid-write (e.g., disk full after partial truncation).
// fsyncDir opens path as a directory and calls Sync() on it so that a preceding
// rename is durable even if the OS crashes before flushing its metadata journal.
// Some filesystems (e.g. tmpfs, some network mounts) do not support this and
// will return an error — callers should treat a non-nil return as a warning, not
// a hard failure, because the rename already succeeded.
func fsyncDir(dirPath string) error {
	f, err := os.Open(dirPath)
	if err != nil {
		return fmt.Errorf("config: fsyncDir open %s: %w", dirPath, err)
	}
	defer f.Close()
	return f.Sync()
}

func (c *Config) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("config.SaveTo: mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config.SaveTo: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config.SaveTo: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config.SaveTo: rename to %s: %w", path, err)
	}
	// Flush directory metadata so the rename is durable on crash.
	// fsyncDir may fail on tmpfs/network mounts — treat as non-fatal.
	_ = fsyncDir(filepath.Dir(path))
	return nil
}
