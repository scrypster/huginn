package agents

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// SpaceChecker validates that an agent is a member of a space.
// Returns (nil, nil) when the space is not found — callers treat that as deny-all.
type SpaceChecker interface {
	SpaceMembers(spaceID string) ([]string, error)
}

// delegationWriteErrorCount is incremented when AppendDelegation returns an error.
// This allows tests and metrics to verify that delegation write failures are tracked.
// Reset by resetDelegationWriteErrorCount (tests only — unexported to prevent misuse).
var delegationWriteErrorCount atomic.Int64

// DelegationWriteErrorCount returns the cumulative count of delegation write errors.
func DelegationWriteErrorCount() int64 { return delegationWriteErrorCount.Load() }

// resetDelegationWriteErrorCount zeroes the counter. Used exclusively by tests to
// provide isolation between test cases; never call from production code.
func resetDelegationWriteErrorCount() { delegationWriteErrorCount.Store(0) }

// ConsultAgentTool implements tools.Tool — lets the active agent consult another
// named agent mid-run. Hard limit: 1 level of delegation (no recursion).
type ConsultAgentTool struct {
	agentReg  *AgentRegistry
	backend   backend.Backend
	depth     *int32 // atomic; 0=top-level, 1=already in delegation

	// Memory persistence (may be nil)
	memoryStore   MemoryStoreIface
	fromAgentName string

	// TUI callbacks (may be nil)
	onDelegate func(from, to, question string) // delegation starting
	onDone     func(from, to, answer string)   // delegation complete
	onToken    func(agent, token string)        // streaming token from delegatee

	// Space membership guard (may be nil — guard is skipped when nil or spaceID is empty)
	spaceChecker SpaceChecker
	spaceID      string
}

// NewConsultAgentTool creates a ConsultAgentTool with no token streaming callback.
func NewConsultAgentTool(reg *AgentRegistry, b backend.Backend, depth *int32,
	onDelegate func(from, to, question string),
	onDone func(from, to, answer string),
) *ConsultAgentTool {
	return &ConsultAgentTool{
		agentReg:   reg,
		backend:    b,
		depth:      depth,
		onDelegate: onDelegate,
		onDone:     onDone,
	}
}

// NewConsultAgentToolFull creates a ConsultAgentTool with streaming token callback.
func NewConsultAgentToolFull(reg *AgentRegistry, b backend.Backend, depth *int32,
	onDelegate func(from, to, question string),
	onDone func(from, to, answer string),
	onToken func(agent, token string),
) *ConsultAgentTool {
	t := NewConsultAgentTool(reg, b, depth, onDelegate, onDone)
	t.onToken = onToken
	return t
}

// NewConsultAgentToolWithMemory creates a ConsultAgentTool that persists delegation entries.
func NewConsultAgentToolWithMemory(reg *AgentRegistry, b backend.Backend, depth *int32,
	onDelegate func(from, to, question string),
	onDone func(from, to, answer string),
	onToken func(agent, token string),
	ms MemoryStoreIface,
	fromAgentName string,
) *ConsultAgentTool {
	t := NewConsultAgentToolFull(reg, b, depth, onDelegate, onDone, onToken)
	t.memoryStore = ms
	t.fromAgentName = fromAgentName
	return t
}

// WithSpaceContext wires space membership validation. When spaceID is non-empty
// and checker is non-nil, Execute() will deny consultations with agents that are
// not members of that space.
func (t *ConsultAgentTool) WithSpaceContext(spaceID string, checker SpaceChecker) *ConsultAgentTool {
	t.spaceID = spaceID
	t.spaceChecker = checker
	return t
}

func (t *ConsultAgentTool) Name() string                      { return "consult_agent" }
func (t *ConsultAgentTool) Permission() tools.PermissionLevel { return tools.PermRead }

func (t *ConsultAgentTool) Description() string {
	names := t.agentReg.Names()
	return fmt.Sprintf(
		"Consult another named agent for their expertise. "+
			"Use this when you need a different perspective, review, or specialized knowledge. "+
			"Available agents: %s. "+
			"The consulted agent will answer your question but cannot use tools or delegate further.",
		strings.Join(names, ", "),
	)
}

