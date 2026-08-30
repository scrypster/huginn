package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/tools"
)

// charStreamClockBackend streams a raw model reply one rune at a time via
// OnToken, simulating a real LLM echoing the injected "Local time now: ..."
// clock line into a hallway fast-path answer (completeTrivialAsk). The
// harness rewrites that raw label into "It's {stamp}." only once the full
// stamp has streamed in, which is exactly the mid-stream prefix rewrite that
// live token emission must reconcile with the client, not silently drop.
type charStreamClockBackend struct {
	reply string
}

func (b *charStreamClockBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		for _, r := range b.reply {
			req.OnToken(string(r))
		}
	}
	return &backend.ChatResponse{DoneReason: "stop", Content: b.reply}, nil
}

func (b *charStreamClockBackend) Health(_ context.Context) error   { return nil }
func (b *charStreamClockBackend) Shutdown(_ context.Context) error { return nil }
func (b *charStreamClockBackend) ContextWindow() int               { return 8192 }

// applyClientTokens replays the WSMessage "token" events the way ChatView.vue's
// token handler does: append msg.Content, unless payload.replace is set, in
// which case the client repaints from msg.Content instead of appending.
func applyClientTokens(msgs []WSMessage) string {
	var content string
	for _, m := range msgs {
		if m.Type != "token" {
			continue
		}
		replace := false
		if m.Payload != nil {
			if v, ok := m.Payload["replace"].(bool); ok {
				replace = v
			}
		}
		if replace {
			content = m.Content
		} else {
			content += m.Content
		}
	}
	return content
}

func TestRunWSChat_HallwayTimeAsk_LiveStreamMatchesPersistedContent(t *testing.T) {
	srv, _ := newTestServer(t)

	orch, err := agent.NewOrchestrator(
		&charStreamClockBackend{reply: "Local time now: Friday, August 28, 2026, 12:13 AM ET."},
		modelconfig.DefaultModels(),
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	srv.orch = orch
	// completeTrivialAsk (the hallway fast path this defect lives in) is only
	// reached via runAgentTurn when a tool registry is configured — without
	// one, ChatWithAgent falls through to its own plain single-turn
	// completion instead. Wire a registry so this test exercises the real
	// hallway code path.
	srv.orch.SetTools(tools.NewRegistry(), nil)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Lead", Model: "legacy-no-tools", IsDefault: true},
			},
		}, nil
	}

	sess := srv.store.New("hallway-channel", "/workspace", "legacy-no-tools")
	sess.Manifest.Agent = "Lead"
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 256), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "what time is it",
	})

	var tokens []WSMessage
	deadline := time.After(5 * time.Second)
	done := false
	for !done {
		select {
		case msg := <-client.send:
			if msg.Type == "error" {
				t.Fatalf("unexpected error from websocket: %v", msg.Content)
			}
			if msg.Type == "token" {
				tokens = append(tokens, msg)
			}
			if msg.Type == "done" {
				done = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for done websocket message")
		}
	}

	liveContent := applyClientTokens(tokens)

	// runWSChat persists the assistant row before emitting "done", but the
	// row still travels through the store asynchronously in places, so poll
	// briefly rather than assuming persistence is visible the instant "done"
	// arrives.
	var persisted string
	persistDeadline := time.Now().Add(2 * time.Second)
	for persisted == "" && time.Now().Before(persistDeadline) {
		msgs, err := srv.store.TailMessages(sess.ID, 10)
		if err != nil {
			t.Fatalf("TailMessages: %v", err)
		}
		for _, m := range msgs {
			if m.Role == "assistant" {
				persisted = m.Content
			}
		}
		if persisted == "" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if persisted == "" {
		t.Fatalf("no assistant message persisted")
	}

	if !strings.HasPrefix(liveContent, "It's Friday") {
		t.Errorf("live-streamed bubble text lost its leading prefix: got %q (persisted=%q)", liveContent, persisted)
	}
	if liveContent != persisted {
		t.Errorf("live-streamed bubble text %q does not match persisted content %q", liveContent, persisted)
	}
}

// ---------------------------------------------------------------------------
// visibleTokenGate
// ---------------------------------------------------------------------------

func TestVisibleTokenGate_PlainAppendGrowth(t *testing.T) {
	var g visibleTokenGate
	delta, replace, emit := g.next("It")
	if !emit || replace || delta != "It" {
		t.Fatalf("first token: got delta=%q replace=%v emit=%v", delta, replace, emit)
	}
	delta, replace, emit = g.next("It's")
	if !emit || replace || delta != "'s" {
		t.Fatalf("growth: got delta=%q replace=%v emit=%v", delta, replace, emit)
	}
	// No change: nothing to emit.
	_, _, emit = g.next("It's")
	if emit {
		t.Fatalf("unchanged visible content should not emit")
	}
}

func TestVisibleTokenGate_DivergenceEmitsReplace(t *testing.T) {
	var g visibleTokenGate
	g.next("Friday, August 28")
	delta, replace, emit := g.next("It's Friday, August 28, 2026, 12:13 AM ET.")
	if !emit || !replace {
		t.Fatalf("divergent rewrite should emit a replace, got delta=%q replace=%v emit=%v", delta, replace, emit)
	}
	if delta != "It's Friday, August 28, 2026, 12:13 AM ET." {
		t.Fatalf("replace delta should be the full corrected visible content, got %q", delta)
	}
}

func TestVisibleTokenGate_DivergenceToEmptyClearsBubble(t *testing.T) {
	var g visibleTokenGate
	g.next("Pong")
	delta, replace, emit := g.next("")
	if !emit || !replace || delta != "" {
		t.Fatalf("clearing to empty should emit a replace with empty content, got delta=%q replace=%v emit=%v", delta, replace, emit)
	}
}

// TestVisibleTokenGate_ReconstructsClockRewrite is the direct regression for
// the reported defect: a char-by-char stream of the raw harness clock label
// rewrites mid-stream into "It's {stamp}.", and replaying the gate's emitted
// tokens client-side (append, or repaint on replace) must always land on
// exactly the final visible content — never a truncated middle fragment.
func TestVisibleTokenGate_ReconstructsClockRewrite(t *testing.T) {
	raw := "Local time now: Friday, August 28, 2026, 12:13 AM ET."
	var g visibleTokenGate
	var client string
	var buf strings.Builder
	for _, r := range raw {
		buf.WriteRune(r)
		visible := backend.VisibleAssistantContent(buf.String())
		delta, replace, emit := g.next(visible)
		if !emit {
			continue
		}
		if replace {
			client = delta
		} else {
			client += delta
		}
	}
	want := backend.VisibleAssistantContent(raw)
	if client != want {
		t.Fatalf("client reconstruction = %q, want %q", client, want)
	}
	if !strings.HasPrefix(client, "It's Friday") {
		t.Fatalf("reconstructed content lost its leading prefix: %q", client)
	}
}
