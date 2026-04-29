package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/security"
)

func TestHandleGetCapabilityMatrix_RequiresAuth(t *testing.T) {
	_, ts := newTestServerWithConnections(t)
	resp, err := http.Get(ts.URL + "/api/v1/agents/capability-matrix")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandleGetCapabilityMatrix_ReturnsConnections(t *testing.T) {
	srv, ts := newTestServerWithConnections(t)
	if err := srv.connStore.Add(connections.Connection{
		ID:           "conn-gh-1",
		Provider:     connections.ProviderGitHub,
		AccountLabel: "Primary GitHub",
	}); err != nil {
		t.Fatalf("Add github conn: %v", err)
	}
	if err := srv.connStore.Add(connections.Connection{
		ID:           "conn-slack-1",
		Provider:     connections.ProviderSlack,
		AccountLabel: "Primary Slack",
	}); err != nil {
		t.Fatalf("Add slack conn: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/capability-matrix", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var matrix security.CapabilityMatrix
	if err := json.NewDecoder(resp.Body).Decode(&matrix); err != nil {
		t.Fatalf("decode matrix: %v", err)
	}
	if len(matrix.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(matrix.Connections))
	}
	if len(matrix.Providers) < 2 {
		t.Fatalf("expected at least 2 provider policies, got %d", len(matrix.Providers))
	}
}

func TestHandleValidateCapabilityMatrix_UnknownConnectionProfileDenied(t *testing.T) {
	_, ts := newTestServerWithConnections(t)
	body := `{
		"toolbelt": [
			{"connection_id":"stale-profile-conn","provider":"github","profile":"default"}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/agents/capability-matrix/validate", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result security.ValidationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Valid {
		t.Fatal("expected valid=false for stale/unknown connection profile")
	}
	denied, ok := result.FirstDenied()
	if !ok {
		t.Fatal("expected at least one denied decision")
	}
	if denied.ReasonCode != security.ReasonUnknownConnectionID {
		t.Fatalf("reason_code = %q, want %q", denied.ReasonCode, security.ReasonUnknownConnectionID)
	}
}

func TestHandleUpdateAgent_RejectsForgedToolbeltProvider(t *testing.T) {
	srv, ts := newTestServerWithConnections(t)
	if err := srv.connStore.Add(connections.Connection{
		ID:       "conn-github-1",
		Provider: connections.ProviderGitHub,
	}); err != nil {
		t.Fatalf("Add connection: %v", err)
	}

	body := `{
		"name": "ForgedProviderAgent",
		"model": "claude-opus-4",
		"toolbelt": [
			{"connection_id":"conn-github-1","provider":"slack"}
		]
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/ForgedProviderAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	gotErr, _ := payload["error"].(string)
	if !strings.Contains(gotErr, security.ReasonProviderMismatch) {
		t.Fatalf("expected error to contain reason code %q, got %q", security.ReasonProviderMismatch, gotErr)
	}
}

func TestHandleUpdateAgent_AllowsMatchingToolbeltProvider(t *testing.T) {
	srv, ts := newTestServerWithConnections(t)
	if err := srv.connStore.Add(connections.Connection{
		ID:       "conn-github-2",
		Provider: connections.ProviderGitHub,
	}); err != nil {
		t.Fatalf("Add connection: %v", err)
	}

	body := `{
		"name": "ValidProviderAgent",
		"model": "claude-opus-4",
		"toolbelt": [
			{"connection_id":"conn-github-2","provider":"github"}
		]
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/ValidProviderAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
