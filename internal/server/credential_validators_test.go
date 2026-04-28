package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateSendGridCredentials_MissingKey(t *testing.T) {
	err := validateSendGridCredentials(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
	if err.Error() != "api_key is required" {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateSendGridCredentials_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/scopes" {
			t.Errorf("expected /v3/scopes, got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized) // 401
	}))
	defer srv.Close()

	origURL := sendgridScopesURL
	sendgridScopesURL = srv.URL + "/v3/scopes"
	defer func() { sendgridScopesURL = origURL }()

	origClient := sendgridHTTPClient
	sendgridHTTPClient = http.DefaultClient
	defer func() { sendgridHTTPClient = origClient }()

	err := validateSendGridCredentials(context.Background(), "SG.badkey")
	if err == nil || err.Error() != "invalid API key" {
		t.Errorf("expected 'invalid API key', got %v", err)
	}
}

func TestValidateSendGridCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origURL := sendgridScopesURL
	sendgridScopesURL = srv.URL + "/v3/scopes"
	defer func() { sendgridScopesURL = origURL }()

	origClient := sendgridHTTPClient
	sendgridHTTPClient = http.DefaultClient
	defer func() { sendgridHTTPClient = origClient }()

	err := validateSendGridCredentials(context.Background(), "SG.goodkey")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// ── OpenWeatherMap validator tests ────────────────────────────────────────────

func TestValidateWeatherCredentials_MissingKey(t *testing.T) {
	err := validateWeatherCredentials(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
	if err.Error() != "api_key is required" {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateWeatherCredentials_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/2.5/weather" {
			t.Errorf("expected /data/2.5/weather, got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	origURL := weatherValidationURL
	weatherValidationURL = srv.URL + "/data/2.5/weather"
	defer func() { weatherValidationURL = origURL }()

	origClient := weatherHTTPClient
	weatherHTTPClient = http.DefaultClient
	defer func() { weatherHTTPClient = origClient }()

	err := validateWeatherCredentials(context.Background(), "badkey")
	if err == nil || err.Error() != "invalid API key" {
		t.Errorf("expected 'invalid API key', got %v", err)
	}
}

func TestValidateWeatherCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origURL := weatherValidationURL
	weatherValidationURL = srv.URL + "/data/2.5/weather"
	defer func() { weatherValidationURL = origURL }()

	origClient := weatherHTTPClient
	weatherHTTPClient = http.DefaultClient
	defer func() { weatherHTTPClient = origClient }()

	err := validateWeatherCredentials(context.Background(), "goodkey")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
