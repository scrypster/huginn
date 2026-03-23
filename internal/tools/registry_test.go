package tools

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// mockTool is a minimal Tool implementation for testing.
type mockTool struct{ name string }

func (m *mockTool) Name() string                                            { return m.name }
func (m *mockTool) Description() string                                     { return "" }
func (m *mockTool) Permission() PermissionLevel                             { return PermRead }
func (m *mockTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: m.name}}
}
func (m *mockTool) Execute(_ context.Context, _ map[string]any) ToolResult { return ToolResult{} }

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "my_tool"})

	tool, ok := r.Get("my_tool")
	if !ok {
		t.Fatal("expected to find registered tool")
	}
	if tool.Name() != "my_tool" {
		t.Errorf("got name %q, want %q", tool.Name(), "my_tool")
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("expected false for unknown tool name")
	}
}

func TestRegistry_BlockedToolHidden(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "bash"})
	r.Register(&mockTool{name: "read_file"})
	r.SetBlocked([]string{"bash"})

	_, ok := r.Get("bash")
	if ok {
		t.Fatal("expected blocked tool to be hidden from Get")
	}

	all := r.All()
	for _, tool := range all {
		if tool.Name() == "bash" {
			t.Fatal("expected blocked tool to be excluded from All()")
		}
	}

	// read_file should still be accessible
	_, ok = r.Get("read_file")
	if !ok {
		t.Fatal("expected non-blocked tool to still be accessible")
	}
}

func TestRegistry_AllowedListExcludes(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "read_file"})
	r.Register(&mockTool{name: "bash"})
	r.Register(&mockTool{name: "write_file"})
	r.SetAllowed([]string{"read_file"})

	_, ok := r.Get("read_file")
	if !ok {
		t.Fatal("expected allowed tool to be accessible")
	}

	_, ok = r.Get("bash")
	if ok {
		t.Fatal("expected non-allowed tool to be inaccessible")
	}

	_, ok = r.Get("write_file")
	if ok {
		t.Fatal("expected non-allowed tool to be inaccessible")
	}
}

func TestRegistry_BlockedPrecedesAllowed(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "bash"})
	r.SetAllowed([]string{"bash"})
	r.SetBlocked([]string{"bash"})

	_, ok := r.Get("bash")
	if ok {
		t.Fatal("expected blocked to take precedence over allowed")
	}
}

func TestRegistry_AllSortedOrder(t *testing.T) {
	r := NewRegistry()
	names := []string{"zebra", "alpha", "mango", "beta"}
	for _, n := range names {
		r.Register(&mockTool{name: n})
	}

	all := r.All()
	if len(all) != len(names) {
		t.Fatalf("expected %d tools, got %d", len(names), len(all))
	}

	got := make([]string, len(all))
	for i, tool := range all {
		got[i] = tool.Name()
	}

	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)

	for i, name := range sorted {
		if got[i] != name {
			t.Errorf("position %d: expected %q, got %q", i, name, got[i])
		}
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	// Pre-register some tools
	for i := 0; i < 5; i++ {
		r.Register(&mockTool{name: "tool"})
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := "concurrent_tool"
			r.Register(&mockTool{name: name})
			r.Get(name)
			r.All()
		}(i)
	}
	wg.Wait()
}

func TestRegistry_OverwriteSilent(t *testing.T) {
	r := NewRegistry()

	first := &mockTool{name: "my_tool"}
	r.Register(first)

	second := &mockTool{name: "my_tool"}
	r.Register(second) // second registration should silently overwrite

	tool, ok := r.Get("my_tool")
	if !ok {
		t.Fatal("expected tool to be found after overwrite")
	}
	// The second registration should win; both have the same Name() so
	// we verify by pointer identity.
	if tool != second {
		t.Errorf("expected second registration to win, but got a different instance")
	}
}

// TestRegistry_IsEnabled verifies IsEnabled reports correctly for plain/blocked/allowed.
func TestRegistry_IsEnabled(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "a"})
	r.Register(&mockTool{name: "b"})

	if !r.IsEnabled("a") {
		t.Error("expected 'a' to be enabled by default")
	}

	r.SetBlocked([]string{"a"})
	if r.IsEnabled("a") {
		t.Error("expected 'a' to be disabled after SetBlocked")
	}
	if !r.IsEnabled("b") {
		t.Error("expected 'b' to remain enabled")
	}

	r.SetBlocked(nil)
	if !r.IsEnabled("a") {
		t.Error("expected 'a' re-enabled after clearing blocked list")
	}
}

