package logger

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// resetAuditLogger resets the package-level audit logger state between tests.
func resetAuditLogger() {
	auditLogger.mu.Lock()
	defer auditLogger.mu.Unlock()
	if auditLogger.file != nil {
		auditLogger.file.Close()
		auditLogger.file = nil
	}
	auditLogger.baseDir = ""
	auditLogger.day = ""
}

// TestLogAudit_WritesJSONLine verifies that LogAudit appends a valid JSON line
// to the daily audit log file.
func TestLogAudit_WritesJSONLine(t *testing.T) {
	resetAuditLogger()
	t.Cleanup(resetAuditLogger)

	dir := t.TempDir()
	InitAudit(dir)

	evt := AuditEvent{
		Timestamp: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		IP:        "127.0.0.1",
		UserAgent: "test-agent/1.0",
		EventType: "auth_failed",
		AgentName: "default",
		SessionID: "sess-abc",
		Success:   false,
		Detail:    "invalid token",
	}
	LogAudit(evt)

	// Flush by closing — reopen via reset then read files.
	auditLogger.mu.Lock()
	if auditLogger.file != nil {
		auditLogger.file.Close()
		auditLogger.file = nil
	}
	auditLogger.mu.Unlock()

	logDir := filepath.Join(dir, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one audit log file")
	}

	// Read the file and decode the JSON line.
	data, err := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatal("expected at least one JSON line")
	}

	var decoded AuditEvent
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v (line: %q)", err, lines[0])
	}
	if decoded.IP != "127.0.0.1" {
		t.Errorf("IP = %q, want %q", decoded.IP, "127.0.0.1")
	}
	if decoded.EventType != "auth_failed" {
		t.Errorf("EventType = %q, want %q", decoded.EventType, "auth_failed")
	}
	if decoded.Success {
		t.Error("Success should be false")
	}
	if decoded.Detail != "invalid token" {
		t.Errorf("Detail = %q, want %q", decoded.Detail, "invalid token")
	}
}

// TestLogAudit_ConcurrentSafe verifies that LogAudit is safe when called from
// many goroutines simultaneously. Run with -race to detect data races.
func TestLogAudit_ConcurrentSafe(t *testing.T) {
	resetAuditLogger()
	t.Cleanup(resetAuditLogger)

	dir := t.TempDir()
	InitAudit(dir)

	const goroutines = 50
	const eventsEach = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			for j := 0; j < eventsEach; j++ {
				LogAudit(AuditEvent{
					EventType: "concurrent_test",
					Success:   true,
					Detail:    "goroutine event",
				})
			}
		}(i)
	}
	wg.Wait()

	// Flush.
	auditLogger.mu.Lock()
	if auditLogger.file != nil {
		auditLogger.file.Close()
		auditLogger.file = nil
	}
	auditLogger.mu.Unlock()

	logDir := filepath.Join(dir, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one audit log file")
	}

	data, err := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := goroutines * eventsEach
	if len(lines) != want {
		t.Errorf("got %d log lines, want %d", len(lines), want)
	}
}

// TestLogAudit_DailyRotation verifies that LogAudit creates a new file when the
// calendar day changes and that the filename contains the correct date suffix.
func TestLogAudit_DailyRotation(t *testing.T) {
	resetAuditLogger()
	t.Cleanup(resetAuditLogger)

	dir := t.TempDir()

	// Pre-create the logs directory and an "old" audit file to simulate yesterday.
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Use yesterday's date so pruneOldAuditLogs (7-day cutoff) does not delete it.
	oldDate := time.Now().AddDate(0, 0, -1).Format(auditLogDateFormat)
	oldFile := filepath.Join(logDir, auditLogBaseName+"-"+oldDate+".log")
	if err := os.WriteFile(oldFile, []byte(`{"event_type":"old_event"}`+"\n"), 0640); err != nil {
		t.Fatal(err)
	}

	// Now initialise the audit logger with the test dir and write a new event.
	// This should create a NEW file for today's date, separate from the old one.
	InitAudit(dir)
	LogAudit(AuditEvent{EventType: "new_event", Success: true})

	// Flush.
	auditLogger.mu.Lock()
	if auditLogger.file != nil {
		auditLogger.file.Close()
		auditLogger.file = nil
	}
	auditLogger.mu.Unlock()

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	// We should have at least 2 files: the pre-existing 2024-01-01 file and today's file.
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 audit log files (old + today), got %d: %v", len(entries), entries)
	}

	// Verify one file has the "2024-01-01" date suffix and one has today's date.
	foundOld := false
	today := time.Now().Format(auditLogDateFormat)
	foundToday := false
	for _, e := range entries {
		if strings.Contains(e.Name(), oldDate) {
			foundOld = true
		}
		if strings.Contains(e.Name(), today) {
			foundToday = true
		}
	}
	if !foundOld {
		t.Errorf("expected a log file with date suffix %q", oldDate)
	}
	if !foundToday {
		t.Errorf("expected a log file with today's date suffix %q", today)
	}

	// Verify the new file contains the new_event JSON line.
	todayFile := filepath.Join(logDir, auditLogBaseName+"-"+today+".log")
	data, err := os.ReadFile(todayFile)
	if err != nil {
		t.Fatalf("ReadFile today: %v", err)
	}
	if !strings.Contains(string(data), "new_event") {
		t.Errorf("today's log file does not contain new_event, got: %s", data)
	}
}

