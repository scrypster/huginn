// internal/agents/heartbeat_yaml_test.go
package agents_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestSyncHeartbeatYAML_CreatesFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	def := agents.AgentDef{
		Name:             "Ares",
		HeartbeatEnabled: true,
		HeartbeatCron:    "0 8 * * 1-5",
	}
	if err := agents.SyncHeartbeatYAMLDefault(def); err != nil {
		t.Fatalf("SyncHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, ".huginn", "workflows", "heartbeat-ares.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	s := string(content)
	if !strings.HasPrefix(s, "# MANAGED BY HUGINN") {
		t.Error("expected managed header")
	}
	if !strings.Contains(s, `name: "Heartbeat: Ares"`) {
		t.Errorf("expected name field, got:\n%s", s)
	}
	if !strings.Contains(s, "enabled: true") {
		t.Errorf("expected enabled: true, got:\n%s", s)
	}
	if !strings.Contains(s, `schedule: "0 8 * * 1-5"`) {
		t.Errorf("expected schedule, got:\n%s", s)
	}
	if !strings.Contains(s, "type: agent_dm") {
		t.Errorf("expected agent_dm delivery, got:\n%s", s)
	}
	if !strings.Contains(s, `from: "Ares"`) {
		t.Errorf("expected from field, got:\n%s", s)
	}
	if !strings.Contains(s, `agent: "Ares"`) {
		t.Errorf("expected agent field, got:\n%s", s)
	}
}

func TestSyncHeartbeatYAML_DisablesExistingFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	// First create the file
	enabled := agents.AgentDef{Name: "Ares", HeartbeatEnabled: true, HeartbeatCron: "0 */4 * * *"}
	_ = agents.SyncHeartbeatYAMLDefault(enabled)

	// Now disable
	disabled := agents.AgentDef{Name: "Ares", HeartbeatEnabled: false, HeartbeatCron: "0 */4 * * *"}
	if err := agents.SyncHeartbeatYAMLDefault(disabled); err != nil {
		t.Fatalf("SyncHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, ".huginn", "workflows", "heartbeat-ares.yaml")
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "enabled: false") {
		t.Errorf("expected enabled: false after disable, got:\n%s", content)
	}
	// File must still exist (cron preserved for re-enable)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file should persist when disabled, not be deleted")
	}
}

func TestSyncHeartbeatYAML_NoFileWhenDisabledAndNoExisting(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	def := agents.AgentDef{Name: "Ghost", HeartbeatEnabled: false}
	if err := agents.SyncHeartbeatYAMLDefault(def); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, ".huginn", "workflows", "heartbeat-ghost.yaml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("no file should be created when disabled and no existing file")
	}
}

func TestDeleteHeartbeatYAML_RemovesManagedFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	def := agents.AgentDef{Name: "Ares", HeartbeatEnabled: true}
	_ = agents.SyncHeartbeatYAMLDefault(def)

	if err := agents.DeleteHeartbeatYAMLDefault("Ares"); err != nil {
		t.Fatalf("DeleteHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, ".huginn", "workflows", "heartbeat-ares.yaml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("managed file should be deleted")
	}
}

func TestDeleteHeartbeatYAML_DoesNotRemoveUserFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	home := os.Getenv("HUGINN_HOME")
	_ = os.MkdirAll(filepath.Join(home, ".huginn", "workflows"), 0o750)
	userFile := filepath.Join(home, ".huginn", "workflows", "heartbeat-ares.yaml")
	// User-customized file does NOT start with managed header
	_ = os.WriteFile(userFile, []byte("# My custom heartbeat\nname: custom\n"), 0o600)

	if err := agents.DeleteHeartbeatYAMLDefault("Ares"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(userFile); os.IsNotExist(err) {
		t.Error("user-customized file should NOT be deleted")
	}
}

func TestRenameHeartbeatYAML_MovesFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	old := agents.AgentDef{Name: "Ares", HeartbeatEnabled: true, HeartbeatCron: "0 8 * * *"}
	_ = agents.SyncHeartbeatYAMLDefault(old)

	newDef := agents.AgentDef{Name: "Aries", HeartbeatEnabled: true, HeartbeatCron: "0 8 * * *"}
	if err := agents.RenameHeartbeatYAMLDefault("Ares", newDef); err != nil {
		t.Fatalf("RenameHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	oldPath := filepath.Join(home, ".huginn", "workflows", "heartbeat-ares.yaml")
	newPath := filepath.Join(home, ".huginn", "workflows", "heartbeat-aries.yaml")

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old heartbeat file should be removed")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new heartbeat file should exist: %v", err)
	}
	if !strings.Contains(string(content), `name: "Heartbeat: Aries"`) {
		t.Errorf("new file should reference new name, got:\n%s", content)
	}
}

func TestIsManaged(t *testing.T) {
	tmp := t.TempDir()
	managed := filepath.Join(tmp, "managed.yaml")
	unmanaged := filepath.Join(tmp, "unmanaged.yaml")

	_ = os.WriteFile(managed, []byte("# MANAGED BY HUGINN\nname: test\n"), 0o600)
	_ = os.WriteFile(unmanaged, []byte("# My custom file\nname: test\n"), 0o600)

	if !agents.IsManaged(managed) {
		t.Error("expected managed file to be detected")
	}
	if agents.IsManaged(unmanaged) {
		t.Error("expected unmanaged file to not be detected")
	}
	if agents.IsManaged(filepath.Join(tmp, "nonexistent.yaml")) {
		t.Error("nonexistent file should not be managed")
	}
}
