// internal/server/handlers_heartbeat_test.go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestHandleUpdateAgent_CreatesHeartbeatYAML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HUGINN_HOME", tmp)

	// Pre-create the agent so the PUT is an update (not creation)
	existing := agents.AgentDef{Name: "HeartbeatTestAgent", Model: "claude-sonnet-4-6"}
	_ = agents.SaveAgentDefault(existing)

	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		srv.handleUpdateAgent(w, r)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	update := map[string]any{
		"name":              "HeartbeatTestAgent",
		"model":             "claude-sonnet-4-6",
		"heartbeat_enabled": true,
		"heartbeat_cron":    "0 8 * * *",
	}
	body, _ := json.Marshal(update)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/agents/HeartbeatTestAgent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// huginnBaseDir() returns filepath.Join(HUGINN_HOME, ".huginn")
	yamlPath := filepath.Join(tmp, ".huginn", "workflows", "heartbeat-heartbeattestagent.yaml")
	content, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("expected heartbeat YAML at %s: %v", yamlPath, err)
	}
	if !strings.Contains(string(content), "enabled: true") {
		t.Errorf("expected enabled: true in heartbeat YAML, got:\n%s", content)
	}
	if !strings.Contains(string(content), `schedule: "0 8 * * *"`) {
		t.Errorf("expected schedule in heartbeat YAML, got:\n%s", content)
	}
}

func TestHandleDeleteAgent_RemovesHeartbeatYAML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HUGINN_HOME", tmp)

	// Pre-create two agents (cannot delete the last agent)
	_ = agents.SaveAgentDefault(agents.AgentDef{Name: "AgentA", Model: "claude-sonnet-4-6"})
	target := agents.AgentDef{Name: "HeartbeatDeleteTarget", Model: "claude-sonnet-4-6", HeartbeatEnabled: true}
	_ = agents.SaveAgentDefault(target)
	_ = agents.SyncHeartbeatYAMLDefault(target)

	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		srv.handleDeleteAgent(w, r)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agents/HeartbeatDeleteTarget", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// huginnBaseDir() returns filepath.Join(HUGINN_HOME, ".huginn")
	yamlPath := filepath.Join(tmp, ".huginn", "workflows", "heartbeat-heartbeatdeletetarget.yaml")
	if _, err := os.Stat(yamlPath); !os.IsNotExist(err) {
		t.Error("heartbeat YAML should be removed on agent deletion")
	}
}
