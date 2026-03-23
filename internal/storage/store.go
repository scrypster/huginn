package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/logger"
)

// safeGet wraps pebble's Get to catch panics (e.g. on a closed DB) and return
// them as errors, preventing the process from crashing on unexpected DB state.
func safeGet(db *pebble.DB, key []byte) (val []byte, closer io.Closer, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pebble panic on Get: %v", r)
		}
	}()
	return db.Get(key)
}

// safeNewIter wraps pebble's NewIter to catch panics similarly.
func safeNewIter(db *pebble.DB, opts *pebble.IterOptions) (iter *pebble.Iterator, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pebble panic on NewIter: %v", r)
		}
	}()
	return db.NewIter(opts)
}

// Store is a Pebble-backed content-addressed KV store for file intelligence.
type Store struct {
	mu     sync.RWMutex
	db     *pebble.DB
	path   string
	closed bool
}

// Open opens (or creates) a Pebble store at dir.
// On failure, logs a warning and returns nil with error.
func Open(dir string) (*Store, error) {
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	dbPath := filepath.Join(dir, "huginn.pebble")

	// Open Pebble DB
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble store at %s: %w", dbPath, err)
	}

	return &Store{
		db:   db,
		path: dbPath,
	}, nil
}

// isClosed checks if the store is closed without holding the lock for other ops.
func (s *Store) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db == nil || s.closed
}

// Close closes the underlying Pebble DB. Safe to call multiple times.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// DB returns the underlying Pebble database. Used by packages that need
// direct Pebble access (e.g., radar.Evaluate).
func (s *Store) DB() *pebble.DB {
	if s.isClosed() {
		return nil
	}
	return s.db
}

// GetGitHead returns the stored git HEAD SHA, or "" if not set.
func (s *Store) GetGitHead() string {
	if s.isClosed() {
		return ""
	}

	val, closer, err := safeGet(s.db, keyMetaGitHead())
	if err != nil {
		if err == pebble.ErrNotFound {
			return ""
		}
		logger.Warn("storage: read git head", "err", err)
		return ""
	}
	defer closer.Close()

	return string(val)
}

// SetGitHead stores the git HEAD SHA.
func (s *Store) SetGitHead(sha string) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	return s.db.Set(keyMetaGitHead(), []byte(sha), &pebble.WriteOptions{Sync: true})
}

// GetFileHash returns the stored SHA-256 for path, or "" if not present.
func (s *Store) GetFileHash(path string) string {
	if s.isClosed() {
		return ""
	}

	val, closer, err := safeGet(s.db, keyFileHash(path))
	if err != nil {
		if err == pebble.ErrNotFound {
			return ""
		}
		logger.Warn("storage: read file hash", "path", path, "err", err)
		return ""
	}
	defer closer.Close()

	return string(val)
}

// SetFileRecord stores the full file record (hash, parser version, indexed_at).
func (s *Store) SetFileRecord(rec FileRecord) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// Store hash
	if err := batch.Set(keyFileHash(rec.Path), []byte(rec.Hash), nil); err != nil {
		return fmt.Errorf("failed to set file hash: %w", err)
	}

	// Store parser version
	if err := batch.Set(keyFileParserVersion(rec.Path), []byte(strconv.Itoa(rec.ParserVersion)), nil); err != nil {
		return fmt.Errorf("failed to set parser version: %w", err)
	}

	// Store indexed_at timestamp
	if err := batch.Set(keyFileIndexedAt(rec.Path), []byte(rec.IndexedAt.Format(time.RFC3339)), nil); err != nil {
		return fmt.Errorf("failed to set indexed_at: %w", err)
	}

	// Commit batch with sync for durability
	return batch.Commit(&pebble.WriteOptions{Sync: true})
}

// GetFileRecord returns the stored FileRecord for path, or zero value if missing.
func (s *Store) GetFileRecord(path string) FileRecord {
	if s.isClosed() {
		return FileRecord{}
	}

	rec := FileRecord{Path: path}

	// Get hash
	val, closer, err := safeGet(s.db, keyFileHash(path))
	if err == nil {
		rec.Hash = string(val)
		closer.Close()
	}

	// Get parser version
	val, closer, err = safeGet(s.db, keyFileParserVersion(path))
	if err == nil {
		if pv, err := strconv.Atoi(string(val)); err == nil {
			rec.ParserVersion = pv
		}
		closer.Close()
	}

	// Get indexed_at
	val, closer, err = safeGet(s.db, keyFileIndexedAt(path))
	if err == nil {
		if t, err := time.Parse(time.RFC3339, string(val)); err == nil {
			rec.IndexedAt = t
		}
		closer.Close()
	}

	return rec
}

