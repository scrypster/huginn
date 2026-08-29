package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/turnmetrics"
)

type fakeTurnMetricsReader struct {
	resp turnmetrics.TurnsResponse
	err  error
}

func (f *fakeTurnMetricsReader) Recent(limit int) (turnmetrics.TurnsResponse, error) {
	if f.err != nil {
		return turnmetrics.TurnsResponse{}, f.err
	}
	return f.resp, nil
}

func TestHandleTurnMetrics_NotWiredReturns503(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/turns", nil)
	w := httptest.NewRecorder()
	srv.handleTurnMetrics(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleTurnMetrics_InvalidLimitReturns400(t *testing.T) {
	srv := &Server{}
	srv.SetTurnMetricsReader(&fakeTurnMetricsReader{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/turns?limit=abc", nil)
	w := httptest.NewRecorder()
	srv.handleTurnMetrics(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleTurnMetrics_ReturnsPayload(t *testing.T) {
	srv := &Server{}
	want := turnmetrics.TurnsResponse{
		Turns: []turnmetrics.TurnRow{
			{ID: 1, SessionID: "s1", Model: "claude-sonnet-4-5", HadFirstToken: true, FirstTokenMs: 120, CompleteMs: 900},
		},
		Summary: []turnmetrics.ModelSummary{
			{Model: "claude-sonnet-4-5", Count: 1, FirstTokenP50Ms: 120, CompleteP50Ms: 900},
		},
	}
	srv.SetTurnMetricsReader(&fakeTurnMetricsReader{resp: want})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/turns?limit=5", nil)
	w := httptest.NewRecorder()
	srv.handleTurnMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got turnmetrics.TurnsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Turns) != 1 || got.Turns[0].SessionID != "s1" {
		t.Fatalf("unexpected turns: %+v", got.Turns)
	}
	if len(got.Summary) != 1 || got.Summary[0].Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected summary: %+v", got.Summary)
	}
}

func TestHandleTurnMetrics_ReaderErrorReturns500(t *testing.T) {
	srv := &Server{}
	srv.SetTurnMetricsReader(&fakeTurnMetricsReader{err: errors.New("boom")})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/turns", nil)
	w := httptest.NewRecorder()
	srv.handleTurnMetrics(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}
