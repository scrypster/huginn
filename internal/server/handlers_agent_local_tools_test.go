package server

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestRedactAgentDef_NormalizesLocalTools verifies that redactAgentDef converts
// nil LocalTools to an empty slice, preventing a JSON "null" in API responses.
func TestRedactAgentDef_NormalizesLocalTools(t *testing.T) {
	t.Run("nil local_tools becomes empty slice", func(t *testing.T) {
		def := agents.AgentDef{Name: "test-agent", LocalTools: nil}
		result := redactAgentDef(def)
		if result.LocalTools == nil {
			t.Error("expected LocalTools to be non-nil empty slice, got nil")
		}
		if len(result.LocalTools) != 0 {
			t.Errorf("expected empty LocalTools, got %v", result.LocalTools)
		}
	})

	t.Run("explicit empty slice stays empty", func(t *testing.T) {
		def := agents.AgentDef{Name: "test-agent", LocalTools: []string{}}
		result := redactAgentDef(def)
		if result.LocalTools == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
	})

	t.Run("named local_tools preserved", func(t *testing.T) {
		def := agents.AgentDef{Name: "test-agent", LocalTools: []string{"read_file", "bash"}}
		result := redactAgentDef(def)
		if len(result.LocalTools) != 2 {
			t.Errorf("expected 2 local tools, got %v", result.LocalTools)
		}
	})

	t.Run("wildcard local_tools preserved", func(t *testing.T) {
		def := agents.AgentDef{Name: "test-agent", LocalTools: []string{"*"}}
		result := redactAgentDef(def)
		if len(result.LocalTools) != 1 || result.LocalTools[0] != "*" {
			t.Errorf("expected [*], got %v", result.LocalTools)
		}
	})
}
