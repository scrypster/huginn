package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testServerToken = "sekrit-token-123"

func TestWSTokenFromRequest_SubprotocolPreferred(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "huginn-token."+testServerToken)

	tok, subprotocol := wsTokenFromRequest(r)

	if tok != testServerToken {
		t.Fatalf("token = %q, want %q", tok, testServerToken)
	}
	if subprotocol != "huginn-token."+testServerToken {
		t.Fatalf("subprotocol = %q, want echoed huginn-token.<token>", subprotocol)
	}
}

func TestWSTokenFromRequest_QueryParamFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws?token="+testServerToken, nil)

	tok, subprotocol := wsTokenFromRequest(r)

	if tok != testServerToken {
		t.Fatalf("token = %q, want %q", tok, testServerToken)
	}
	if subprotocol != "" {
		t.Fatalf("subprotocol = %q, want empty (query-param path never echoes a subprotocol)", subprotocol)
	}
}

func TestWSTokenFromRequest_SubprotocolWinsOverQueryParam(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws?token=wrong-token", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "huginn-token."+testServerToken)

	tok, _ := wsTokenFromRequest(r)

	if tok != testServerToken {
		t.Fatalf("token = %q, want subprotocol token %q to win over query param", tok, testServerToken)
	}
}

func TestWSTokenFromRequest_IgnoresOtherSubprotocols(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws?token="+testServerToken, nil)
	r.Header.Set("Sec-WebSocket-Protocol", "some-other-protocol, another-one")

	tok, subprotocol := wsTokenFromRequest(r)

	if tok != testServerToken {
		t.Fatalf("token = %q, want query-param fallback %q", tok, testServerToken)
	}
	if subprotocol != "" {
		t.Fatalf("subprotocol = %q, want empty when no huginn-token.* offered", subprotocol)
	}
}

func TestWSAuthorizeToken_ValidSubprotocol(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "huginn-token."+testServerToken)

	ok, subprotocol := wsAuthorizeToken(r, testServerToken)

	if !ok {
		t.Fatal("expected ok = true for matching subprotocol token")
	}
	if subprotocol != "huginn-token."+testServerToken {
		t.Fatalf("subprotocol = %q, want the matched subprotocol echoed back", subprotocol)
	}
}

func TestWSAuthorizeToken_ValidQueryParam(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws?token="+testServerToken, nil)

	ok, subprotocol := wsAuthorizeToken(r, testServerToken)

	if !ok {
		t.Fatal("expected ok = true for matching query-param token (TUI/back-compat path)")
	}
	if subprotocol != "" {
		t.Fatalf("subprotocol = %q, want empty for query-param auth", subprotocol)
	}
}

func TestWSAuthorizeToken_InvalidToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws?token=nope", nil)

	ok, _ := wsAuthorizeToken(r, testServerToken)

	if ok {
		t.Fatal("expected ok = false for mismatched token")
	}
}

func TestWSAuthorizeToken_InvalidSubprotocolToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "huginn-token.nope")

	ok, _ := wsAuthorizeToken(r, testServerToken)

	if ok {
		t.Fatal("expected ok = false for mismatched subprotocol token")
	}
}

func TestWSAuthorizeToken_Empty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)

	ok, subprotocol := wsAuthorizeToken(r, testServerToken)

	if ok {
		t.Fatal("expected ok = false when neither subprotocol nor query param is present")
	}
	if subprotocol != "" {
		t.Fatalf("subprotocol = %q, want empty", subprotocol)
	}
}
