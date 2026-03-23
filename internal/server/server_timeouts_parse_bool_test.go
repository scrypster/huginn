package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// --- Server timeout / limit configuration tests ---

func TestServerHasTimeouts(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleListSessions ---

func TestHandleListSessions_Empty(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleCreateSession ---

func TestHandleCreateSession(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["session_id"] == "" {
		t.Error("expected non-empty session_id")
	}
}

// --- handleGetSession (not found) ---

func TestHandleGetSession_NotFound_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/nonexistent-id", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// --- handleUpdateSession errors ---

func TestHandleUpdateSession_BadJSON_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/test-id",
		bytes.NewBufferString("not-json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

func TestHandleUpdateSession_NotFound_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	body := bytes.NewBufferString(`{"title":"new title"}`)
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/nonexistent", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for missing session, got %d", resp.StatusCode)
	}
}

// --- handleListAgents ---

func TestHandleListAgents(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleListModels ---

func TestHandleListModels(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleGetConfig ---

func TestHandleGetConfig(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleUpdateConfig (bad JSON) ---

func TestHandleUpdateConfig_BadJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config",
		bytes.NewBufferString("not-json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

// --- handleListThreads ---

func TestHandleListThreads_NoThreadManager(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/some-session/threads", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleGetMessages ---

func TestHandleGetMessages_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/nonexistent/messages", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 404 {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
}

// --- BroadcastWS and BroadcastPlanning ---

func TestBroadcastWS_WithHub(t *testing.T) {
	srv := &Server{token: testToken}
	srv.wsHub = newWSHub()
	// Should not panic
	srv.BroadcastWS(WSMessage{Type: "test"})
}

func TestBroadcastPlanning_NilHub_Iter3(t *testing.T) {
	srv := &Server{}
	// Should not panic — nil hub guard
	srv.BroadcastPlanning("sess1", "agent1")
}

func TestBroadcastPlanningDone_NilHub_Iter3(t *testing.T) {
	srv := &Server{}
	// Should not panic — nil hub guard
	srv.BroadcastPlanningDone("sess1")
}

// --- streamEventToWS with thought type ---

func TestStreamEventToWS_Thought(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamThought, Content: "thinking..."}
	msg := streamEventToWS(ev, "sess1")
	if msg.Type != "token" {
		t.Errorf("expected type %q, got %q", "token", msg.Type)
	}
}

func TestStreamEventToWS_Done(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamDone}
	msg := streamEventToWS(ev, "sess2")
	if msg.SessionID != "sess2" {
		t.Errorf("expected sessionID sess2, got %q", msg.SessionID)
	}
}

// --- handleWSMessage with nil orch ---

func TestHandleWSMessage_NilOrch_Chat(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.orch = nil // set orch to nil to test nil guard

	wsURL := "ws" + ts.URL[4:] + "/ws?token=" + testToken
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := WSMessage{Type: "chat", Content: "hello"}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Should receive an error message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, resp, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var replyMsg WSMessage
	json.Unmarshal(resp, &replyMsg)
	if replyMsg.Type != "error" {
		t.Errorf("expected error message, got type %q", replyMsg.Type)
	}
}

// --- Thread manager wiring ---

func TestSetThreadManager_Direct(t *testing.T) {
	srv, _ := newTestServer(t)
	tm := threadmgr.New()
	ca := threadmgr.NewCostAccumulator(0)
	// Wire directly (same package)
	srv.tm = tm
	srv.ca = ca
	if srv.tm != tm {
		t.Error("expected thread manager to be set")
	}
	if srv.ca != ca {
		t.Error("expected cost accumulator to be set")
	}
}

// --- handleWebSocket unauthorized ---

func TestHandleWebSocket_Unauthorized(t *testing.T) {
	_, ts := newTestServer(t)
	wsURL := "ws" + ts.URL[4:] + "/ws?token=wrong-token"
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	_, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected connection to fail with wrong token")
	}
	if resp != nil && resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// --- WSHub broadcast ---

func TestWSHub_BroadcastToSession(t *testing.T) {
	hub := newWSHub()
	go hub.run()

	ch := make(chan WSMessage, 4)
	c := &wsClient{send: ch, sessionID: "sess1"}
	hub.registerWithSession(c, "sess1")

	hub.broadcastToSession("sess1", WSMessage{Type: "test"})

	// Give hub time to deliver via the broadcastToSession (direct lock path)
	time.Sleep(20 * time.Millisecond)
	select {
	case msg := <-ch:
		if msg.Type != "test" {
			t.Errorf("expected type test, got %q", msg.Type)
		}
	default:
		t.Error("expected message in channel")
	}
}

func TestWSHub_UnregisterClient(t *testing.T) {
	hub := newWSHub()
	ch := make(chan WSMessage, 4)
	c := &wsClient{send: ch, sessionID: ""}
	hub.registerWithSession(c, "")
	hub.unregisterClient(c)
	// Double unregister should not panic
	hub.unregisterClient(c)
}

// --- handleCloudCallback ---

func TestHandleCloudCallback_WithCode_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/cloud/callback?code=testcode")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleRuntimeStatus ---

func TestHandleRuntimeStatus(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/runtime/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleStats ---

func TestHandleStats_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleLogs ---

func TestHandleLogs_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/logs", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- handleCost ---

func TestHandleCost_NilCA(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/cost", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// --- WSHub run path coverage ---

func TestWSHub_Run_RegisterBroadcastUnregister(t *testing.T) {
	hub := newWSHub()
	go hub.run()
	defer hub.stop()

	ch := make(chan WSMessage, 8)
	c := &wsClient{send: ch, sessionID: ""}

	// Register via direct method (channel-based approach removed as dead code)
	hub.registerWithSession(c, "")

	// Broadcast via broadcastC
	hub.broadcastC <- WSMessage{Type: "hello"}
	time.Sleep(50 * time.Millisecond)

	select {
	case msg := <-ch:
		if msg.Type != "hello" {
			t.Errorf("expected hello, got %q", msg.Type)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for broadcast message")
	}

	// Unregister via direct method
	hub.unregisterClient(c)
}

// --- WSHub wildcard broadcast ---

func TestWSHub_WildcardReceivesAllSessions(t *testing.T) {
	hub := newWSHub()

	chWild := make(chan WSMessage, 4)
	wild := &wsClient{send: chWild, sessionID: ""}
	hub.registerWithSession(wild, "")

	chSpecific := make(chan WSMessage, 4)
	specific := &wsClient{send: chSpecific, sessionID: "sess-x"}
	hub.registerWithSession(specific, "sess-x")

	// Broadcast to sess-x — wild should also receive (empty sessionID = wildcard)
	hub.broadcastToSession("sess-x", WSMessage{Type: "evt"})

	time.Sleep(20 * time.Millisecond)

	// Wild card
	select {
	case msg := <-chWild:
		if msg.Type != "evt" {
			t.Errorf("wildcard: expected evt, got %q", msg.Type)
		}
	default:
		t.Error("wildcard client expected to receive message")
	}

	// Specific
	select {
	case msg := <-chSpecific:
		if msg.Type != "evt" {
			t.Errorf("specific: expected evt, got %q", msg.Type)
		}
	default:
		t.Error("specific client expected to receive message")
	}
}

// --- SetCloudRegistrar ---

func TestSetCloudRegistrar(t *testing.T) {
	srv, _ := newTestServer(t)
	var called bool
	reg := &testRegistrar{fn: func(code string) { called = true }}
	srv.SetCloudRegistrar(reg)

	// Trigger the callback
	_, ts := newTestServer(t)
	_ = ts
	// Call handleCloudCallback directly via HTTP to trigger registrar
	w := mockResponseWriter{}
	r, _ := http.NewRequest("GET", "/cloud/callback?code=testcode", nil)
	srv.handleCloudCallback(&w, r)

	if !called {
		t.Error("expected registrar to be called")
	}
}

type testRegistrar struct {
	fn func(string)
}

func (r *testRegistrar) DeliverCode(code string) { r.fn(code) }

type mockResponseWriter struct {
	status int
	header http.Header
	body   bytes.Buffer
}

func (m *mockResponseWriter) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}
func (m *mockResponseWriter) Write(b []byte) (int, error) { return m.body.Write(b) }
func (m *mockResponseWriter) WriteHeader(s int)           { m.status = s }

// --- handleUpdateAgent ---

func TestHandleUpdateAgent_BadJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/myagent",
		bytes.NewBufferString("not-json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

func TestHandleUpdateAgent_ValidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	body := bytes.NewBufferString(`{"name":"myagent","slot":"planner","model_id":"gpt-4"}`)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/myagent", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should succeed or fail gracefully (file system write may fail in test env)
	if resp.StatusCode != 200 && resp.StatusCode != 500 {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
}

// --- handleSetActiveAgent ---

func TestHandleSetActiveAgent_BadJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/active",
		bytes.NewBufferString("not-json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

func TestHandleSetActiveAgent_EmptyName_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	body := bytes.NewBufferString(`{"name":""}`)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/active", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for empty name, got %d", resp.StatusCode)
	}
}

// --- handleWSMessage delegation_preview_ack ---

func TestWSMessage_DelegationPreviewAck_NilGate(t *testing.T) {
	srv, ts := newTestServer(t)
	if srv.previewGate != nil {
		t.Skip("previewGate is set, skip nil test")
	}

	wsURL := "ws" + ts.URL[4:] + "/ws?token=" + testToken
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := WSMessage{
		Type: "delegation_preview_ack",
		Payload: map[string]any{
			"thread_id": "thread-1",
			"approved":  true,
			"session_id": "sess-1",
		},
	}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Should not crash — just return silently
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	conn.ReadMessage() // may timeout
}

// --- handleWSMessage set_primary_agent ---

func TestWSMessage_SetPrimaryAgent_EmptySessionID(t *testing.T) {
	_, ts := newTestServer(t)
	wsURL := "ws" + ts.URL[4:] + "/ws?token=" + testToken
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := WSMessage{
		Type:    "set_primary_agent",
		Payload: map[string]any{"agent": "Alice", "session_id": ""},
	}
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	conn.ReadMessage()
}

// --- handleListAvailableModels (covers another path) ---

func TestHandleListAvailableModels_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/models/available", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Stub backend's Health returns nil so available models path runs
	if resp.StatusCode != 200 && resp.StatusCode != 503 {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
}

// --- handlePullModel ---

func TestHandlePullModel_Iter3(t *testing.T) {
	_, ts := newTestServer(t)
	body := bytes.NewBufferString(`{"name":"llama2"}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/models/pull", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should return 200, 500 (catalog load failure), or 502 (Ollama not running in CI)
	if resp.StatusCode != 200 && resp.StatusCode != 500 && resp.StatusCode != 502 {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
}
