package server

import "testing"

// TestPayloadString_NilValue verifies that a nil value in the payload returns
// empty string, not "<nil>".
func TestPayloadString_NilValue(t *testing.T) {
	m := map[string]any{"key": nil}
	if got := payloadString(m, "key"); got != "" {
		t.Errorf("payloadString nil: got %q, want empty string", got)
	}
}

// TestPayloadString_MissingKey verifies that a missing key returns empty string.
func TestPayloadString_MissingKey(t *testing.T) {
	m := map[string]any{}
	if got := payloadString(m, "missing"); got != "" {
		t.Errorf("payloadString missing key: got %q, want empty string", got)
	}
}

// TestPayloadString_StringValue verifies that a string value is returned as-is.
func TestPayloadString_StringValue(t *testing.T) {
	m := map[string]any{"key": "hello"}
	if got := payloadString(m, "key"); got != "hello" {
		t.Errorf("payloadString string: got %q, want %q", got, "hello")
	}
}

// TestPayloadString_NonStringValue verifies that non-string values are
// formatted via %v (e.g. numbers, booleans).
func TestPayloadString_NonStringValue(t *testing.T) {
	m := map[string]any{"count": 42}
	if got := payloadString(m, "count"); got != "42" {
		t.Errorf("payloadString int: got %q, want %q", got, "42")
	}
}

// TestPayloadString_NilMap verifies that a nil map returns empty string without
// panicking.
func TestPayloadString_NilMap(t *testing.T) {
	if got := payloadString(nil, "key"); got != "" {
		t.Errorf("payloadString nil map: got %q, want empty string", got)
	}
}
