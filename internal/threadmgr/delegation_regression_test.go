package threadmgr

// Regression tests for the @mention delegation pipeline.
//
// These tests cover three bugs found during integration testing:
//
// 1. buildContext produced a history ending with an assistant message.
//    The Anthropic API rejects such requests with "This model does not
//    support assistant message prefill." Fix: append the task as a final
//    user message when the snapshot ends with an assistant turn.
//
// 2. SpawnThread used the raw fallback backend (Ollama) instead of the
//    agent's configured provider backend (e.g. Anthropic). Fix: resolve
//    the correct backend via tm.backendFor inside the goroutine.
//
// 3. ParseMentions must correctly match @AgentName tokens and produce
//    DelegationRequest values with canonicalised names and the full
//    original message as the Task.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// ---------------------------------------------------------------------------
// 1. Context builder: history must end with a user message
// ---------------------------------------------------------------------------

func TestBuildContext_EndsWithUserMessage_WhenSnapshotEndsWithAssistant(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Sam", ModelID: "claude-sonnet-4"})

	sess := store.New("test", "/tmp", "claude-sonnet-4")

	// Simulate the real delegation scenario: user message followed by
	// Tom's assistant @mention. The snapshot will end with the assistant.
	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "Tom, ask Sam what 2+2 is.",
	})
	_ = store.Append(sess, session.SessionMessage{
		Role:    "assistant",
		Content: "@Sam, what is 2+2? Please reply in one word only.",
	})

	thread, err := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Sam",
		Task:      "@Sam, what is 2+2? Please reply in one word only.",
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := buildContext(thread, store, tm, reg)
	if len(msgs) == 0 {
		t.Fatal("expected at least one message from buildContext")
	}

	last := msgs[len(msgs)-1]
	if last.Role != "user" {
		t.Errorf("last message role = %q, want \"user\" (Anthropic rejects assistant-ending history)", last.Role)
	}
	if !strings.Contains(last.Content, "2+2") {
		t.Errorf("last user message should contain the task, got: %q", last.Content)
	}
}

func TestBuildContext_NoExtraUserMessage_WhenSnapshotEndsWithUser(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Bot", ModelID: "claude-haiku-4"})

	sess := store.New("test", "/tmp", "claude-haiku-4")

	// Session ends with a user message — no fixup needed.
	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "hello bot, do the thing",
	})

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Bot",
		Task:      "do the thing",
	})

	msgs := buildContext(thread, store, tm, reg)
	// Count how many times the task appears as a standalone user message.
	// Should be at most once (the snapshot user msg), not duplicated.
	taskCount := 0
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "do the thing") {
			taskCount++
		}
	}
	if taskCount > 1 {
		t.Errorf("task appeared %d times as user message; expected at most 1 (no duplication)", taskCount)
	}
	// Last message should still be user.
	last := msgs[len(msgs)-1]
	if last.Role != "user" {
		t.Errorf("last message role = %q, want \"user\"", last.Role)
	}
}

func TestBuildContext_SystemOnlyProducesUserFallback(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Bot", ModelID: "claude-haiku-4"})

	// Empty session — no snapshot messages. Only the system persona.
	sess := store.New("test", "/tmp", "claude-haiku-4")

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Bot",
		Task:      "explain recursion",
	})

	msgs := buildContext(thread, store, tm, reg)
	if len(msgs) == 0 {
		t.Fatal("expected at least system message")
	}
	// The system message is role="system", so the last message is system.
	// Our fix should append a user message with the task.
	last := msgs[len(msgs)-1]
	if last.Role != "user" {
		t.Errorf("last message role = %q, want \"user\" (system-only context should get task appended)", last.Role)
	}
}

// ---------------------------------------------------------------------------
// 2. SpawnThread: backend resolution via backendFor
// ---------------------------------------------------------------------------

// trackingBackend wraps a fakeBackend and records which instance was called.
type trackingBackend struct {
	fakeBackend
	name  string
	mu2   sync.Mutex
	used  bool
}

func (tb *trackingBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	tb.mu2.Lock()
	tb.used = true
	tb.mu2.Unlock()
	return tb.fakeBackend.ChatCompletion(ctx, req)
}

func (tb *trackingBackend) wasUsed() bool {
	tb.mu2.Lock()
	defer tb.mu2.Unlock()
	return tb.used
}

