package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// validateID returns an error if id is empty, too long, or contains
// path-traversal / non-allowlisted characters.
// Session and thread IDs are generated internally (ULIDs) and should only
// contain alphanumerics plus _-.
// Max length 128 chars provides a generous headroom above ULID (26 chars)
// while bounding any path-based operations the OS or file layer performs.
func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("session: id must not be empty")
	}
	if len(id) > 128 {
		return fmt.Errorf("session: id too long: %d chars (max 128)", len(id))
	}
	// Reject any ID containing ".." — it forms a path traversal segment.
	if strings.Contains(id, "..") {
		return fmt.Errorf("session: id must not contain path traversal sequence")
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			return fmt.Errorf("session: id contains invalid character %q", c)
		}
	}
	return nil
}

// DefaultMaxMessagesPerSession is the default cap on messages persisted per session.
// Very long sessions can exhaust disk and memory; when the limit is exceeded,
// the oldest messages are trimmed from the JSONL file on the next Append.
const DefaultMaxMessagesPerSession = 10000

// maxMessageContentBytes is the maximum allowed size for a single message's
// Content field. Messages exceeding this limit are rejected by Append.
const maxMessageContentBytes = 64 * 1024

// Store manages session directories at baseDir.
type Store struct {
	baseDir              string
	mu                   sync.Mutex // guards manifest update operations in SaveManifest
	MaxMessagesPerSession int        // 0 means use DefaultMaxMessagesPerSession

	// appendMu is a per-session-ID mutex map that serialises concurrent JSONL
	// appends for the same session. Using sync.Map (lazy init, never evicts
	// on its own) means we get one *sync.Mutex per session ID without a global
	// lock on every write. Entries are removed in Delete() when the session
	// directory is removed to prevent unbounded growth.
	appendMu sync.Map // map[string]*sync.Mutex
}

// sessionAppendMu returns (creating if needed) the mutex that serialises JSONL
// writes for the given session ID. Safe for concurrent use.
func (s *Store) sessionAppendMu(id string) *sync.Mutex {
	v, _ := s.appendMu.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// NewStore creates a Store backed by baseDir (e.g. ~/.huginn/sessions).
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir:               baseDir,
		MaxMessagesPerSession: DefaultMaxMessagesPerSession,
	}
}

// maxMessages returns the effective per-session message cap.
func (s *Store) maxMessages() int {
	if s.MaxMessagesPerSession > 0 {
		return s.MaxMessagesPerSession
	}
	return DefaultMaxMessagesPerSession
}

// trimMessagesIfNeeded rewrites the JSONL file to keep only the newest
// maxMessages entries. Called after Append when the count exceeds the cap.
// The trim is best-effort — failures are logged but not surfaced to callers.
func (s *Store) trimMessagesIfNeeded(id string) {
	limit := s.maxMessages()
	all, err := s.TailMessages(id, limit+1) // read one extra to detect whether trim is needed
	if err != nil || len(all) <= limit {
		return // nothing to do
	}
	// Keep only the newest `limit` messages.
	keep := all[len(all)-limit:]
	tmp := s.jsonlPath(id) + ".trim"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		slog.Error("session: trim messages failed: open temp file", "session_id", id, "err", err)
		return
	}
	// Ensure the file is closed even if encoding panics.
	enc := json.NewEncoder(f)
	for _, m := range keep {
		if encErr := enc.Encode(m); encErr != nil {
			f.Close()
			os.Remove(tmp) // clean up partial file
			slog.Error("session: trim messages failed: encode", "session_id", id, "err", encErr)
			return
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		slog.Error("session: trim messages failed: close temp file", "session_id", id, "err", err)
		return
	}
	if err := os.Rename(tmp, s.jsonlPath(id)); err != nil {
		os.Remove(tmp)
		slog.Error("session: trim messages failed: rename", "session_id", id, "err", err)
	}
}

func (s *Store) sessionDir(id string) string {
	return filepath.Join(s.baseDir, id)
}

func (s *Store) jsonlPath(id string) string {
	return filepath.Join(s.sessionDir(id), "messages.jsonl")
}

func (s *Store) manifestPath(id string) string {
	return filepath.Join(s.sessionDir(id), "manifest.json")
}

// New creates a new Session in memory (does not write to disk yet).
func (s *Store) New(title, workspaceRoot, model string) *Session {
	id := NewID()
	now := time.Now().UTC()
	wname := filepath.Base(workspaceRoot)
	if wname == "." || wname == "" {
		wname = workspaceRoot
	}
	return &Session{
		ID: id,
		Manifest: Manifest{
			SessionID:     id,
			ID:            id,
			Title:         title,
			Model:         model,
			WorkspaceRoot: workspaceRoot,
			WorkspaceName: wname,
			CreatedAt:     now,
			UpdatedAt:     now,
			Status:        "active",
			Version:       1,
		},
	}
}

