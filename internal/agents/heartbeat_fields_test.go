// internal/agents/heartbeat_fields_test.go
package agents_test

import (
	"encoding/json"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestAgentDef_HeartbeatFields_RoundTrip(t *testing.T) {
	def := agents.AgentDef{
		Name:             "Ares",
		HeartbeatEnabled: true,
		HeartbeatCron:    "0 8 * * *",
	}
	b, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	var out agents.AgentDef
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.HeartbeatEnabled {
		t.Error("HeartbeatEnabled should round-trip")
	}
	if out.HeartbeatCron != "0 8 * * *" {
		t.Errorf("HeartbeatCron: got %q, want %q", out.HeartbeatCron, "0 8 * * *")
	}
}

func TestAgentDef_HeartbeatFields_OmittedByDefault(t *testing.T) {
	def := agents.AgentDef{Name: "Ghost"}
	b, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{}` {
		// heartbeat fields must be omitempty — check they don't appear in minimal output
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if _, ok := m["heartbeat_enabled"]; ok {
			t.Error("heartbeat_enabled should be omitted when false")
		}
		if _, ok := m["heartbeat_cron"]; ok {
			t.Error("heartbeat_cron should be omitted when empty")
		}
	}
}
