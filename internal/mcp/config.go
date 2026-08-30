package mcp

type MCPServerConfig struct {
	Name      string   `json:"name"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	Transport string   `json:"transport,omitempty"` // "stdio" or "sse"
	URL       string   `json:"url,omitempty"`
	Env       []string `json:"env,omitempty"`
	// ApprovalGate controls whether server startup marks this server's name
	// as a base-watched provider (see permissions.Gate.SetBaseWatchedProviders
	// in main.go), so every call to one of its tools always reaches a
	// permission prompt regardless of skipAll / per-agent toolbelt settings.
	// Defaults to true (fail-closed) when nil or unset — an operator must
	// explicitly opt a server OUT with `"approval_gate": false` to run its
	// tools unprompted. This is the per-server escape hatch for trusted,
	// non-outward-facing MCP servers; it does not affect per-agent
	// "Always allow" grants (SeedSessionAllowed), which are a separate
	// escape hatch.
	ApprovalGate *bool `json:"approval_gate,omitempty"`
}

// ApprovalGateEnabled reports whether cfg opts into base-watch approval
// gating. Defaults to true (nil/unset = gated) so a server is fail-closed
// unless an operator explicitly sets approval_gate: false.
func (cfg MCPServerConfig) ApprovalGateEnabled() bool {
	return cfg.ApprovalGate == nil || *cfg.ApprovalGate
}
