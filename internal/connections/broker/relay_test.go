package broker_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/scrypster/huginn/internal/connections/broker"
)

// makeTestRelayJWT creates a signed relay JWT for use in tests.
// key must be the 32 raw bytes derived from validRelayChallenge.
func makeTestRelayJWT(t *testing.T, key []byte, provider, accessToken, refreshToken string, expiry int64) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"provider":      provider,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expiry":        float64(expiry),
		"account_label": "testuser",
		"iat":           now.Unix(),
		"exp":           now.Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign test relay JWT: %v", err)
	}
	return signed
}

func TestParseRelayJWT_Valid(t *testing.T) {
	key, err := base64.RawURLEncoding.DecodeString(validRelayChallenge)
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	expiry := time.Now().Add(time.Hour).Unix()
	signed := makeTestRelayJWT(t, key, "github", "gho_access", "ghr_refresh", expiry)

	result, err := broker.ParseRelayJWT(signed, validRelayChallenge)
	if err != nil {
		t.Fatalf("ParseRelayJWT: %v", err)
	}
	if result.Provider != "github" {
		t.Errorf("provider: got %q, want github", result.Provider)
	}
	if result.AccessToken != "gho_access" {
		t.Errorf("access_token: got %q, want gho_access", result.AccessToken)
	}
	if result.RefreshToken != "ghr_refresh" {
		t.Errorf("refresh_token: got %q, want ghr_refresh", result.RefreshToken)
	}
	if result.Expiry != expiry {
		t.Errorf("expiry: got %d, want %d", result.Expiry, expiry)
	}
	if result.AccountLabel != "testuser" {
		t.Errorf("account_label: got %q, want testuser", result.AccountLabel)
	}
}

func TestParseRelayJWT_WrongKey(t *testing.T) {
	key, _ := base64.RawURLEncoding.DecodeString(validRelayChallenge)
	signed := makeTestRelayJWT(t, key, "github", "tok", "", 0)

	// Use a different (but valid) relay_challenge — should fail verification.
	wrongChallenge := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXowMTIzNDU"
	_, err := broker.ParseRelayJWT(signed, wrongChallenge)
	if err == nil {
		t.Fatal("expected error for wrong relay_challenge, got nil")
	}
}

func TestParseRelayJWT_Expired(t *testing.T) {
	key, _ := base64.RawURLEncoding.DecodeString(validRelayChallenge)
	// Build JWT with exp in the past.
	past := time.Now().Add(-time.Hour)
	claims := jwt.MapClaims{
		"provider":     "github",
		"access_token": "tok",
		"iat":          past.Add(-10 * time.Minute).Unix(),
		"exp":          past.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := tok.SignedString(key)

	_, err := broker.ParseRelayJWT(signed, validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for expired relay JWT, got nil")
	}
}

func TestParseRelayJWT_MissingRequiredFields(t *testing.T) {
	key, _ := base64.RawURLEncoding.DecodeString(validRelayChallenge)

	cases := []struct {
		name   string
		claims jwt.MapClaims
	}{
		{
			"missing provider",
			jwt.MapClaims{
				"access_token": "tok",
				"iat":          time.Now().Unix(),
				"exp":          time.Now().Add(10 * time.Minute).Unix(),
			},
		},
		{
			"missing access_token",
			jwt.MapClaims{
				"provider": "github",
				"iat":      time.Now().Unix(),
				"exp":      time.Now().Add(10 * time.Minute).Unix(),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := jwt.NewWithClaims(jwt.SigningMethodHS256, tc.claims)
			signed, _ := tok.SignedString(key)
			_, err := broker.ParseRelayJWT(signed, validRelayChallenge)
			if err == nil {
				t.Errorf("expected error for %q, got nil", tc.name)
			}
		})
	}
}

func TestParseRelayJWT_InvalidChallenge(t *testing.T) {
	_, err := broker.ParseRelayJWT("some.token", "not-valid-base64!")
	if err == nil {
		t.Fatal("expected error for invalid relay_challenge, got nil")
	}
}

func TestParseRelayJWT_WrongSigningMethod(t *testing.T) {
	// Build a JWT with RSA signing method — must be rejected.
	// We can't easily sign with RSA in a unit test without generating a key pair,
	// so we tamper with the algorithm header manually.
	// Instead, sign with HS256 but check the "none" algorithm is rejected.
	claims := jwt.MapClaims{
		"provider":     "github",
		"access_token": "tok",
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(10 * time.Minute).Unix(),
	}
	// Use jwt.UnsafeAllowNoneSignatureType — should be rejected by ParseRelayJWT.
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign with none: %v", err)
	}
	_, err = broker.ParseRelayJWT(signed, validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for 'none' signing method, got nil")
	}
}

