package session

import (
	"sync"
	"time"
)

// Manifest is the lightweight metadata file for a session (< 1 KB).
type Manifest struct {
	SessionID     string    `json:"session_id"`
	ID            string    `json:"id"` // alias for SessionID for convenience
	Title         string    `json:"title"`
	Model         string    `json:"model"`
	Agent         string    `json:"agent,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	MessageCount  int       `json:"message_count"`
	LastMessageID string    `json:"last_message_id"`
	WorkspaceRoot string    `json:"workspace_root"`
	WorkspaceName string    `json:"workspace_name"`
	Status        string    `json:"status"` // "active" | "archived"
	Version       int       `json:"version"`
	// Routine fields (empty string means user-initiated session)
	Source    string `json:"source,omitempty"`     // "routine" | ""
	RoutineID string `json:"routine_id,omitempty"` // ULID of the owning Routine
	RunID     string `json:"run_id,omitempty"`     // ULID for this specific run

	// Space fields (empty string means no space assigned)
	SpaceID string `json:"space_id,omitempty"` // ID of the Space this session belongs to
}

// SessionMessage is one line in messages.jsonl.
type SessionMessage struct {
	ID              string    `json:"id"`
	Ts              time.Time `json:"ts"`
	Seq             int64     `json:"seq"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	Agent           string    `json:"agent,omitempty"`
	ToolName        string    `json:"tool_name,omitempty"`
	ToolCallID      string    `json:"tool_call_id,omitempty"`
	Type            string    `json:"type,omitempty"` // "cost" for cost records
	PromptTok       int       `json:"prompt_tokens,omitempty"`
	CompTok         int       `json:"completion_tokens,omitempty"`
	CostUSD         float64   `json:"cost_usd,omitempty"`
	ModelName       string    `json:"model,omitempty"`
	ParentMessageID  string    `json:"parent_message_id,omitempty"`  // for thread replies
	ThreadReplyCount int       `json:"thread_reply_count,omitempty"` // count of thread replies on this message
}

// Session is the in-memory representation of an active session.
type Session struct {
	ID       string
	Manifest Manifest
	seq      int64      // monotonic counter, updated via atomic
	mu       sync.Mutex // guards concurrent access to Manifest fields
}

// PrimaryAgentID returns the session's primary agent name.
func (s *Session) PrimaryAgentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Manifest.Agent
}

// SetPrimaryAgent updates the session's primary agent name.
func (s *Session) SetPrimaryAgent(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Manifest.Agent = name
}

// SpaceID returns the space the session belongs to, or an empty string
// if the session is not associated with a space.
func (s *Session) SpaceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Manifest.SpaceID
}

// Touch bumps the session's UpdatedAt to now under the manifest lock.
// Call this after each agent reply so the SQLite sessions table reflects
// the time of the last activity — required for accurate UnseenCount queries.
func (s *Session) Touch() {
	s.mu.Lock()
	s.Manifest.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}
