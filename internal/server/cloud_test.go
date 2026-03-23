package server

import (
	"net/http"
	"testing"
)

func TestCloudCallback_MissingCode(t *testing.T) {
	_, ts := newTestServer(t) // cloudRegistrar is nil

	resp, err := http.Get(ts.URL + "/cloud/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for missing code, got %d", resp.StatusCode)
	}
}

func TestCloudCallback_WithCode_NilRegistrar(t *testing.T) {
	_, ts := newTestServer(t) // cloudRegistrar is nil — should not panic

	resp, err := http.Get(ts.URL + "/cloud/callback?code=test-code")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 (nil registrar = no-op), got %d", resp.StatusCode)
	}
}
