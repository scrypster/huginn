package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleUpdateSession_RenamesTitle(t *testing.T) {
	s, ts := newTestServerWithConnections(t)
	defer ts.Close()

	// Create a session so the store knows about it
	sess := s.store.New("", "", "")
	if err := s.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"title": "My Cool Session"})
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/"+sess.ID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	loaded, err := s.store.Load(sess.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Manifest.Title != "My Cool Session" {
		t.Errorf("expected title %q, got %q", "My Cool Session", loaded.Manifest.Title)
	}
}

func TestHandleUpdateSession_ClearsTitle(t *testing.T) {
	s, ts := newTestServerWithConnections(t)
	defer ts.Close()

	sess := s.store.New("initial title", "", "")
	_ = s.store.SaveManifest(sess)

	body, _ := json.Marshal(map[string]string{"title": ""})
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/"+sess.ID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	loaded, _ := s.store.Load(sess.ID)
	if loaded.Manifest.Title != "" {
		t.Errorf("expected empty title, got %q", loaded.Manifest.Title)
	}
}

func TestHandleUpdateSession_NotFound(t *testing.T) {
	_, ts := newTestServerWithConnections(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"title": "ghost"})
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/doesnotexist", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
