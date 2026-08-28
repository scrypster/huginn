package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// auditEvent is a single permission decision to be written to audit_log.
type auditEvent struct {
	action   string
	resource string
	allowed  bool
	reason   string
}

// auditLogger writes permission gate decisions to the SQLite audit_log table
// via a non-blocking buffered channel + background goroutine.
// Pruning of old rows is handled by stats.Persister on its 5-minute flush cycle.
type auditLogger struct {
	db   *sqlitedb.DB
	ch   chan auditEvent
	wg   sync.WaitGroup
	done chan struct{}
}

const auditChanSize = 512

// newAuditLogger creates an auditLogger and starts its drain goroutine.
// db may be nil (no-op mode). Call Close() before db.Close().
func newAuditLogger(db *sqlitedb.DB) *auditLogger {
	a := &auditLogger{
		db:   db,
		ch:   make(chan auditEvent, auditChanSize),
		done: make(chan struct{}),
	}
	a.wg.Add(1)
	go a.run()
	return a
}

// Log enqueues an audit event non-blocking. If the channel is full, the event
// is dropped and a warning is logged — audit events are best-effort.
func (a *auditLogger) Log(action, resource string, allowed bool, reason string) {
	if a.db == nil {
		return
	}
	select {
	case a.ch <- auditEvent{action: action, resource: resource, allowed: allowed, reason: reason}:
	default:
		slog.Warn("audit: channel full, dropping event", "action", action, "resource", resource)
	}
}

// Close stops the drain goroutine and flushes remaining events.
// Must be called before db.Close().
func (a *auditLogger) Close() {
	select {
	case <-a.done:
	default:
		close(a.done)
	}
	a.wg.Wait()
}

func (a *auditLogger) run() {
	defer a.wg.Done()
	for {
		select {
		case ev := <-a.ch:
			a.write(ev)
		case <-a.done:
			// Drain remaining events before exit.
			for {
				select {
				case ev := <-a.ch:
					a.write(ev)
				default:
					return
				}
			}
		}
	}
}

// auditActor identifies who performed an audited entity action. Huginn is a
// single-operator local app with no multi-user identity system yet, so this
// is a fixed label rather than a per-request principal — it exists so the
// audit schema does not need to change when multi-user support lands.
const auditActor = "user"

// logEntityAudit appends one entity-lifecycle audit entry, best-effort.
// A write failure is logged but never blocks or fails the caller's action —
// the audit trail must not become a reason a hire/delete/seat/forget fails.
func (s *Server) logEntityAudit(action, what string, detail map[string]any) {
	if s.entityAudit == nil {
		return
	}
	if err := s.entityAudit.Append(action, auditActor, what, detail); err != nil {
		slog.Warn("entity audit: append failed", "action", action, "err", err)
	}
}

// handleGetAudit returns the most recent entity-lifecycle audit entries,
// newest first.
//
//	GET /api/v1/audit?limit=100
func (s *Server) handleGetAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	if s.entityAudit == nil {
		jsonOK(w, map[string]any{"entries": []entityAuditEntry{}})
		return
	}
	entries, err := s.entityAudit.List(limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read audit log: "+err.Error())
		return
	}
	jsonOK(w, map[string]any{"entries": entries})
}

func (a *auditLogger) write(ev auditEvent) {
	if a.db == nil {
		return
	}
	db := a.db.Write()
	if db == nil {
		return
	}
	allowed := 0
	if ev.allowed {
		allowed = 1
	}
	var reason *string
	if ev.reason != "" {
		reason = &ev.reason
	}
	_, err := db.Exec(
		`INSERT INTO audit_log (ts, action, resource, allowed, reason) VALUES (?, ?, ?, ?, ?)`,
		time.Now().Unix(), ev.action, ev.resource, allowed, reason,
	)
	if err != nil {
		slog.Debug("audit: write failed", "err", err)
	}
}