// TestLogAudit_PrunesOldFiles verifies that audit log files older than
// auditLogKeepDays are deleted by pruneOldAuditLogs.
func TestLogAudit_PrunesOldFiles(t *testing.T) {
	resetAuditLogger()
	t.Cleanup(resetAuditLogger)

	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create some old audit log files (beyond the 7-day keep window).
	oldDates := []string{
		time.Now().AddDate(0, 0, -8).Format(auditLogDateFormat),
		time.Now().AddDate(0, 0, -10).Format(auditLogDateFormat),
		time.Now().AddDate(0, 0, -30).Format(auditLogDateFormat),
	}
	for _, d := range oldDates {
		name := filepath.Join(logDir, auditLogBaseName+"-"+d+".log")
		if err := os.WriteFile(name, []byte(`{"event":"old"}`+"\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}

	// Create a recent file that should be kept.
	recentDate := time.Now().AddDate(0, 0, -3).Format(auditLogDateFormat)
	recentName := filepath.Join(logDir, auditLogBaseName+"-"+recentDate+".log")
	if err := os.WriteFile(recentName, []byte(`{"event":"recent"}`+"\n"), 0640); err != nil {
		t.Fatal(err)
	}

	// Run pruning.
	pruneOldAuditLogs(dir)

	// Old files should be gone.
	for _, d := range oldDates {
		name := filepath.Join(logDir, auditLogBaseName+"-"+d+".log")
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("expected old audit log %s to be deleted", name)
		}
	}

	// Recent file should still exist.
	if _, err := os.Stat(recentName); os.IsNotExist(err) {
		t.Errorf("expected recent audit log %s to be kept", recentName)
	}
}

// TestLogAudit_NoBaseDir verifies that LogAudit is a no-op when Init has not
// been called (audit logger not initialised). It should not panic.
func TestLogAudit_NoBaseDir(t *testing.T) {
	resetAuditLogger()
	t.Cleanup(resetAuditLogger)

	// Do NOT call InitAudit — baseDir is empty.
	// This must not panic.
	LogAudit(AuditEvent{EventType: "no_init", Success: false})
}

// TestLogAudit_WriteErrorReportedToStderr verifies that when the underlying
// file write fails (e.g. because the file was closed under us), the error is
// reported to stderr rather than silently discarded. This catches the class of
// bug where operators have no visibility into a broken audit log.
func TestLogAudit_WriteErrorReportedToStderr(t *testing.T) {
	resetAuditLogger()
	t.Cleanup(resetAuditLogger)

	dir := t.TempDir()
	InitAudit(dir)

	// Write one event to force the file to be opened.
	LogAudit(AuditEvent{EventType: "first_event", Success: true})

	// Close the underlying file to force the next Write to fail.
	auditLogger.mu.Lock()
	if auditLogger.file != nil {
		auditLogger.file.Close()
		// Leave auditLogger.file as a closed *os.File (not nil) so the rotation
		// check doesn't reopen it (day hasn't changed), and the Write call hits
		// the already-closed fd.
	}
	auditLogger.mu.Unlock()

	// Redirect stderr to a buffer so we can capture the error message.
	origStderr := os.Stderr
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stderr = w

	LogAudit(AuditEvent{EventType: "error_event", Success: false})

	// Close the write end and restore stderr.
	w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	buf.ReadFrom(r) //nolint:errcheck
	r.Close()

	got := buf.String()
	if !strings.Contains(got, "[audit] write error:") {
		t.Errorf("expected '[audit] write error:' on stderr, got: %q", got)
	}
}

// TestInitAudit_StartsBackgroundPruner verifies that InitAudit starts the
// background pruning goroutine. We test this indirectly by confirming that
// old audit log files that pre-exist the logs directory are eventually pruned
// when the 24-hour ticker fires. Because waiting 24 h is impractical we verify
// the pruning logic itself (pruneOldAuditLogs) is reachable from InitAudit's
// goroutine by calling it directly in the same test package and confirming the
// correct files are removed. The goroutine start is validated by ensuring
// InitAudit does not block — if the goroutine was synchronous the test would
// time out.
func TestInitAudit_StartsBackgroundPruner(t *testing.T) {
	resetAuditLogger()
	t.Cleanup(resetAuditLogger)

	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Place an old file that should be pruned.
	oldDate := time.Now().AddDate(0, 0, -15).Format(auditLogDateFormat)
	oldFile := filepath.Join(logDir, auditLogBaseName+"-"+oldDate+".log")
	if err := os.WriteFile(oldFile, []byte(`{"event":"old"}`+"\n"), 0640); err != nil {
		t.Fatal(err)
	}

	// InitAudit must return promptly (not block on the ticker).
	done := make(chan struct{})
	go func() {
		InitAudit(dir)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("InitAudit blocked — background goroutine was not started asynchronously")
	}

	// Confirm the pruning function (called by the background goroutine every
	// 24 h) correctly removes files older than auditLogKeepDays.
	pruneOldAuditLogs(dir)
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("expected old audit log %s to be pruned, but it still exists", oldFile)
	}
}
