package sqlitedb_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// TestSmokeOpenHuginnHome exercises Open+ApplySchema+Migrate against ~/.huginn/huginn.db
// to confirm the wiring in main.go will work at runtime.
func TestSmokeOpenHuginnHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	huginnHome := filepath.Join(home, ".huginn")
	if err := os.MkdirAll(huginnHome, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dbPath := filepath.Join(huginnHome, "huginn.db")

	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := db.Migrate(nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Logf("sqlite DB opened and schema applied at %s", dbPath)
}
