package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/spaces"
)

const liveWinston1200 = "Local time now: Thursday, August 27, 2026, 12:00 PM ET"
const wantWinston1200 = "It's Thursday, August 27, 2026, 12:00 PM ET."

type fixedContentBackend struct {
	content string
}

func (b *fixedContentBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken(b.content)
	}
	return &backend.ChatResponse{Content: b.content, DoneReason: "stop"}, nil
}
func (b *fixedContentBackend) Health(_ context.Context) error   { return nil }
func (b *fixedContentBackend) Shutdown(_ context.Context) error { return nil }
func (b *fixedContentBackend) ContextWindow() int               { return 4096 }

func winstonLoader() (*agents.AgentsConfig, error) {
	return &agents.AgentsConfig{Agents: []agents.AgentDef{{
		Name:      "Winston",
		Model:     "test-model",
		IsDefault: true,
	}}}, nil
}

func hallwayThinkingServer(t *testing.T, content string) (*Server, *spaces.Space, *spaces.Space) {
	t.Helper()
	srv, _ := newTestServer(t)
	b := &fixedContentBackend{content: content}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(b, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	srv.orch = orch
	srv.agentLoader = winstonLoader

	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatal(err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)
	ch, err := store.CreateChannel("Huginn", "Winston", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	dm, err := store.OpenDM("Winston")
	if err != nil {
		t.Fatal(err)
	}
	return srv, ch, dm
}

func bindSpaceSession(t *testing.T, srv *Server, spaceID, agent string) string {
	t.Helper()
	sess := srv.store.New("hallway-"+agent, "/workspace", "test-model")
	sess.SetSpaceID(spaceID)
	sess.SetPrimaryAgent(agent)
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

func lastAssistant(t *testing.T, srv *Server, sessionID string) string {
	t.Helper()
	msgs, err := srv.store.TailMessages(sessionID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var assistant string
	for _, m := range msgs {
		if m.Role == "assistant" {
			assistant = m.Content
		}
	}
	return assistant
}

func TestEmitAgentThinking_HallwaySendRelayAndLocalWS(t *testing.T) {
	srv, ch, _ := hallwayThinkingServer(t, "ok")
	hub := attachRecordingRelay(srv)
	var local []WSMessage
	srv.onSpaceWS = func(msg WSMessage) { local = append(local, msg) }

	srv.emitAgentThinking(ch.ID, "sess-huginn", "Winston", true)

	if len(local) != 1 || local[0].Type != "space_reply_typing" {
		t.Fatalf("local WS: %+v", local)
	}
	if local[0].Payload["agent"] != "Winston" {
		t.Fatalf("agent=%v", local[0].Payload["agent"])
	}
	if local[0].Payload["space_id"] != ch.ID {
		t.Fatalf("space_id=%v", local[0].Payload["space_id"])
	}
	if _, ok := local[0].Payload["parent_id"]; ok {
		t.Fatalf("hallway thinking must omit parent_id: %+v", local[0].Payload)
	}

	got := hub.snapshot()
	if len(got) != 1 || string(got[0].Type) != "space_reply_typing" {
		t.Fatalf("relay: %+v", got)
	}
	if got[0].SpaceID != ch.ID {
		t.Fatalf("relay SpaceID=%q", got[0].SpaceID)
	}
	if got[0].Payload["agent"] != "Winston" {
		t.Fatalf("relay agent=%v", got[0].Payload["agent"])
	}
}

func TestEmitAgentThinking_DeskDMNoSpaceIDStillWires(t *testing.T) {
	srv, _, _ := hallwayThinkingServer(t, "ok")
	hub := attachRecordingRelay(srv)
	var local []WSMessage
	srv.onSpaceWS = func(msg WSMessage) { local = append(local, msg) }

	srv.emitAgentThinking("", "sess-winston-dm", "Winston", true)

	if len(local) != 1 || local[0].Type != "space_reply_typing" {
		t.Fatalf("local WS: %+v", local)
	}
	if local[0].Payload["agent"] != "Winston" || local[0].Payload["session_id"] != "sess-winston-dm" {
		t.Fatalf("payload: %+v", local[0].Payload)
	}
	got := hub.snapshot()
	if len(got) != 1 || string(got[0].Type) != "space_reply_typing" {
		t.Fatalf("relay: %+v", got)
	}
	if got[0].SessionID != "sess-winston-dm" {
		t.Fatalf("relay session=%q", got[0].SessionID)
	}
}

func collectThinking(t *testing.T, srv *Server, run func()) []WSMessage {
	t.Helper()
	var mu sync.Mutex
	var local []WSMessage
	srv.onSpaceWS = func(msg WSMessage) {
		if msg.Type == "space_reply_typing" || msg.Type == "space_reply_typing_done" {
			mu.Lock()
			local = append(local, msg)
			mu.Unlock()
		}
	}
	run()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(local)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]WSMessage, len(local))
	copy(out, local)
	return out
}

func TestWSChat_HallwayNamedAgentEmitsThinking(t *testing.T) {
	srv, ch, _ := hallwayThinkingServer(t, "ok")
	sid := bindSpaceSession(t, srv, ch.ID, "Winston")
	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}

	events := collectThinking(t, srv, func() {
		srv.handleWSMessage(client, WSMessage{
			Type:      "chat",
			SessionID: sid,
			Content:   "@Winston what time is it",
			RunID:     "run-pulse",
		})
		drainToCompletion(t, client)
	})

	if len(events) < 2 {
		t.Fatalf("want typing + typing_done, got %+v", events)
	}
	if events[0].Type != "space_reply_typing" || events[0].Payload["agent"] != "Winston" {
		t.Fatalf("first event: %+v", events[0])
	}
	if events[0].Payload["space_id"] != ch.ID {
		t.Fatalf("hallway space_id=%v want %s", events[0].Payload["space_id"], ch.ID)
	}
	done := events[len(events)-1]
	if done.Type != "space_reply_typing_done" || done.Payload["agent"] != "Winston" {
		t.Fatalf("last event: %+v", done)
	}
}

func TestWSChat_DeskDMNamedAgentEmitsThinking(t *testing.T) {
	srv, _, dm := hallwayThinkingServer(t, "ok")
	sid := bindSpaceSession(t, srv, dm.ID, "Winston")
	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}

	events := collectThinking(t, srv, func() {
		srv.handleWSMessage(client, WSMessage{
			Type:      "chat",
			SessionID: sid,
			Content:   "what time is it",
			RunID:     "run-dm-pulse",
		})
		drainToCompletion(t, client)
	})

	if len(events) < 2 {
		t.Fatalf("desk DM want typing + done, got %+v", events)
	}
	if events[0].Payload["agent"] != "Winston" || events[0].Payload["space_id"] != dm.ID {
		t.Fatalf("desk DM first: %+v", events[0])
	}
}

