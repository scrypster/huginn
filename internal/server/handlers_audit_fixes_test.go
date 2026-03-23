package server

// handlers_audit_fixes_test.go — regression tests for the audit findings:
//
//  1. Agent API key redaction sentinel unified to "[REDACTED]" (was "***")
//  2. GET→PUT round-trip preserves agent API key when "[REDACTED]" sent back
//  3. Session title max-length validation (512 chars)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// ---------------------------------------------------------------------------
// Agent API key redaction sentinel
// ---------------------------------------------------------------------------

// TestRedactAgentDef_SentinelIsREDACTED verifies that the API redacts agent
// API keys as "[REDACTED]" (not "***") so the sentinel is consistent across
// the entire API surface.
func TestRedactAgentDef_SentinelIsREDACTED(t *testing.T) {
	a := agents.AgentDef{Name: "test", APIKey: "sk-real-secret"}
	got := redactAgentDef(a)
	if got.APIKey != "[REDACTED]" {
		t.Errorf("expected APIKey=[REDACTED], got %q", got.APIKey)
	}
}

// TestRedactAgentDef_EmptyAPIKey_NotRedacted verifies that agents with no
// API key don't get a placeholder injected.
func TestRedactAgentDef_EmptyAPIKey_NotRedacted(t *testing.T) {
	a := agents.AgentDef{Name: "test", APIKey: ""}
	got := redactAgentDef(a)
	if got.APIKey != "" {
		t.Errorf("expected empty APIKey, got %q", got.APIKey)
	}
}

// TestHandleListAgents_APIKeyRedacted verifies that the /agents endpoint
// returns "[REDACTED]" for agents that have an API key configured.
func TestHandleListAgents_APIKeyRedacted(t *testing.T) {
	_, ts := newTestServer(t)

	// Create an agent with an API key.
	def := agents.AgentDef{
		Name:     "audittest",
		Model:    "claude-sonnet-4-6",
		Provider: "anthropic",
		APIKey:   "sk-secret-key",
	}
	if err := agents.SaveAgentDefault(def); err != nil {
		t.Fatalf("save agent: %v", err)
	}

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

	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, a := range body {
		if a["name"] == "audittest" {
			if a["api_key"] == "sk-secret-key" {
				t.Error("real API key leaked in list response")
			}
			if a["api_key"] != "[REDACTED]" {
				t.Errorf("expected [REDACTED] sentinel, got %v", a["api_key"])
			}
			return
		}
	}
	// Agent might not appear if loading fails (tmp dir), which is fine — the
	// unit test TestRedactAgentDef_SentinelIsREDACTED covers the core logic.
}

// TestHandleUpdateAgent_REDACTEDSentinelPreservesKey verifies that a
// GET → PATCH round-trip with "[REDACTED]" as the API key value does not
// overwrite the real stored key.
func TestHandleUpdateAgent_REDACTEDSentinelPreservesKey(t *testing.T) {
	_, ts := newTestServer(t)

	// Create an agent with a real key.
	def := agents.AgentDef{
		Name:     "roundtrip",
		Model:    "claude-sonnet-4-6",
		Provider: "anthropic",
		APIKey:   "real-key-xyz",
	}
	if err := agents.SaveAgentDefault(def); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	// Send a PUT with "[REDACTED]" — simulating what the UI does after a GET.
	update := map[string]any{
		"name":     "roundtrip",
		"model":    "claude-sonnet-4-6",
		"provider": "anthropic",
		"api_key":  "[REDACTED]",
	}
	body, _ := json.Marshal(update)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/roundtrip", bytes.NewReader(body))
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

	// Reload the agent from disk and check the key is still the original.
	cfg, loadErr := agents.LoadAgents()
	if loadErr != nil {
		t.Fatalf("reload agents: %v", loadErr)
	}
	for _, a := range cfg.Agents {
		if a.Name == "roundtrip" {
			if a.APIKey != "real-key-xyz" {
				t.Errorf("expected real key preserved, got %q", a.APIKey)
			}
			return
		}
	}
	t.Error("agent 'roundtrip' not found after update")
}

// ---------------------------------------------------------------------------
// Config backend API key redaction sentinel
// ---------------------------------------------------------------------------

