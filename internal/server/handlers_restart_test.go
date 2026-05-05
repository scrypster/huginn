package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleHealth_StaleField(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	s.handleHealth(rr, req)

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["stale"]; !ok {
		t.Error("expected 'stale' key in health response")
	}
	if body["stale"] != false {
		t.Errorf("expected stale=false, got %v", body["stale"])
	}
}

func TestHandleRestart_UnsupportedPlatform(t *testing.T) {
	if execSupported {
		t.Skip("skipping unsupported-platform test on this platform")
	}
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/restart", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rr := httptest.NewRecorder()
	s.handleRestart(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}
