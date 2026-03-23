package memory_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

func TestPebbleBackend_Contract(t *testing.T) {
	dir := t.TempDir()
	b, err := memory.NewPebbleBackend(filepath.Join(dir, "test.pebble"))
	if err != nil {
		t.Fatalf("NewPebbleBackend: %v", err)
	}
	defer b.Close()
	VerifyBackendContract(t, b)
}

func TestPebbleBackend_RecallRelevance(t *testing.T) {
	dir := t.TempDir()
	b, err := memory.NewPebbleBackend(filepath.Join(dir, "rel.pebble"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	ctx := context.Background()
	vault := "test"
	_ = b.Write(ctx, vault, "k1", "Go generics enable type-safe data structures", []string{"go", "generics"})
	_ = b.Write(ctx, vault, "k2", "Python pandas DataFrame for data analysis", []string{"python", "data"})
	_ = b.Write(ctx, vault, "k3", "Go interfaces define behavior contracts", []string{"go", "interfaces"})

	results, err := b.Recall(ctx, vault, []string{"Go type parameters"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if strings.Contains(results[0].Content, "Python") {
		t.Error("expected Go result to rank higher than Python result")
	}
}

func TestPebbleBackend_VaultIsolation(t *testing.T) {
	dir := t.TempDir()
	b, err := memory.NewPebbleBackend(filepath.Join(dir, "iso.pebble"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	ctx := context.Background()
	_ = b.Write(ctx, "vault-a", "k1", "content in vault A", []string{"a"})
	_ = b.Write(ctx, "vault-b", "k1", "content in vault B", []string{"b"})

	results, err := b.Recall(ctx, "vault-a", []string{"content"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if strings.Contains(r.Content, "vault B") {
			t.Error("vault-b content leaked into vault-a recall")
		}
	}
}
