package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestHandleHealth_DefaultVersionIsDev verifies the health endpoint reports
// "dev" when no build-time version was injected. The frontend uses this value
// to surface "different version" confirmation in the UI; an empty string would
// render as a blank label and silently regress the feature.
func TestHandleHealth_DefaultVersionIsDev(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	v, ok := body["version"].(string)
	if !ok {
		t.Fatalf("expected string version, got %T (%v)", body["version"], body["version"])
	}
	if v != "dev" {
		t.Errorf("expected default version=dev, got %q", v)
	}
}

// TestHandleHealth_SetVersionPropagates verifies that whatever the host
// process injects via SetVersion (typically the git-describe tag baked in by
// the Makefile's -ldflags) ends up in the /health response verbatim. Without
// this wiring the UI would show a stale hardcoded version forever, which is
// exactly the bug we're fixing.
func TestHandleHealth_SetVersionPropagates(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.SetVersion("v9.9.9-test-gabcdef")

	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["version"] != "v9.9.9-test-gabcdef" {
		t.Errorf("expected version=v9.9.9-test-gabcdef, got %q", body["version"])
	}
}

// TestHandleHealth_SetVersionEmptyFallsBackToDev guards against accidentally
// blanking the version when the build was made without -ldflags (e.g. plain
// `go build`). The label must always render *something* recognisable.
func TestHandleHealth_SetVersionEmptyFallsBackToDev(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.SetVersion("")

	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["version"] != "dev" {
		t.Errorf("expected fallback version=dev for empty SetVersion, got %q", body["version"])
	}
}
