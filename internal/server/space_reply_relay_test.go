package server

import (
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
)

// recordingRelayHub captures SendRelay messages for tests.
type recordingRelayHub struct {
	mu   sync.Mutex
	msgs []relay.Message
}

func (h *recordingRelayHub) Send(_ string, msg relay.Message) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, msg)
	h.mu.Unlock()
	return nil
}

func (h *recordingRelayHub) Close(_ string) {}

func (h *recordingRelayHub) snapshot() []relay.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]relay.Message, len(h.msgs))
	copy(out, h.msgs)
	return out
}

func attachRecordingRelay(srv *Server) *recordingRelayHub {
	hub := &recordingRelayHub{}
	sat := relay.NewSatelliteWithStore("", relay.NewMemoryTokenStore())
	sat.SetHub(hub)
	srv.SetSatellite(sat)
	return hub
}

func TestEmitSpaceReplyTyping_SendRelayAndLocalWS(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	hub := attachRecordingRelay(srv)

	var local []WSMessage
	srv.onSpaceWS = func(msg WSMessage) { local = append(local, msg) }

	srv.emitSpaceReplyTyping(ch.ID, "parent-1", "Steve")

	if len(local) != 1 || local[0].Type != "space_reply_typing" {
		t.Fatalf("local WS: %+v", local)
	}
	if local[0].Payload["space_id"] != ch.ID || local[0].Payload["parent_id"] != "parent-1" || local[0].Payload["agent"] != "Steve" {
		t.Fatalf("local payload: %+v", local[0].Payload)
	}

	got := hub.snapshot()
	if len(got) != 1 {
		t.Fatalf("relay msgs=%d %+v", len(got), got)
	}
	msg := got[0]
	if msg.Type != "space_reply_typing" {
		t.Fatalf("relay type=%q", msg.Type)
	}
	if msg.SpaceID != ch.ID {
		t.Fatalf("relay SpaceID=%q want %q", msg.SpaceID, ch.ID)
	}
	if msg.Payload["space_id"] != ch.ID {
		t.Fatalf("relay payload space_id=%v", msg.Payload["space_id"])
	}
	if msg.Payload["parent_id"] != "parent-1" {
		t.Fatalf("relay parent_id=%v", msg.Payload["parent_id"])
	}
	if msg.Payload["agent"] != "Steve" {
		t.Fatalf("relay agent=%v", msg.Payload["agent"])
	}
}

func TestEmitSpaceActivity_SendRelayAndLocalWS(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	hub := attachRecordingRelay(srv)

	var local []WSMessage
	srv.onSpaceWS = func(msg WSMessage) { local = append(local, msg) }

	srv.emitSpaceActivity(ch.ID)

	if len(local) != 1 || local[0].Type != "space_activity" {
		t.Fatalf("local WS: %+v", local)
	}
	if local[0].Payload["space_id"] != ch.ID {
		t.Fatalf("local space_id=%v", local[0].Payload["space_id"])
	}
	if _, ok := local[0].Payload["unseen_count"]; !ok {
		t.Fatalf("local missing unseen_count: %+v", local[0].Payload)
	}

	got := hub.snapshot()
	if len(got) != 1 {
		t.Fatalf("relay msgs=%d %+v", len(got), got)
	}
	msg := got[0]
	if msg.Type != "space_activity" {
		t.Fatalf("relay type=%q", msg.Type)
	}
	if msg.SpaceID != ch.ID {
		t.Fatalf("relay SpaceID=%q want %q", msg.SpaceID, ch.ID)
	}
	if msg.Payload["space_id"] != ch.ID {
		t.Fatalf("relay payload space_id=%v", msg.Payload["space_id"])
	}
	if _, ok := msg.Payload["unseen_count"]; !ok {
		t.Fatalf("relay missing unseen_count: %+v", msg.Payload)
	}
}

func TestEmitSpaceScoped_NilSatelliteNoPanic(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	srv.wsHub = nil
	srv.SetSatellite(nil)
	srv.emitSpaceReplyTyping(ch.ID, "parent-1", "Steve")
	srv.emitSpaceActivity(ch.ID)
}

func TestEmitSpaceScoped_DisconnectedSatelliteNoPanic(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	sat := relay.NewSatelliteWithStore("", relay.NewMemoryTokenStore())
	// hub stays nil → ActiveHub returns InProcessHub (no-op)
	srv.SetSatellite(sat)
	srv.emitSpaceReplyTyping(ch.ID, "parent-1", "Steve")
	srv.emitSpaceActivity(ch.ID)
}

func TestEmitSpaceScoped_EmptySpaceIDSkipsRelay(t *testing.T) {
	srv := testServer(t)
	hub := attachRecordingRelay(srv)
	var n int
	srv.onSpaceWS = func(WSMessage) { n++ }
	srv.emitSpaceReplyTyping("", "parent-1", "Steve")
	srv.emitSpaceActivity("")
	if n != 0 {
		t.Fatalf("empty space_id leaked local WS %d", n)
	}
	if got := hub.snapshot(); len(got) != 0 {
		t.Fatalf("empty space_id leaked relay: %+v", got)
	}
}
