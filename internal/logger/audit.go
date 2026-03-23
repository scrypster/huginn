package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	auditLogBaseName   = "audit"
	auditLogKeepDays   = 7
	auditLogDateFormat = "2006-01-02"
	auditSyncEvery     = 10 // call Sync() after every N events
)

// AuditEvent represents a single structured security/audit log entry.
type AuditEvent struct {
	Timestamp time.Time `json:"timestamp"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	EventType string    `json:"event_type"`
	AgentName string    `json:"agent_name,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Success   bool      `json:"success"`
	Detail    string    `json:"detail,omitempty"`
}

// auditLogger is the package-level audit logger state.
var auditLogger struct {
	mu         sync.Mutex
	baseDir    string
	file       *os.File
	day        string // current date suffix, e.g. "2006-01-02"
	eventCount int    // monotonic counter for periodic fsync
}

// InitAudit initialises the audit logger to write into baseDir/logs/audit-YYYY-MM-DD.log.
// Safe to call multiple times — only the first call with a non-empty baseDir takes effect.
// A background goroutine is started to prune old audit log files every 24 hours so that
// pruning occurs even when the server is otherwise idle (not just on the first LogAudit call
// after a day boundary).
func InitAudit(baseDir string) {
	auditLogger.mu.Lock()
	defer auditLogger.mu.Unlock()
	if auditLogger.baseDir == "" && baseDir != "" {
		auditLogger.baseDir = baseDir
		// Start a background goroutine to prune old audit logs once per day.
		go func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				pruneOldAuditLogs(baseDir)
			}
		}()
	}
}

// LogAudit writes a JSON-line representation of event to the daily audit log
// file inside baseDir/logs/. The file is automatically rotated each calendar
// day and old files beyond auditLogKeepDays are removed.
// It is safe to call from multiple goroutines concurrently.
func LogAudit(event AuditEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	auditLogger.mu.Lock()
	defer auditLogger.mu.Unlock()

	if auditLogger.baseDir == "" {
		// Audit logging not initialised; silently drop.
		return
	}

	today := time.Now().Format(auditLogDateFormat)

	// Rotate when the day has changed or the file is not yet open.
	if auditLogger.file == nil || today != auditLogger.day {
		if auditLogger.file != nil {
			auditLogger.file.Close()
			auditLogger.file = nil
		}
		if err := openAuditFile(today); err != nil {
			// Non-fatal: drop the event rather than crashing.
			return
		}
		auditLogger.day = today
		// Best-effort cleanup of old audit log files.
		go pruneOldAuditLogs(auditLogger.baseDir)
	}

	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	line = append(line, '\n')
	if _, err := auditLogger.file.Write(line); err != nil {
		// Non-fatal but visible — log to stderr so operators see write failures
		// without crashing the server.
		fmt.Fprintf(os.Stderr, "[audit] write error: %v\n", err)
		return
	}

	// Periodic fsync: flush kernel buffers every auditSyncEvery events so that
	// recent audit events survive an unclean shutdown.
	auditLogger.eventCount++
	if auditLogger.eventCount%auditSyncEvery == 0 {
		_ = auditLogger.file.Sync()
	}
}

// openAuditFile opens (or creates) the audit log file for the given date string.
// Must be called with auditLogger.mu held.
func openAuditFile(day string) error {
	logDir := filepath.Join(auditLogger.baseDir, "logs")
	// 0700 — owner-only access; audit logs may contain sensitive IP/UA data.
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("audit: mkdir %s: %w", logDir, err)
	}
	name := filepath.Join(logDir, auditLogBaseName+"-"+day+".log")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", name, err)
	}
	auditLogger.file = f
	return nil
}

// pruneOldAuditLogs removes audit log files older than auditLogKeepDays.
func pruneOldAuditLogs(baseDir string) {
	logDir := filepath.Join(baseDir, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -auditLogKeepDays)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Match files like "audit-2006-01-02.log"
		prefix := auditLogBaseName + "-"
		suffix := ".log"
		if len(name) < len(prefix)+len(auditLogDateFormat)+len(suffix) {
			continue
		}
		if name[:len(prefix)] != prefix {
			continue
		}
		if name[len(name)-len(suffix):] != suffix {
			continue
		}
		dateStr := name[len(prefix) : len(name)-len(suffix)]
		t, err := time.Parse(auditLogDateFormat, dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			_ = os.Remove(filepath.Join(logDir, name))
		}
	}
}
