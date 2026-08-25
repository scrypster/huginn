package claudecode

// Config controls the Claude Code bridge. It is stored under the
// "claude_code" key in ~/.huginn/config.json.
//
// Enabled defaults to false: turning the bridge on means Huginn reads every
// Claude Code transcript on the machine, which is a deliberate opt-in.
type Config struct {
	Enabled bool   `json:"enabled"`
	Binary  string `json:"binary"`

	Watch    WatchConfig    `json:"watch"`
	Delegate DelegateConfig `json:"delegate"`
}

// WatchConfig controls transcript ingestion.
type WatchConfig struct {
	Enabled bool `json:"enabled"`
	// Backfill imports existing transcripts on startup.
	Backfill bool `json:"backfill"`
	// MaxFileMB skips transcripts larger than this, with a log line.
	MaxFileMB int `json:"max_file_mb"`
}

// DelegateConfig controls the claude_code tool.
type DelegateConfig struct {
	Enabled        bool   `json:"enabled"`
	DefaultModel   string `json:"default_model"`
	PermissionMode string `json:"permission_mode"`
	MaxTurns       int    `json:"max_turns"`
	TimeoutSecs    int    `json:"timeout_secs"`
	// SkipPermissions passes --dangerously-skip-permissions. It must be set
	// explicitly and is never inferred.
	SkipPermissions bool `json:"skip_permissions"`
}

// DefaultConfig returns the bridge defaults: off, with sensible values for
// when it is switched on.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Binary:  "claude",
		Watch: WatchConfig{
			Enabled:   true,
			Backfill:  true,
			MaxFileMB: 50,
		},
		Delegate: DelegateConfig{
			Enabled:         true,
			DefaultModel:    "sonnet",
			PermissionMode:  "acceptEdits",
			MaxTurns:        30,
			TimeoutSecs:     900,
			SkipPermissions: false,
		},
	}
}

// WatchEnabled reports whether the transcript watcher should run.
func (c Config) WatchEnabled() bool { return c.Enabled && c.Watch.Enabled }

// DelegateEnabled reports whether the claude_code tool should be registered.
func (c Config) DelegateEnabled() bool { return c.Enabled && c.Delegate.Enabled }
