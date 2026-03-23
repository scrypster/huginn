// internal/session/store_iface.go
package session

// SessionFilter controls which sessions are returned by ListFiltered.
// The zero value excludes archived sessions (IncludeArchived: false).
type SessionFilter struct {
	// IncludeArchived includes sessions with status="archived" when true.
	// Default (false): archived sessions are excluded from the list.
	IncludeArchived bool
}

// StoreInterface is the full contract for a session store.
// Both *Store (filesystem-backed) and *SQLiteSessionStore (SQLite-backed) implement it.
type StoreInterface interface {
	// --- Core session lifecycle ---
	New(title, workspaceRoot, model string) *Session
	Append(sess *Session, msg SessionMessage) error
	SaveManifest(sess *Session) error
	Exists(id string) bool
	Load(id string) (*Session, error)
	LoadOrReconstruct(id string) (*Session, error)
	Delete(id string) error
	List() ([]Manifest, error)
	// ListFiltered returns sessions matching the given filter.
	// Pass SessionFilter{} (zero value) to exclude archived sessions.
	// Pass SessionFilter{IncludeArchived: true} to include them.
	ListFiltered(filter SessionFilter) ([]Manifest, error)
	// ArchiveSession sets the session status to "archived" without deleting data.
	ArchiveSession(id string) error

	// --- Messages ---
	TailMessages(id string, n int) ([]SessionMessage, error)
	// TailMessagesBefore returns the last n messages with seq < beforeSeq,
	// in ascending seq order. Used for reverse pagination of session history.
	TailMessagesBefore(id string, n int, beforeSeq int64) ([]SessionMessage, error)

	// --- Threads ---
	AppendToThread(sessionID, threadID string, msg SessionMessage) error
	TailThreadMessages(sessionID, threadID string, n int) ([]SessionMessage, error)
	ListThreadIDs(sessionID string) ([]string, error)

	// --- Persist layer (used by routine runner and cloud sync) ---
	Create(ps *PersistentSession) error
	LoadManifest(id string) (*PersistentSession, error)
	AppendMessage(sessionID string, msg *PersistedMessage) error
	ReadMessages(sessionID string) ([]*PersistedMessage, error)
	ReadLastN(sessionID string, n int) ([]*PersistedMessage, error)
	RepairJSONL(sessionID string) (int, error)
	UpdateManifest(sessionID string, fn func(*PersistentSession)) error

	// --- Full-text search ---
	// SearchSessions queries the sessions_fts FTS5 index using MATCH syntax.
	// Returns an empty slice (never nil) when no sessions match.
	SearchSessions(query string) ([]Manifest, error)
}
