package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
)

func TestAgentsWatcher_CallbackOnChange(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	w := agent.NewAgentsWatcher(dir, func(reg *agents.AgentRegistry) {
		calls.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(ctx)

	// Give watcher time to seed initial hash
	time.Sleep(100 * time.Millisecond)

	// Write an agent file to trigger change
	agentYAML := "name: TestAgent\nmodel: test-model\nsystem_prompt: \"hello\"\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "test.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait up to 5s for callback to fire
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if calls.Load() == 0 {
		t.Error("expected callback to fire after agent file change, got 0 calls")
	}
}
