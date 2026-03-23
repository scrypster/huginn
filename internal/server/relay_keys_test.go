package server

import "testing"

func TestRelayKeys_TwoFlowsSameProvider_DoNotCollide(t *testing.T) {
	s := &Server{
		relayKeys: make(map[string]string),
	}
	// Simulate two simultaneous flows with different IDs
	s.storeRelayKey("flow-aaa", "key-1")
	s.storeRelayKey("flow-bbb", "key-2")

	k1, ok1 := s.claimRelayKey("flow-aaa")
	k2, ok2 := s.claimRelayKey("flow-bbb")

	if !ok1 || k1 != "key-1" {
		t.Errorf("flow 1: expected key-1, got %q (ok=%v)", k1, ok1)
	}
	if !ok2 || k2 != "key-2" {
		t.Errorf("flow 2: expected key-2, got %q (ok=%v)", k2, ok2)
	}
}
