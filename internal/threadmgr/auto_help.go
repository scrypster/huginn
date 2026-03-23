package threadmgr

import (
	"context"
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// HelpResolver handles a sub-agent's request_help() call autonomously.
type HelpResolver interface {
	// Resolve is called when a sub-agent (agentID) raises ErrHelp with helpMessage.
	// It returns the resolved answer or an error.
	Resolve(ctx context.Context, sessionID, threadID, agentID, helpMessage string) (string, error)
}

// autoHelpSnapshotBudget is the token budget for session context injected into
// the auto-help prompt. Sized conservatively to leave headroom for the system
// prompt and the LLM's response within a typical 4k–8k context window.
const autoHelpSnapshotBudget = 3000

// AutoHelpResolver implements HelpResolver by making a focused LLM call using
// the primary agent's persona to answer the sub-agent's question.
type AutoHelpResolver struct {
	Backend backend.Backend
	// AgentReg is populated during server setup for future use by resolvers that
	// need to look up agents by name or slot. It is not consumed by Resolve itself.
	AgentReg     *agents.AgentRegistry
	Store        session.StoreInterface
	Broadcast    BroadcastFn
	PrimaryAgent func(sessionID string) *agents.Agent
}

// Resolve satisfies HelpResolver. It looks up the primary agent, builds a
// focused prompt, optionally includes session snapshot messages, then calls
// the LLM and returns the trimmed response.
func (r *AutoHelpResolver) Resolve(ctx context.Context, sessionID, threadID, agentID, helpMessage string) (string, error) {
	ag := r.PrimaryAgent(sessionID)
	if ag == nil {
		return "", fmt.Errorf("auto_help: no primary agent for session %s", sessionID)
	}

	r.Broadcast(sessionID, "thread_help_resolving", map[string]any{
		"thread_id": threadID,
	})

	systemPrompt := ag.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are " + ag.Name + ", an expert assistant."
	}
	systemPrompt += "\n\nA sub-agent needs your help. Answer concisely and directly."

	msgs := []backend.Message{
		{Role: "system", Content: systemPrompt},
	}

	if r.Store != nil {
		msgs = append(msgs, buildSnapshotMessages(sessionID, r.Store, autoHelpSnapshotBudget)...)
	}

	msgs = append(msgs, backend.Message{
		Role:    "user",
		Content: fmt.Sprintf("Sub-agent %s needs your help:\n\n%s", agentID, helpMessage),
	})

	var buf strings.Builder
	resp, err := r.Backend.ChatCompletion(ctx, backend.ChatRequest{
		Model:    ag.GetModelID(),
		Messages: msgs,
		OnToken: func(tok string) {
			buf.WriteString(tok)
		},
	})
	if err != nil {
		return "", err
	}

	// If OnToken wasn't called (non-streaming backend), use resp.Content directly.
	answer := buf.String()
	if answer == "" {
		answer = resp.Content
	}

	r.Broadcast(sessionID, "thread_help_resolved", map[string]any{
		"thread_id": threadID,
	})

	return strings.TrimSpace(answer), nil
}