func TestSpawnThread_UsesResolvedBackend_NotFallback(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-sonnet-4")

	// Ensure there's a user message in the session so context ends with user.
	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "what is 2+2?",
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:     "Sam",
		ModelID:  "claude-sonnet-4",
		Provider: "anthropic",
	})

	// The fallback backend should NOT be used.
	fallbackBackend := &trackingBackend{
		name: "fallback-ollama",
		fakeBackend: fakeBackend{
			response: &backend.ChatResponse{
				Content:    "wrong backend",
				DoneReason: "stop",
			},
		},
	}

	// The resolved backend SHOULD be used.
	resolvedBackend := &trackingBackend{
		name: "resolved-anthropic",
		fakeBackend: fakeBackend{
			response: &backend.ChatResponse{
				Content:    "Four.",
				DoneReason: "stop",
			},
		},
	}

	// Wire the backend resolver.
	tm.SetBackendResolver(func(provider, endpoint, apiKey, model string) (backend.Backend, error) {
		if provider == "anthropic" {
			return resolvedBackend, nil
		}
		return nil, fmt.Errorf("unknown provider: %s", provider)
	})

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Sam",
		Task:      "what is 2+2?",
	})

	var bmu sync.Mutex
	var broadcasts []capturedBroadcast
	broadcastFn := func(sid, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, capturedBroadcast{sid, msgType, payload})
		bmu.Unlock()
	}

	ca := NewCostAccumulator(0)
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fallbackBackend, broadcastFn, ca, nil)

	// Wait for the goroutine to complete.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if fallbackBackend.wasUsed() {
		t.Error("fallback backend was called — SpawnThread should use the resolved backend for agents with a provider")
	}
	if !resolvedBackend.wasUsed() {
		t.Error("resolved backend was NOT called — SpawnThread should resolve the agent's provider backend")
	}

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found after spawn")
	}
	if got.Status != StatusDone {
		t.Errorf("thread status = %s, want StatusDone", got.Status)
	}
}

func TestSpawnThread_FallsBackToRawBackend_WhenNoResolver(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "hello",
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:     "Bot",
		ModelID:  "claude-haiku-4",
		Provider: "ollama",
	})

	// No resolver set — should fall back to the raw backend.
	rawBackend := &trackingBackend{
		name: "raw",
		fakeBackend: fakeBackend{
			response: &backend.ChatResponse{
				Content:    "hi",
				DoneReason: "stop",
			},
		},
	}

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Bot",
		Task:      "say hi",
	})

	broadcastFn := func(string, string, map[string]any) {}
	ca := NewCostAccumulator(0)
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, rawBackend, broadcastFn, ca, nil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !rawBackend.wasUsed() {
		t.Error("raw backend should be used when no resolver is set")
	}
}

func TestSpawnThread_FallsBackToRawBackend_WhenResolverFails(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "test",
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:     "Bot",
		ModelID:  "claude-haiku-4",
		Provider: "broken-provider",
	})

	// Resolver always errors.
	tm.SetBackendResolver(func(provider, endpoint, apiKey, model string) (backend.Backend, error) {
		return nil, fmt.Errorf("resolver error: unknown provider %s", provider)
	})

	rawBackend := &trackingBackend{
		name: "raw-fallback",
		fakeBackend: fakeBackend{
			response: &backend.ChatResponse{
				Content:    "fallback response",
				DoneReason: "stop",
			},
		},
	}

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Bot",
		Task:      "test task",
	})

	broadcastFn := func(string, string, map[string]any) {}
	ca := NewCostAccumulator(0)
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, rawBackend, broadcastFn, ca, nil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !rawBackend.wasUsed() {
		t.Error("raw backend should be used as fallback when resolver fails")
	}
}

// ---------------------------------------------------------------------------
// 3. ParseMentions: correct @agent detection and deduplication
// ---------------------------------------------------------------------------

func TestParseMentions_BasicMatch(t *testing.T) {
	names := []string{"Sam", "Tom", "Adam"}
	reqs := ParseMentions("@Sam, what is 2+2?", names)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].AgentName != "Sam" {
		t.Errorf("agent = %q, want \"Sam\"", reqs[0].AgentName)
	}
	if reqs[0].Task != "@Sam, what is 2+2?" {
		t.Errorf("task = %q, want full original message", reqs[0].Task)
	}
}

func TestParseMentions_CaseInsensitive(t *testing.T) {
	names := []string{"Sam"}
	reqs := ParseMentions("@sam please help", names)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].AgentName != "Sam" {
		t.Errorf("agent = %q, want canonical \"Sam\"", reqs[0].AgentName)
	}
}

