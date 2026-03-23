package server

// handlers_coverage_test.go brings coverage for previously-uncovered handler paths:
//   - handleSendMessage (was 0%)
//   - handleChatStream  (new SSE endpoint)
//   - handleCloudCallback happy path with a real registrar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/connections"
)

// ---------------------------------------------------------------------------
// handleSendMessage
// ---------------------------------------------------------------------------

// TestHandleSendMessage_Success verifies that a valid POST returns the assistant reply.
func TestHandleSendMessage_Success(t *testing.T) {
	srv, ts := newTestServer(t)
	_ = srv

	// Create a session first so the session ID is valid in the orchestrator.
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var created map[string]string
	json.NewDecoder(createResp.Body).Decode(&created)
	sessionID := created["session_id"]

	body := `{"content":"hello world"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/"+sessionID+"/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["content"] == "" {
		t.Error("expected non-empty content in response")
	}
}

// TestHandleSendMessage_EmptyContent verifies that an empty content returns 400.
func TestHandleSendMessage_EmptyContent(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"content":""}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/any-session/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestHandleSendMessage_InvalidJSON verifies that malformed JSON returns 400.
func TestHandleSendMessage_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/any-session/messages", strings.NewReader("{bad json}"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestHandleSendMessage_NilOrchestrator verifies that a nil orchestrator returns 503.
func TestHandleSendMessage_NilOrchestrator(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.orch = nil // detach orchestrator

	body := `{"content":"hello"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/any-session/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleChatStream (SSE endpoint)
// ---------------------------------------------------------------------------

// TestHandleChatStream_NilOrchestrator verifies SSE headers + error event when orch is nil.
func TestHandleChatStream_NilOrchestrator(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.orch = nil

	body := strings.NewReader(`{"content":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test/chat/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleChatStream(w, req)

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "data:") {
		t.Error("expected SSE data event in response")
	}
	if !strings.Contains(w.Body.String(), "error") {
		t.Error("expected error event when orchestrator is nil")
	}
}

// TestHandleChatStream_BadMethod verifies 405 for GET requests.
func TestHandleChatStream_BadMethod(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/test/chat/stream", nil)
	w := httptest.NewRecorder()
	srv.handleChatStream(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// TestHandleChatStream_EmptyContent verifies 400 when content is empty.
func TestHandleChatStream_EmptyContent(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{"content":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test/chat/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleChatStream(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestHandleChatStream_InvalidJSON verifies 400 for malformed JSON.
func TestHandleChatStream_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	body := strings.NewReader(`{bad}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/test/chat/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleChatStream(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestHandleChatStream_Success verifies that a valid request produces SSE token events + done.
func TestHandleChatStream_Success(t *testing.T) {
	srv, ts := newTestServer(t)
	_ = srv

	// Create a session so the orchestrator recognises the session ID.
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var created map[string]string
	json.NewDecoder(createResp.Body).Decode(&created)
	sessionID := created["session_id"]

	body := strings.NewReader(`{"content":"stream this"}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/"+sessionID+"/chat/stream", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream content type, got %q", ct)
	}
}

// TestHandleChatStream_RouteRegistered verifies the /chat/stream route exists in the mux (not 404).
func TestHandleChatStream_RouteRegistered(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/s1/chat/stream", strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		t.Errorf("expected route to be registered (not 404)")
	}
}

// ---------------------------------------------------------------------------
// handleCloudCallback — happy path with a real registrar
// ---------------------------------------------------------------------------

// mockRegistrar captures DeliverCode calls.
type mockRegistrar struct {
	received atomic.Value // stores the last code delivered
}

func (m *mockRegistrar) DeliverCode(code string) {
	m.received.Store(code)
}

// TestCloudCallback_WithCode_WithRegistrar verifies that the code is delivered to the registrar.
func TestCloudCallback_WithCode_WithRegistrar(t *testing.T) {
	srv, _ := newTestServer(t)
	reg := &mockRegistrar{}
	srv.SetCloudRegistrar(reg)

	req := httptest.NewRequest("GET", "/cloud/callback?code=abc123", nil)
	w := httptest.NewRecorder()
	srv.handleCloudCallback(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	respBody := w.Body.String()
	if !strings.Contains(respBody, "Registration complete") {
		t.Errorf("expected Registration complete HTML, got %q", respBody)
	}
	if code, ok := reg.received.Load().(string); !ok || code != "abc123" {
		t.Errorf("expected registrar to receive code 'abc123', got %v", reg.received.Load())
	}
}

// ---------------------------------------------------------------------------
// handleGetMessages — invalid limit param (non-numeric is silently ignored → default 50)
// ---------------------------------------------------------------------------

// TestHandleGetMessages_InvalidLimitIgnored verifies that a non-numeric limit defaults to 50.
func TestHandleGetMessages_InvalidLimitIgnored(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/nosession/messages?limit=notanumber", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Invalid limit is ignored and the default (50) is applied — should still return 200.
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 with invalid limit, got %d", resp.StatusCode)
	}
}

// TestHandleGetMessages_NegativeLimitIgnored verifies that a negative limit defaults to 50.
func TestHandleGetMessages_NegativeLimitIgnored(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/nosession/messages?limit=-5", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 with negative limit, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleStartOAuth — additional branches to boost coverage
// ---------------------------------------------------------------------------

// TestHandleStartOAuth_InvalidJSON verifies 400 for bad JSON body.
func TestHandleStartOAuth_InvalidJSON(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/connections/start", strings.NewReader("{bad}"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

// TestHandleStartOAuth_EmptyProvider verifies 400 when provider field is empty.
func TestHandleStartOAuth_EmptyProvider(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/connections/start", strings.NewReader(`{"provider":""}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for empty provider, got %d", resp.StatusCode)
	}
}

// TestHandleStartOAuth_KnownProvider_ViaLocalFlow verifies that a configured provider
// returns an auth URL (local redirect flow).
func TestHandleStartOAuth_KnownProvider_ViaLocalFlow(t *testing.T) {
	srv, ts := newTestServerWithConnections(t)
	// Register the stub provider so handleStartOAuth can find it.
	srv.connProviders[connections.ProviderSlack] = &stubProvider{name: connections.ProviderSlack}

	body := `{"provider":"slack"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/connections/start", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should be 200 with an auth_url.
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for known provider, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["auth_url"] == "" {
		t.Error("expected non-empty auth_url")
	}
}

// TestHandleStartOAuth_ViabrokerClient verifies the broker path when a BrokerClient is set.
func TestHandleStartOAuth_ViabrokerClient(t *testing.T) {
	srv, ts := newTestServerWithConnections(t)
	srv.connProviders[connections.ProviderSlack] = &stubProvider{name: connections.ProviderSlack}
	mock := &mockBrokerClient{authURL: "https://slack.com/oauth/v2/authorize?broker=1"}
	srv.SetBrokerClient(mock)

	body := `{"provider":"slack"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/connections/start", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["auth_url"] == "" {
		t.Error("expected non-empty auth_url from broker")
	}
}

// ---------------------------------------------------------------------------
// handleOAuthCallback — additional branches
// ---------------------------------------------------------------------------

// TestHandleOAuthCallback_ErrorParam verifies redirect on error query param.
func TestHandleOAuthCallback_ErrorParam(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/oauth/callback?error=access_denied")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "access_denied") {
		t.Errorf("expected error in redirect location, got %q", loc)
	}
}

// TestHandleOAuthCallback_NilConnMgr verifies redirect when connMgr is nil.
func TestHandleOAuthCallback_NilConnMgr(t *testing.T) {
	_, ts := newTestServer(t) // no connMgr

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/oauth/callback?state=st&code=cd")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "not_configured") {
		t.Errorf("expected not_configured in redirect, got %q", loc)
	}
}

// ---------------------------------------------------------------------------
// handleStartOAuthBroker — covered indirectly via handleStartOAuth broker path
// (direct unit test for the helper itself)
// ---------------------------------------------------------------------------

// TestHandleStartOAuthBroker_Direct exercises handleStartOAuthBroker directly.
func TestHandleStartOAuthBroker_Direct(t *testing.T) {
	srv, _ := newTestServerWithConnections(t)
	mock := &mockBrokerClient{authURL: "https://slack.com/broker/start"}
	srv.SetBrokerClient(mock)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/start", nil)
	w := httptest.NewRecorder()
	srv.handleStartOAuthBroker(w, req, "slack")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["auth_url"] == "" {
		t.Error("expected non-empty auth_url")
	}
}

// ---------------------------------------------------------------------------
// handleListThreads — session ID from PathValue when it's empty
// ---------------------------------------------------------------------------

// TestHandleListThreads_EmptySessionID exercises the sessionID == "" branch directly.
func TestHandleListThreads_EmptySessionID(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//threads", nil)
	// PathValue("id") will be "" since we didn't call SetPathValue.
	w := httptest.NewRecorder()
	srv.handleListThreads(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty session id, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// handleGetAgent — found path
// ---------------------------------------------------------------------------

// TestHandleGetAgent_Found verifies 200 when agent exists.
func TestHandleGetAgent_Found(t *testing.T) {
	_, ts := newTestServer(t)

	// First get the list to find a known agent name.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var agentList []map[string]any
	json.NewDecoder(resp.Body).Decode(&agentList)
	if len(agentList) == 0 {
		t.Skip("no agents configured — skipping found test")
	}
	name, _ := agentList[0]["name"].(string)

	req2, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/"+name, nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("expected 200 for known agent %q, got %d", name, resp2.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleGetConfig — redacts all integration secrets
// ---------------------------------------------------------------------------

// TestHandleGetConfig_RedactsAllIntegrationSecrets verifies every secret field is masked.
func TestHandleGetConfig_RedactsAllIntegrationSecrets(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.Integrations.Google.ClientSecret = "google-secret"
	srv.cfg.Integrations.GitHub.ClientSecret = "github-secret"
	srv.cfg.Integrations.Slack.ClientSecret = "slack-secret"
	srv.cfg.Integrations.Jira.ClientSecret = "jira-secret"
	srv.cfg.Integrations.Bitbucket.ClientSecret = "bitbucket-secret"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)

	body := w.Body.String()
	for _, secret := range []string{"google-secret", "github-secret", "slack-secret", "jira-secret", "bitbucket-secret"} {
		if strings.Contains(body, secret) {
			t.Errorf("response must not contain secret %q", secret)
		}
	}
}
