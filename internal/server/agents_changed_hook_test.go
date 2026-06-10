package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestAgentMutationHandlers_FireOnAgentsChanged verifies that creating,
// updating, and deleting an agent via the API invokes the onAgentsChanged
// hook so main.go can reload the live registry (issue #124). Without the
// hook, runtime-created agents are invisible to delegation until restart.
func TestAgentMutationHandlers_FireOnAgentsChanged(t *testing.T) {
	srv, _ := newTestServer(t)

	var fired atomic.Int64
	srv.SetOnAgentsChanged(func() { fired.Add(1) })

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/agents/{name}", srv.handleUpdateAgent)
	mux.HandleFunc("DELETE /api/v1/agents/{name}", srv.handleDeleteAgent)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	t.Cleanup(func() { _ = agents.DeleteAgentDefault("HookTestAgent9001") })

	// Create.
	body := `{"name":"HookTestAgent9001","model":"claude-sonnet-4-6","color":"#aabbcc","icon":"H"}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/agents/HookTestAgent9001", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("create: expected 200, got %d", resp.StatusCode)
	}
	if fired.Load() != 1 {
		t.Fatalf("hook fired %d times after create, want 1", fired.Load())
	}

	// Update.
	body = `{"name":"HookTestAgent9001","model":"claude-haiku-4-5-20251001","color":"#aabbcc","icon":"H"}`
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/api/v1/agents/HookTestAgent9001", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("update: expected 200, got %d", resp.StatusCode)
	}
	if fired.Load() != 2 {
		t.Fatalf("hook fired %d times after update, want 2", fired.Load())
	}

	// Delete.
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agents/HookTestAgent9001", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("delete: expected 200, got %d", resp.StatusCode)
	}
	if fired.Load() != 3 {
		t.Fatalf("hook fired %d times after delete, want 3", fired.Load())
	}
}

// TestAgentMutationHandlers_ValidationFailureDoesNotFire verifies the hook is
// NOT invoked when the mutation is rejected before persisting.
func TestAgentMutationHandlers_ValidationFailureDoesNotFire(t *testing.T) {
	srv, _ := newTestServer(t)

	var fired atomic.Int64
	srv.SetOnAgentsChanged(func() { fired.Add(1) })

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/agents/{name}", srv.handleUpdateAgent)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Invalid color → 422 → no hook.
	body := `{"name":"BadAgent","model":"claude-sonnet-4-6","color":"notacolor"}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/agents/BadAgent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
	if fired.Load() != 0 {
		t.Errorf("hook fired %d times on rejected mutation, want 0", fired.Load())
	}
}