// SetSymbols stores JSON-encoded symbols for path.
func (s *Store) SetSymbols(path string, symbols []Symbol) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	data, err := json.Marshal(symbols)
	if err != nil {
		return fmt.Errorf("failed to marshal symbols: %w", err)
	}

	return s.db.Set(keyFileSymbols(path), data, &pebble.WriteOptions{Sync: true})
}

// GetSymbols returns stored symbols for path.
func (s *Store) GetSymbols(path string) []Symbol {
	if s.isClosed() {
		return []Symbol{}
	}

	val, closer, err := safeGet(s.db, keyFileSymbols(path))
	if err != nil {
		if err == pebble.ErrNotFound {
			return []Symbol{}
		}
		logger.Warn("storage: read symbols", "path", path, "err", err)
		return []Symbol{}
	}
	defer closer.Close()

	var symbols []Symbol
	if err := json.Unmarshal(val, &symbols); err != nil {
		logger.Warn("storage: unmarshal symbols", "path", path, "err", err)
		return []Symbol{}
	}

	return symbols
}

// SetChunks stores JSON-encoded chunks for path.
func (s *Store) SetChunks(path string, chunks []FileChunk) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	data, err := json.Marshal(chunks)
	if err != nil {
		return fmt.Errorf("failed to marshal chunks: %w", err)
	}

	return s.db.Set(keyFileChunks(path), data, &pebble.WriteOptions{Sync: true})
}

// GetChunks returns stored chunks for path.
func (s *Store) GetChunks(path string) []FileChunk {
	if s.isClosed() {
		return []FileChunk{}
	}

	val, closer, err := safeGet(s.db, keyFileChunks(path))
	if err != nil {
		if err == pebble.ErrNotFound {
			return []FileChunk{}
		}
		logger.Warn("storage: read chunks", "path", path, "err", err)
		return []FileChunk{}
	}
	defer closer.Close()

	var chunks []FileChunk
	if err := json.Unmarshal(val, &chunks); err != nil {
		logger.Warn("storage: unmarshal chunks", "path", path, "err", err)
		return []FileChunk{}
	}

	return chunks
}

// SetEdge stores an edge between two files.
// It writes both the forward key (edge:from:to) and the reverse index key
// (edgeto:to:from) so GetEdgesTo can use a fast prefix scan.
func (s *Store) SetEdge(from, to string, edge Edge) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	data, err := json.Marshal(edge)
	if err != nil {
		return fmt.Errorf("failed to marshal edge: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(keyEdge(from, to), data, nil); err != nil {
		return fmt.Errorf("failed to set edge: %w", err)
	}
	// Reverse index: store the from path as value so GetEdgesTo can resolve it.
	if err := batch.Set(keyEdgeTo(to, from), []byte(from), nil); err != nil {
		return fmt.Errorf("failed to set reverse edge: %w", err)
	}
	return batch.Commit(&pebble.WriteOptions{Sync: true})
}

// DeleteEdge removes the edge from→to (both forward and reverse index keys).
// No-op if the edge doesn't exist.
func (s *Store) DeleteEdge(from, to string) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := batch.Delete(keyEdge(from, to), nil); err != nil {
		return fmt.Errorf("failed to delete edge: %w", err)
	}
	if err := batch.Delete(keyEdgeTo(to, from), nil); err != nil {
		return fmt.Errorf("failed to delete reverse edge: %w", err)
	}
	return batch.Commit(&pebble.WriteOptions{Sync: true})
}

// GetEdgesFrom returns all edges where From == path.
// Scans keys with prefix "edge:<path>:".
func (s *Store) GetEdgesFrom(path string) []Edge {
	if s.isClosed() {
		return []Edge{}
	}

	var edges []Edge
	prefix := keyEdgePrefix(path)

	iter, err := safeNewIter(s.db, &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		logger.Warn("storage: create edges-from iterator", "path", path, "err", err)
		return edges
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var edge Edge
		if err := json.Unmarshal(iter.Value(), &edge); err != nil {
			logger.Warn("storage: unmarshal edge", "err", err)
			continue
		}
		edges = append(edges, edge)
	}
	if err := iter.Error(); err != nil {
		logger.Warn("storage: iterate edges-from", "path", path, "err", err)
	}

	return edges
}

