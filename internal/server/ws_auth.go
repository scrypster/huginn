// internal/server/ws_auth.go
package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/session"
)

// wsTokenSubprotocolPrefix is the Sec-WebSocket-Protocol prefix the web
// client uses to authenticate: "huginn-token.<token>". The browser cannot
// set custom headers on a WebSocket upgrade, so a query-string ?token=
// leaks the server token into browser console/network logs whenever a
// connection attempt fails (the full URL, including the query string, is
// logged verbatim). The subprotocol never appears in those logs.
const wsTokenSubprotocolPrefix = "huginn-token."

// wsTokenFromRequest extracts the auth token offered by a WebSocket upgrade
// request. It prefers a "huginn-token.<token>" entry in the
// Sec-WebSocket-Protocol header (used by the web client) and returns that
// exact subprotocol string so the caller can echo it back per RFC 6455 (a
// server that accepts a subprotocol MUST include it in the response, or the
// browser tears the connection down). Falls back to the legacy ?token=
// query parameter — kept for the TUI client and back-compat — in which case
// the returned subprotocol is empty (nothing to echo).
func wsTokenFromRequest(r *http.Request) (token string, subprotocol string) {
	for _, proto := range websocket.Subprotocols(r) {
		if tok, ok := strings.CutPrefix(proto, wsTokenSubprotocolPrefix); ok {
			return tok, proto
		}
	}
	return r.URL.Query().Get("token"), ""
}

// wsAuthorizeToken reports whether the request's token (from subprotocol or
// query param — see wsTokenFromRequest) matches serverToken. Uses a
// constant-time comparison to avoid a timing oracle. subprotocol is the
// exact string to echo in the Sec-WebSocket-Protocol response header when
// non-empty.
func wsAuthorizeToken(r *http.Request, serverToken string) (ok bool, subprotocol string) {
	tok, subprotocol := wsTokenFromRequest(r)
	ok = subtle.ConstantTimeCompare([]byte(tok), []byte(serverToken)) == 1
	if !ok {
		subprotocol = ""
	}
	return ok, subprotocol
}

// wsAuthorizeSession verifies that sessionID refers to an existing session in
// the store. Returns false (with a WSMessage to send back to the client) when:
//   - sessionID is empty
//   - the session does not exist in the store
//
// When the store is nil the check is skipped — single-user local mode without
// persistence is still valid.
func wsAuthorizeSession(store session.StoreInterface, sessionID string) (ok bool, errMsg WSMessage) {
	if sessionID == "" {
		return false, WSMessage{Type: "error", Content: "session_id required"}
	}
	if store == nil {
		return true, WSMessage{}
	}
	if !store.Exists(sessionID) {
		return false, WSMessage{Type: "error", Content: "session not found"}
	}
	return true, WSMessage{}
}
