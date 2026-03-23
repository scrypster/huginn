package connections

import "time"

// StoreInterface is the read/write contract for a connections store.
// Both *Store (JSON-backed) and *SQLiteConnectionStore (SQLite-backed) implement it.
type StoreInterface interface {
	List() ([]Connection, error)
	ListByProvider(p Provider) ([]Connection, error)
	Get(id string) (Connection, bool)
	Add(conn Connection) error
	Remove(id string) error
	UpdateExpiry(id string, expiresAt time.Time) error
	// SetDefault makes the given connection the first (default) for its provider.
	SetDefault(id string) error
	// UpdateRefreshError persists the last refresh failure state for a connection.
	// Pass empty errMsg to clear a previously recorded error (success path).
	UpdateRefreshError(id string, errMsg string) error
}

// Compile-time assertion: *Store must satisfy StoreInterface.
var _ StoreInterface = (*Store)(nil)
