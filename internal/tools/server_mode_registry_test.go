package tools_test

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// TestServerModeToolRegistry verifies the tool registration pattern used in
// startServer (main.go). Prior to the fix for issue #34, the server-mode
// toolReg was never populated with builtin tools, so applyToolbelt returned
// empty schemas for any agent with local_tools configured.
func TestServerModeToolRegistry_BuiltinsAvailableAfterRegistration(t *testing.T) {
	tmpDir := t.TempDir()
	reg := tools.NewRegistry()

	// Simulate the server-mode registration that was missing before the fix.
	tools.RegisterBuiltins(reg, tmpDir, 10*time.Second)
	tools.RegisterGitTools(reg, tmpDir)
	tools.RegisterTestsTool(reg, tmpDir, 10*time.Second)
	reg.TagTools(tools.BuiltinToolNames(), "builtin")

	// ["*"] wildcard must resolve to all builtin schemas.
	all := reg.AllBuiltinSchemas()
	if len(all) == 0 {
		t.Fatal("AllBuiltinSchemas returned empty after RegisterBuiltins+TagTools; server-mode agents would have no tools")
	}

	// Named list must resolve correctly.
	named := reg.SchemasByNames([]string{"bash", "read_file"})
	if len(named) != 2 {
		t.Fatalf("SchemasByNames([bash, read_file]) returned %d schemas, want 2", len(named))
	}
	gotNames := map[string]bool{}
	for _, s := range named {
		gotNames[s.Function.Name] = true
	}
	if !gotNames["bash"] || !gotNames["read_file"] {
		t.Errorf("expected bash and read_file in named schemas, got %v", gotNames)
	}
}

// TestServerModeToolRegistry_EmptyRegistryHasNoBuiltins verifies the OLD (broken)
// state: a fresh registry with no registration returns empty schemas. This
// documents the pre-fix behaviour and acts as a regression baseline.
func TestServerModeToolRegistry_EmptyRegistryHasNoBuiltins(t *testing.T) {
	reg := tools.NewRegistry()
	// No RegisterBuiltins, no TagTools — the old server-mode state.
	all := reg.AllBuiltinSchemas()
	if len(all) != 0 {
		t.Errorf("empty registry should return 0 builtin schemas, got %d", len(all))
	}
}

// TestServerModeToolRegistry_ForkedRegistryInheritsBuiltins verifies that a
// forked registry (as used by connectAgentVault in agent_dispatcher.go) still
// provides builtin schemas after the parent is correctly populated.
func TestServerModeToolRegistry_ForkedRegistryInheritsBuiltins(t *testing.T) {
	tmpDir := t.TempDir()
	reg := tools.NewRegistry()
	tools.RegisterBuiltins(reg, tmpDir, 10*time.Second)
	reg.TagTools(tools.BuiltinToolNames(), "builtin")

	fork := reg.Fork()
	all := fork.AllBuiltinSchemas()
	if len(all) == 0 {
		t.Fatal("forked registry returned 0 builtin schemas; applyToolbelt fork would still fail")
	}
}

// TestServerModeToolRegistry_ConfigFiltersApplied verifies that AllowedTools /
// DisallowedTools filters from config are applied in server mode.
func TestServerModeToolRegistry_ConfigFiltersApplied(t *testing.T) {
	tmpDir := t.TempDir()
	reg := tools.NewRegistry()
	tools.RegisterBuiltins(reg, tmpDir, 10*time.Second)
	reg.TagTools(tools.BuiltinToolNames(), "builtin")

	// Block bash — agents with local_tools=["bash"] should get nothing.
	reg.SetBlocked([]string{"bash"})
	named := reg.SchemasByNames([]string{"bash"})
	if len(named) != 0 {
		t.Errorf("blocked bash should return 0 schemas, got %d", len(named))
	}

	// read_file is not blocked.
	named = reg.SchemasByNames([]string{"read_file"})
	if len(named) != 1 {
		t.Errorf("unblocked read_file should return 1 schema, got %d", len(named))
	}
}
