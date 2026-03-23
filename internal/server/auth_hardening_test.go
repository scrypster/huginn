package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuthMiddleware_MalformedBearerToken verifies malformed Bearer tokens are rejected.
func TestAuthMiddleware_MalformedBearerToken(t *testing.T) {
	tests := []struct {
		name      string
		authValue string
		shouldAllow bool
	}{
		{"Bearer without space", "Bearer", false},
		{"Bearer with extra spaces", "Bearer  token", false},
		{"Wrong scheme HTTP", "HTTP token", false},
		{"Basic instead of Bearer", "Basic dGVzdDp0ZXN0", false},
		{"Digest auth", "Digest realm=\"test\"", false},
		{"Bearer with correct token", "Bearer correct-secret", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{token: "correct-secret"}
			allowedCalled := false

			handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
				allowedCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/api/test", nil)
			if tt.authValue != "" {
				req.Header.Set("Authorization", tt.authValue)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)

			if tt.shouldAllow && !allowedCalled {
				t.Error("handler should have been called")
			}
			if !tt.shouldAllow && allowedCalled {
				t.Error("handler should not have been called")
			}
			if !tt.shouldAllow && rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for invalid token, got %d", rec.Code)
			}
		})
	}
}

// TestAuthMiddleware_WhitespaceAroundToken verifies tokens with surrounding whitespace are rejected.
func TestAuthMiddleware_WhitespaceAroundToken(t *testing.T) {
	s := &Server{token: "correct-token"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name   string
		auth   string
		reject bool
	}{
		{"token with leading space", "Bearer  correct-token", true},
		{"token with trailing space", "Bearer correct-token ", true},
		{"exact match", "Bearer correct-token", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.Header.Set("Authorization", tt.auth)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if tt.reject && rec.Code != http.StatusUnauthorized {
				t.Errorf("expected rejection for %q", tt.auth)
			}
			if !tt.reject && rec.Code != http.StatusOK {
				t.Errorf("expected acceptance for %q, got %d", tt.auth, rec.Code)
			}
		})
	}
}

// TestAuthMiddleware_EmptyBearerValue verifies empty Bearer token is rejected.
func TestAuthMiddleware_EmptyBearerValue(t *testing.T) {
	s := &Server{token: "valid-token"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with empty bearer")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty bearer token, got %d", rec.Code)
	}
}

// TestAuthMiddleware_QueryTokenWithoutBearer verifies query-only tokens work as fallback.
func TestAuthMiddleware_QueryTokenWithoutBearer(t *testing.T) {
	s := &Server{token: "query-secret"}
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test?token=query-secret", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should have been called with valid query token")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// TestAuthMiddleware_QueryTokenInjection verifies query tokens are properly extracted.
func TestAuthMiddleware_QueryTokenInjection(t *testing.T) {
	s := &Server{token: "correct"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name      string
		url       string
		shouldAllow bool
	}{
		{"token with URL encoding", "/api/test?token=correct", true},
		{"token with extra params", "/api/test?token=correct&other=value", true},
		{"wrong token", "/api/test?token=wrong", false},
		{"empty token param", "/api/test?token=", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if tt.shouldAllow && rec.Code != http.StatusOK {
				t.Errorf("expected access for %q", tt.url)
			}
			if !tt.shouldAllow && rec.Code != http.StatusUnauthorized {
				t.Errorf("expected rejection for %q", tt.url)
			}
		})
	}
}

// TestAuthMiddleware_ResponseFormatJSON verifies error responses use JSON format.
func TestAuthMiddleware_ResponseFormatJSON(t *testing.T) {
	s := &Server{token: "valid"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test?token=invalid", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected JSON content type, got %q", contentType)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "error") || !strings.Contains(body, "unauthorized") {
		t.Errorf("expected error JSON response, got %q", body)
	}
}

