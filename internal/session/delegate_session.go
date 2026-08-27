package session

import (
	"fmt"
	"strings"
	"time"
)

// LoadForDelegate loads the session delegate_to_agent will pass to
// ThreadManager.Create. An empty session ID is always an error — never a
// silent stub. If the store has no row yet but spaceID is known, a real
// session is persisted with that space_id so Create gets desk-mesh / roster
// membership. If the store is missing and there is no space_id, it errors
// instead of inventing a fake session.
func LoadForDelegate(store StoreInterface, sessionID, spaceID string) (*Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	spaceID = strings.TrimSpace(spaceID)
	if sessionID == "" {
		return nil, fmt.Errorf("delegate_to_agent: no session ID in context")
	}
	if store != nil {
		if sess, err := store.Load(sessionID); err == nil && sess != nil {
			if spaceID != "" && sess.SpaceID() == "" {
				sess.SetSpaceID(spaceID)
				if saveErr := store.SaveManifest(sess); saveErr != nil {
					return nil, fmt.Errorf("delegate_to_agent: bind space: %w", saveErr)
				}
			}
			return sess, nil
		}
		now := time.Now().UTC()
		sess := &Session{
			ID: sessionID,
			Manifest: Manifest{
				ID:        sessionID,
				SessionID: sessionID,
				SpaceID:   spaceID,
				Status:    "active",
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
			},
		}
		if err := store.SaveManifest(sess); err != nil {
			return nil, fmt.Errorf("delegate_to_agent: persist session: %w", err)
		}
		return sess, nil
	}
	if spaceID == "" {
		return nil, fmt.Errorf("delegate_to_agent: session %q not found", sessionID)
	}
	return &Session{
		ID: sessionID,
		Manifest: Manifest{
			ID:        sessionID,
			SessionID: sessionID,
			SpaceID:   spaceID,
			Status:    "active",
			Version:   1,
		},
	}, nil
}
