package server

import (
	"log/slog"
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
	db  *sqlitedb.DB
	ch  chan auditEvent
	wg  sync.WaitGroup
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
