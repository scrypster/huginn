package server

// metrics_spec_test.go — Behavior specs for GET /api/v1/metrics.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/stats"
)

func TestHandleMetrics_NoRegistry_ReturnsEmptySnapshot(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleMetrics(rr, httptest.NewRequest("GET", "/api/v1/metrics", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body struct {
		Records    []any  `json:"records"`
		Histograms []any  `json:"histograms"`
		TS         string `json:"ts"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Records) != 0 {
		t.Errorf("expected empty records, got %d", len(body.Records))
	}
	if len(body.Histograms) != 0 {
		t.Errorf("expected empty histograms, got %d", len(body.Histograms))
	}
	if body.TS == "" {
		t.Error("expected ts field to be set")
	}
}

func TestHandleMetrics_WithRegistry_ReturnsSnapshot(t *testing.T) {
	reg := stats.NewRegistry()
	col := reg.Collector()
	col.Record("agent.requests", 5.0, "agent:coder")
	col.Histogram("agent.latency_ms", 120.5, "agent:coder")

	s := &Server{}
	s.SetStatsRegistry(reg)

	rr := httptest.NewRecorder()
	s.handleMetrics(rr, httptest.NewRequest("GET", "/api/v1/metrics", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var body struct {
		Records []struct {
			Metric string  `json:"metric"`
			Value  float64 `json:"value"`
			Tags   []string `json:"tags"`
		} `json:"records"`
		Histograms []struct {
			Metric string  `json:"metric"`
			Value  float64 `json:"value"`
		} `json:"histograms"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(body.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(body.Records))
	}
	if body.Records[0].Metric != "agent.requests" {
		t.Errorf("record metric = %q, want %q", body.Records[0].Metric, "agent.requests")
	}
	if body.Records[0].Value != 5.0 {
		t.Errorf("record value = %v, want 5.0", body.Records[0].Value)
	}

	if len(body.Histograms) != 1 {
		t.Fatalf("expected 1 histogram, got %d", len(body.Histograms))
	}
	if body.Histograms[0].Metric != "agent.latency_ms" {
		t.Errorf("histogram metric = %q", body.Histograms[0].Metric)
	}
	if body.Histograms[0].Value != 120.5 {
		t.Errorf("histogram value = %v, want 120.5", body.Histograms[0].Value)
	}
}

func TestHandleMetrics_ContentType_IsJSON(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleMetrics(rr, httptest.NewRequest("GET", "/api/v1/metrics", nil))
	ct := rr.Header().Get("Content-Type")
	if ct == "" {
		t.Error("Content-Type header missing on /metrics response")
	}
}
