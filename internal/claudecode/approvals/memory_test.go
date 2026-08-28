package approvals

import (
	"fmt"
	"testing"
)

func TestMemoryExactMatchOnly(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "go test ./...")
	if !m.has("codey", "Bash", "go test ./...") {
		t.Fatal("exact command did not match")
	}
	// One byte different must NOT match. This is the whole security property:
	// no prefix matching, no whitespace collapsing, no case folding.
	for _, other := range []string{
		"go test ./...x",
		"go  test ./...",
		"GO TEST ./...",
		"go test ./... && rm -rf /",
		"go test",
	} {
		if m.has("codey", "Bash", other) {
			t.Fatalf("command %q matched a different remembered command", other)
		}
	}
}

func TestMemoryTrimsOnlyTrailingWhitespace(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "go test ./...  ")
	if !m.has("codey", "Bash", "go test ./...") {
		t.Fatal("trailing whitespace should be trimmed on both add and has")
	}
	if m.has("codey", "Bash", "  go test ./...") {
		t.Fatal("LEADING whitespace was trimmed; only trailing may be")
	}
}

func TestMemoryIsPerAgent(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "go test ./...")
	if m.has("other", "Bash", "go test ./...") {
		t.Fatal("a remembered command leaked to a different agent")
	}
}

func TestMemoryIsPerTool(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "x")
	if m.has("codey", "Write", "x") {
		t.Fatal("a remembered command leaked across tool names")
	}
}

func TestMemoryLRUEvictsOldest(t *testing.T) {
	m := newCmdMemory(3)
	m.add("codey", "Bash", "one")
	m.add("codey", "Bash", "two")
	m.add("codey", "Bash", "three")
	// Touch "one" so "two" becomes least-recently-used.
	if !m.has("codey", "Bash", "one") {
		t.Fatal("one should still be present")
	}
	m.add("codey", "Bash", "four")
	if m.has("codey", "Bash", "two") {
		t.Fatal("two should have been evicted as least-recently-used")
	}
	for _, keep := range []string{"one", "three", "four"} {
		if !m.has("codey", "Bash", keep) {
			t.Fatalf("%q was evicted but should have been kept", keep)
		}
	}
}

func TestMemoryCapIsPerAgent(t *testing.T) {
	m := newCmdMemory(2)
	m.add("a", "Bash", "one")
	m.add("a", "Bash", "two")
	m.add("b", "Bash", "three")
	m.add("b", "Bash", "four")
	for _, c := range []struct{ agent, cmd string }{
		{"a", "one"}, {"a", "two"}, {"b", "three"}, {"b", "four"},
	} {
		if !m.has(c.agent, "Bash", c.cmd) {
			t.Fatalf("%s/%s evicted; the cap must be per agent", c.agent, c.cmd)
		}
	}
}

func TestMemoryReAddDoesNotGrow(t *testing.T) {
	m := newCmdMemory(2)
	m.add("codey", "Bash", "one")
	m.add("codey", "Bash", "one")
	m.add("codey", "Bash", "two")
	if !m.has("codey", "Bash", "one") {
		t.Fatal("re-adding the same command consumed two slots")
	}
	_ = fmt.Sprint()
}
