// internal/server/ws_auth.go
package server

import (
	"github.com/scrypster/huginn/internal/session"
)

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