func (t *ConsultAgentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "consult_agent",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"agent_name": {
						Type:        "string",
						Description: "Name of the agent to consult (e.g. 'Mark', 'Chris')",
					},
					"question": {
						Type:        "string",
						Description: "The specific question or request for the consulted agent",
					},
					"context_summary": {
						Type:        "string",
						Description: "Brief summary of what you're working on to orient the consulted agent",
					},
				},
				Required: []string{"agent_name", "question"},
			},
		},
	}
}

func (t *ConsultAgentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	agentName, _ := args["agent_name"].(string)
	question, _ := args["question"].(string)
	contextSummary, _ := args["context_summary"].(string)

	if agentName == "" || question == "" {
		return tools.ToolResult{IsError: true, Error: "agent_name and question are required"}
	}

	// 0. Space membership guard — runs before depth check to fail-fast.
	if t.spaceID != "" && t.spaceChecker != nil {
		members, err := t.spaceChecker.SpaceMembers(t.spaceID)
		if err != nil {
			return tools.ToolResult{
				IsError: true,
				Error:   fmt.Sprintf("space lookup failed: %v", err),
			}
		}
		allowed := make(map[string]struct{}, len(members))
		for _, m := range members {
			allowed[strings.ToLower(m)] = struct{}{}
		}
		if _, ok := allowed[strings.ToLower(agentName)]; !ok {
			return tools.ToolResult{
				IsError: true,
				Error:   fmt.Sprintf("agent %q is not a member of this space", agentName),
			}
		}
	}

	// 1. Hard depth check + increment — atomic to prevent race
	newDepth := atomic.AddInt32(t.depth, 1)
	if newDepth > 1 {
		atomic.AddInt32(t.depth, -1)
		return tools.ToolResult{
			IsError: true,
			Error:   "delegation depth limit reached: agents cannot consult other agents during a consultation",
		}
	}
	defer atomic.AddInt32(t.depth, -1)

	// 2. Look up target agent
	target, ok := t.agentReg.ByName(agentName)
	if !ok {
		available := strings.Join(t.agentReg.Names(), ", ")
		return tools.ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("unknown agent %q; available: %s", agentName, available),
		}
	}

	// 3. Notify TUI: delegation starting
	if t.onDelegate != nil {
		t.onDelegate("active", target.Name, question)
	}

	// 5. Build delegatee's message list
	systemPrompt := target.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are %s, an expert assistant.", target.Name)
	}
	systemPrompt += "\n\nYou are being consulted by a colleague. Answer their question directly and concisely."

	msgs := []backend.Message{
		{Role: "system", Content: systemPrompt},
	}
	msgs = append(msgs, target.DelegationContext()...)

	// Build user message
	userContent := question
	if contextSummary != "" {
		userContent = fmt.Sprintf("Context: %s\n\nQuestion: %s", contextSummary, question)
	}
	msgs = append(msgs, backend.Message{Role: "user", Content: userContent})

	// 6. Single-turn call — NO tools for delegatees (CRITICAL)
	var buf strings.Builder
	_, err := t.backend.ChatCompletion(ctx, backend.ChatRequest{
		Model:    target.GetModelID(),
		Messages: msgs,
		Tools:    nil, // CRITICAL: no tools for delegatees
		OnToken: func(token string) {
			buf.WriteString(token)
			if t.onToken != nil {
				t.onToken(target.Name, token)
			}
		},
	})
	if err != nil {
		return tools.ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("consultation with %s failed: %v", target.Name, err),
		}
	}

	answer := buf.String()

	// 7. Update delegatee's local history
	target.AppendHistory(
		backend.Message{Role: "user", Content: userContent},
		backend.Message{Role: "assistant", Content: answer},
	)

	// 7b. Persist delegation to MemoryStore (if configured)
	if t.memoryStore != nil {
		from := t.fromAgentName
		if from == "" {
			from = "unknown"
		}
		entry := DelegationEntry{From: from, To: target.Name, Question: question, Answer: answer, Timestamp: time.Now()}
		if err := t.memoryStore.AppendDelegation(ctx, entry); err != nil {
			slog.Warn("consult: failed to persist delegation memory", "from", from, "to", target.Name, "err", err)
			delegationWriteErrorCount.Add(1)
		}
	}

	// 8. Notify TUI: delegation complete
	if t.onDone != nil {
		t.onDone("active", target.Name, answer)
	}

	return tools.ToolResult{
		Output: fmt.Sprintf("[%s's response]\n%s", target.Name, answer),
	}
}
