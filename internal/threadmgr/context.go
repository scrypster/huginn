package threadmgr

import (
	"context"
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

type callingAgentKey struct{}

// SetCallingAgent stores the calling agent's name in ctx so that tools
// (e.g. DelegateToAgentTool) can record which agent initiated the delegation.
func SetCallingAgent(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, callingAgentKey{}, name)
}

// GetCallingAgent retrieves the calling agent's name from ctx.
// Returns "" if not set.
func GetCallingAgent(ctx context.Context) string {
	name, _ := ctx.Value(callingAgentKey{}).(string)
	return name
}

// ContextBudget controls how many tokens each section of the prompt may use.
type ContextBudget struct {
	Total     int // from ModelInfo.PromptBudget
	Persona   int // Total * 0.20 — never trimmed
	Artifacts int // Total * 0.40 — upstream FinishSummary blocks
	Snapshot  int // remainder — recent session messages
}

// NewContextBudget derives a ContextBudget from a raw prompt budget.
func NewContextBudget(promptBudget int) ContextBudget {
	persona := promptBudget / 5        // 20%
	artifacts := promptBudget * 2 / 5  // 40%
	snapshot := promptBudget - persona - artifacts // remainder
	return ContextBudget{
		Total:     promptBudget,
		Persona:   persona,
		Artifacts: artifacts,
		Snapshot:  snapshot,
	}
}

// estimateTokens estimates the token count for text using the 4-chars-per-token
// rule of thumb. Returns at least 1 for non-empty text.
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	tokens := (len(text) + 3) / 4 // ceil(len/4)
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// buildContextWithBudget constructs the []backend.Message slice for a thread's LLM loop.
//
// Section order:
//  1. System message: agent persona (never trimmed)
//  2. User messages: upstream FinishSummary blocks (trimmed to Artifacts budget)
//  3. User messages: session snapshot tail (trimmed to Snapshot budget)
func buildContextWithBudget(
	t *Thread,
	store session.StoreInterface,
	tm *ThreadManager,
	reg *agents.AgentRegistry,
	budget ContextBudget,
) []backend.Message {
	var msgs []backend.Message

	// 1. Persona — always included.
	personaContent := buildPersonaContent(t, reg)
	msgs = append(msgs, backend.Message{Role: "system", Content: personaContent})

	// 2. Upstream artifacts from completed dependency threads.
	artifactMsgs := buildArtifactMessages(t, tm, budget.Artifacts)
	msgs = append(msgs, artifactMsgs...)

	// 3. Session snapshot — tail of recent messages, trimmed to budget.
	snapshotMsgs := buildSnapshotMessages(t.SessionID, store, budget.Snapshot)
	msgs = append(msgs, snapshotMsgs...)

	// Ensure the conversation ends with a user message. Some providers
	// (Anthropic) reject requests where the last message is an assistant
	// message. When the session snapshot ends with an assistant message
	// (common — the lead agent's @mention) we append the task as a user
	// turn so the sub-agent knows what to do.
	if len(msgs) > 0 && msgs[len(msgs)-1].Role != "user" {
		msgs = append(msgs, backend.Message{
			Role:    "user",
			Content: t.Task,
		})
	}

	return msgs
}

// buildContext is the production entry point; derives budget from the thread's TokenBudget.
func buildContext(t *Thread, store session.StoreInterface, tm *ThreadManager, reg *agents.AgentRegistry) []backend.Message {
	budgetTokens := t.TokenBudget
	if budgetTokens <= 0 {
		budgetTokens = 4096 // safe default
	}
	return buildContextWithBudget(t, store, tm, reg, NewContextBudget(budgetTokens))
}

func buildPersonaContent(t *Thread, reg *agents.AgentRegistry) string {
	var base string
	if reg == nil {
		base = fmt.Sprintf("You are an AI agent. Your task: %s", t.Task)
	} else {
		ag, ok := reg.ByName(t.AgentID)
		if !ok {
			base = fmt.Sprintf("You are an AI agent named %s. Your task: %s", t.AgentID, t.Task)
		} else {
			prompt := agents.BuildPersonaPrompt(ag, "")
			base = prompt + "\n\n## Current Task\n" + t.Task
		}
	}
	// Append delegation rationale when provided by the lead agent.
	if t.Rationale != "" {
		base += "\n\n## Why You Were Chosen\n" + t.Rationale
	}
	return base
}

func buildArtifactMessages(t *Thread, tm *ThreadManager, budgetTokens int) []backend.Message {
	if len(t.DependsOn) == 0 || budgetTokens <= 0 {
		return nil
	}

	var msgs []backend.Message
	used := 0

	for _, depID := range t.DependsOn {
		dep, ok := tm.Get(depID)
		if !ok || dep.Summary == nil {
			continue
		}
		content := formatFinishSummary(dep.AgentID, dep.Summary)
		tokens := estimateTokens(content)
		if used+tokens > budgetTokens {
			break // drop remaining artifacts to stay within budget
		}
		msgs = append(msgs, backend.Message{
			Role:    "user",
			Content: content,
		})
		used += tokens
	}

	return msgs
}

func buildSnapshotMessages(sessionID string, store session.StoreInterface, budgetTokens int) []backend.Message {
	if store == nil || budgetTokens <= 0 {
		return nil
	}

	// Pull a large tail and then trim from the front if over budget.
	raw, err := store.TailMessages(sessionID, 50)
	if err != nil || len(raw) == 0 {
		return nil
	}

	// Walk newest-to-oldest, collecting until budget is reached.
	var selected []session.SessionMessage
	used := 0
	for i := len(raw) - 1; i >= 0; i-- {
		tokens := estimateTokens(raw[i].Content)
		if used+tokens > budgetTokens {
			break
		}
		selected = append(selected, raw[i])
		used += tokens
	}

	// Reverse to restore chronological order.
	for l, r := 0, len(selected)-1; l < r; l, r = l+1, r-1 {
		selected[l], selected[r] = selected[r], selected[l]
	}

	var msgs []backend.Message
	for _, sm := range selected {
		msgs = append(msgs, backend.Message{
			Role:    sm.Role,
			Content: sm.Content,
		})
	}
	return msgs
}

func formatFinishSummary(agentID string, fs *FinishSummary) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Result from agent %q\n\n", agentID))
	sb.WriteString("**Summary:** " + fs.Summary + "\n\n")
	if len(fs.FilesModified) > 0 {
		sb.WriteString("**Files modified:** " + strings.Join(fs.FilesModified, ", ") + "\n")
	}
	if len(fs.KeyDecisions) > 0 {
		sb.WriteString("**Key decisions:** " + strings.Join(fs.KeyDecisions, "; ") + "\n")
	}
	if len(fs.Artifacts) > 0 {
		sb.WriteString("**Artifacts:** " + strings.Join(fs.Artifacts, ", ") + "\n")
	}
	sb.WriteString("**Status:** " + fs.Status + "\n")
	return sb.String()
}
