package claudecode

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/scrypster/huginn/internal/session"
)

// ExternalKind is the value written to Manifest.ExternalKind for sessions
// bridged from Claude Code.
const ExternalKind = "claude-code"

// SessionSink is the slice of the session store the ingester needs. It is an
// interface so tests can substitute a fake without a database.
type SessionSink interface {
	LoadByExternalID(kind, externalID string) (*session.Session, error)
	New(title, workspaceRoot, model string) *session.Session
	SaveManifest(s *session.Session) error
	Append(s *session.Session, m session.SessionMessage) error
	AppendToThread(sessionID, threadID string, m session.SessionMessage) error
}

// Broadcaster pushes live updates to connected web UI clients. *server.Server
// satisfies this.
type Broadcaster interface {
	BroadcastToSession(sessionID, msgType string, payload map[string]any)
}

// Ingester converts Claude Code transcripts into Huginn sessions.
//
// Two pieces of per-session state make tool results work. Both exist because a
// tool_use block and the tool_result that completes it routinely arrive in
// DIFFERENT reads: a tool takes longer to run than the watcher's 150ms debounce.
//
//   - mappers retains each session's Mapper across IngestFile calls. A fresh
//     Mapper per call would forget every open tool call between reads.
//   - pending holds messages whose tool calls are not yet resolved. Messages are
//     appended to the store one batch LATE, after the following read has had a
//     chance to fill in their results. Appending eagerly writes the message to
//     storage before its results exist, and the Mapper's later in-place fill-in
//     never reaches the row that was already written.
type Ingester struct {
	sink  SessionSink
	state *IngestStore
	bc    Broadcaster

	mu      sync.Mutex
	mappers map[string]*Mapper
	pending map[string][]Mapped
	fileMu  map[string]*sync.Mutex
}

// NewIngester wires an ingester. bc may be nil (backfill runs without a web UI).
func NewIngester(sink SessionSink, state *IngestStore, bc Broadcaster) *Ingester {
	return &Ingester{
		sink:    sink,
		state:   state,
		bc:      bc,
		mappers: map[string]*Mapper{},
		pending: map[string][]Mapped{},
		fileMu:  map[string]*sync.Mutex{},
	}
}

// mapperFor returns the retained Mapper for a Claude session, creating it on
// first use. Backfill calls IngestFile concurrently across distinct sessions,
// so the map is mutex-guarded.
func (i *Ingester) mapperFor(externalID string) *Mapper {
	i.mu.Lock()
	defer i.mu.Unlock()
	m, ok := i.mappers[externalID]
	if !ok {
		m = NewMapper()
		i.mappers[externalID] = m
	}
	return m
}

// lockFor returns the mutex serialising work for one Claude session. Two
// concurrent IngestFile calls for the same transcript would share one retained
// Mapper, whose internal state is deliberately unguarded, and race on it. The
// watcher can produce exactly that overlap: it clears a path's debounce entry
// before ingesting, so a write arriving mid-ingest schedules a second pass.
func (i *Ingester) lockFor(externalID string) *sync.Mutex {
	i.mu.Lock()
	defer i.mu.Unlock()
	m, ok := i.fileMu[externalID]
	if !ok {
		m = &sync.Mutex{}
		i.fileMu[externalID] = m
	}
	return m
}

// takePending removes and returns the messages held back from the previous read.
func (i *Ingester) takePending(externalID string) []Mapped {
	i.mu.Lock()
	defer i.mu.Unlock()
	held := i.pending[externalID]
	delete(i.pending, externalID)
	return held
}

