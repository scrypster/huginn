package threadmgr

import (
	"fmt"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/session"
)

func makeTestStore(t *testing.T) *session.Store {
	t.Helper()
	dir := t.TempDir()
	return session.NewStore(dir)
}

func TestEstimateTokens_FourCharsPerToken(t *testing.T) {
	got := estimateTokens("hello world") // 11 chars → ceil(11/4) = 3
	if got < 2 || got > 4 {
		t.Errorf("estimateTokens('hello world') = %d, want ~3", got)
	}
}

func TestBuildContext_PersonaAlwaysPresent(t *testing.T) {
	tm := New()
	store := makeTestStore(t)

	ag := &agents.Agent{
		Name:         "Coder",
		SystemPrompt: "You are Coder, expert in Go.",
		ModelID:      "claude-sonnet-4",
	}
	reg := agents.NewRegistry()
	reg.Register(ag)

	thread, _ := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "Coder",
		Task:      "fix the bug",
	})

	budget := ContextBudget{Total: 100, Persona: 20, Artifacts: 40, Snapshot: 40}
	msgs := buildContextWithBudget(thread, store, tm, reg, budget)

	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	if msgs[0].Role != "system" {
		t.Errorf("first message should be system, got %q", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "Coder") {
		t.Errorf("persona should be in system prompt, got: %q", msgs[0].Content)
	}
}

func TestBuildContext_UpstreamArtifacts(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4"})

	// Create upstream thread marked done with a summary
	upstream, _ := tm.Create(CreateParams{SessionID: "sess-1", AgentID: "Worker", Task: "prep work"})
	tm.Complete(upstream.ID, FinishSummary{
		Summary:       "prepared the fixtures",
		FilesModified: []string{"fixtures.go"},
		Status:        "completed",
	})

	// Create downstream thread that depends on upstream
	downstream, _ := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "Worker",
		Task:      "use the fixtures",
		DependsOn: []string{upstream.ID},
	})

	budget := ContextBudget{Total: 4096, Persona: 512, Artifacts: 1024, Snapshot: 2560}
	msgs := buildContextWithBudget(downstream, store, tm, reg, budget)

	// Find an artifact message
	var found bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "prepared the fixtures") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected upstream FinishSummary to appear in context messages")
	}
}

func TestBuildContext_RespectsSnapshotBudget(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Bot", ModelID: "claude-haiku-4"})

	sess := store.New("test", "/tmp", "claude-haiku-4")
	// Append many large messages to session store
	for i := 0; i < 20; i++ {
		_ = store.Append(sess, session.SessionMessage{
			Role:    "user",
			Content: fmt.Sprintf("message %d: %s", i, strings.Repeat("x", 400)),
		})
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "do stuff"})

	// Very tight snapshot budget (32 tokens ≈ 128 chars)
	budget := ContextBudget{Total: 64, Persona: 16, Artifacts: 16, Snapshot: 32}
	msgs := buildContextWithBudget(thread, store, tm, reg, budget)

	// Count non-system messages (snapshot messages)
	var snapshotMsgs int
	for _, m := range msgs {
		if m.Role != "system" {
			snapshotMsgs++
		}
	}
	// With a tight budget, we should get very few or zero snapshot messages
	// (certainly not all 20)
	if snapshotMsgs >= 20 {
		t.Errorf("expected snapshot trimming, got %d messages (all 20 included)", snapshotMsgs)
	}
}
