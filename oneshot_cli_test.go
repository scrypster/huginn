package main

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/oneshot"
)

func TestNewOneShotConfig_MapsOpts(t *testing.T) {
	cfg := newOneShotConfig(oneshotRunOpts{
		prompt:          "list agents",
		agentName:       "Steve",
		model:           "qwen2.5-coder:14b",
		noTools:         false,
		skipPermissions: false,
		maxTurns:        8,
		cwd:             "/tmp",
		bashTimeoutSecs: 30,
	})
	if cfg.Prompt != "list agents" || cfg.AgentName != "Steve" {
		t.Fatalf("prompt/agent = %q %q", cfg.Prompt, cfg.AgentName)
	}
	if cfg.SkipPermissions {
		t.Fatal("skipPermissions should be false so deny leftover JSON is filtered")
	}
	if cfg.BashTimeout != 30*time.Second {
		t.Errorf("BashTimeout = %s", cfg.BashTimeout)
	}
	if cfg.MaxTurns != 8 {
		t.Errorf("MaxTurns = %d", cfg.MaxTurns)
	}
}

func TestOneshotToolNames(t *testing.T) {
	if oneshotToolNames(nil) != nil {
		t.Fatal("nil calls should stay nil")
	}
	got := oneshotToolNames([]oneshot.ToolCall{{Name: "bash"}, {Name: ""}, {Name: "gh_issue_create"}})
	if len(got) != 2 || got[0] != "bash" || got[1] != "gh_issue_create" {
		t.Errorf("names = %#v", got)
	}
}
