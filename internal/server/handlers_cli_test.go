package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCLIStatus_ReturnsThreeTools(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("GET", "/api/v1/integrations/cli-status", nil)
	w := httptest.NewRecorder()
	s.handleCLIStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []cliToolStatus
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 CLI tools, got %d", len(result))
	}

	names := make(map[string]bool)
	for _, r := range result {
		names[r.Name] = true
		if r.InstallCommands == nil {
			t.Errorf("tool %q has nil InstallCommands", r.Name)
		}
	}
	for _, want := range []string{"gh", "aws", "gcloud"} {
		if !names[want] {
			t.Errorf("missing tool %q in response", want)
		}
	}
}

func TestHandleCLIStatus_StructFields(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("GET", "/api/v1/integrations/cli-status", nil)
	w := httptest.NewRecorder()
	s.handleCLIStatus(w, req)

	var result []cliToolStatus
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, tool := range result {
		if tool.Name == "" {
			t.Error("tool has empty Name")
		}
		if tool.DisplayName == "" {
			t.Error("tool has empty DisplayName")
		}
		if tool.InstallCommands == nil {
			t.Errorf("tool %q has nil InstallCommands", tool.Name)
		}
	}
}
