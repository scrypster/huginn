package server

import (
	"bytes"
	"strings"
	"testing"
)

// mockSSEWriter is a test double that implements io.Writer and http.Flusher.
// It records all written bytes and counts how many times Flush was called.
type mockSSEWriter struct {
	buf       bytes.Buffer
	flushes   int
	failWrite bool // if true, Write returns an error
}

func (m *mockSSEWriter) Write(p []byte) (int, error) {
	if m.failWrite {
		return 0, bytes.ErrTooLarge
	}
	return m.buf.Write(p)
}

func (m *mockSSEWriter) Flush() {
	m.flushes++
}

// TestWriteSSEEvent_Basic verifies that writeSSEEvent produces a valid
// "data: <json>\n\n" line and calls Flush exactly once.
func TestWriteSSEEvent_Basic(t *testing.T) {
	mock := &mockSSEWriter{}
	err := writeSSEEvent(mock, mock, map[string]string{
		"type": "done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := mock.buf.String()
	if !strings.HasPrefix(output, "data: ") {
		t.Errorf("expected output to start with 'data: ', got: %q", output)
	}
	if !strings.HasSuffix(output, "\n\n") {
		t.Errorf("expected output to end with '\\n\\n', got: %q", output)
	}
	if mock.flushes != 1 {
		t.Errorf("expected 1 flush, got %d", mock.flushes)
	}
}

// TestWriteSSEEvent_ContainsJSON verifies that the event payload is valid JSON.
func TestWriteSSEEvent_ContainsJSON(t *testing.T) {
	mock := &mockSSEWriter{}
	payload := map[string]any{
		"type":       "progress",
		"downloaded": int64(1024),
		"total":      int64(4096),
	}
	err := writeSSEEvent(mock, mock, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := mock.buf.String()
	// Strip "data: " prefix and "\n\n" suffix.
	jsonStr := strings.TrimSuffix(strings.TrimPrefix(output, "data: "), "\n\n")
	if !strings.Contains(jsonStr, `"type"`) {
		t.Errorf("expected JSON to contain 'type' field, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"progress"`) {
		t.Errorf("expected JSON to contain 'progress' value, got: %s", jsonStr)
	}
}

// TestWriteSSEEvent_MultipleEvents verifies that multiple calls each produce
// their own event line and flush.
func TestWriteSSEEvent_MultipleEvents(t *testing.T) {
	mock := &mockSSEWriter{}

	events := []map[string]string{
		{"type": "progress"},
		{"type": "progress"},
		{"type": "done"},
	}
	for _, ev := range events {
		if err := writeSSEEvent(mock, mock, ev); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	output := mock.buf.String()
	count := strings.Count(output, "data: ")
	if count != 3 {
		t.Errorf("expected 3 'data:' lines, got %d", count)
	}
	if mock.flushes != 3 {
		t.Errorf("expected 3 flushes, got %d", mock.flushes)
	}
}

// TestWriteSSEEvent_MarshalError verifies that writeSSEEvent returns an error
// when the payload cannot be marshalled (e.g. an unmarshalable function value).
func TestWriteSSEEvent_MarshalError(t *testing.T) {
	mock := &mockSSEWriter{}
	// Functions cannot be JSON-marshalled.
	err := writeSSEEvent(mock, mock, func() {})
	if err == nil {
		t.Error("expected error when marshalling an unmarshalable value, got nil")
	}
	// Flush should not be called if marshal fails.
	if mock.flushes != 0 {
		t.Errorf("expected 0 flushes on marshal error, got %d", mock.flushes)
	}
}

// TestWriteSSEEvent_ErrorPayload verifies that error events are properly formatted.
func TestWriteSSEEvent_ErrorPayload(t *testing.T) {
	mock := &mockSSEWriter{}
	err := writeSSEEvent(mock, mock, map[string]string{
		"type":    "error",
		"content": "something went wrong",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := mock.buf.String()
	if !strings.Contains(output, "error") {
		t.Errorf("expected 'error' in output, got: %q", output)
	}
	if !strings.Contains(output, "something went wrong") {
		t.Errorf("expected error message in output, got: %q", output)
	}
}
