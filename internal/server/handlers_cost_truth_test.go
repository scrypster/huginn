package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/config"
)

// TestHandleCost_LocalModel_ReportsTokensNotDollarCost verifies that when the
// configured backend is local (no known $ pricing), the cost handler still
// surfaces real token totals pulled from cost_history, and flags the
// response as local so the UI can render "n/a (local)" instead of a
// misleading "$0.00" / "—".
func TestHandleCost_LocalModel_ReportsTokensNotDollarCost(t *testing.T) {
	db := openTestSQLiteDB(t)
	defer db.Close()

	now := time.Now().Unix()
	wdb := db.Write()
	if wdb == nil {
		t.Fatal("Write() returned nil")
	}
	if _, err := wdb.Exec(
		`INSERT INTO cost_history (ts, session_id, cost_usd, prompt_tokens, completion_tokens) VALUES (?, ?, ?, ?, ?)`,
		now, "ses_local", 0.0, 1234, 567,
	); err != nil {
		t.Fatalf("insert cost_history: %v", err)
	}

	s := &Server{}
	s.db = db
	s.cfg = *config.Default()
	s.cfg.Backend.Provider = "ollama" // local

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cost", nil)
	s.handleCost(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		SessionTotalUSD  float64 `json:"session_total_usd"`
		PromptTokens     int     `json:"prompt_tokens_total"`
		CompletionTokens int     `json:"completion_tokens_total"`
		IsLocal          bool    `json:"is_local"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.IsLocal {
		t.Error("expected is_local=true for ollama backend")
	}
	if body.PromptTokens != 1234 {
		t.Errorf("prompt_tokens_total = %d, want 1234", body.PromptTokens)
	}
	if body.CompletionTokens != 567 {
		t.Errorf("completion_tokens_total = %d, want 567", body.CompletionTokens)
	}
}

// TestHandleCost_CloudModel_NotLocal verifies a non-local backend is not
// flagged as local.
func TestHandleCost_CloudModel_NotLocal(t *testing.T) {
	s := &Server{}
	s.cfg = *config.Default()
	s.cfg.Backend.Provider = "anthropic"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cost", nil)
	s.handleCost(rr, req)

	var body struct {
		IsLocal bool `json:"is_local"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.IsLocal {
		t.Error("expected is_local=false for anthropic backend")
	}
}
