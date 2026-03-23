package connections_test

import (
	"testing"
	"github.com/scrypster/huginn/internal/connections"
)

func TestStoreAndGetAPIKeyCredentials(t *testing.T) {
	store, err := connections.NewStore(t.TempDir() + "/conns.json")
	if err != nil {
		t.Fatal(err)
	}
	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost")

	creds := map[string]string{"api_key": "test-key", "app_key": "test-app-key"}
	meta := map[string]string{"url": "https://api.datadoghq.com"}

	conn, err := mgr.StoreAPIKeyConnection(connections.ProviderDatadog, "test-label", meta, creds)
	if err != nil {
		t.Fatalf("StoreAPIKeyConnection: %v", err)
	}
	if conn.Provider != connections.ProviderDatadog {
		t.Errorf("provider: got %q, want %q", conn.Provider, connections.ProviderDatadog)
	}
	if conn.Type != connections.ConnectionTypeAPIKey {
		t.Errorf("type: got %q, want api_key", conn.Type)
	}
	if conn.Metadata["url"] != "https://api.datadoghq.com" {
		t.Errorf("metadata url: got %q", conn.Metadata["url"])
	}

	got, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if got["api_key"] != "test-key" {
		t.Errorf("api_key: got %q, want %q", got["api_key"], "test-key")
	}

	// GetCredentials on unknown connID should error
	if _, err := mgr.GetCredentials("nonexistent"); err == nil {
		t.Error("expected error for nonexistent connID, got nil")
	}

	// RemoveConnection should clean up both store entry and credentials
	if err := mgr.RemoveConnection(conn.ID); err != nil {
		t.Fatalf("RemoveConnection: %v", err)
	}
	if _, err := mgr.GetCredentials(conn.ID); err == nil {
		t.Error("expected error after removal, got nil")
	}
}
