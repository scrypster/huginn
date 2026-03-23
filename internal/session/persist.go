package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PersistentSession represents session metadata stored on disk.
// It mirrors Manifest but uses the field names and tags prescribed by the
// persistence spec so that external tools reading manifest.json see a
// consistent schema.
type PersistentSession struct {
	ID        string    `json:"session_id"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	Agent     string    `json:"agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	MsgCount  int       `json:"message_count"`
	LastMsgID string    `json:"last_message_id,omitempty"`
	Workspace string    `json:"workspace_root,omitempty"`
	WsName    string    `json:"workspace_name,omitempty"`
	Status    string    `json:"status"` // "active" | "closed"
	Version   int       `json:"version"`
	Source    string `json:"source,omitempty"`
	RoutineID string `json:"routine_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
}

// PersistedMessage is a single JSONL record in messages.jsonl.
type PersistedMessage struct {
	ID           string  `json:"id"`
	Ts           string  `json:"ts"`
	Seq          int64   `json:"seq"`
	Role         string  `json:"role"`
	Content      string  `json:"content"`
	Agent        string  `json:"agent,omitempty"`
	ToolCalls    any     `json:"tool_calls,omitempty"`
	ToolName     string  `json:"tool_name,omitempty"`
	ToolCallID   string  `json:"tool_call_id,omitempty"`
	Type         string  `json:"type,omitempty"` // "cost" for cost records
	PromptTokens int     `json:"prompt_tokens,omitempty"`
	CompTokens   int     `json:"completion_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Model        string  `json:"model,omitempty"`
}

// NewMessageID generates a unique message ID using the ULID generator.
func NewMessageID() string {
	return NewID()
}

// Create creates a new session directory and writes its PersistentSession
// manifest under <baseDir>/<id>/manifest.json.
func (s *Store) Create(ps *PersistentSession) error {
	dir := s.sessionDir(ps.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	return s.writePersistentManifest(ps)
}

// writePersistentManifest atomically writes a PersistentSession manifest.
func (s *Store) writePersistentManifest(ps *PersistentSession) error {
	dir := s.sessionDir(ps.ID)
	data, err := json.Marshal(ps)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	tmp := filepath.Join(dir, "manifest.json.tmp")
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write manifest tmp: %w", err)
	}
	return os.Rename(tmp, filepath.Join(dir, "manifest.json"))
}

// LoadManifest loads a PersistentSession manifest by session ID.
func (s *Store) LoadManifest(id string) (*PersistentSession, error) {
	path := filepath.Join(s.sessionDir(id), "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ps PersistentSession
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("unmarshal manifest %s: %w", id, err)
	}
	return &ps, nil
}

// AppendMessage appends a PersistedMessage as a JSONL line to the session's
// messages.jsonl file.
func (s *Store) AppendMessage(sessionID string, msg *PersistedMessage) error {
	path := filepath.Join(s.sessionDir(sessionID), "messages.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open jsonl: %w", err)
	}
	defer f.Close()
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

// ReadMessages reads all PersistedMessages from the session's messages.jsonl.
// Returns nil, nil if the file does not exist.
// Corrupt lines are silently skipped.
func (s *Store) ReadMessages(sessionID string) ([]*PersistedMessage, error) {
	path := filepath.Join(s.sessionDir(sessionID), "messages.jsonl")
	return readPersistedJSONL(path)
}

// ReadLastN returns the last n PersistedMessages from the session's JSONL file.
// If fewer than n messages exist, all messages are returned.
// If n <= 0, an empty slice is returned.
func (s *Store) ReadLastN(sessionID string, n int) ([]*PersistedMessage, error) {
	if n <= 0 {
		return []*PersistedMessage{}, nil
	}
	all, err := s.ReadMessages(sessionID)
	if err != nil {
		return nil, err
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// readPersistedJSONL reads and parses a PersistedMessage JSONL file, skipping
// corrupt lines. Returns nil, nil if the file does not exist.
func readPersistedJSONL(path string) ([]*PersistedMessage, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var msgs []*PersistedMessage
	// Walk lines manually to avoid bufio.Scanner 64 KB default limit.
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[start:i]
			start = i + 1
			// Trim any trailing \r
			for len(line) > 0 && (line[len(line)-1] == '\r' || line[len(line)-1] == ' ' || line[len(line)-1] == '\t') {
				line = line[:len(line)-1]
			}
			if len(line) == 0 {
				continue
			}
			var msg PersistedMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				continue // skip corrupt lines
			}
			msgs = append(msgs, &msg)
		}
	}
	return msgs, nil
}

// RepairJSONL scans the session's messages.jsonl and truncates the file at the
// last valid JSON line, removing any partially-written trailing data.
// Returns the number of valid lines retained.
// Returns 0, nil if the file does not exist.
func (s *Store) RepairJSONL(sessionID string) (int, error) {
	path := filepath.Join(s.sessionDir(sessionID), "messages.jsonl")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	// Walk lines and track byte offset of last valid line end.
	validCount := 0
	validEnd := 0
	offset := 0
	for offset <= len(data) {
		// Find end of current line.
		end := offset
		for end < len(data) && data[end] != '\n' {
			end++
		}
		lineEnd := end
		if end < len(data) {
			lineEnd = end + 1 // include the newline
		}

		// Trim the line content for JSON validation.
		line := data[offset:end]
		for len(line) > 0 && (line[len(line)-1] == '\r' || line[len(line)-1] == ' ') {
			line = line[:len(line)-1]
		}

		if len(line) > 0 {
			if json.Valid(line) {
				validCount++
				validEnd = lineEnd
			}
			// If not valid JSON and non-empty, stop — it's corrupt trailing data.
		}

		offset = lineEnd
		if offset >= len(data) {
			break
		}
	}

	if validEnd < len(data) {
		return validCount, os.Truncate(path, int64(validEnd))
	}
	return validCount, nil
}

// UpdateManifest loads the PersistentSession manifest for sessionID, calls fn
// to mutate it, sets UpdatedAt = now, then atomically rewrites it.
// The method is safe for concurrent use.
func (s *Store) UpdateManifest(sessionID string, fn func(*PersistentSession)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ps, err := s.LoadManifest(sessionID)
	if err != nil {
		return err
	}
	fn(ps)
	ps.UpdatedAt = time.Now().UTC()
	return s.writePersistentManifest(ps)
}