func TestWSChat_PersistsLiveWinston1200ClockRewrite(t *testing.T) {
	srv, ch, _ := hallwayThinkingServer(t, liveWinston1200)
	sid := bindSpaceSession(t, srv, ch.ID, "Winston")
	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sid,
		Content:   "@Winston what time is it",
		RunID:     "run-clock",
	})
	drainToCompletion(t, client)
	time.Sleep(40 * time.Millisecond)

	if got := lastAssistant(t, srv, sid); got != wantWinston1200 {
		t.Fatalf("hallway persist got %q, want %q", got, wantWinston1200)
	}
}

func TestHandleSendMessage_HallwayPersistsLiveWinston1200Clock(t *testing.T) {
	srv, ch, _ := hallwayThinkingServer(t, liveWinston1200)
	sid := bindSpaceSession(t, srv, ch.ID, "Winston")

	body := `{"content":"@Winston what time is it"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sid+"/messages", strings.NewReader(body))
	req.SetPathValue("id", sid)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSendMessage(w, req)
	if w.Code != 200 {
		t.Fatalf("REST chat %d %s", w.Code, w.Body.String())
	}
	if got := lastAssistant(t, srv, sid); got != wantWinston1200 {
		t.Fatalf("REST hallway persist got %q, want %q", got, wantWinston1200)
	}
}