func TestToOAuthToken_WithExpiry(t *testing.T) {
	expiry := time.Now().Add(time.Hour).Unix()
	result := &broker.RelayResult{
		Provider:     "github",
		AccessToken:  "gho_tok",
		RefreshToken: "ghr_tok",
		AccountLabel: "user",
		Expiry:       expiry,
	}
	tok := result.ToOAuthToken()
	if tok.AccessToken != "gho_tok" {
		t.Errorf("access_token: got %q, want gho_tok", tok.AccessToken)
	}
	if tok.RefreshToken != "ghr_tok" {
		t.Errorf("refresh_token: got %q, want ghr_tok", tok.RefreshToken)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("token_type: got %q, want Bearer", tok.TokenType)
	}
	if tok.Expiry.Unix() != expiry {
		t.Errorf("expiry: got %d, want %d", tok.Expiry.Unix(), expiry)
	}
}

func TestToOAuthToken_NoExpiry(t *testing.T) {
	result := &broker.RelayResult{
		Provider:    "github",
		AccessToken: "gho_tok",
		Expiry:      0,
	}
	tok := result.ToOAuthToken()
	if !tok.Expiry.IsZero() {
		t.Errorf("expected zero expiry, got %v", tok.Expiry)
	}
}

func TestClient_Refresh_Success(t *testing.T) {
	key, _ := base64.RawURLEncoding.DecodeString(validRelayChallenge)
	expiry := time.Now().Add(time.Hour).Unix()
	relayJWT := makeTestRelayJWT(t, key, "github", "gho_new", "ghr_new", expiry)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.URL.Path != "/oauth/refresh" {
			t.Errorf("path: got %s, want /oauth/refresh", r.URL.Path)
		}

		var req struct {
			Provider       string `json:"provider"`
			RefreshToken   string `json:"refresh_token"`
			RelayChallenge string `json:"relay_challenge"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Provider != "github" {
			t.Errorf("provider: got %q, want github", req.Provider)
		}
		if req.RefreshToken != "ghr_old" {
			t.Errorf("refresh_token: got %q, want ghr_old", req.RefreshToken)
		}
		if req.RelayChallenge != validRelayChallenge {
			t.Errorf("relay_challenge: got %q", req.RelayChallenge)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": relayJWT})
	}))
	defer srv.Close()

	client := broker.NewClient(srv.URL)
	result, err := client.Refresh(context.Background(), "github", "ghr_old", validRelayChallenge)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.AccessToken != "gho_new" {
		t.Errorf("access_token: got %q, want gho_new", result.AccessToken)
	}
	if result.RefreshToken != "ghr_new" {
		t.Errorf("refresh_token: got %q, want ghr_new", result.RefreshToken)
	}
}

func TestClient_Refresh_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"token refresh failed"}`))
	}))
	defer srv.Close()

	client := broker.NewClient(srv.URL)
	_, err := client.Refresh(context.Background(), "github", "tok", validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for 502, got nil")
	}
}

func TestClient_Refresh_InvalidRelayJWT(t *testing.T) {
	// Server returns a token signed with a DIFFERENT key — Huginn should reject it.
	wrongKey := make([]byte, 32) // all-zero key
	claims := jwt.MapClaims{
		"provider":     "github",
		"access_token": "tok",
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	badJWT, _ := tok.SignedString(wrongKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": badJWT})
	}))
	defer srv.Close()

	client := broker.NewClient(srv.URL)
	_, err := client.Refresh(context.Background(), "github", "tok", validRelayChallenge)
	if err == nil {
		t.Fatal("expected error for invalid relay JWT signature, got nil")
	}
}
