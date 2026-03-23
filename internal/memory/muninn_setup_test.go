package memory_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

func TestMuninnSetup_Login_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			http.SetCookie(w, &http.Cookie{Name: "muninn_session", Value: "testtoken"})
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := memory.NewMuninnSetupClient(srv.URL)
	cookie, err := client.Login("root", "password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if cookie == "" {
		t.Error("expected non-empty session cookie")
	}
	if cookie != "testtoken" {
		t.Errorf("got cookie %q, want %q", cookie, "testtoken")
	}
}

func TestMuninnSetup_Login_WrongCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := memory.NewMuninnSetupClient(srv.URL)
	_, err := client.Login("root", "wrong")
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

func TestMuninnSetup_Login_NoCookieReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 200 but no cookie set
	}))
	defer srv.Close()

	client := memory.NewMuninnSetupClient(srv.URL)
	_, err := client.Login("root", "password")
	if err == nil {
		t.Error("expected error when no muninn_session cookie in response")
	}
}

func TestMuninnSetup_CreateVaultAndKey_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CreateVaultAndKey first calls CreateVault (PUT /api/admin/vaults/config), then creates a key.
		if r.URL.Path == "/api/admin/vaults/config" && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/admin/keys" && r.Method == http.MethodPost {
			// Verify cookie is sent
			cookie, err := r.Cookie("muninn_session")
			if err != nil || cookie.Value != "session_cookie_value" {
				http.Error(w, "missing or wrong session cookie", http.StatusUnauthorized)
				return
			}
			resp := map[string]any{
				"token": "mk_faketoken123",
				"key":   map[string]string{"id": "abc", "vault": "huginn-steve"},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := memory.NewMuninnSetupClient(srv.URL)
	token, err := client.CreateVaultAndKey("session_cookie_value", "huginn-steve", "huginn-agent")
	if err != nil {
		t.Fatalf("CreateVaultAndKey: %v", err)
	}
	if token != "mk_faketoken123" {
		t.Errorf("got token %q, want mk_faketoken123", token)
	}
}

func TestMuninnSetup_CreateVaultAndKey_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := memory.NewMuninnSetupClient(srv.URL)
	_, err := client.CreateVaultAndKey("cookie", "huginn-steve", "huginn-agent")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestMuninnSetup_CreateVaultAndKey_EmptyTokenReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Returns 200 but with empty token
		json.NewEncoder(w).Encode(map[string]any{"token": "", "key": map[string]string{}})
	}))
	defer srv.Close()

	client := memory.NewMuninnSetupClient(srv.URL)
	_, err := client.CreateVaultAndKey("cookie", "huginn-steve", "huginn-agent")
	if err == nil {
		t.Error("expected error when token is empty")
	}
}
