package agent

import (
	huginsession "github.com/scrypster/huginn/internal/session"
)

// generateSessionID produces a unique time-sortable ULID session identifier.
func generateSessionID() (string, error) {
	return huginsession.NewID(), nil
}

// SessionID returns the unique identifier generated for this orchestrator's default session.
func (o *Orchestrator) SessionID() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.defaultSessionID
}

// NewSession creates a new session and stores it in the sessions map.
// If id is empty, a unique ID is generated.
func (o *Orchestrator) NewSession(id string) (*Session, error) {
	if id == "" {
		var err error
		id, err = generateSessionID()
		if err != nil {
			return nil, err
		}
	}
	sess := newSession(id)
	o.mu.Lock()
	o.sessions[sess.ID] = sess
	o.mu.Unlock()
	if o.sc != nil {
		o.sc.Record("sessions.created", 1)
	}
	return sess, nil
}

// GetSession retrieves a session by ID. Returns the session and true if found.
func (o *Orchestrator) GetSession(id string) (*Session, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	s, ok := o.sessions[id]
	return s, ok
}

// defaultSession returns the default session. Must be called with o.mu held.
func (o *Orchestrator) defaultSession() *Session {
	return o.sessions[o.defaultSessionID]
}

// CurrentState returns the current orchestrator state for the default session.
func (o *Orchestrator) CurrentState() State {
	o.mu.RLock()
	sess := o.defaultSession()
	o.mu.RUnlock()
	if sess == nil {
		return StateIdle
	}
	return sess.getState()
}

// sessionForIDOrDefault returns the session for the given ID, or the default session if id is empty.
// Returns nil if the id is non-empty and not found.
func (o *Orchestrator) sessionForIDOrDefault(id string) *Session {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if id == "" {
		return o.defaultSession()
	}
	return o.sessions[id]
}