// GetEdgesTo returns all edges where To == path.
// Uses the reverse index (edgeto:path:*) for an O(k) prefix scan.
func (s *Store) GetEdgesTo(path string) []Edge {
	if s.isClosed() {
		return []Edge{}
	}

	var edges []Edge
	prefix := keyEdgeToPrefix(path)

	iter, err := safeNewIter(s.db, &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		logger.Warn("storage: create edges-to iterator", "path", path, "err", err)
		return edges
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		// The reverse index value is the "from" path.
		from := string(iter.Value())
		// Look up the actual edge data from the forward index.
		val, closer, err := safeGet(s.db, keyEdge(from, path))
		if err != nil {
			if err != pebble.ErrNotFound {
				logger.Warn("storage: get edge from reverse index", "from", from, "to", path, "err", err)
			}
			continue
		}
		var edge Edge
		if err := json.Unmarshal(val, &edge); err != nil {
			closer.Close()
			logger.Warn("storage: unmarshal edge", "err", err)
			continue
		}
		closer.Close()
		edges = append(edges, edge)
	}
	if err := iter.Error(); err != nil {
		logger.Warn("storage: iterate edges-to", "path", path, "err", err)
	}

	return edges
}

// GetAllEdges returns all stored edges. For /impact symbol lookups.
func (s *Store) GetAllEdges() []Edge {
	if s.isClosed() {
		return nil
	}
	prefix := []byte("edge:")
	iter, err := safeNewIter(s.db, &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return nil
	}
	defer iter.Close()
	var edges []Edge
	for iter.First(); iter.Valid(); iter.Next() {
		var e Edge
		if err := json.Unmarshal(iter.Value(), &e); err != nil {
			continue
		}
		edges = append(edges, e)
	}
	return edges
}

// SetWorkspaceSummary stores the workspace summary.
func (s *Store) SetWorkspaceSummary(ws WorkspaceSummary) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	data, err := json.Marshal(ws)
	if err != nil {
		return fmt.Errorf("failed to marshal workspace summary: %w", err)
	}

	return s.db.Set(keyWSSummary(), data, &pebble.WriteOptions{Sync: true})
}

// GetWorkspaceSummary returns the stored workspace summary.
func (s *Store) GetWorkspaceSummary() (WorkspaceSummary, bool) {
	if s.isClosed() {
		return WorkspaceSummary{}, false
	}

	val, closer, err := safeGet(s.db, keyWSSummary())
	if err != nil {
		if err == pebble.ErrNotFound {
			return WorkspaceSummary{}, false
		}
		logger.Warn("storage: read workspace summary", "err", err)
		return WorkspaceSummary{}, false
	}
	defer closer.Close()

	var ws WorkspaceSummary
	if err := json.Unmarshal(val, &ws); err != nil {
		logger.Warn("storage: unmarshal workspace summary", "err", err)
		return WorkspaceSummary{}, false
	}

	return ws, true
}

// Invalidate marks file records as stale by deleting hash key for each path.
// Called by the patch layer after applying diffs so next context build re-indexes.
func (s *Store) Invalidate(paths []string) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	for _, path := range paths {
		if err := batch.Delete(keyFileHash(path), nil); err != nil {
			return fmt.Errorf("failed to invalidate %s: %w", path, err)
		}
	}

	return batch.Commit(&pebble.WriteOptions{Sync: true})
}

// Compact triggers a manual compaction of the Pebble store.
// This is a no-op in tests and useful after bulk deletes.
func (s *Store) Compact() error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}
	// Compact the entire key space.
	return s.db.Compact(context.Background(), []byte{0x00}, []byte{0xff}, true)
}

// incrementLastByte returns a byte slice that is the exclusive upper bound
// for a Pebble prefix scan. It increments the last byte, carrying over on 0xFF.
func incrementLastByte(b []byte) []byte {
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end
		}
	}
	return append(b, 0x00)
}

// DeleteFileRecords removes all keys for a path (hash, symbols, chunks, indexed_at, parser_version).
func (s *Store) DeleteFileRecords(path string) error {
	if s.isClosed() {
		return fmt.Errorf("storage: store is closed")
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	keys := [][]byte{
		keyFileHash(path),
		keyFileParserVersion(path),
		keyFileSymbols(path),
		keyFileChunks(path),
		keyFileIndexedAt(path),
	}

	for _, key := range keys {
		if err := batch.Delete(key, nil); err != nil {
			return fmt.Errorf("failed to delete file record for %s: %w", path, err)
		}
	}

	return batch.Commit(&pebble.WriteOptions{Sync: true})
}