// Append writes a single message to messages.jsonl (append-only).
// The per-session mutex (sessionAppendMu) serialises concurrent callers for the
// same session ID, ensuring JSONL lines are never interleaved even when multiple
// goroutines (e.g. parallel tool calls) write simultaneously.
func (s *Store) Append(sess *Session, msg SessionMessage) error {
	if len(msg.Content) > maxMessageContentBytes {
		return fmt.Errorf("session: message content exceeds %d byte limit (%d bytes)", maxMessageContentBytes, len(msg.Content))
	}

	// Acquire the per-session append mutex before any file I/O.
	mu := s.sessionAppendMu(sess.ID)
	mu.Lock()
	defer mu.Unlock()

	dir := s.sessionDir(sess.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("session mkdir: %w", err)
	}
	msg.ID = NewID()
	msg.Seq = atomic.AddInt64(&sess.seq, 1)
	if msg.Ts.IsZero() {
		msg.Ts = time.Now().UTC()
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(s.jsonlPath(sess.ID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open jsonl: %w", err)
	}
	defer f.Close()
	_, err = f.Write(line)
	if err != nil {
		return fmt.Errorf("write jsonl: %w", err)
	}
	sess.mu.Lock()
	sess.Manifest.MessageCount++
	sess.Manifest.LastMessageID = msg.ID
	count := sess.Manifest.MessageCount
	sess.mu.Unlock()

	// Trim oldest messages in a background goroutine to avoid blocking the caller.
	// Only trigger trim when the count is a multiple of 100 to amortise the cost.
	if count > 0 && count%100 == 0 && count > s.maxMessages() {
		go s.trimMessagesIfNeeded(sess.ID)
	}
	return nil
}

// SaveManifest atomically writes manifest.json.
// The store-level mutex serialises concurrent writes for the same session so
// that two goroutines cannot interleave their tmp-write/rename operations.
// The session mutex is held while snapshotting the manifest fields to avoid
// racing with concurrent Append calls that mutate MessageCount/LastMessageID.
func (s *Store) SaveManifest(sess *Session) error {
	// Snapshot the manifest under the session lock, then release it before I/O.
	sess.mu.Lock()
	sess.Manifest.UpdatedAt = time.Now().UTC()
	snapshot := sess.Manifest
	sess.mu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	dir := s.sessionDir(sess.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := s.manifestPath(sess.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.manifestPath(sess.ID))
}

// Exists reports whether the session directory exists on disk.
func (s *Store) Exists(id string) bool {
	if validateID(id) != nil {
		return false
	}
	_, err := os.Stat(s.sessionDir(id))
	return err == nil
}

// Load reads a session's manifest from disk.
// Safe defaults are applied for fields added in newer schema versions so that
// sessions written by older builds do not cause nil-pointer dereferences.
func (s *Store) Load(id string) (*Session, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.manifestPath(id))
	if err != nil {
		return nil, fmt.Errorf("load manifest %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", id, err)
	}
	// Apply safe defaults for fields that may be absent in older manifests.
	if m.SessionID == "" {
		m.SessionID = id
	}
	if m.ID == "" {
		m.ID = id
	}
	if m.Status == "" {
		m.Status = "active"
	}
	if m.Version == 0 {
		m.Version = 1
	}
	return &Session{ID: id, Manifest: m}, nil
}

// LoadOrReconstruct tries to load a session from its manifest. If the manifest
// is missing or corrupt, it reconstructs a partial session by counting lines in
// the JSONL file and using the session ID as a fallback title.
func (s *Store) LoadOrReconstruct(id string) (*Session, error) {
	sess, err := s.Load(id)
	if err == nil {
		return sess, nil
	}

	// Manifest unreadable — reconstruct from JSONL.
	msgs, jsonlErr := s.TailMessages(id, 1<<30) // request all messages
	if jsonlErr != nil {
		return nil, fmt.Errorf("manifest corrupt and jsonl unreadable for %s: manifest=%w", id, err)
	}
	count := len(msgs)
	lastID := ""
	if count > 0 {
		lastID = msgs[count-1].ID
	}
	return &Session{
		ID: id,
		Manifest: Manifest{
			SessionID:     id,
			ID:            id,
			Title:         "(recovered) " + id,
			MessageCount:  count,
			LastMessageID: lastID,
			Status:        "active",
			Version:       1,
		},
	}, nil
}

// TailMessages returns the last n messages from messages.jsonl.
// Repairs corrupt trailing lines before reading.
func (s *Store) TailMessages(id string, n int) ([]SessionMessage, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	path := s.jsonlPath(id)
	if err := repairJSONL(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []SessionMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg SessionMessage
		if json.Unmarshal([]byte(line), &msg) == nil {
			all = append(all, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("session: scan messages %s: %w", id, err)
	}
	if n <= 0 {
		return []SessionMessage{}, nil
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// TailMessagesBefore returns the last n messages from messages.jsonl where seq < beforeSeq.
// NOTE: filesystem store reads the entire JSONL into memory; use the SQLite backend for large sessions.
func (s *Store) TailMessagesBefore(id string, n int, beforeSeq int64) ([]SessionMessage, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	path := s.jsonlPath(id)
	if err := repairJSONL(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var candidates []SessionMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg SessionMessage
		if json.Unmarshal([]byte(line), &msg) == nil && msg.Seq < beforeSeq {
			candidates = append(candidates, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("session: scan messages %s: %w", id, err)
	}
	if n <= 0 {
		return []SessionMessage{}, nil
	}
	if len(candidates) <= n {
		return candidates, nil
	}
	return candidates[len(candidates)-n:], nil
}

// threadJSONLPath returns the path for a thread's JSONL file within the session directory.
// Files are named "thread-{threadID}.jsonl" to distinguish them from the main messages.jsonl.
func (s *Store) threadJSONLPath(sessionID, threadID string) string {
	return filepath.Join(s.sessionDir(sessionID), "thread-"+threadID+".jsonl")
}

// AppendToThread appends a single message to a thread's JSONL file.
// Creates the session directory and file if they don't exist.
// Assigns a new ID and timestamps the message if Ts is zero.
func (s *Store) AppendToThread(sessionID, threadID string, msg SessionMessage) error {
	if err := validateID(sessionID); err != nil {
		return err
	}
	if err := validateID(threadID); err != nil {
		return err
	}
	dir := s.sessionDir(sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("session mkdir: %w", err)
	}
	msg.ID = NewID()
	if msg.Ts.IsZero() {
		msg.Ts = time.Now().UTC()
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal thread message: %w", err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(s.threadJSONLPath(sessionID, threadID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open thread jsonl: %w", err)
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// TailThreadMessages returns the last n messages from a thread's JSONL file.
// Returns nil, nil if the thread has no JSONL file yet (not an error).
func (s *Store) TailThreadMessages(sessionID, threadID string, n int) ([]SessionMessage, error) {
	if err := validateID(sessionID); err != nil {
		return nil, err
	}
	if err := validateID(threadID); err != nil {
		return nil, err
	}
	path := s.threadJSONLPath(sessionID, threadID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []SessionMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg SessionMessage
		if json.Unmarshal([]byte(line), &msg) == nil {
			all = append(all, msg)
		}
	}
	if n <= 0 {
		return []SessionMessage{}, nil
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// ListThreadIDs returns the IDs of all threads that have JSONL files in the session directory.
// Returns an empty (not nil) slice if no thread files exist.
func (s *Store) ListThreadIDs(sessionID string) ([]string, error) {
	entries, err := os.ReadDir(s.sessionDir(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "thread-") && strings.HasSuffix(name, ".jsonl") {
			id := strings.TrimPrefix(name, "thread-")
			id = strings.TrimSuffix(id, ".jsonl")
			ids = append(ids, id)
		}
	}
	if ids == nil {
		return []string{}, nil
	}
	return ids, nil
}

// List returns session manifests sorted by UpdatedAt descending (newest first).
func (s *Store) List() ([]Manifest, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var manifests []Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mpath := filepath.Join(s.baseDir, e.Name(), "manifest.json")
		data, err := os.ReadFile(mpath)
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(data, &m) == nil {
			manifests = append(manifests, m)
		}
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].UpdatedAt.After(manifests[j].UpdatedAt)
	})
	return manifests, nil
}

// Delete removes a session directory entirely.
// It also evicts the per-session append mutex from appendMu to prevent
// unbounded sync.Map growth over the lifetime of the process.
func (s *Store) Delete(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	if err := os.RemoveAll(s.sessionDir(id)); err != nil {
		return err
	}
	s.appendMu.Delete(id)
	return nil
}

// ArchiveSession sets the session status to "archived" by updating the manifest.
// The session data (messages, threads) is preserved on disk; it is simply hidden
// from normal List() results. Use Delete() for permanent removal.
func (s *Store) ArchiveSession(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	sess, err := s.Load(id)
	if err != nil {
		return fmt.Errorf("session: archive %s: %w", id, err)
	}
	sess.Manifest.Status = "archived"
	return s.SaveManifest(sess)
}

// ListFiltered returns session manifests sorted by UpdatedAt descending, applying
// the given filter. By default (IncludeArchived: false) archived sessions are
// excluded so they don't clutter the session list.
func (s *Store) ListFiltered(filter SessionFilter) ([]Manifest, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	if filter.IncludeArchived {
		return all, nil
	}
	out := all[:0]
	for _, m := range all {
		if m.Status != "archived" {
			out = append(out, m)
		}
	}
	return out, nil
}

// SearchSessions is not supported by the filesystem-backed Store.
// It always returns an empty slice so callers can degrade gracefully.
// FTS search is only available when using SQLiteSessionStore.
func (s *Store) SearchSessions(_ string) ([]Manifest, error) {
	return []Manifest{}, nil
}

// Compile-time assertion: *Store must satisfy StoreInterface.
var _ StoreInterface = (*Store)(nil)
