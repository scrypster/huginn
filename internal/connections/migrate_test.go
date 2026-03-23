package connections_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func writeTestConnectionsJSON(t *testing.T, path string, conns []connections.Connection) {
	t.Helper()
	b, err := json.Marshal(conns)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func openMigrateTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateFromJSON_MigratesConnections(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	conns := []connections.Connection{
		{
			ID:           "uuid-001",
			Provider:     connections.ProviderGitHub,
			Type:         connections.ConnectionTypeOAuth,
			AccountLabel: "mjbonanno",
			AccountID:    "12345",
			Scopes:       []string{"repo"},
			CreatedAt:    time.Now().UTC().Truncate(time.Second),
		},
	}
	writeTestConnectionsJSON(t, jsonPath, conns)

	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}

	store := connections.NewSQLiteConnectionStore(db)
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 connection, got %d", len(list))
	}
	if list[0].ID != "uuid-001" {
		t.Errorf("ID = %q, want %q", list[0].ID, "uuid-001")
	}
}

func TestMigrateFromJSON_CreatesBakFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	writeTestConnectionsJSON(t, jsonPath, []connections.Connection{})
	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}

	// Original should be gone
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Error("connections.json should be renamed, but still exists")
	}
	// Bak file should exist
	bakPath := jsonPath + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Error("connections.json.bak not created")
	}
}

func TestMigrateFromJSON_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	conns := []connections.Connection{{
		ID:           "uuid-idem",
		Provider:     connections.ProviderSlack,
		Type:         connections.ConnectionTypeOAuth,
		AccountLabel: "test",
		AccountID:    "sl-001",
		CreatedAt:    time.Now().UTC(),
	}}
	writeTestConnectionsJSON(t, jsonPath, conns)

	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("first MigrateFromJSON: %v", err)
	}

	// Second call: json.bak exists, M1_connections already recorded — should be no-op
	bakPath := jsonPath + ".bak"
	if err := connections.MigrateFromJSON(bakPath, db); err != nil {
		t.Fatalf("second MigrateFromJSON (bak): %v", err)
	}

	store := connections.NewSQLiteConnectionStore(db)
	list, _ := store.List()
	if len(list) != 1 {
		t.Errorf("idempotent: want 1, got %d", len(list))
	}
}

func TestMigrateFromJSON_MissingFile_NoOp(t *testing.T) {
	t.Parallel()
	db := openMigrateTestDB(t)
	// Non-existent file should be a no-op, not an error
	if err := connections.MigrateFromJSON("/nonexistent/connections.json", db); err != nil {
		t.Fatalf("missing file should be no-op: %v", err)
	}
}

func TestMigrateFromJSON_RecordsMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	writeTestConnectionsJSON(t, jsonPath, nil)
	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}

	var count int
	db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = 'M1_connections'`).Scan(&count)
	if count != 1 {
		t.Errorf("_migrations count = %d, want 1", count)
	}
}

func TestMigrateFromJSON_MultipleConnections(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	conns := []connections.Connection{
		{
			ID:           "github-1",
			Provider:     connections.ProviderGitHub,
			Type:         connections.ConnectionTypeOAuth,
			AccountLabel: "mjbonanno",
			AccountID:    "12345",
			Scopes:       []string{"repo", "read:org"},
			CreatedAt:    time.Now().UTC().Truncate(time.Second),
		},
		{
			ID:           "slack-1",
			Provider:     connections.ProviderSlack,
			Type:         connections.ConnectionTypeOAuth,
			AccountLabel: "workspace",
			AccountID:    "T0001",
			Scopes:       []string{"chat:write", "users:read"},
			CreatedAt:    time.Now().UTC().Truncate(time.Second),
		},
	}
	writeTestConnectionsJSON(t, jsonPath, conns)

	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}

	store := connections.NewSQLiteConnectionStore(db)
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 connections, got %d", len(list))
	}

	// Verify both are present
	ids := make(map[string]bool)
	for _, c := range list {
		ids[c.ID] = true
	}
	if !ids["github-1"] || !ids["slack-1"] {
		t.Error("both connections not found after migration")
	}
}

func TestMigrateFromJSON_WithMetadata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	conns := []connections.Connection{
		{
			ID:           "google-1",
			Provider:     connections.ProviderGoogle,
			Type:         connections.ConnectionTypeOAuth,
			AccountLabel: "user@gmail.com",
			AccountID:    "xyz789",
			Scopes:       []string{"calendar", "gmail.send"},
			Metadata: map[string]string{
				"refresh_token_updated": "2026-03-10T10:00:00Z",
				"custom_field":          "custom_value",
			},
			CreatedAt: time.Now().UTC().Truncate(time.Second),
		},
	}
	writeTestConnectionsJSON(t, jsonPath, conns)

	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}

	store := connections.NewSQLiteConnectionStore(db)
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 connection, got %d", len(list))
	}

	conn := list[0]
	if conn.Metadata == nil {
		t.Fatal("metadata is nil, expected map")
	}
	if conn.Metadata["custom_field"] != "custom_value" {
		t.Errorf("metadata custom_field = %q, want %q", conn.Metadata["custom_field"], "custom_value")
	}
}

func TestMigrateFromJSON_WithExpiry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	expiresAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	conns := []connections.Connection{
		{
			ID:           "oauth-expiry",
			Provider:     connections.ProviderJira,
			Type:         connections.ConnectionTypeOAuth,
			AccountLabel: "jira.example.com",
			AccountID:    "jira-123",
			Scopes:       []string{"read"},
			ExpiresAt:    expiresAt,
			CreatedAt:    time.Now().UTC().Truncate(time.Second),
		},
	}
	writeTestConnectionsJSON(t, jsonPath, conns)

	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}

	store := connections.NewSQLiteConnectionStore(db)
	conn, found := store.Get("oauth-expiry")
	if !found {
		t.Fatal("connection not found")
	}
	if conn.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero, expected a timestamp")
	}
}

func TestMigrateFromJSON_TransactionRollback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "connections.json")
	db := openMigrateTestDB(t)

	// Add a connection manually first
	store := connections.NewSQLiteConnectionStore(db)
	initial := connections.Connection{
		ID:           "initial-1",
		Provider:     connections.ProviderGitHub,
		Type:         connections.ConnectionTypeOAuth,
		AccountLabel: "initial",
		AccountID:    "initial-id",
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.Add(initial); err != nil {
		t.Fatalf("initial Add: %v", err)
	}

	// Create JSON with valid connections
	conns := []connections.Connection{
		{
			ID:           "migrated-1",
			Provider:     connections.ProviderSlack,
			Type:         connections.ConnectionTypeOAuth,
			AccountLabel: "slack",
			AccountID:    "slack-id",
			CreatedAt:    time.Now().UTC(),
		},
	}
	writeTestConnectionsJSON(t, jsonPath, conns)

	if err := connections.MigrateFromJSON(jsonPath, db); err != nil {
		t.Fatalf("MigrateFromJSON: %v", err)
	}

	// Both initial and migrated should be present
	list, _ := store.List()
	if len(list) != 2 {
		t.Fatalf("want 2 connections, got %d", len(list))
	}
}
