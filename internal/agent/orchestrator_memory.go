package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/logger"
)


// loadAgentSummaries loads recent session summaries for an agent. Returns nil on error (non-fatal).
func (o *Orchestrator) loadAgentSummaries(ctx context.Context, agentName string) []agents.SessionSummary {
	o.mu.Lock()
	ms := o.memoryStore
	o.mu.Unlock()
	if ms == nil {
		return nil
	}
	summaries, err := ms.LoadRecentSummaries(ctx, agentName, 3)
	if err != nil {
		logger.Warn("load summaries failed", "agent", agentName, "err", err)
		return nil
	}
	return summaries
}

// SessionClose summarizes all agents with history and persists summaries to the MemoryStore.
// Called on shutdown to capture cross-session context.
func (o *Orchestrator) SessionClose(ctx context.Context) error {
	o.mu.Lock()
	ms := o.memoryStore
	reg := o.agentReg
	sessionID := o.defaultSessionID
	machineID := o.machineID
	sessionCloseBackend := o.backend
	spaceID := o.spaceID
	spaceName := o.spaceName
	o.mu.Unlock()

	if ms == nil || reg == nil || sessionCloseBackend == nil {
		return nil
	}

	allAgents := reg.All()
	for _, ag := range allAgents {
		if ag.HistoryLen() == 0 {
			continue
		}
		summary, err := o.summarizeAgent(ctx, ag, sessionID, machineID, spaceID, spaceName)
		if err != nil {
			logger.Warn("session close: summarize agent failed — session context may be lost", "agent", ag.Name, "err", err)
			o.sc.Record("agent.summary_errors", 1, "agent:"+ag.Name, "error:summarize")
			continue
		}
		if saveErr := ms.SaveSummary(ctx, summary); saveErr != nil {
			logger.Warn("session close: save summary failed — session context may be lost", "agent", ag.Name, "err", saveErr)
			o.sc.Record("agent.summary_errors", 1, "agent:"+ag.Name, "error:save")
		}
		// D4: MuninnDB persistence now happens per-session via MCP connection in AgentChat.
	}
	return nil
}


// summarizeAgent asks the LLM to produce a structured summary of an agent's session history.
func (o *Orchestrator) summarizeAgent(ctx context.Context, ag *agents.Agent, sessionID, machineID, spaceID, spaceName string) (agents.SessionSummary, error) {
	history := ag.SnapshotHistory(20)
	var convBuf strings.Builder
	for _, msg := range history {
		convBuf.WriteString(msg.Role)
		convBuf.WriteString(": ")
		convBuf.WriteString(msg.Content)
		convBuf.WriteString("\n")
	}

	summaryPrompt := "You are summarizing a work session. Return JSON only, no prose:\n" +
		"{\n" +
		"  \"summary\": \"one sentence describing what was accomplished\",\n" +
		"  \"files_touched\": [\"list of files mentioned\"],\n" +
		"  \"decisions\": [\"key decisions made\"],\n" +
		"  \"open_questions\": [\"unresolved items\"]\n" +
		"}\n\n" +
		"Conversation:\n" + convBuf.String()

	sumBackend, sumErr := o.backendFor(ag)
	if sumErr != nil {
		return agents.SessionSummary{}, sumErr
	}
	var buf strings.Builder
	_, err := sumBackend.ChatCompletion(ctx, backend.ChatRequest{
		Model:    ag.GetModelID(),
		Messages: []backend.Message{{Role: "user", Content: summaryPrompt}},
		OnToken:  func(token string) { buf.WriteString(token) },
	})
	if err != nil {
		return agents.SessionSummary{}, fmt.Errorf("chat completion: %w", err)
	}

	raw := strings.TrimSpace(buf.String())
	// Strip markdown fences if present.
	if i := strings.Index(raw, "{"); i > 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 && i < len(raw)-1 {
		raw = raw[:i+1]
	}

	var parsed struct {
		Summary       string   `json:"summary"`
		FilesTouched  []string `json:"files_touched"`
		Decisions     []string `json:"decisions"`
		OpenQuestions []string `json:"open_questions"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return agents.SessionSummary{}, fmt.Errorf("parse summary JSON: %w", err)
	}

	return agents.SessionSummary{
		SessionID:     sessionID,
		MachineID:     machineID,
		AgentName:     ag.Name,
		Timestamp:     time.Now(),
		Summary:       parsed.Summary,
		FilesTouched:  parsed.FilesTouched,
		Decisions:     parsed.Decisions,
		OpenQuestions: parsed.OpenQuestions,
		SpaceID:       spaceID,
		SpaceName:     spaceName,
	}, nil
}
