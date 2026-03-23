package broker_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/connections/broker"
)

// mockTokenStore implements broker.TokenStorer for tests.
type mockTokenStore struct {
	token string
	err   error
}

func (m *mockTokenStore) Load() (string, error)  { return m.token, m.err }
func (m *mockTokenStore) IsRegistered() bool      { return m.err == nil && m.token != "" }

func TestClient_Start_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request shape.
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.URL.Path != "/oauth/start" {
			t.Errorf("path: got %s, want /oauth/start", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-jwt" {
			t.Errorf("Authorization: got %q, want %q", auth, "Bearer test-jwt")
		}

		var req struct {
			Provider            string `json:"provider"`
			CodeChallenge       string `json:"code_challenge"`
			CodeChallengeMethod string `json:"code_challenge_method"`
			Port                int    `json:"port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Provider != "github" {
			t.Errorf("provider: got %q, want github", req.Provider)
		}
		if req.CodeChallengeMethod != "S256" {
			t.Errorf("code_challenge_method: got %q, want S256", req.CodeChallengeMethod)
		}
		if req.Port != 8477 {
			t.Errorf("port: got %d, want 8477", req.Port)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"auth_url": "https://github.com/login/oauth/authorize?state=brokered",
			"state":    "teststate",
		})
	}))
	defer srv.Close()

	store := &mockTokenStore{token: "test-jwt"}
	client := broker.NewClient(srv.URL, store)

	url, err := client.Start(context.Background(), "github", "challenge123", 8477)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if url != "https://github.com/login/oauth/authorize?state=brokered" {
		t.Errorf("auth_url: got %q", url)
	}
}

func TestClient_Start_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"machine not registered"}`))
	}))
	defer srv.Close()

	store := &mockTokenStore{token: "test-jwt"}
	client := broker.NewClient(srv.URL, store)

	_, err := client.Start(context.Background(), "github", "challenge", 8477)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}

func TestClient_Start_TokenLoadError(t *testing.T) {
	store := &mockTokenStore{err: errors.New("keyring: not found")}
	client := broker.NewClient("https://example.com", store)

	_, err := client.Start(context.Background(), "github", "challenge", 8477)
	if err == nil {
		t.Fatal("expected error when token load fails, got nil")
	}
}

func TestClient_Start_EmptyAuthURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"auth_url": "", "state": "x"})
	}))
	defer srv.Close()

	store := &mockTokenStore{token: "test-jwt"}
	client := broker.NewClient(srv.URL, store)

	_, err := client.Start(context.Background(), "github", "challenge", 8477)
	if err == nil {
		t.Fatal("expected error for empty auth_url, got nil")
	}
}

func TestStartCloudFlow_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/start" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Provider string `json:"provider"`
			RelayKey string `json:"relay_key"`
		}
		json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
		if req.Provider == "" || req.RelayKey == "" {
			http.Error(w, `{"error":"missing fields"}`, 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"auth_url": "https://github.com/login/oauth/authorize?state=xyz"}) //nolint:errcheck
	}))
	defer ts.Close()

	store := &mockTokenStore{token: "machine.jwt.here"}
	c := broker.NewClient(ts.URL, store)

	relayKey := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	authURL, err := c.StartCloudFlow(context.Background(), "github", relayKey)
	if err != nil {
		t.Fatalf("StartCloudFlow: %v", err)
	}
	if authURL == "" {
		t.Fatal("expected non-empty auth URL")
	}
}

func TestStartCloudFlow_BrokerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"broker unavailable"}`, 503)
	}))
	defer ts.Close()

	store := &mockTokenStore{token: "machine.jwt.here"}
	c := broker.NewClient(ts.URL, store)

	_, err := c.StartCloudFlow(context.Background(), "github", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if err == nil {
		t.Fatal("expected error from broker")
	}
}
