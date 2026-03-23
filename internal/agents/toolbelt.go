package agents

// ToolbeltEntry records a connection assigned to an agent's toolbelt.
type ToolbeltEntry struct {
	ConnectionID string `json:"connection_id"`
	Provider     string `json:"provider"`
	Profile      string `json:"profile,omitempty"`
	ApprovalGate bool   `json:"approval_gate,omitempty"`
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

// AllowedProviders returns the set of all provider names referenced by the
// toolbelt. Returns nil for an empty toolbelt; nil means all providers allowed.
func AllowedProviders(tb []ToolbeltEntry) map[string]bool {
	if len(tb) == 0 {
		return nil
	}
	out := make(map[string]bool, len(tb))
	for _, e := range tb {
		out[e.Provider] = true
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
