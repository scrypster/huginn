package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/session"
)

func newTestServerWithConnections(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	connStore, err := connections.NewStore(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	connMgr := connections.NewManager(connStore, connections.NewMemoryStore(), "http://localhost/oauth/callback")

	b := &stubBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	cfg := *config.Default()
	srv := New(cfg, orch, session.NewStore(t.TempDir()), testToken, t.TempDir(), connMgr, connStore, nil)
	srv.openBrowserFn = func(_ string) error { return nil }
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestConnectionsManagerNilSafe(t *testing.T) {
	_, ts := newTestServer(t) // nil connMgr

	for _, ep := range []struct{ method, path string }{
		{"GET", "/api/v1/connections"},
		{"GET", "/api/v1/providers"},
	} {
		req, _ := http.NewRequest(ep.method, ts.URL+ep.path, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", ep.method, ep.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Errorf("%s %s: expected 503, got %d", ep.method, ep.path, resp.StatusCode)
		}
	}
}

func TestListConnections_Empty(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var conns []connections.Connection
	json.NewDecoder(resp.Body).Decode(&conns)
	if conns == nil {
		t.Fatal("expected empty array, got nil")
	}
}

func TestListProviders_Returns5(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/providers", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var providers []map[string]any
	json.NewDecoder(resp.Body).Decode(&providers)
	if len(providers) != 5 {
		t.Fatalf("expected 5 providers, got %d", len(providers))
	}
}

func TestStartOAuth_ProviderNotConfigured(t *testing.T) {
	_, ts := newTestServerWithConnections(t) // no providers registered

	body := `{"provider":"github"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/connections/start", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for unconfigured provider, got %d", resp.StatusCode)
	}
}

func TestOAuthCallback_MissingParams(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/#/connections?error=missing_params" {
		t.Fatalf("unexpected redirect: %q", loc)
	}
}

func TestDeleteConnection_NotFound(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/connections/nonexistent-id", nil)
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
