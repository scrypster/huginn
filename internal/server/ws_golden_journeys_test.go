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
)

type mentionGoldenBackend struct {
	reply string
}

func (b *mentionGoldenBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken(b.reply)
	}
	return &backend.ChatResponse{DoneReason: "stop", Content: b.reply}, nil
}

func (b *mentionGoldenBackend) Health(_ context.Context) error   { return nil }
func (b *mentionGoldenBackend) Shutdown(_ context.Context) error { return nil }
func (b *mentionGoldenBackend) ContextWindow() int               { return 8192 }

func TestHandleWSMessage_ChannelLeadDelegation_GoldenPath(t *testing.T) {
	srv, _ := newTestServer(t)

	orch, err := agent.NewOrchestrator(
		&mentionGoldenBackend{reply: "I'll ask @GitAgent to investigate this and report back."},
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
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Lead", Model: "claude-haiku-4", IsDefault: true},
				{Name: "GitAgent", Model: "claude-haiku-4"},
			},
		}, nil
	}

	sess := srv.store.New("golden-channel", "/workspace", "claude-haiku-4")
	sess.Manifest.Agent = "Lead"
	sess.Manifest.SpaceID = "space-golden"
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	type delegateCall struct {
		sessionID   string
		assistant   string
		parentMsgID string
	}
	callCh := make(chan delegateCall, 1)
	srv.SetMentionDelegate(func(_ context.Context, sessionID, assistantMsg, _ /* originalUserMsg */, parentMsgID string) {
		callCh <- delegateCall{
			sessionID:   sessionID,
			assistant:   assistantMsg,
			parentMsgID: parentMsgID,
		}
	})

	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "please coordinate this",
	})

	var doneMessageID string
	deadline := time.After(5 * time.Second)
	for doneMessageID == "" {
		select {
		case msg := <-client.send:
			if msg.Type == "error" {
				t.Fatalf("unexpected error from websocket: %v", msg.Content)
			}
			if msg.Type == "done" && msg.Payload != nil {
				doneMessageID, _ = msg.Payload["message_id"].(string)
			}
		case <-deadline:
			t.Fatal("timed out waiting for done websocket message")
		}
	}

	select {
	case got := <-callCh:
		if got.sessionID != sess.ID {
			t.Fatalf("delegation session_id = %q, want %q", got.sessionID, sess.ID)
		}
		if !strings.Contains(got.assistant, "@GitAgent") {
			t.Fatalf("delegation body %q does not include assistant @mention", got.assistant)
		}
		if got.parentMsgID == "" {
			t.Fatal("expected non-empty parent message ID for mention delegation")
		}
		if doneMessageID == "" {
			t.Fatal("expected done message payload to include message_id")
		}
		if got.parentMsgID != doneMessageID {
			t.Fatalf("delegation parent message_id = %q, want done payload message_id %q", got.parentMsgID, doneMessageID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for mention delegate callback")
	}

	// Persisted history must include both user and assistant messages.
	msgs, err := srv.store.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 persisted messages, got %d", len(msgs))
	}
	if got := msgs[0].Role; got != "user" {
		t.Fatalf("first message role = %q, want user", got)
	}
	if got := msgs[1].Role; got != "assistant" {
		t.Fatalf("second message role = %q, want assistant", got)
	}
	if !strings.Contains(msgs[1].Content, "@GitAgent") {
		t.Fatalf("assistant persisted content %q missing @GitAgent", msgs[1].Content)
	}
}
