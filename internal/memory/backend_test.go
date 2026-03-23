package memory_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

// VerifyBackendContract is a shared contract test helper.
// Call it with any MemoryBackend implementation.
func VerifyBackendContract(t *testing.T, b memory.MemoryBackend) {
	t.Helper()
	ctx := context.Background()

	// Write returns no error
	err := b.Write(ctx, "test-vault", "key1", "content about Go generics", []string{"go", "generics"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Recall returns at least the written item
	results, err := b.Recall(ctx, "test-vault", []string{"Go generics"}, 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Recall: expected at least one result, got 0")
	}

	// Available does not panic
	_ = b.Available()
}

type mockBackend struct{}

func (m *mockBackend) Available() bool { return true }
func (m *mockBackend) Write(_ context.Context, _, _, _ string, _ []string) error { return nil }
func (m *mockBackend) Recall(_ context.Context, _ string, _ []string, _ int) ([]memory.MemoryRecord, error) {
	return []memory.MemoryRecord{{Content: "Go generics allow type parameters", Score: 0.9}}, nil
}
func (m *mockBackend) EnsureVault(_ context.Context, _ string) error { return nil }

func TestBackendContract_Mock(t *testing.T) {
	VerifyBackendContract(t, &mockBackend{})
}
