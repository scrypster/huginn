package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

// newAppWithArtifact creates a minimal App with one artifact chat line for testing.
func newAppWithArtifact(artifactID, artifactTitle string) *App {
	a := New(nil, nil, nil, "test")
	a.chat.history = append(a.chat.history, chatLine{
		role:           "artifact",
		isArtifactLine: true,
		artifactID:     artifactID,
		artifactTitle:  artifactTitle,
		artifactKind:   "document",
		artifactStatus: "draft",
	})
	return a
}

// TestAcceptArtifactAtCursor_UpdatesLocalState verifies that accepting an
// artifact changes its status to "accepted" in local history.
func TestAcceptArtifactAtCursor_UpdatesLocalState(t *testing.T) {
	a := newAppWithArtifact("art-001", "my-patch.diff")

	a.acceptArtifactAtCursor()

	// Find the artifact line and check its status.
	found := false
	for _, line := range a.chat.history {
		if line.isArtifactLine {
			found = true
			if line.artifactStatus != "accepted" {
				t.Errorf("expected artifactStatus=%q, got %q", "accepted", line.artifactStatus)
			}
		}
	}
	if !found {
		t.Fatal("no artifact line found in history")
	}
}

// TestRejectArtifactAtCursor_UpdatesLocalState verifies that rejecting an
// artifact changes its status to "rejected" in local history.
func TestRejectArtifactAtCursor_UpdatesLocalState(t *testing.T) {
	a := newAppWithArtifact("art-002", "report.md")

	a.rejectArtifactAtCursor()

	found := false
	for _, line := range a.chat.history {
		if line.isArtifactLine {
			found = true
			if line.artifactStatus != "rejected" {
				t.Errorf("expected artifactStatus=%q, got %q", "rejected", line.artifactStatus)
			}
		}
	}
	if !found {
		t.Fatal("no artifact line found in history")
	}
}

// TestAcceptArtifactAtCursor_NoArtifactLine_NoPanic verifies that calling
// acceptArtifactAtCursor on an App with no artifact lines does not panic.
func TestAcceptArtifactAtCursor_NoArtifactLine_NoPanic(t *testing.T) {
	a := New(nil, nil, nil, "test")
	a.chat.history = append(a.chat.history, chatLine{role: "user", content: "hello"})

	// Must not panic.
	a.acceptArtifactAtCursor()
}

// TestRejectArtifactAtCursor_NoArtifactLine_NoPanic verifies that calling
// rejectArtifactAtCursor on an App with no artifact lines does not panic.
func TestRejectArtifactAtCursor_NoArtifactLine_NoPanic(t *testing.T) {
	a := New(nil, nil, nil, "test")
	a.chat.history = append(a.chat.history, chatLine{role: "user", content: "hello"})

	// Must not panic.
	a.rejectArtifactAtCursor()
}

// TestAcceptArtifactAtCursor_AddsConfirmLine verifies that a system
// confirmation line is appended to history after acceptance.
func TestAcceptArtifactAtCursor_AddsConfirmLine(t *testing.T) {
	a := newAppWithArtifact("art-003", "auth-refactor.diff")
	histLenBefore := len(a.chat.history)

	a.acceptArtifactAtCursor()

	if len(a.chat.history) <= histLenBefore {
		t.Fatal("expected a new system line to be appended after accepting artifact")
	}
	lastLine := a.chat.history[len(a.chat.history)-1]
	if lastLine.role != "system" {
		t.Errorf("expected last line role=%q, got %q", "system", lastLine.role)
	}
	if !strings.Contains(lastLine.content, "auth-refactor.diff") {
		t.Errorf("expected confirmation line to contain artifact title, got %q", lastLine.content)
	}
	if !strings.Contains(lastLine.content, "accepted") {
		t.Errorf("expected confirmation line to mention 'accepted', got %q", lastLine.content)
	}
}

// TestPatchArtifactStatus_CallsAPI verifies that patchArtifactStatus sends a
// PATCH request to the correct endpoint with the expected JSON body.
func TestPatchArtifactStatus_CallsAPI(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody map[string]string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"accepted"}`)
	}))
	defer ts.Close()

	// Parse host and port from test server URL.
	// ts.URL is like "http://127.0.0.1:PORT".
	addr := strings.TrimPrefix(ts.URL, "http://")
	parts := strings.SplitN(addr, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected test server URL: %s", ts.URL)
	}
	port := 0
	fmt.Sscanf(parts[1], "%d", &port)

	cfg := &config.Config{}
	cfg.WebUI.Bind = parts[0]
	cfg.WebUI.Port = port

	a := New(cfg, nil, nil, "test")
	a.patchArtifactStatus("art-xyz", "accepted")

	if capturedMethod != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", capturedMethod)
	}
	if capturedPath != "/api/v1/artifacts/art-xyz/status" {
		t.Errorf("expected path /api/v1/artifacts/art-xyz/status, got %s", capturedPath)
	}
	if capturedBody["status"] != "accepted" {
		t.Errorf("expected body status=%q, got %q", "accepted", capturedBody["status"])
	}
}

// TestArtifactServerBaseURL_NilConfig verifies that artifactServerBaseURL
// returns an empty string when cfg is nil.
func TestArtifactServerBaseURL_NilConfig(t *testing.T) {
	a := New(nil, nil, nil, "test")
	if got := a.artifactServerBaseURL(); got != "" {
		t.Errorf("expected empty string for nil cfg, got %q", got)
	}
}

// TestArtifactServerBaseURL_ZeroPort verifies that artifactServerBaseURL
// returns empty string when Port is 0 (dynamic allocation not yet resolved).
func TestArtifactServerBaseURL_ZeroPort(t *testing.T) {
	cfg := &config.Config{}
	cfg.WebUI.Bind = "127.0.0.1"
	cfg.WebUI.Port = 0
	a := New(cfg, nil, nil, "test")
	if got := a.artifactServerBaseURL(); got != "" {
		t.Errorf("expected empty string for port=0, got %q", got)
	}
}

// TestArtifactServerBaseURL_DefaultBind verifies that a missing Bind field
// falls back to "127.0.0.1".
func TestArtifactServerBaseURL_DefaultBind(t *testing.T) {
	cfg := &config.Config{}
	cfg.WebUI.Bind = ""
	cfg.WebUI.Port = 8421
	a := New(cfg, nil, nil, "test")
	got := a.artifactServerBaseURL()
	expected := "http://127.0.0.1:8421"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}
