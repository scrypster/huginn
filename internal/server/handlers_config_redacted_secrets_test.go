package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// Opus-vet finding 2026-08-28: GET /api/v1/config redacts all five
// integrations.*.client_secret; the Settings form sends the redacted value
// back on Save. PUT must restore the live secrets, never persist the
// literal "[REDACTED]".
func TestHandleUpdateConfig_RestoresRedactedOAuthSecrets(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.Integrations.Google.ClientSecret = "google-real"
	srv.cfg.Integrations.GitHub.ClientSecret = "github-real"
	srv.cfg.Integrations.Slack.ClientSecret = "slack-real"
	srv.cfg.Integrations.Jira.ClientSecret = "jira-real"
	srv.cfg.Integrations.Bitbucket.ClientSecret = "bb-real"

	body := srv.cfg // full-body PUT, as the shipped Settings form sends
	body.Integrations.Google.ClientSecret = "[REDACTED]"
	body.Integrations.GitHub.ClientSecret = "[REDACTED]"
	body.Integrations.Slack.ClientSecret = "[REDACTED]"
	body.Integrations.Jira.ClientSecret = "[REDACTED]"
	body.Integrations.Bitbucket.ClientSecret = "new-bb-secret" // a real edit must stick
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/config", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	srv.handleUpdateConfig(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT config = %d body=%s", rec.Code, rec.Body.String())
	}
	got := srv.cfg.Integrations
	if got.Google.ClientSecret != "google-real" || got.GitHub.ClientSecret != "github-real" ||
		got.Slack.ClientSecret != "slack-real" || got.Jira.ClientSecret != "jira-real" {
		t.Fatalf("redacted secrets not restored: %+v", got)
	}
	if got.Bitbucket.ClientSecret != "new-bb-secret" {
		t.Fatalf("real secret edit lost: %q", got.Bitbucket.ClientSecret)
	}
}
