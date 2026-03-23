package relay

import (
	"context"
	"encoding/base64"
	"testing"
	"time"
)

func newShellDispatcher(t *testing.T) (func(context.Context, Message), *collectHub, *ShellManager) {
	t.Helper()
	hub := &collectHub{}
	sm := NewShellManager()
	cfg := DispatcherConfig{
		MachineID: "test-machine",
		Hub:       hub,
		Shell:     sm,
	}
	return NewDispatcher(cfg), hub, sm
}

func TestDispatcher_ShellStart(t *testing.T) {
	dispatch, hub, sm := newShellDispatcher(t)
	defer sm.Exit()

	dispatch(context.Background(), Message{
		Type:    MsgShellStart,
		Payload: map[string]any{"cols": float64(80), "rows": float64(24)},
	})

	hub.waitFor(t, MsgShellReady, 3*time.Second)
}

func TestDispatcher_ShellStartNotWired(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID: "test-machine",
		Hub:       hub,
		Shell:     nil,
	}
	dispatch := NewDispatcher(cfg)

	// Should not panic — just log a warning
	dispatch(context.Background(), Message{
		Type:    MsgShellStart,
		Payload: map[string]any{"cols": float64(80), "rows": float64(24)},
	})
}

func TestDispatcher_ShellInput(t *testing.T) {
	dispatch, hub, sm := newShellDispatcher(t)
	defer sm.Exit()

	dispatch(context.Background(), Message{
		Type:    MsgShellStart,
		Payload: map[string]any{"cols": float64(80), "rows": float64(24)},
	})
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	dispatch(context.Background(), Message{
		Type:    MsgShellInput,
		Payload: map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("echo hi\n"))},
	})
}

func TestDispatcher_ShellResize(t *testing.T) {
	dispatch, hub, sm := newShellDispatcher(t)
	defer sm.Exit()

	dispatch(context.Background(), Message{
		Type:    MsgShellStart,
		Payload: map[string]any{"cols": float64(80), "rows": float64(24)},
	})
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	dispatch(context.Background(), Message{
		Type:    MsgShellResize,
		Payload: map[string]any{"cols": float64(132), "rows": float64(40)},
	})
}

func TestDispatcher_ShellExit(t *testing.T) {
	dispatch, hub, _ := newShellDispatcher(t)

	dispatch(context.Background(), Message{
		Type:    MsgShellStart,
		Payload: map[string]any{"cols": float64(80), "rows": float64(24)},
	})
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	dispatch(context.Background(), Message{
		Type:    MsgShellExit,
		Payload: map[string]any{},
	})

	hub.waitFor(t, MsgShellExit, 3*time.Second)
}

func TestDispatcher_ShellInputMissingData(t *testing.T) {
	dispatch, hub, sm := newShellDispatcher(t)
	defer sm.Exit()

	dispatch(context.Background(), Message{
		Type:    MsgShellStart,
		Payload: map[string]any{"cols": float64(80), "rows": float64(24)},
	})
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	// Missing data field — should not panic
	dispatch(context.Background(), Message{
		Type:    MsgShellInput,
		Payload: map[string]any{},
	})
}

func TestDispatcher_ShellStartDefaults(t *testing.T) {
	dispatch, hub, sm := newShellDispatcher(t)
	defer sm.Exit()

	// No cols/rows — should use defaults (220x50)
	dispatch(context.Background(), Message{
		Type:    MsgShellStart,
		Payload: map[string]any{},
	})

	ready := hub.waitFor(t, MsgShellReady, 3*time.Second)
	cols, _ := ready.Payload["cols"].(float64)
	rows, _ := ready.Payload["rows"].(float64)
	if cols != 220 || rows != 50 {
		t.Fatalf("expected defaults cols=220 rows=50, got cols=%v rows=%v", cols, rows)
	}
}

func TestDispatcher_ShellResizeNotWired(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID: "test-machine",
		Hub:       hub,
		Shell:     nil,
	}
	dispatch := NewDispatcher(cfg)

	// Must not panic when Shell is nil
	dispatch(context.Background(), Message{
		Type:    MsgShellResize,
		Payload: map[string]any{"cols": float64(80), "rows": float64(24)},
	})
}

func TestDispatcher_ShellExitNotWired(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID: "test-machine",
		Hub:       hub,
		Shell:     nil,
	}
	dispatch := NewDispatcher(cfg)

	// Must not panic when Shell is nil
	dispatch(context.Background(), Message{
		Type:    MsgShellExit,
		Payload: map[string]any{},
	})
}

func TestDispatcher_ShellInputNotWired(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID: "test-machine",
		Hub:       hub,
		Shell:     nil,
	}
	dispatch := NewDispatcher(cfg)

	dispatch(context.Background(), Message{
		Type:    MsgShellInput,
		Payload: map[string]any{"data": "aGVsbG8="},
	})
}

func TestSafeFloat_Fallback(t *testing.T) {
	if got := safeFloat(nil, 99); got != 99 {
		t.Fatalf("expected 99, got %v", got)
	}
	if got := safeFloat("not a number", 42); got != 42 {
		t.Fatalf("expected 42, got %v", got)
	}
	if got := safeFloat(float64(7), 0); got != 7 {
		t.Fatalf("expected 7, got %v", got)
	}
}
