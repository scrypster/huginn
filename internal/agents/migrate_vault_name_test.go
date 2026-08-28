package agents

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMigrateLegacyVaultNames(t *testing.T) {
	baseDir := t.TempDir()
	cfg := &AgentsConfig{
		Agents: []AgentDef{
			{Name: "vaultprobe", VaultName: "vaultprobe-huginn"},
			{Name: "custom agent", VaultName: "my-special-vault"},
			{Name: "no vault agent", VaultName: ""},
		},
	}
	// Seed on-disk files as SaveAgent would have written them, so the
	// migration exercises the real persist path.
	for _, def := range cfg.Agents {
		if err := SaveAgent(baseDir, def); err != nil {
			t.Fatalf("seed SaveAgent(%s): %v", def.Name, err)
		}
	}

	migrated := MigrateLegacyVaultNames(baseDir, cfg, "mj")
	if len(migrated) != 1 || migrated[0] != "vaultprobe" {
		t.Fatalf("expected only vaultprobe migrated, got %v", migrated)
	}

	// In-memory def updated to canonical form.
	var got AgentDef
	for _, def := range cfg.Agents {
		if def.Name == "vaultprobe" {
			got = def
		}
	}
	wantVault := "huginn:agent:mj:vaultprobe"
	if got.VaultName != wantVault {
		t.Fatalf("in-memory VaultName = %q, want %q", got.VaultName, wantVault)
	}

	// Persisted to disk.
	data, err := os.ReadFile(filepath.Join(baseDir, "agents", "vaultprobe.yaml"))
	if err != nil {
		t.Fatalf("read persisted agent file: %v", err)
	}
	var persisted AgentDef
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal persisted agent file: %v", err)
	}
	if persisted.VaultName != wantVault {
		t.Fatalf("persisted VaultName = %q, want %q", persisted.VaultName, wantVault)
	}

	// Custom vault name untouched.
	for _, def := range cfg.Agents {
		if def.Name == "custom agent" && def.VaultName != "my-special-vault" {
			t.Fatalf("custom agent VaultName changed: %q", def.VaultName)
		}
		if def.Name == "no vault agent" && def.VaultName != "" {
			t.Fatalf("no vault agent VaultName changed: %q", def.VaultName)
		}
	}

	// Second run is a no-op.
	migratedAgain := MigrateLegacyVaultNames(baseDir, cfg, "mj")
	if len(migratedAgain) != 0 {
		t.Fatalf("second run should be a no-op, got %v", migratedAgain)
	}
}

func TestLegacyAutoVaultNameMatchesHireVaultNamePattern(t *testing.T) {
	// Mirrors internal/tools/create_agent.go's hireVaultName behavior for the
	// cases that matter to the migration: lowercase, spaces/hyphens preserved
	// as hyphens, other punctuation dropped, "-huginn" suffix, empty->teammate.
	cases := map[string]string{
		"vaultprobe":   "vaultprobe-huginn",
		"Custom Agent": "custom-agent-huginn",
		"":             "teammate-huginn",
		"a_b@c":        "abc-huginn", // punctuation dropped, no substitution
	}
	for in, want := range cases {
		if got := legacyAutoVaultName(in); got != want {
			t.Errorf("legacyAutoVaultName(%q) = %q, want %q", in, got, want)
		}
	}
}
