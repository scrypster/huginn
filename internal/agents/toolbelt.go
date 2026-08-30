package agents

// ToolbeltEntry records a connection assigned to an agent's toolbelt.
type ToolbeltEntry struct {
	ConnectionID string `json:"connection_id" yaml:"connection_id"`
	Provider     string `json:"provider"      yaml:"provider"`
	Profile      string `json:"profile,omitempty"       yaml:"profile,omitempty"`
	ApprovalGate bool   `json:"approval_gate,omitempty" yaml:"approval_gate,omitempty"`
}

// ToolbeltProviders returns the deduplicated list of provider names
// referenced by the toolbelt entries. Returns nil for an empty toolbelt.
func ToolbeltProviders(tb []ToolbeltEntry) []string {
	if len(tb) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tb))
	out := make([]string, 0, len(tb))
	for _, e := range tb {
		if _, ok := seen[e.Provider]; !ok {
			seen[e.Provider] = struct{}{}
			out = append(out, e.Provider)
		}
	}
	return out
}

// isAllowAllEntry reports whether a toolbelt entry is an explicit allow-all
// wildcard. Either provider: "*" or connection_id: "*" grants every external
// provider (used by operators who opt in, e.g. Astra on MJ Mac).
func isAllowAllEntry(e ToolbeltEntry) bool {
	return e.Provider == "*" || e.ConnectionID == "*"
}

// AllowedProviders returns the set of connection providers this toolbelt
// grants. Semantics are fail-closed:
//
//   - nil or empty toolbelt → empty map (deny every external provider)
//   - any entry with provider "*" or connection_id "*" → {"*": true} (explicit allow-all)
//   - otherwise → the named providers
//
// An empty map is not the same as nil. Callers that pass the result to
// permissions.Gate should keep that distinction: the gate treats nil as
// unrestricted (legacy / no toolbelt configured on the gate itself) and an
// empty map as deny-all external providers.
func AllowedProviders(tb []ToolbeltEntry) map[string]bool {
	if len(tb) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(tb))
	for _, e := range tb {
		if isAllowAllEntry(e) {
			return map[string]bool{"*": true}
		}
		if e.Provider != "" {
			out[e.Provider] = true
		}
	}
	return out
}

// WatchedProviders returns a set of provider names for which ApprovalGate is true.
func WatchedProviders(tb []ToolbeltEntry) map[string]bool {
	out := make(map[string]bool)
	for _, e := range tb {
		if e.ApprovalGate {
			out[e.Provider] = true
		}
	}
	return out
}
