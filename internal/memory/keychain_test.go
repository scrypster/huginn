package memory_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

func TestMuninnKeychain_StoreAndGet(t *testing.T) {
	// Use an in-memory store for testing (no real OS keychain needed).
	store := memory.NewMemoryPasswordStore()

	if err := store.StorePassword("s3cr3t"); err != nil {
		t.Fatalf("StorePassword: %v", err)
	}
	got, err := store.GetPassword()
	if err != nil {
		t.Fatalf("GetPassword: %v", err)
	}
	if got != "s3cr3t" {
		t.Errorf("got %q want %q", got, "s3cr3t")
	}
}

func TestMuninnKeychain_Delete(t *testing.T) {
	store := memory.NewMemoryPasswordStore()
	_ = store.StorePassword("s3cr3t")
	if err := store.DeletePassword(); err != nil {
		t.Fatalf("DeletePassword: %v", err)
	}
	_, err := store.GetPassword()
	if err == nil {
		t.Error("expected error after delete")
	}
}