// holdPending stores messages whose tool calls are still unresolved.
func (i *Ingester) holdPending(externalID string, held []Mapped) {
	if len(held) == 0 {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.pending[externalID] = append(i.pending[externalID], held...)
}

// resolved reports whether every tool call on a message has a result, so the
// message is safe to persist.
func resolved(m Mapped) bool {
	for _, c := range m.Msg.ToolCalls {
		if c.Result == "" {
			return false
		}
	}
	return true
}

// FlushIdle appends any messages still held back for sessions that have stopped
// producing output, so a conversation that ends on an unanswered tool call is
// not left unpersisted. The watcher calls this on its rescan tick.
func (i *Ingester) FlushIdle() {
	i.mu.Lock()
	ids := make([]string, 0, len(i.pending))
	for id := range i.pending {
		ids = append(ids, id)
	}
	i.mu.Unlock()

	for _, id := range ids {
		fl := i.lockFor(id)
		fl.Lock()
		func() {
			defer fl.Unlock()
			sess, err := i.sink.LoadByExternalID(ExternalKind, id)
			if err != nil {
				slog.Warn("claudecode: FlushIdle could not load session, pending messages not flushed", "external_id", id, "err", err)
				return
			}
			if sess == nil {
				slog.Warn("claudecode: FlushIdle found no session for pending messages", "external_id", id)
				return
			}
			for _, m := range i.takePending(id) {
				i.appendOne(sess, m)
			}
		}()
	}
}

// IngestFile consumes everything appended to a transcript since the last call
// and returns the number of messages appended. It is safe to call repeatedly;
// an unchanged file appends nothing.
func (i *Ingester) IngestFile(path string) (int, error) {
	externalID := sessionIDFromPath(path)
	if externalID == "" {
		return 0, nil
	}

	fl := i.lockFor(externalID)
	fl.Lock()
	defer fl.Unlock()

	sessionID, st, found, err := i.state.Get(externalID)
	if err != nil {
		return 0, err
	}
	if !found {
		st = TailState{Path: path}
	}
	_ = sessionID

	lines, next, err := ReadNew(path, st)
	if err != nil {
		return 0, err
	}
	if len(lines) == 0 {
		return 0, nil
	}

	sess, err := i.resolveSession(externalID, lines)
	if err != nil {
		return 0, err
	}

	mapper := i.mapperFor(externalID)
	var fresh []Mapped
	var appended int
	for _, raw := range lines {
		if title, ok := ParseTitle(raw); ok && sess.Manifest.Title != title {
			sess.Manifest.Title = title
		}
		l, ok := ParseLine(raw)
		if !ok {
			continue
		}
		if sess.Manifest.WorkspaceRoot == "" && l.CWD != "" {
			sess.Manifest.WorkspaceRoot = l.CWD
			sess.Manifest.WorkspaceName = filepath.Base(l.CWD)
		}
		fresh = append(fresh, mapper.Add(l)...)
	}

	// Order matters. Every line in this batch has now been through the Mapper,
	// so tool_results in this batch have already filled in the results of
	// messages held from the previous batch (the Mapper mutates the tool-call
	// slice those Mapped values share). Only now is it safe to persist them,
	// and they must go in before this batch's own messages to keep chronology.
	for _, m := range i.takePending(externalID) {
		if i.appendOne(sess, m) {
			appended++
		}
	}

	var hold []Mapped
	for _, m := range fresh {
		if !resolved(m) {
			hold = append(hold, m)
			continue
		}
		if i.appendOne(sess, m) {
			appended++
		}
	}
	i.holdPending(externalID, hold)

	if mdl := mapper.Model(); mdl != "" {
		sess.Manifest.Model = mdl
	}
	if err := i.sink.SaveManifest(sess); err != nil {
		return appended, fmt.Errorf("claudecode: save manifest: %w", err)
	}
	if err := i.state.Put(externalID, sess.ID, next); err != nil {
		return appended, err
	}
	return appended, nil
}

// resolveSession finds or creates the Huginn session for a Claude Code
// session id, seeding metadata from the first content line.
func (i *Ingester) resolveSession(externalID string, lines [][]byte) (*session.Session, error) {
	existing, err := i.sink.LoadByExternalID(ExternalKind, externalID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	title, cwd := seedMetadata(lines)
	s := i.sink.New(title, cwd, "")
	s.Manifest.ExternalKind = ExternalKind
	s.Manifest.ExternalID = externalID
	if cwd != "" {
		s.Manifest.WorkspaceRoot = cwd
		s.Manifest.WorkspaceName = filepath.Base(cwd)
	}
	if err := i.sink.SaveManifest(s); err != nil {
		return nil, fmt.Errorf("claudecode: create session: %w", err)
	}
	return s, nil
}

// seedMetadata scans the first lines for a title and a working directory.
// The custom-title line wins; otherwise the first user text becomes the title.
func seedMetadata(lines [][]byte) (title, cwd string) {
	for _, raw := range lines {
		if t, ok := ParseTitle(raw); ok && title == "" {
			title = t
		}
		l, ok := ParseLine(raw)
		if !ok {
			continue
		}
		if cwd == "" && l.CWD != "" {
			cwd = l.CWD
		}
		if title == "" && l.Type == "user" {
			m := NewMapper()
			for _, mapped := range m.Add(l) {
				title = firstLine(mapped.Msg.Content)
			}
		}
		if title != "" && cwd != "" {
			return title, cwd
		}
	}
	return title, cwd
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// appendOne persists a single mapped message and broadcasts it. It returns
// false if the store rejected it, which is logged but never fatal to the file.
func (i *Ingester) appendOne(sess *session.Session, m Mapped) bool {
	if m.ThreadID != "" {
		if err := i.sink.AppendToThread(sess.ID, m.ThreadID, m.Msg); err != nil {
			slog.Warn("claudecode: append to thread failed", "session", sess.ID, "err", err)
			return false
		}
	} else if err := i.sink.Append(sess, m.Msg); err != nil {
		slog.Warn("claudecode: append failed", "session", sess.ID, "err", err)
		return false
	}
	i.broadcast(sess.ID, m)
	return true
}

func (i *Ingester) broadcast(sessionID string, m Mapped) {
	if i.bc == nil {
		return
	}
	i.bc.BroadcastToSession(sessionID, "message_new", map[string]any{
		"role":      m.Msg.Role,
		"content":   m.Msg.Content,
		"thread_id": m.ThreadID,
		"source":    ExternalKind,
	})
}

// sessionIDFromPath extracts the Claude Code session UUID from a transcript
// filename. Returns "" for any file that is not a .jsonl transcript.
func sessionIDFromPath(path string) string {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".jsonl") {
		return ""
	}
	return strings.TrimSuffix(base, ".jsonl")
}