func TestParseMentions_DeduplicatesSameAgent(t *testing.T) {
	names := []string{"Sam"}
	reqs := ParseMentions("@Sam do this and @Sam do that", names)
	if len(reqs) != 1 {
		t.Errorf("expected 1 deduplicated request, got %d", len(reqs))
	}
}

func TestParseMentions_MultipleDifferentAgents(t *testing.T) {
	names := []string{"Sam", "Adam", "Tom"}
	reqs := ParseMentions("@Sam and @Adam, work together on this", names)
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}
	agentSet := map[string]bool{}
	for _, r := range reqs {
		agentSet[r.AgentName] = true
	}
	if !agentSet["Sam"] || !agentSet["Adam"] {
		t.Errorf("expected Sam and Adam, got: %v", agentSet)
	}
}

func TestParseMentions_IgnoresUnknownAgents(t *testing.T) {
	names := []string{"Sam"}
	reqs := ParseMentions("@Unknown do something", names)
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests for unknown agent, got %d", len(reqs))
	}
}

func TestParseMentions_EmptyInput(t *testing.T) {
	reqs := ParseMentions("", []string{"Sam"})
	if len(reqs) != 0 {
		t.Errorf("expected 0 for empty msg, got %d", len(reqs))
	}

	reqs = ParseMentions("@Sam hello", nil)
	if len(reqs) != 0 {
		t.Errorf("expected 0 for nil agent names, got %d", len(reqs))
	}
}

func TestParseMentions_MidSentenceMention(t *testing.T) {
	names := []string{"Sam"}
	reqs := ParseMentions("Hey @Sam, what do you think?", names)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request for mid-sentence @mention, got %d", len(reqs))
	}
}

func TestParseMentions_TruncatesAtMaxMentions(t *testing.T) {
	// Create more than maxMentionsPerMessage unique agent names.
	names := make([]string, 15)
	mentions := make([]string, 15)
	for i := 0; i < 15; i++ {
		names[i] = fmt.Sprintf("Agent%d", i)
		mentions[i] = fmt.Sprintf("@Agent%d", i)
	}
	msg := strings.Join(mentions, " ")
	reqs := ParseMentions(msg, names)
	if len(reqs) > maxMentionsPerMessage {
		t.Errorf("expected at most %d requests, got %d", maxMentionsPerMessage, len(reqs))
	}
}

func TestParseMentions_NoFalsePositiveOnEmail(t *testing.T) {
	names := []string{"Bob"}
	// alice@Bob should NOT match — @Bob must be preceded by whitespace or start-of-string.
	reqs := ParseMentions("email alice@Bob about this", names)
	if len(reqs) != 0 {
		t.Errorf("expected 0 (email false positive avoided), got %d", len(reqs))
	}
}

func TestParseMentions_WordBoundaryPreventsPartialMatch(t *testing.T) {
	names := []string{"Sam"}
	// @SamuelJackson should NOT match agent "Sam" due to word boundary.
	reqs := ParseMentions("@SamuelJackson did something", names)
	// The regex captures "SamuelJackson" which won't match "Sam" in canonical lookup.
	if len(reqs) != 0 {
		t.Errorf("expected 0 (partial name should not match), got %d", len(reqs))
	}
}

// ---------------------------------------------------------------------------
// Self-Delegation Guard in CreateFromMentions
// ---------------------------------------------------------------------------

