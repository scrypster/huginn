package agents_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestAgentRegistry_VaultCollision(t *testing.T) {
	// Capture log output.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(orig) })

	reg := agents.NewRegistry()

	reg.Register(&agents.Agent{
		Name:      "Alpha",
		VaultName: "shared-vault",
		ModelID:   "m1",
	})
	reg.Register(&agents.Agent{
		Name:      "Beta",
		VaultName: "shared-vault",
		ModelID:   "m2",
	})

	logged := buf.String()
	if !strings.Contains(logged, "vault name collision") {
		t.Errorf("expected 'vault name collision' warning in logs, got:\n%s", logged)
	}
}

func FuzzAgentRegister(f *testing.F) {
	f.Add("")
	f.Add("valid-agent")
	f.Add("UPPER")
	f.Add("agent with spaces")
	f.Add("agent/slash")
	f.Add(strings.Repeat("a", 200))

	f.Fuzz(func(t *testing.T, name string) {
		reg := agents.NewRegistry()
		// Must not panic regardless of input.
		reg.Register(&agents.Agent{
			Name:    name,
			ModelID: "m",
		})
	})
}
