package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMain sets HUGINN_HOME to a temporary directory for the entire server test
// package so that tests which call SaveAgentDefault / DeleteAgentDefault
// never write to the developer's real ~/.huginn/agents/.
//
// It also pre-populates the agents directory with a few baseline agents so
// that the "cannot delete last agent" guard in handleDeleteAgent does not
// interfere with tests that add and remove individual agents.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "huginn-server-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("HUGINN_HOME", tmp) //nolint:errcheck

	// Pre-populate with a few stable baseline agents so LoadAgents() always
	// returns >1 agent; this prevents the "last agent" guard from firing
	// during tests that create and delete individual agents.
	agentsDir := filepath.Join(tmp, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		panic(err)
	}
	baseline := []map[string]any{
		{"name": "BaselineAlpha", "slot": "planner", "model": "test-model", "color": "#58a6ff", "icon": "A"},
		{"name": "BaselineBeta", "slot": "coder", "model": "test-model", "color": "#3fb950", "icon": "B"},
		{"name": "BaselineGamma", "slot": "general", "model": "test-model", "color": "#f78166", "icon": "G"},
	}
	for _, ag := range baseline {
		data, _ := json.Marshal(ag)
		name := ag["name"].(string)
		path := filepath.Join(agentsDir, filepath.Base(filepath.Clean(name+".json")))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			panic(err)
		}
	}

	os.Exit(m.Run())
}
