package broker_test

// coverage_boost_test.go — additional tests to push broker package to 90%+.
// Targets:
//   - client.go Start: invalid URL (NewRequestWithContext error), connection refused (Do error),
//     non-JSON response (json.Unmarshal error), server error JSON (result.Error != "")
//   - client.go Refresh: connection refused, non-JSON response, result.Error, empty token
//   - relay.go ParseRelayJWT: relay_challenge decodes to != 32 bytes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/connections/broker"
)

// ---------------------------------------------------------------------------
// Start — invalid URL causes http.NewRequestWithContext to fail
// ---------------------------------------------------------------------------

func TestClient_Start_InvalidURL(t *testing.T) {
	store := &mockTokenStore{token: "jwt"}
	// Use a URL with an invalid scheme so NewRequestWithContext returns an error.
	client := broker.NewClient("://invalid-url", store)

	_, err := client.Start(context.Background(), "github", "challenge", 8477)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// Start — connection refused: httpClient.Do returns error
// ---------------------------------------------------------------------------

func TestClient_Start_ConnectionRefused(t *testing.T) {
	store := &mockTokenStore{token: "jwt"}
	client := broker.NewClient("http://127.0.0.1:1", store)

	_, err := client.Start(context.Background(), "github", "challenge", 8477)
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

// ---------------------------------------------------------------------------
// Start — server returns non-JSON response body: json.Unmarshal fails
// ---------------------------------------------------------------------------

func TestClient_Start_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("this is not json"))
	}))
	defer srv.Close()

	store := &mockTokenStore{token: "jwt"}
	client := broker.NewClient(srv.URL, store)

	_, err := client.Start(context.Background(), "github", "challenge", 8477)
	if err == nil {
		t.Fatal("expected error for non-JSON response, got nil")
	}
}

// ---------------------------------------------------------------------------
// Start — server returns JSON with error field populated
// ---------------------------------------------------------------------------

func TestClient_Start_JSONErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"error": "provider not supported",
		})
	}))
	defer srv.Close()

	store := &mockTokenStore{token: "jwt"}
	client := broker.NewClient(srv.URL, store)

	_, err := client.Start(context.Background(), "github", "challenge", 8477)
	if err == nil {
		t.Fatal("expected error when JSON error field is set, got nil")
	}
}

// ---------------------------------------------------------------------------
// Refresh — invalid URL causes http.NewRequestWithContext to fail
// ---------------------------------------------------------------------------

func TestClient_Refresh_InvalidURL(t *testing.T) {
	client := broker.NewClient("://invalid-url")

	_, err := client.Refresh(context.Background(), "github", "refresh-tok", validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// Refresh — connection refused: httpClient.Do returns error
// ---------------------------------------------------------------------------

func TestClient_Refresh_ConnectionRefused(t *testing.T) {
	client := broker.NewClient("http://127.0.0.1:1")

	_, err := client.Refresh(context.Background(), "github", "refresh-tok", validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

// ---------------------------------------------------------------------------
// Refresh — server returns non-JSON response: json.Unmarshal fails
// ---------------------------------------------------------------------------

func TestClient_Refresh_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	client := broker.NewClient(srv.URL)
	_, err := client.Refresh(context.Background(), "github", "tok", validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for non-JSON refresh response, got nil")
	}
}

// ---------------------------------------------------------------------------
// Refresh — server returns JSON with error field set
// ---------------------------------------------------------------------------

func TestClient_Refresh_JSONErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid refresh token",
		})
	}))
	defer srv.Close()

	client := broker.NewClient(srv.URL)
	_, err := client.Refresh(context.Background(), "github", "tok", validRelayChallenge)
	if err == nil {
		t.Fatal("expected error when refresh JSON error field is set, got nil")
	}
}

// ---------------------------------------------------------------------------
// Refresh — server returns JSON with empty token field
// ---------------------------------------------------------------------------

func TestClient_Refresh_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token": "",
		})
	}))
	defer srv.Close()

	client := broker.NewClient(srv.URL)
	_, err := client.Refresh(context.Background(), "github", "tok", validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for empty token in refresh response, got nil")
	}
}

// ---------------------------------------------------------------------------
// relay.go — ParseRelayJWT: relay_challenge decodes to != 32 bytes
// A valid base64url string that decodes to fewer than 32 bytes.
// ---------------------------------------------------------------------------

func TestParseRelayJWT_ShortKey(t *testing.T) {
	// "aGVsbG8=" decodes to "hello" (5 bytes), not 32.
	shortChallenge := "aGVsbG8"
	_, err := broker.ParseRelayJWT("any.token.value", shortChallenge)
	if err == nil {
		t.Fatal("expected error for relay_challenge that decodes to < 32 bytes, got nil")
	}
}
