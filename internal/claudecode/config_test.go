package claudecode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultConfigIsDisabled(t *testing.T) {
	c := DefaultConfig()
	if c.Enabled {
		t.Error("DefaultConfig().Enabled = true, want false — the bridge reads every transcript on the machine and must be opt-in")
	}
	if c.WatchEnabled() {
		t.Error("WatchEnabled() = true while Enabled is false")
	}
	if c.DelegateEnabled() {
		t.Error("DelegateEnabled() = true while Enabled is false")
	}
}

func TestSubsystemsRequireTopLevelEnable(t *testing.T) {
	c := DefaultConfig()
	c.Enabled = true
	if !c.WatchEnabled() {
		t.Error("WatchEnabled() = false with Enabled=true and Watch.Enabled default true")
	}
	c.Watch.Enabled = false
	if c.WatchEnabled() {
		t.Error("WatchEnabled() = true with Watch.Enabled=false")
	}
}

func TestDefaultsMatchTheSpec(t *testing.T) {
	c := DefaultConfig()
	if c.Binary != "claude" {
		t.Errorf("Binary = %q, want claude", c.Binary)
	}
	if c.Watch.MaxFileMB != 50 {
		t.Errorf("MaxFileMB = %d, want 50", c.Watch.MaxFileMB)
	}
	if c.Delegate.PermissionMode != "acceptEdits" {
		t.Errorf("PermissionMode = %q, want acceptEdits", c.Delegate.PermissionMode)
	}
	if c.Delegate.MaxTurns != 30 {
		t.Errorf("MaxTurns = %d, want 30", c.Delegate.MaxTurns)
	}
	if c.Delegate.TimeoutSecs != 900 {
		t.Errorf("TimeoutSecs = %d, want 900", c.Delegate.TimeoutSecs)
	}
	if c.Delegate.SkipPermissions {
		t.Error("SkipPermissions must default to false")
	}
}

func TestConfigJSONTags(t *testing.T) {
	b, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"enabled"`, `"binary"`, `"watch"`, `"delegate"`,
		`"max_file_mb"`, `"permission_mode"`, `"timeout_secs"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("marshalled config missing %s: %s", want, b)
		}
	}
}