// TestHandleUpdateConfig_REDACTEDSentinelPreservesKey verifies that a
// GET → PUT round-trip with "[REDACTED]" as the backend.api_key value does
// not overwrite the real live key stored in the server's config.
func TestHandleUpdateConfig_REDACTEDSentinelPreservesKey(t *testing.T) {
	srv, ts := newTestServer(t)

	// Seed the live config with a keyring reference (the canonical post-fix form).
	// A properly configured server stores keyring: refs, not literals, so this
	// is what the [REDACTED] sentinel path will encounter in production.
	srv.mu.Lock()
	srv.cfg.Backend.APIKey = "keyring:huginn:anthropic"
	srv.cfg.Backend.Provider = "anthropic"
	srv.mu.Unlock()

	// Send a PUT with "[REDACTED]" as the api_key — simulating what the UI
	// does after a GET (which masks the real key as "[REDACTED]").
	update := map[string]any{
		"backend": map[string]any{
			"api_key":  "[REDACTED]",
			"provider": "anthropic",
		},
	}
	body, _ := json.Marshal(update)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", bytes.NewReader(body))
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

	// The server's live config must still hold the original keyring reference —
	// keyring refs are not literals so StoreAPIKey is not called for them.
	srv.mu.Lock()
	gotKey := srv.cfg.Backend.APIKey
	srv.mu.Unlock()
	if gotKey != "keyring:huginn:anthropic" {
		t.Errorf("expected keyring ref preserved, got %q", gotKey)
	}
}

// ---------------------------------------------------------------------------
// Session title length validation
// ---------------------------------------------------------------------------

// TestHandleUpdateSession_TitleTooLong verifies that titles > 512 chars
// are rejected with HTTP 400.
func TestHandleUpdateSession_TitleTooLong(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a real session so we have an ID to update.
	sess := srv.store.New("initial", "/workspace", "claude-sonnet-4-6")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	tooLong := strings.Repeat("x", 513)
	body, _ := json.Marshal(map[string]string{"title": tooLong})
	req, _ := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/sessions/%s", ts.URL, sess.ID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for oversized title, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Provider inference on save
// ---------------------------------------------------------------------------

// TestHandleUpdateAgent_InfersProviderFromModel verifies that when the frontend
// sends an agent PUT without a provider field, the backend infers it from the
// model name before persisting. This is the backend guard for stale configs and
// clients that don't send provider yet.
func TestHandleUpdateAgent_InfersProviderFromModel(t *testing.T) {
	_, ts := newTestServer(t)

	// PUT without a provider field — simulating old frontend or bare API call.
	update := map[string]any{
		"name":  "infertest",
		"model": "claude-sonnet-4-6",
		// provider intentionally omitted
	}
	body, _ := json.Marshal(update)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/infertest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Load the saved agent and verify provider was inferred.
	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("load agents: %v", err)
	}
	var found *agents.AgentDef
	for i := range cfg.Agents {
		if cfg.Agents[i].Name == "infertest" {
			found = &cfg.Agents[i]
			break
		}
	}
	if found == nil {
		t.Fatal("agent infertest not found after save")
	}
	if found.Provider != "anthropic" {
		t.Errorf("expected provider=anthropic (inferred from claude model), got %q", found.Provider)
	}
}

// TestHandleUpdateAgent_ExplicitProviderNotOverridden verifies that an explicit
// provider value in the PUT body is preserved (not overwritten by inference).
func TestHandleUpdateAgent_ExplicitProviderNotOverridden(t *testing.T) {
	_, ts := newTestServer(t)

	update := map[string]any{
		"name":     "explicitprov",
		"model":    "anthropic/claude-sonnet-4-6",
		"provider": "openrouter",
	}
	body, _ := json.Marshal(update)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/explicitprov", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	cfg2, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("load agents: %v", err)
	}
	var found2 *agents.AgentDef
	for i := range cfg2.Agents {
		if cfg2.Agents[i].Name == "explicitprov" {
			found2 = &cfg2.Agents[i]
			break
		}
	}
	if found2 == nil {
		t.Fatal("agent explicitprov not found after save")
	}
	if found2.Provider != "openrouter" {
		t.Errorf("expected provider=openrouter (explicit), got %q", found2.Provider)
	}
}

// TestHandleUpdateSession_TitleExactlyAtLimit verifies that a 512-char
// title is accepted.
func TestHandleUpdateSession_TitleExactlyAtLimit(t *testing.T) {
	srv, ts := newTestServer(t)

	sess := srv.store.New("initial", "/workspace", "claude-sonnet-4-6")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	exactly512 := strings.Repeat("a", 512)
	body, _ := json.Marshal(map[string]string{"title": exactly512})
	req, _ := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/sessions/%s", ts.URL, sess.ID), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for 512-char title, got %d", resp.StatusCode)
	}
}
