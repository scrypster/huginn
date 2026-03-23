package tui

import "testing"

func TestAgentColorFromName_Deterministic(t *testing.T) {
	names := []string{"Alice", "Bob", "Tom", "Steve", "Dave", "Eve"}
	for _, name := range names {
		c1 := agentColorFromName(name)
		c2 := agentColorFromName(name)
		if c1 != c2 {
			t.Errorf("agentColorFromName(%q) not deterministic: %q vs %q", name, c1, c2)
		}
		if c1 == "" {
			t.Errorf("agentColorFromName(%q) returned empty string", name)
		}
	}
}

func TestAgentColorFromName_InPalette(t *testing.T) {
	for _, name := range []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta"} {
		color := agentColorFromName(name)
		found := false
		for _, p := range agentPalette {
			if p == color {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("agentColorFromName(%q) = %q, not in palette", name, color)
		}
	}
}

func TestAgentColorFromName_Distribution(t *testing.T) {
	// Ensure different names don't all map to the same color (basic sanity).
	seen := map[string]bool{}
	names := []string{"Alice", "Bob", "Carol", "Dave", "Eve", "Frank", "Grace", "Hank", "Iris", "Jack"}
	for _, name := range names {
		seen[agentColorFromName(name)] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected at least 2 distinct colors for %d names, got %d", len(names), len(seen))
	}
}

func TestAgentIconFromName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Tom", "T"},
		{"alice", "A"},
		{"Bob", "B"},
		{"", "?"},
		{"δelta", "Δ"}, // unicode first char
	}
	for _, tt := range tests {
		got := agentIconFromName(tt.name)
		if got != tt.want {
			t.Errorf("agentIconFromName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
