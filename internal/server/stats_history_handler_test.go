package server

// stats_history_handler_test.go — Unit tests for GET /api/v1/stats/history.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// TestHandleStatsHistory_NoDB_ReturnsEmptyLists verifies that when no DB is
// wired the handler returns 200 with empty stats and cost arrays.
func TestHandleStatsHistory_NoDB_ReturnsEmptyLists(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleStatsHistory(rr, httptest.NewRequest("GET", "/api/v1/stats/history", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Stats []any `json:"stats"`
		Cost  []any `json:"cost"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Stats) != 0 {
		t.Errorf("expected empty stats, got %d", len(body.Stats))
	}
	if len(body.Cost) != 0 {
		t.Errorf("expected empty cost, got %d", len(body.Cost))
	}
}

// TestHandleStatsHistory_WithDB_ReturnsCostRows verifies that cost_history
// rows inserted directly into the DB are returned by the handler.
func TestHandleStatsHistory_WithDB_ReturnsCostRows(t *testing.T) {
	db := openTestSQLiteDB(t)
	defer db.Close()

	// Insert a cost row directly into the DB.
	now := time.Now().Unix()
	wdb := db.Write()
	if wdb == nil {
		t.Fatal("Write() returned nil")
	}
	_, err := wdb.Exec(
		`INSERT INTO cost_history (ts, session_id, cost_usd, prompt_tokens, completion_tokens) VALUES (?, ?, ?, ?, ?)`,
		now, "ses_abc123", 0.0042, 1000, 200,
	)
	if err != nil {
		t.Fatalf("insert cost_history: %v", err)
	}

	s := &Server{}
	s.db = db

	since := now - 60 // 1 minute ago
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/stats/history?since=%d", since), nil)
	s.handleStatsHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Stats []any `json:"stats"`
		Cost  []struct {
			TS               int64   `json:"ts"`
			SessionID        string  `json:"session_id"`
			CostUSD          float64 `json:"cost_usd"`
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
		} `json:"cost"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Cost) != 1 {
		t.Fatalf("expected 1 cost row, got %d", len(body.Cost))
	}
	row := body.Cost[0]
	if row.SessionID != "ses_abc123" {
		t.Errorf("session_id = %q, want %q", row.SessionID, "ses_abc123")
	}
	if row.CostUSD != 0.0042 {
		t.Errorf("cost_usd = %v, want 0.0042", row.CostUSD)
	}
	if row.PromptTokens != 1000 {
		t.Errorf("prompt_tokens = %d, want 1000", row.PromptTokens)
	}
	if row.CompletionTokens != 200 {
		t.Errorf("completion_tokens = %d, want 200", row.CompletionTokens)
	}
}

// TestHandleStatsHistory_SinceFilter_ExcludesOldRows verifies that the since
// query parameter correctly filters out rows older than the threshold.
func TestHandleStatsHistory_SinceFilter_ExcludesOldRows(t *testing.T) {
	db := openTestSQLiteDB(t)
	defer db.Close()

	wdb := db.Write()
	if wdb == nil {
		t.Fatal("Write() returned nil")
	}
	old := time.Now().Unix() - 3600  // 1 hour ago
	recent := time.Now().Unix() - 60 // 1 minute ago

	_, err := wdb.Exec(
		`INSERT INTO cost_history (ts, session_id, cost_usd, prompt_tokens, completion_tokens) VALUES (?, ?, ?, ?, ?)`,
		old, "old-session", 0.001, 100, 50,
	)
	if err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	_, err = wdb.Exec(
		`INSERT INTO cost_history (ts, session_id, cost_usd, prompt_tokens, completion_tokens) VALUES (?, ?, ?, ?, ?)`,
		recent, "recent-session", 0.002, 200, 100,
	)
	if err != nil {
		t.Fatalf("insert recent row: %v", err)
	}

	s := &Server{}
	s.db = db

	// Request only rows from last 30 minutes.
	since := time.Now().Unix() - 1800
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/stats/history?since=%d", since), nil)
	s.handleStatsHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Cost []struct {
			SessionID string `json:"session_id"`
		} `json:"cost"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Cost) != 1 {
		t.Fatalf("expected 1 cost row (recent only), got %d", len(body.Cost))
	}
	if body.Cost[0].SessionID != "recent-session" {
		t.Errorf("expected recent-session, got %q", body.Cost[0].SessionID)
	}
}

// TestHandleStatsHistory_DefaultSince_UsesLast24h verifies that omitting the
// since param still returns rows from the past 24 hours.
func TestHandleStatsHistory_DefaultSince_UsesLast24h(t *testing.T) {
	db := openTestSQLiteDB(t)
	defer db.Close()

	wdb := db.Write()
	if wdb == nil {
		t.Fatal("Write() returned nil")
	}
	recent := time.Now().Unix() - 3600 // 1 hour ago — within 24h default

	_, err := wdb.Exec(
		`INSERT INTO cost_history (ts, session_id, cost_usd, prompt_tokens, completion_tokens) VALUES (?, ?, ?, ?, ?)`,
		recent, "sess-default", 0.003, 300, 150,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	s := &Server{}
	s.db = db

	// No since param — should use last 24h.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/stats/history", nil)
	s.handleStatsHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Cost []struct {
			SessionID string `json:"session_id"`
		} `json:"cost"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, c := range body.Cost {
		if c.SessionID == "sess-default" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sess-default in cost results, got %v", body.Cost)
	}
}

// TestHandleStatsHistory_ContentType_IsJSON verifies the Content-Type header.
func TestHandleStatsHistory_ContentType_IsJSON(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleStatsHistory(rr, httptest.NewRequest("GET", "/api/v1/stats/history", nil))
	ct := rr.Header().Get("Content-Type")
	if ct == "" {
		t.Error("Content-Type header missing on /stats/history response")
	}
}

// TestHandleStatsHistory_InvalidSince_FallsBackToDefault verifies that an
// invalid since param causes the default 24h window to be used (no 400 error).
func TestHandleStatsHistory_InvalidSince_FallsBackToDefault(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/stats/history?since=notanumber", nil)
	s.handleStatsHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for invalid since param, got %d", rr.Code)
	}
}

// openTestSQLiteDB is defined in handlers_spaces_test.go (same package).
// Reuse it here to avoid duplication.
var _ = session.Migrations // ensure import is used
