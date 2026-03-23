package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- cmdStats aggregation helpers ---

// buildTestSessionDir creates a temporary sessions directory populated with
// the given manifests and returns its path.  The caller is responsible for
// cleanup (t.TempDir handles it automatically).
func buildTestSessionDir(t *testing.T, manifests []sessionManifestStub) string {
	t.Helper()
	base := t.TempDir()
	for _, m := range manifests {
		dir := filepath.Join(base, m.SessionID)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
	return base
}

// sessionManifestStub mirrors the fields of session.Manifest that cmdStats reads.
type sessionManifestStub struct {
	SessionID    string    `json:"session_id"`
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Model        string    `json:"model"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

func TestAggregateSessionStats_Empty(t *testing.T) {
	dir := t.TempDir() // empty — no session subdirs
	got, err := aggregateSessionStats(dir)
	if err != nil {
		t.Fatalf("unexpected error on empty dir: %v", err)
	}
	if got.TotalSessions != 0 {
		t.Errorf("TotalSessions = %d, want 0", got.TotalSessions)
	}
	if got.TotalMessages != 0 {
		t.Errorf("TotalMessages = %d, want 0", got.TotalMessages)
	}
}

func TestAggregateSessionStats_MissingDir(t *testing.T) {
	// Directory that doesn't exist should not error — just return zero counts.
	got, err := aggregateSessionStats(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("unexpected error on missing dir: %v", err)
	}
	if got.TotalSessions != 0 {
		t.Errorf("TotalSessions = %d, want 0", got.TotalSessions)
	}
}

func TestAggregateSessionStats_SingleSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	dir := buildTestSessionDir(t, []sessionManifestStub{
		{
			SessionID:    "sess-001",
			ID:           "sess-001",
			Title:        "My session",
			Model:        "llama3:8b",
			CreatedAt:    now,
			UpdatedAt:    now,
			MessageCount: 5,
		},
	})
	got, err := aggregateSessionStats(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", got.TotalSessions)
	}
	if got.TotalMessages != 5 {
		t.Errorf("TotalMessages = %d, want 5", got.TotalMessages)
	}
	if len(got.Models) != 1 || got.Models["llama3:8b"] != 1 {
		t.Errorf("Models = %v, want map[llama3:8b:1]", got.Models)
	}
}

func TestAggregateSessionStats_MultipleSessions(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)

	dir := buildTestSessionDir(t, []sessionManifestStub{
		{SessionID: "a", ID: "a", Model: "llama3:8b", CreatedAt: t1, UpdatedAt: t2, MessageCount: 10},
		{SessionID: "b", ID: "b", Model: "llama3:8b", CreatedAt: t0, UpdatedAt: t1, MessageCount: 3},
		{SessionID: "c", ID: "c", Model: "qwen3:30b", CreatedAt: t2, UpdatedAt: t2, MessageCount: 7},
	})
	got, err := aggregateSessionStats(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalSessions != 3 {
		t.Errorf("TotalSessions = %d, want 3", got.TotalSessions)
	}
	if got.TotalMessages != 20 {
		t.Errorf("TotalMessages = %d, want 20", got.TotalMessages)
	}
	if got.Models["llama3:8b"] != 2 {
		t.Errorf("Models[llama3:8b] = %d, want 2", got.Models["llama3:8b"])
	}
	if got.Models["qwen3:30b"] != 1 {
		t.Errorf("Models[qwen3:30b] = %d, want 1", got.Models["qwen3:30b"])
	}
	// Oldest should be t0, newest should be t2
	if !got.Oldest.Equal(t0) {
		t.Errorf("Oldest = %v, want %v", got.Oldest, t0)
	}
	if !got.Newest.Equal(t2) {
		t.Errorf("Newest = %v, want %v", got.Newest, t2)
	}
}

func TestFormatStatsOutput_Empty(t *testing.T) {
	sa := sessionAggregation{}
	out := formatStatsOutput(sa)
	if !strings.Contains(out, "0") {
		t.Errorf("expected '0' in output for empty stats, got: %q", out)
	}
	if !strings.Contains(out, "Sessions") {
		t.Errorf("expected 'Sessions' in output, got: %q", out)
	}
}

func TestFormatStatsOutput_WithData(t *testing.T) {
	now := time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)
	sa := sessionAggregation{
		TotalSessions: 4,
		TotalMessages: 42,
		Models:        map[string]int{"llama3:8b": 3, "qwen3:30b": 1},
		Oldest:        now.Add(-7 * 24 * time.Hour),
		Newest:        now,
	}
	out := formatStatsOutput(sa)
	if !strings.Contains(out, "4") {
		t.Errorf("expected '4' (sessions) in output, got: %q", out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("expected '42' (messages) in output, got: %q", out)
	}
	if !strings.Contains(out, "llama3:8b") {
		t.Errorf("expected model name in output, got: %q", out)
	}
}

// --- formatBytes ---

func TestFormatBytes_Zero(t *testing.T) {
	if got := formatBytes(0); got != "0B" {
		t.Errorf("formatBytes(0) = %q, want %q", got, "0B")
	}
}

func TestFormatBytes_Negative(t *testing.T) {
	if got := formatBytes(-1); got != "?" {
		t.Errorf("formatBytes(-1) = %q, want %q", got, "?")
	}
}

func TestFormatBytes_Kilobytes(t *testing.T) {
	// 1024 bytes is under the 1MB threshold — should be rendered as bytes.
	if got := formatBytes(1024); got != "1024B" {
		t.Errorf("formatBytes(1024) = %q, want %q", got, "1024B")
	}
}

func TestFormatBytes_Megabytes(t *testing.T) {
	if got := formatBytes(1024 * 1024); got != "1.0M" {
		t.Errorf("formatBytes(1048576) = %q, want %q", got, "1.0M")
	}
}

func TestFormatBytes_Gigabytes(t *testing.T) {
	if got := formatBytes(1024 * 1024 * 1024); got != "1.0G" {
		t.Errorf("formatBytes(1073741824) = %q, want %q", got, "1.0G")
	}
}

// --- formatDuration ---

func TestFormatDuration_Seconds(t *testing.T) {
	got := formatDuration(45.0)
	if !strings.Contains(got, "s") {
		t.Errorf("formatDuration(45.0) = %q, expected to contain 's'", got)
	}
}

func TestFormatDuration_Zero(t *testing.T) {
	// %.0fs with 0 produces "0s"
	got := formatDuration(0)
	if got != "0s" {
		t.Errorf("formatDuration(0) = %q, want %q", got, "0s")
	}
}

func TestFormatDuration_Minutes(t *testing.T) {
	got := formatDuration(90)
	if !strings.Contains(got, "m") {
		t.Errorf("formatDuration(90) = %q, expected to contain 'm'", got)
	}
}

func TestFormatDuration_Hours(t *testing.T) {
	got := formatDuration(3600)
	if !strings.Contains(got, "h") {
		t.Errorf("formatDuration(3600) = %q, expected to contain 'h'", got)
	}
}

// --- progressBar ---

func TestProgressBar_Empty(t *testing.T) {
	got := progressBar(0, 0, 20)
	if len([]rune(got)) != 20 {
		t.Errorf("progressBar(0, 0, 20) rune length = %d, want 20", len([]rune(got)))
	}
	// All chars should be the empty block rune.
	if strings.ContainsRune(got, '█') {
		t.Errorf("progressBar(0, 0, 20) should contain no filled chars, got %q", got)
	}
}

func TestProgressBar_Full(t *testing.T) {
	got := progressBar(100, 100, 20)
	if len([]rune(got)) != 20 {
		t.Errorf("progressBar(100, 100, 20) rune length = %d, want 20", len([]rune(got)))
	}
	if strings.ContainsRune(got, '░') {
		t.Errorf("progressBar(100, 100, 20) should contain no empty chars, got %q", got)
	}
}

func TestProgressBar_Half(t *testing.T) {
	got := progressBar(50, 100, 20)
	runes := []rune(got)
	if len(runes) != 20 {
		t.Fatalf("progressBar(50, 100, 20) rune length = %d, want 20", len(runes))
	}
	filled := 0
	empty := 0
	for _, r := range runes {
		switch r {
		case '█':
			filled++
		case '░':
			empty++
		}
	}
	if filled != 10 {
		t.Errorf("expected 10 filled chars, got %d", filled)
	}
	if empty != 10 {
		t.Errorf("expected 10 empty chars, got %d", empty)
	}
}

func TestProgressBar_OverFull(t *testing.T) {
	// Should not panic and should clamp to full bar.
	got := progressBar(200, 100, 20)
	runes := []rune(got)
	if len(runes) != 20 {
		t.Errorf("progressBar(200, 100, 20) rune length = %d, want 20", len(runes))
	}
	if strings.ContainsRune(got, '░') {
		t.Errorf("progressBar(200, 100, 20) should be all filled, got %q", got)
	}
}

// --- truncate ---

func TestTruncate_Short(t *testing.T) {
	if got := truncate("hi", 10); got != "hi" {
		t.Errorf("truncate(%q, 10) = %q, want %q", "hi", got, "hi")
	}
}

func TestTruncate_Exact(t *testing.T) {
	if got := truncate("hello", 5); got != "hello" {
		t.Errorf("truncate(%q, 5) = %q, want %q", "hello", got, "hello")
	}
}

func TestTruncate_Long(t *testing.T) {
	got := truncate("hello world", 5)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate(%q, 5) = %q, expected to end with '…'", "hello world", got)
	}
}

func TestTruncate_Zero(t *testing.T) {
	got := truncate("hello", 0)
	if got != "" {
		t.Errorf("truncate(%q, 0) = %q, want %q", "hello", got, "")
	}
}

func TestTruncate_Negative(t *testing.T) {
	got := truncate("hello", -1)
	if got != "" {
		t.Errorf("truncate(%q, -1) = %q, want %q", "hello", got, "")
	}
}

// --- sanitizePath ---

func TestSanitizePath_SpecialChars(t *testing.T) {
	input := `/\:*?"<>|`
	got := sanitizePath(input)
	// Every character should have been replaced with '_'
	for _, r := range got {
		if r != '_' {
			t.Errorf("sanitizePath(%q) = %q, expected all '_', got rune %q", input, got, r)
		}
	}
	if len([]rune(got)) != len([]rune(input)) {
		t.Errorf("sanitizePath(%q) length mismatch: got %d, want %d", input, len([]rune(got)), len([]rune(input)))
	}
}

func TestSanitizePath_Normal(t *testing.T) {
	input := "normalpath123"
	got := sanitizePath(input)
	if got != input {
		t.Errorf("sanitizePath(%q) = %q, want %q (unchanged)", input, got, input)
	}
}

func TestSanitizePath_Empty(t *testing.T) {
	got := sanitizePath("")
	if got != "" {
		t.Errorf("sanitizePath(%q) = %q, want %q", "", got, "")
	}
}
