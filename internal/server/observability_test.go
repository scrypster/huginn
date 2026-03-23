package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/stats"
)

func TestRequestIDMiddleware_StoresInContext(t *testing.T) {
	t.Parallel()
	var gotID string
	handler := requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(200)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if gotID == "" {
		t.Error("RequestIDFromContext returned empty string")
	}
	// The header should also be set
	if rec.Header().Get(requestIDHeader) == "" {
		t.Error("X-Request-ID header not set on response")
	}
	// Context value and header should match
	if gotID != rec.Header().Get(requestIDHeader) {
		t.Errorf("context ID %q != header ID %q", gotID, rec.Header().Get(requestIDHeader))
	}
}

func TestRequestIDMiddleware_PreservesIncoming(t *testing.T) {
	t.Parallel()
	var gotID string
	handler := requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(200)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(requestIDHeader, "custom-id-123")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if gotID != "custom-id-123" {
		t.Errorf("expected custom ID, got %q", gotID)
	}
}

func TestSanitizePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"/api/v1/sessions", "/api/v1/sessions"},
		{"/api/v1/sessions/01HXYZ1234567890ABCDEF/messages", "/api/v1/sessions/:id/messages"},
		{"/api/v1/short/id", "/api/v1/short/id"},
	}
	for _, tt := range tests {
		got := sanitizePath(tt.input)
		if got != tt.want {
			t.Errorf("sanitizePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoggingMiddlewareWithStats_RecordsHistogram(t *testing.T) {
	t.Parallel()
	reg := stats.NewRegistry()
	s := &Server{statsReg: reg}

	handler := s.loggingMiddlewareWithStats(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	snap := reg.Snapshot()
	found := false
	for _, h := range snap.Histograms {
		if h.Metric == "http.request.duration_ms" {
			found = true
			// Check tags
			hasPath := false
			hasStatus := false
			for i := 0; i < len(h.Tags)-1; i += 2 {
				if h.Tags[i] == "path" {
					hasPath = true
				}
				if h.Tags[i] == "status" && h.Tags[i+1] == "200" {
					hasStatus = true
				}
			}
			if !hasPath {
				t.Error("missing path tag")
			}
			if !hasStatus {
				t.Error("missing status tag")
			}
		}
	}
	if !found {
		t.Error("http.request.duration_ms histogram not recorded")
	}
}
