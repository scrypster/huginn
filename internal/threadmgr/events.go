package threadmgr

// ThreadEvent is emitted at thread lifecycle points and forwarded to the browser
// via the relay as a "thread_event" envelope.
type ThreadEvent struct {
	Event          string `json:"event"`                      // spawned|started|token|completed|error
	ThreadID       string `json:"thread_id"`
	AgentID        string `json:"agent_id"`
	Task           string `json:"task"`
	SpaceID        string `json:"space_id,omitempty"`
	SessionID      string `json:"session_id"`
	ChildSessionID string `json:"child_session_id,omitempty"`
	Text           string `json:"text,omitempty"` // only on "token" events
}

// EventEmitter delivers thread lifecycle events to a registered callback.
// It is nil-safe: calling Emit on a nil EventEmitter is a no-op.
type EventEmitter struct {
	fn func(ThreadEvent)
}

// NewEventEmitter creates an EventEmitter that calls fn for every event.
func NewEventEmitter(fn func(ThreadEvent)) *EventEmitter {
	return &EventEmitter{fn: fn}
}

// Emit delivers an event to the registered callback. Safe to call on nil receiver.
func (e *EventEmitter) Emit(ev ThreadEvent) {
	if e == nil || e.fn == nil {
		return
	}
	e.fn(ev)
}
