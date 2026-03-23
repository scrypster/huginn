package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	defaultMaxSizeBytes = 10 * 1024 * 1024 // 10 MB
	logFileName         = "huginn.log"
	// maxRotatedFiles is the maximum number of rotated log copies kept on disk.
	// Oldest copies beyond this count are deleted on rotation.
	maxRotatedFiles = 3
)

// global is a package-level logger for use via Init/L/Info/Error/Warn/Debug.
var (
	globalLogger *Logger
	globalMu     sync.RWMutex
)

// Init initializes the global logger. Safe to call multiple times (idempotent after first call).
// baseDir is ~/.huginn or equivalent.
func Init(baseDir string) error {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalLogger != nil {
		return nil
	}
	logPath := filepath.Join(baseDir, "logs", logFileName)
	l, err := New(logPath)
	if err != nil {
		return err
	}
	globalLogger = l
	return nil
}

// L returns the global logger. Falls back to a no-op logger if Init was not called.
func L() *Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()
	if globalLogger == nil {
		return Discard()
	}
	return globalLogger
}

// Info logs an info message on the global logger.
func Info(msg string, args ...any) { L().Info(msg, args...) }

// Error logs an error message on the global logger.
func Error(msg string, args ...any) { L().Error(msg, args...) }

// Warn logs a warning message on the global logger.
func Warn(msg string, args ...any) { L().Warn(msg, args...) }

// Debug logs a debug message on the global logger.
func Debug(msg string, args ...any) { L().Debug(msg, args...) }

// LogPath returns the path to the current log file for the given base dir.
func LogPath(baseDir string) string {
	return filepath.Join(baseDir, "logs", logFileName)
}

// TailLog returns the last n lines across the current log and up to 3 rotated
// files (.1, .2, .3), read from oldest to newest.
func TailLog(baseDir string, n int) ([]string, error) {
	path := LogPath(baseDir)
	// Read from oldest rotated files first, then the current log.
	candidates := []string{path + ".3", path + ".2", path + ".1", path}
	var all []string
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		all = append(all, splitLines(string(data))...)
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				lines = append(lines, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// Logger wraps slog with file rotation.
type Logger struct {
	mu           sync.Mutex
	path         string
	file         *os.File
	maxSizeBytes int64
	*slog.Logger
}

// New creates a Logger writing to path. Rotates at 10 MB.
func New(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("logger mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("logger open: %w", err)
	}
	l := &Logger{path: path, file: f, maxSizeBytes: defaultMaxSizeBytes}
	l.Logger = slog.New(slog.NewJSONHandler(l, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return l, nil
}

// Write implements io.Writer, checks rotation before writing.
func (l *Logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if info, err := l.file.Stat(); err == nil && info.Size() >= l.maxSizeBytes {
		l.rotate()
	}
	return l.file.Write(p)
}

// rotate shifts existing log files (.2→.3, .1→.2, current→.1) and opens a
// fresh log file. Keeps at most 3 rotated files. Must be called while holding l.mu.
//
// Rename failures for older slots (.2→.3, .1→.2) are tolerable: we log them to
// stderr and continue. The critical rename (current→.1) aborts rotation on failure
// to avoid losing active log data — the old file is reopened for continued writing.
func (l *Logger) rotate() {
	l.file.Close()
	// Shift older rotated files to make room (drop .3 if it exists).
	_ = os.Remove(l.path + ".3")
	if err := os.Rename(l.path+".2", l.path+".3"); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "huginn: log rotate rename .2→.3 failed: %v\n", err)
	}
	if err := os.Rename(l.path+".1", l.path+".2"); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "huginn: log rotate rename .1→.2 failed: %v\n", err)
	}
	// Critical: if current→.1 fails, do not create a new file (would truncate active log).
	// Reopen the existing file in append mode so writes continue.
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		fmt.Fprintf(os.Stderr, "huginn: log rotate rename current→.1 failed: %v — continuing on existing file\n", err)
		if f, reopenErr := os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY, 0644); reopenErr == nil {
			l.file = f
		}
		// If reopen also fails, l.file is closed; subsequent writes will error.
		// This is an unrecoverable log-infrastructure failure requiring restart.
		return
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		l.file = f
	} else {
		fmt.Fprintf(os.Stderr, "huginn: log rotate open new file failed: %v\n", err)
	}
}

// Close flushes and closes the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}

// Discard returns a no-op logger (for tests and when log file unavailable).
func Discard() *Logger {
	l := &Logger{maxSizeBytes: defaultMaxSizeBytes}
	l.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	return l
}
