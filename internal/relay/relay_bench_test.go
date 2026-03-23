package relay

import (
	"testing"

	"github.com/scrypster/huginn/internal/storage"
)

// BenchmarkOutboxEnqueue benchmarks the Outbox.Enqueue path which serialises
// a Message to JSON and writes it to Pebble with a monotonic sequence key.
func BenchmarkOutboxEnqueue(b *testing.B) {
	dir := b.TempDir()
	store, err := storage.Open(dir)
	if err != nil {
		b.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	outbox := NewOutbox(store, nil)

	msg := Message{
		Type:      MsgToken,
		MachineID: "bench-machine",
		Payload: map[string]any{
			"session_id": "sess-bench",
			"content":    "benchmark token payload with some representative content length for realistic sizing",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := outbox.Enqueue(msg); err != nil {
			b.Fatalf("Enqueue: %v", err)
		}
	}
}

// BenchmarkOutboxDrain benchmarks Outbox.Drain which reads and deletes N
// messages from the Pebble-backed queue.
func BenchmarkOutboxDrain(b *testing.B) {
	dir := b.TempDir()
	store, err := storage.Open(dir)
	if err != nil {
		b.Fatalf("storage.Open: %v", err)
	}
	defer store.Close()

	outbox := NewOutbox(store, nil)

	msg := Message{
		Type:      MsgToken,
		MachineID: "bench-machine",
		Payload:   map[string]any{"content": "drain benchmark payload"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Pre-fill 10 messages.
		for j := 0; j < 10; j++ {
			if err := outbox.Enqueue(msg); err != nil {
				b.Fatalf("Enqueue: %v", err)
			}
		}
		b.StartTimer()

		msgs, err := outbox.Drain(10)
		if err != nil {
			b.Fatalf("Drain: %v", err)
		}
		if len(msgs) != 10 {
			b.Fatalf("expected 10 messages, got %d", len(msgs))
		}
	}
}
