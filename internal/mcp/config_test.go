package mcp

import "testing"

// TestMCPServerConfig_ApprovalGateEnabled is V4(b): approval_gate defaults
// to true (fail-closed / base-watched) when nil or unset, and an operator
// must explicitly set approval_gate: false to opt a server out of
// base-watch (main.go's serverGate.SetBaseWatchedProviders /
// orch.SetConfiguredMCPProviders both filter on this).
func TestMCPServerConfig_ApprovalGateEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	cases := []struct {
		name string
		cfg  MCPServerConfig
		want bool
	}{
		{"nil (unset) defaults to gated", MCPServerConfig{Name: "playwright"}, true},
		{"explicit true stays gated", MCPServerConfig{Name: "playwright", ApprovalGate: &trueVal}, true},
		{"explicit false opts out", MCPServerConfig{Name: "trusted-internal", ApprovalGate: &falseVal}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.ApprovalGateEnabled(); got != tc.want {
				t.Errorf("ApprovalGateEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBaseWatchFilter_ExcludesOptedOutServers mirrors the exact filter
// main.go applies when building the base-watch / configured-MCP-provider
// set from cfg.MCPServers: named servers with approval_gate left at its
// true default are included; a server with approval_gate: false is
// excluded (it runs unprompted, same as a builtin).
func TestBaseWatchFilter_ExcludesOptedOutServers(t *testing.T) {
	falseVal := false
	servers := []MCPServerConfig{
		{Name: "playwright"},
		{Name: "trusted-internal", ApprovalGate: &falseVal},
		{Name: ""}, // unnamed — never base-watched (V5)
	}

	providers := make(map[string]bool, len(servers))
	for _, mcfg := range servers {
		if mcfg.Name != "" && mcfg.ApprovalGateEnabled() {
			providers[mcfg.Name] = true
		}
	}

	if !providers["playwright"] {
		t.Error("expected playwright (default-gated) to be base-watched")
	}
	if providers["trusted-internal"] {
		t.Error("expected trusted-internal (approval_gate: false) to be excluded from base-watch")
	}
	if len(providers) != 1 {
		t.Errorf("expected exactly 1 base-watched provider, got %d: %v", len(providers), providers)
	}
}