// TestRegistry_IsEnabled_Unregistered verifies that IsEnabled returns false for
// a name not in the registry when an allowed list is active.
func TestRegistry_IsEnabled_Unregistered(t *testing.T) {
	r := NewRegistry()
	r.SetAllowed([]string{"real_tool"})
	if r.IsEnabled("nonexistent") {
		t.Error("expected non-registered tool to not be enabled under allowed-list")
	}
}

// TestRegistry_ClearAllowed verifies that passing nil/empty to SetAllowed restores
// the all-tools-enabled state.
func TestRegistry_ClearAllowed(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "t1"})
	r.Register(&mockTool{name: "t2"})

	r.SetAllowed([]string{"t1"})
	_, ok := r.Get("t2")
	if ok {
		t.Fatal("t2 should be disabled under allowed list")
	}

	// Clear the allowed list
	r.SetAllowed(nil)
	_, ok = r.Get("t2")
	if !ok {
		t.Error("t2 should be enabled after clearing allowed list")
	}
}

// TestRegistry_AllSchemas verifies AllSchemas returns one entry per enabled tool.
func TestRegistry_AllSchemas(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "tool1"})
	r.Register(&mockTool{name: "tool2"})
	r.Register(&mockTool{name: "tool3"})
	r.SetBlocked([]string{"tool2"})

	schemas := r.AllSchemas()
	if len(schemas) != 2 {
		t.Errorf("expected 2 schemas (tool2 blocked), got %d", len(schemas))
	}
}

// TestRegistry_EmptyRegistry verifies that an empty registry is safe to query.
func TestRegistry_EmptyRegistry(t *testing.T) {
	r := NewRegistry()
	all := r.All()
	if len(all) != 0 {
		t.Errorf("expected empty All() result, got %d tools", len(all))
	}
	schemas := r.AllSchemas()
	if len(schemas) != 0 {
		t.Errorf("expected empty AllSchemas() result, got %d schemas", len(schemas))
	}
	_, ok := r.Get("anything")
	if ok {
		t.Error("Get on empty registry should return false")
	}
}

// TestRegistry_BlockedAndAllowedBothActive verifies behaviour when both lists set.
func TestRegistry_BlockedAndAllowedBothActive(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "a"})
	r.Register(&mockTool{name: "b"})
	r.Register(&mockTool{name: "c"})

	// Allow a and b, block b — only a should be accessible.
	r.SetAllowed([]string{"a", "b"})
	r.SetBlocked([]string{"b"})

	_, aOk := r.Get("a")
	_, bOk := r.Get("b")
	_, cOk := r.Get("c")

	if !aOk {
		t.Error("expected 'a' to be accessible (allowed and not blocked)")
	}
	if bOk {
		t.Error("expected 'b' to be inaccessible (blocked takes precedence over allowed)")
	}
	if cOk {
		t.Error("expected 'c' to be inaccessible (not in allowed list)")
	}
}

// TestRegistryUnregister verifies that Unregister removes a tool and is idempotent.
func TestRegistryUnregister(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "my_tool"})

	// Tool should be present before unregister
	_, ok := reg.Get("my_tool")
	if !ok {
		t.Fatal("expected tool to be present before Unregister")
	}

	reg.Unregister("my_tool")

	// Tool should be gone after unregister
	_, ok = reg.Get("my_tool")
	if ok {
		t.Fatal("expected tool to be absent after Unregister")
	}

	// Calling Unregister again on missing tool must not panic
	reg.Unregister("my_tool")
	reg.Unregister("nonexistent_tool")
}

// TestRegistry_ConcurrentRegisterAndBlock stress-tests concurrent register + block.
func TestRegistry_ConcurrentRegisterAndBlock(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Register(&mockTool{name: "tool"})
			r.SetBlocked([]string{"tool"})
			r.SetBlocked(nil)
			r.All()
			r.AllSchemas()
		}(i)
	}
	wg.Wait()
}

// TestGitHubCLIToolNames_AllRegistered verifies that all names returned by
// GitHubCLIToolNames are actually registered when RegisterGitHubTools is called.
func TestGitHubCLIToolNames_AllRegistered(t *testing.T) {
	reg := NewRegistry()
	RegisterGitHubTools(reg)
	names := GitHubCLIToolNames()
	for _, name := range names {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}
