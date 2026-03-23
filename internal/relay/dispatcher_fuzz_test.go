//go:build go1.18

package relay_test

import (
	"context"
	"testing"
)

// FuzzDispatcher feeds arbitrary bytes to the message dispatcher to catch
// panics, nil dereferences, and unhandled type assertions on malformed input.
func FuzzDispatcher(f *testing.F) {
	// Seed corpus: valid messages
	f.Add([]byte(`{"type":"chat_message","payload":{"session_id":"s1","content":"hi"}}`))
	f.Add([]byte(`{"type":"session_start","payload":{"session_id":"s1"}}`))
	f.Add([]byte(`{"type":"session_resume","payload":{"session_id":"s1"}}`))
	f.Add([]byte(`{"type":"cancel_session","payload":{"session_id":"s1"}}`))
	f.Add([]byte(`{"type":"permission_response","payload":{"request_id":"r1","approved":true}}`))
	f.Add([]byte(`{"type":"session_list_request"}`))
	f.Add([]byte(`{"type":"model_list_request"}`))
	f.Add([]byte(`{"type":"satellite_hello"}`))
	f.Add([]byte(`{"type":"satellite_heartbeat"}`))

	// Malformed / edge case inputs
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add([]byte(`{`))
	f.Add([]byte(`}`))
	f.Add([]byte(`{"type":""}`))
	f.Add([]byte(`{"type":null}`))
	f.Add([]byte(`{"type":123}`))
	f.Add([]byte(`{"type":"unknown_type"}`))
	f.Add([]byte(`{"payload":"not_a_map"}`))
	f.Add([]byte(`{"payload":null}`))
	f.Add([]byte(`{"machine_id":"wrong_machine"}`))
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte(`"string"`))
	f.Add([]byte(`true`))
	f.Add([]byte(`false`))
	f.Add([]byte(`0`))

	f.Fuzz(func(t *testing.T, data []byte) {
		d := NewTestDispatcher(t)
		// Must not panic regardless of input
		_ = d.DispatchRaw(context.Background(), data)
	})
}
