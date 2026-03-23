package threadmgr_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/threadmgr"
)

func TestEventEmitter_Fires(t *testing.T) {
	var captured []threadmgr.ThreadEvent
	emitter := threadmgr.NewEventEmitter(func(e threadmgr.ThreadEvent) {
		captured = append(captured, e)
	})
	emitter.Emit(threadmgr.ThreadEvent{
		Event: "spawned", ThreadID: "thr_01", AgentID: "coder", Task: "do thing",
	})
	if len(captured) != 1 {
		t.Errorf("expected 1 event, got %d", len(captured))
	}
	if captured[0].Event != "spawned" {
		t.Errorf("wrong event: %s", captured[0].Event)
	}
}

func TestEventEmitter_NilSafe(t *testing.T) {
	var emitter *threadmgr.EventEmitter
	// Should not panic
	emitter.Emit(threadmgr.ThreadEvent{Event: "spawned"})
}