// TestCreateFromMentions_SkipsSelfDelegation verifies that when CreateFromMentions
// is called with callerAgent="Tom", it skips creating threads for @Tom mentions
// (self-delegation guard). It should still delegate to other agents like @Sam.
func TestCreateFromMentions_SkipsSelfDelegation(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	// Append a user message that will be the trigger for CreateFromMentions.
	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "@Tom and @Sam please help",
	})

	// Register Tom and Sam as agents.
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Tom", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Sam", ModelID: "claude-haiku-4"})

	// Create fake backend and broadcast function.
	b := &fakeBackend{
		response: &backend.ChatResponse{Content: "Done", DoneReason: "stop"},
	}
	broadcastFn := func(string, string, map[string]any) {}
	ca := NewCostAccumulator(0)

	// Call CreateFromMentions with callerAgent="Tom".
	// It should skip @Tom and only create a thread for @Sam.
	CreateFromMentions(context.Background(), sess.ID, "@Tom and @Sam please help",
		"", reg, store, sess, b, broadcastFn, ca, tm, "Tom")

	// Give time for goroutines to complete.
	deadline := time.Now().Add(2 * time.Second)
	var tomThread, samThread *Thread
	for time.Now().Before(deadline) {
		for _, thr := range tm.ListBySession(sess.ID) {
			if thr.AgentID == "Tom" {
				tomThread = thr
			}
			if thr.AgentID == "Sam" {
				samThread = thr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Tom thread should NOT exist (self-delegation was skipped).
	if tomThread != nil {
		t.Error("Tom thread should not have been created (self-delegation guard should skip it)")
	}

	// Sam thread SHOULD exist.
	if samThread == nil {
		t.Error("Sam thread should have been created (not a self-delegation)")
	}
}

// TestCreateFromMentions_NoCallerAgent_DelegatesAll verifies that when
// callerAgent is empty, CreateFromMentions creates threads for all mentions.
func TestCreateFromMentions_NoCallerAgent_DelegatesAll(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "@Tom and @Sam please help",
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Tom", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Sam", ModelID: "claude-haiku-4"})

	b := &fakeBackend{
		response: &backend.ChatResponse{Content: "Done", DoneReason: "stop"},
	}
	broadcastFn := func(string, string, map[string]any) {}
	ca := NewCostAccumulator(0)

	// Call CreateFromMentions with callerAgent="" (empty).
	// Both @Tom and @Sam should get delegated to.
	CreateFromMentions(context.Background(), sess.ID, "@Tom and @Sam please help",
		"", reg, store, sess, b, broadcastFn, ca, tm, "")

	deadline := time.Now().Add(2 * time.Second)
	var tomThread, samThread *Thread
	for time.Now().Before(deadline) {
		for _, thr := range tm.ListBySession(sess.ID) {
			if thr.AgentID == "Tom" {
				tomThread = thr
			}
			if thr.AgentID == "Sam" {
				samThread = thr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Both threads should exist when callerAgent is empty.
	if tomThread == nil {
		t.Error("Tom thread should have been created when callerAgent is empty")
	}
	if samThread == nil {
		t.Error("Sam thread should have been created when callerAgent is empty")
	}
}

// TestCreateFromMentions_MarkdownWrappedMentionsSpawnTwoThreads is the E2E
// regression for issue #3 — orchestrator delegation died.
//
// The user's scenario: lead agent "Max" emits a markdown-formatted reply like
// "Delegating to **@Stacy** and **@Bob**, please collaborate." The mentions
// must be parsed AND each must spawn a thread that fires a `thread_started`
// broadcast so the UI shows reply badges.
//
// This test wires CreateFromMentions exactly the way main.go does and asserts
// two threads are created (one per mention) and the broadcast pipeline emits
// thread_started for each.
func TestCreateFromMentions_MarkdownWrappedMentionsSpawnTwoThreads(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("e2e-markdown", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Max", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Stacy", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Bob", ModelID: "claude-haiku-4"})

	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "Max, please coordinate the team to investigate the auth bug.",
	})

	b := &fakeBackend{response: &backend.ChatResponse{
		Content:    "Done.",
		DoneReason: "stop",
	}}

	var bmu sync.Mutex
	var broadcasts []capturedBroadcast
	broadcastFn := func(sid, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, capturedBroadcast{sid, msgType, payload})
		bmu.Unlock()
	}

	maxReply := "I'll coordinate. Delegating to **@Stacy** for triage and **@Bob** for the fix."

	CreateFromMentions(
		context.Background(),
		sess.ID,
		maxReply,
		"parent-msg-1",
		reg,
		store,
		sess,
		b,
		broadcastFn,
		NewCostAccumulator(0),
		tm,
		"Max",
	)

	deadline := time.Now().Add(2 * time.Second)
	var stacy, bob *Thread
	for time.Now().Before(deadline) {
		for _, thr := range tm.ListBySession(sess.ID) {
			switch thr.AgentID {
			case "Stacy":
				stacy = thr
			case "Bob":
				bob = thr
			}
		}
		if stacy != nil && bob != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if stacy == nil {
		t.Error("expected a thread for @Stacy from Max's markdown-wrapped mention")
	}
	if bob == nil {
		t.Error("expected a thread for @Bob from Max's markdown-wrapped mention")
	}

	// Wait until each agent's thread broadcasts thread_started before counting.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bmu.Lock()
		startedAgents := map[string]bool{}
		for _, ev := range broadcasts {
			if ev.msgType != "thread_started" {
				continue
			}
			if name, ok := ev.payload["agent_id"].(string); ok {
				startedAgents[name] = true
			}
		}
		bmu.Unlock()
		if startedAgents["Stacy"] && startedAgents["Bob"] {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	bmu.Lock()
	startedCount := 0
	startedAgents := map[string]bool{}
	for _, ev := range broadcasts {
		if ev.msgType == "thread_started" {
			startedCount++
			if name, ok := ev.payload["agent_id"].(string); ok {
				startedAgents[name] = true
			}
		}
	}
	bmu.Unlock()
	if startedCount < 2 {
		t.Errorf("expected at least 2 thread_started broadcasts, got %d", startedCount)
	}
	if !startedAgents["Stacy"] || !startedAgents["Bob"] {
		t.Errorf("thread_started should fire for both Stacy and Bob, got %v", startedAgents)
	}

	// Wait for both spawned goroutines to reach a terminal state. Without
	// this, t.TempDir cleanup races with the spawned threads' jsonl writes
	// and emits "directory not empty" errors at test shutdown.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		threads := tm.ListBySession(sess.ID)
		if len(threads) < 2 {
			allDone = false
		} else {
			for _, thr := range threads {
				if thr.Status != StatusDone && thr.Status != StatusError {
					allDone = false
					break
				}
			}
		}
		if allDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCreateFromMentions_UnknownAgentEmitsDelegationWarning verifies the
// diagnostic broadcast that surfaces hallucinated agent names to the UI when
// the orchestrator mentions an agent that doesn't exist in the registry.
func TestCreateFromMentions_UnknownAgentEmitsDelegationWarning(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("e2e-unknown", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Stacy", ModelID: "claude-haiku-4"})

	b := &fakeBackend{response: &backend.ChatResponse{
		Content: "ok", DoneReason: "stop",
	}}
	var bmu sync.Mutex
	var broadcasts []capturedBroadcast
	broadcastFn := func(sid, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, capturedBroadcast{sid, msgType, payload})
		bmu.Unlock()
	}

	CreateFromMentions(
		context.Background(),
		sess.ID,
		"Delegating to @Stacy and @GhostAgent who doesn't exist.",
		"",
		reg,
		store,
		sess,
		b,
		broadcastFn,
		NewCostAccumulator(0),
		tm,
		"Max",
	)

	// Wait for the (real) Stacy thread to finish so the goroutine isn't still
	// writing to TempDir during test cleanup.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		for _, thr := range tm.ListBySession(sess.ID) {
			if thr.Status != StatusDone && thr.Status != StatusError {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	bmu.Lock()
	defer bmu.Unlock()
	var sawWarning bool
	for _, ev := range broadcasts {
		if ev.msgType != "delegation_warning" {
			continue
		}
		unknown, ok := ev.payload["unknown"].([]string)
		if !ok {
			continue
		}
		for _, name := range unknown {
			if strings.EqualFold(name, "GhostAgent") {
				sawWarning = true
			}
		}
	}
	if !sawWarning {
		t.Errorf("expected a delegation_warning broadcast naming GhostAgent, got events: %v", broadcasts)
	}
}

// TestCreateFromMentions_CaseInsensitiveSelfCheck verifies that the self-delegation
// guard is case-insensitive (e.g., callerAgent="tom" matches @Tom).
func TestCreateFromMentions_CaseInsensitiveSelfCheck(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "@tom please help", // lowercase @tom
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Tom", ModelID: "claude-haiku-4"}) // canonical: "Tom"

	b := &fakeBackend{
		response: &backend.ChatResponse{Content: "Done", DoneReason: "stop"},
	}
	broadcastFn := func(string, string, map[string]any) {}
	ca := NewCostAccumulator(0)

	// Call CreateFromMentions with callerAgent="tom" (lowercase).
	// Even though @tom is mentioned, it should still be skipped because
	// it's a case-insensitive match with the caller.
	CreateFromMentions(context.Background(), sess.ID, "@tom please help",
		"", reg, store, sess, b, broadcastFn, ca, tm, "tom")

	deadline := time.Now().Add(2 * time.Second)
	var tomThread *Thread
	for time.Now().Before(deadline) {
		for _, thr := range tm.ListBySession(sess.ID) {
			if thr.AgentID == "Tom" {
				tomThread = thr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Tom thread should NOT exist (case-insensitive self-delegation guard).
	if tomThread != nil {
		t.Error("Tom thread should not have been created (case-insensitive self-delegation guard should skip it)")
	}
}