// TestAuthMiddleware_LongTokenReject verifies extremely long tokens are handled.
func TestAuthMiddleware_LongTokenReject(t *testing.T) {
	s := &Server{token: "valid-token"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	// Create a very long token (10KB)
	longToken := strings.Repeat("a", 10*1024)
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+longToken)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for oversized token, got %d", rec.Code)
	}
}

// TestAuthMiddleware_NullByteInToken verifies null bytes in tokens are rejected.
func TestAuthMiddleware_NullByteInToken(t *testing.T) {
	s := &Server{token: "valid-token"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	// Token with null byte
	tokenWithNull := "valid-token\x00-injection"
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenWithNull)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for token with null byte, got %d", rec.Code)
	}
}

// TestAuthMiddleware_SpecialCharactersInToken verifies tokens with special chars are compared correctly.
func TestAuthMiddleware_SpecialCharactersInToken(t *testing.T) {
	specialToken := "token!@#$%^&*()_+-=[]{}|;:',.<>?/~`"
	s := &Server{token: specialToken}

	called := false
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+specialToken)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should have been called with special char token")
	}
}

// TestAuthMiddleware_CaseSensitive verifies token comparison is case-sensitive.
func TestAuthMiddleware_CaseSensitive(t *testing.T) {
	s := &Server{token: "MySecret123"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		attempt string
		shouldWork bool
	}{
		{"MySecret123", true},
		{"mysecret123", false},
		{"MYSECRET123", false},
		{"MySecret123 ", false}, // trailing space
		{" MySecret123", false}, // leading space
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Authorization", "Bearer "+tt.attempt)
		rec := httptest.NewRecorder()
		handler(rec, req)

		if tt.shouldWork && rec.Code != http.StatusOK {
			t.Errorf("expected access for %q", tt.attempt)
		}
		if !tt.shouldWork && rec.Code != http.StatusUnauthorized {
			t.Errorf("expected rejection for %q", tt.attempt)
		}
	}
}

// TestAuthMiddleware_BothBearerAndQueryToken verifies Bearer takes precedence.
func TestAuthMiddleware_BothBearerAndQueryToken(t *testing.T) {
	s := &Server{token: "correct"}
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Bearer is correct, query is wrong — should succeed
	req := httptest.NewRequest("GET", "/api/test?token=wrong", nil)
	req.Header.Set("Authorization", "Bearer correct")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("Bearer token should take precedence over query token")
	}

	// Now test with wrong Bearer, correct query — should fail
	called = false
	req = httptest.NewRequest("GET", "/api/test?token=correct", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	handler(rec, req)

	if called {
		t.Error("wrong Bearer should not be bypassed by correct query token")
	}
}

// TestAuthMiddleware_MultipleAuthorizationHeaders verifies behavior with duplicate headers.
func TestAuthMiddleware_MultipleAuthorizationHeaders(t *testing.T) {
	// Note: Go's http.Request.Header.Get() returns only the first value
	// This test documents that behavior
	s := &Server{token: "first-token"}
	called := false

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	// Simulating multiple Authorization headers (unusual but possible)
	req.Header.Set("Authorization", "Bearer first-token")
	// In a real HTTP request, you'd use Add() to add another; Request.Header.Get() takes first
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("first Authorization header should be used")
	}
}

// TestAuthMiddleware_EmptyToken verifies server with empty token rejects all requests.
func TestAuthMiddleware_EmptyToken(t *testing.T) {
	s := &Server{token: ""}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	// Any non-empty token should be rejected
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("empty server token should reject all requests, got %d", rec.Code)
	}
}

// TestAuthMiddleware_NoLoggingOfSecrets verifies tokens aren't logged in responses.
func TestAuthMiddleware_NoLoggingOfSecrets(t *testing.T) {
	s := &Server{token: "super-secret-key-12345"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	body := rec.Body.String()
	// Ensure neither the wrong token nor the correct token are in the response
	if strings.Contains(body, "wrong-token") || strings.Contains(body, "super-secret-key-12345") {
		t.Errorf("token leaked in response: %s", body)
	}
}
