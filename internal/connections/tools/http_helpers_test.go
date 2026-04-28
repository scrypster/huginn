package conntools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTodoistDo_SetsAuthAndContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer token-123" {
			t.Fatalf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected Content-Type header: %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	out, err := todoistDo(context.Background(), http.MethodPost, srv.URL+"/rest/v2/tasks", "token-123", strings.NewReader(`{"content":"x"}`))
	if err != nil {
		t.Fatalf("todoistDo returned error: %v", err)
	}
	if out != `{"ok":true}` {
		t.Fatalf("expected body passthrough, got %q", out)
	}
}

func TestTodoistDo_EmptyBodyReturnsSentinelJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := todoistDo(context.Background(), http.MethodPost, srv.URL+"/rest/v2/tasks/1/close", "token-123", nil)
	if err != nil {
		t.Fatalf("todoistDo returned error: %v", err)
	}
	if out != `{"ok": true}` {
		t.Fatalf("expected sentinel json for empty body, got %q", out)
	}
}

func TestTodoistDo_HTTPErrorIncludesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	_, err := todoistDo(context.Background(), http.MethodGet, srv.URL+"/rest/v2/tasks", "token-123", nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 400") || !strings.Contains(msg, "bad request") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestWeatherDo_SuccessAndErrorPaths(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"temp":21}`))
	}))
	defer okSrv.Close()

	out, err := weatherDo(context.Background(), okSrv.URL+"/data/2.5/weather?q=London")
	if err != nil {
		t.Fatalf("weatherDo returned error: %v", err)
	}
	if out != `{"temp":21}` {
		t.Fatalf("unexpected output: %q", out)
	}

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid key"}`))
	}))
	defer errSrv.Close()

	_, err = weatherDo(context.Background(), errSrv.URL+"/data/2.5/weather?q=London")
	if err == nil {
		t.Fatal("expected error for unauthorized response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 401") || !strings.Contains(msg, "invalid key") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestHADo_SetsHeadersAndHandlesEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ha-token" {
			t.Fatalf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected Content-Type header: %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	out, err := haDo(context.Background(), http.MethodPost, srv.URL+"/api/services/light/turn_on", "ha-token", strings.NewReader(`{"entity_id":"light.kitchen"}`))
	if err != nil {
		t.Fatalf("haDo returned error: %v", err)
	}
	if out != `{"ok": true}` {
		t.Fatalf("expected sentinel json for empty body, got %q", out)
	}
}

func TestHADo_HTTPErrorIncludesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	_, err := haDo(context.Background(), http.MethodGet, srv.URL+"/api/states", "ha-token", nil)
	if err == nil {
		t.Fatal("expected error for forbidden response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 403") || !strings.Contains(msg, "forbidden") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestCalendarDo_SetsContentTypeAndHandlesResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected Content-Type header: %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"evt-1"}`))
	}))
	defer srv.Close()

	out, err := calendarDo(context.Background(), srv.Client(), http.MethodPost, srv.URL+"/calendar/v3/calendars/primary/events", strings.NewReader(`{"summary":"test"}`))
	if err != nil {
		t.Fatalf("calendarDo returned error: %v", err)
	}
	if out != `{"id":"evt-1"}` {
		t.Fatalf("unexpected output: %q", out)
	}

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"backend down"}`))
	}))
	defer errSrv.Close()

	_, err = calendarDo(context.Background(), errSrv.Client(), http.MethodGet, errSrv.URL+"/calendar/v3/calendars/primary/events", nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 500") || !strings.Contains(msg, "backend down") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}
