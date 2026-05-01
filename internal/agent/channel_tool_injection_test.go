package agent_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
)

// TestChannelContextPromptMentionsDelegateToAgent verifies that the channel
// prompt instructs the lead agent to use the delegate_to_agent tool.
func TestChannelContextPromptMentionsDelegateToAgent(t *testing.T) {
	members := []agent.SpaceMember{
		{Name: "Sam", Description: "Backend engineer"},
	}
	result := agent.BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", members)
	if !strings.Contains(result, "delegate_to_agent") {
		t.Errorf("expected channel prompt to mention delegate_to_agent, got:\n%s", result)
	}
	if strings.Contains(result, "use @mentions, NOT tools") {
		t.Errorf("expected old @mention instruction to be removed, still present in:\n%s", result)
	}
}

// TestChannelContextPromptPreservesMainChannelDiscipline verifies that the
// "speak only when additive" guidance is still present after the prompt change.
func TestChannelContextPromptPreservesMainChannelDiscipline(t *testing.T) {
	members := []agent.SpaceMember{
		{Name: "Sam", Description: "Backend"},
	}
	result := agent.BuildSpaceContextBlock("Eng", "channel", "Tom", "Tom", members)
	if !strings.Contains(result, "Main channel discipline") {
		t.Errorf("Main channel discipline section missing from channel prompt")
	}
}

// TestAgentDispatcherChannelCommentNotStale verifies that the stale comment
// "delegate_to_agent is NOT injected" has been removed from agent_dispatcher.go
// so future maintainers are not misled.
func TestAgentDispatcherChannelCommentNotStale(t *testing.T) {
	content, err := os.ReadFile("agent_dispatcher.go")
	if err != nil {
		t.Fatalf("could not read agent_dispatcher.go: %v", err)
	}
	if bytes.Contains(content, []byte("delegate_to_agent is NOT injected")) {
		t.Error("stale comment 'delegate_to_agent is NOT injected' still present in agent_dispatcher.go — update it")
	}
}
