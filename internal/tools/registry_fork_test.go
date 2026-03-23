package tools

import (
	"testing"
)

func TestRegistryFork_IndependentFromParent(t *testing.T) {
	parent := NewRegistry()
	parent.Register(&mockTool{name: "parent_tool"})
	parent.TagTools([]string{"parent_tool"}, "builtin")

	fork := parent.Fork()

	// Fork inherits parent tools and tags.
	if _, ok := fork.Get("parent_tool"); !ok {
		t.Fatal("fork should inherit parent tools")
	}
	if fork.ProviderFor("parent_tool") != "builtin" {
		t.Fatal("fork should inherit parent provider tags")
	}

	// Registering into fork does NOT affect parent.
	fork.Register(&mockTool{name: "fork_only_tool"})
	if _, ok := parent.Get("fork_only_tool"); ok {
		t.Fatal("fork mutation must not affect parent registry")
	}

	// Unregistering from fork does NOT affect parent.
	fork.Unregister("parent_tool")
	if _, ok := parent.Get("parent_tool"); !ok {
		t.Fatal("fork Unregister must not affect parent")
	}
}

func TestRegistryFork_TagsIndependent(t *testing.T) {
	parent := NewRegistry()
	parent.Register(&mockTool{name: "shared"})
	parent.TagTools([]string{"shared"}, "provider_a")

	fork := parent.Fork()
	fork.TagTools([]string{"shared"}, "provider_b")

	if parent.ProviderFor("shared") != "provider_a" {
		t.Errorf("parent tag changed by fork: got %q want %q", parent.ProviderFor("shared"), "provider_a")
	}
	if fork.ProviderFor("shared") != "provider_b" {
		t.Errorf("fork tag wrong: got %q want %q", fork.ProviderFor("shared"), "provider_b")
	}
}

func TestRegistryFork_EmptyParent(t *testing.T) {
	parent := NewRegistry()
	fork := parent.Fork()

	if len(fork.All()) != 0 {
		t.Fatalf("fork of empty parent should be empty, got %d tools", len(fork.All()))
	}

	// Registering into fork of empty parent must not panic.
	fork.Register(&mockTool{name: "new_tool"})
	if _, ok := fork.Get("new_tool"); !ok {
		t.Fatal("tool registered into empty-parent fork not found")
	}
}

func TestRegistryFork_ConcurrentSessionIsolation(t *testing.T) {
	parent := NewRegistry()
	parent.Register(&mockTool{name: "shared_tool"})

	// Simulate two concurrent sessions.
	fork1 := parent.Fork()
	fork2 := parent.Fork()

	fork1.Register(&mockTool{name: "session1_vault_tool"})
	fork2.Register(&mockTool{name: "session2_vault_tool"})

	// Each fork sees only its own session tool.
	if _, ok := fork1.Get("session2_vault_tool"); ok {
		t.Fatal("fork1 must not see fork2 session tools")
	}
	if _, ok := fork2.Get("session1_vault_tool"); ok {
		t.Fatal("fork2 must not see fork1 session tools")
	}

	// Parent is unaffected.
	if _, ok := parent.Get("session1_vault_tool"); ok {
		t.Fatal("parent must not see fork1 session tools")
	}
	if _, ok := parent.Get("session2_vault_tool"); ok {
		t.Fatal("parent must not see fork2 session tools")
	}

	// Both forks still inherit the shared parent tool.
	if _, ok := fork1.Get("shared_tool"); !ok {
		t.Fatal("fork1 should see shared parent tool")
	}
	if _, ok := fork2.Get("shared_tool"); !ok {
		t.Fatal("fork2 should see shared parent tool")
	}
}

func TestRegistryFork_MultipleForks_NoInterference(t *testing.T) {
	parent := NewRegistry()
	parent.Register(&mockTool{name: "base"})

	const n = 10
	forks := make([]*Registry, n)
	for i := range forks {
		forks[i] = parent.Fork()
	}

	// Each fork gets a unique tool.
	for i, f := range forks {
		name := "vault_" + string(rune('a'+i))
		f.Register(&mockTool{name: name})
	}

	// Verify no cross-contamination across any pair.
	for i, f := range forks {
		for j, g := range forks {
			if i == j {
				continue
			}
			toolName := "vault_" + string(rune('a'+j))
			if _, ok := f.Get(toolName); ok {
				t.Errorf("fork[%d] should not see tool from fork[%d]", i, j)
			}
			_ = g
		}
	}

	// Parent still only has "base".
	if len(parent.All()) != 1 {
		t.Errorf("parent should have 1 tool, got %d", len(parent.All()))
	}
}
