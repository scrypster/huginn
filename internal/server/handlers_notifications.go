// internal/server/handlers_notifications.go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/relay"
)

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	store := s.notifStore
	s.mu.Unlock()
	if store == nil {
		jsonOK(w, []any{})
		return
	}
	var notifications []*notification.Notification
	var err error
	switch {
	case r.URL.Query().Get("routine_id") != "":
		notifications, err = store.ListByRoutine(r.URL.Query().Get("routine_id"))
	case r.URL.Query().Get("workflow_id") != "":
		notifications, err = store.ListByWorkflow(r.URL.Query().Get("workflow_id"))
	default:
		notifications, err = store.ListPending()
	}
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if notifications == nil {
		notifications = []*notification.Notification{}
	}
	jsonOK(w, notifications)
}

func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	store := s.notifStore
	s.mu.Unlock()
	if store == nil {
		jsonError(w, 503, "notification store not configured")
		return
	}
	n, err := store.Get(id)
	if err != nil {
		jsonError(w, 404, "notification not found")
		return
	}
	jsonOK(w, n)
}

func (s *Server) handleNotificationAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	store := s.notifStore
	s.mu.Unlock()
	if store == nil {
		jsonError(w, 503, "notification store not configured")
		return
	}

	var body struct {
		Action           string `json:"action"`
		ProposedActionID string `json:"proposed_action_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}

	var newStatus notification.Status
	switch body.Action {
	case "dismiss":
		newStatus = notification.StatusDismissed
	case "seen":
		newStatus = notification.StatusSeen
	case "approve":
		// Guard: approving a notification that has no proposed actions leaves it
		// permanently stuck in StatusApproved with nothing to execute. Reject early
		// so the client knows the operation is a no-op rather than silently succeeding.
		n, err := store.Get(id)
		if err != nil {
			jsonError(w, 404, "notification not found")
			return
		}
		if len(n.ProposedActions) == 0 {
			jsonError(w, 422, "notification has no proposed actions to approve")
			return
		}
		newStatus = notification.StatusApproved
	default:
		jsonError(w, 400, "unknown action: "+body.Action)
		return
	}

	if err := store.Transition(id, newStatus); err != nil {
		jsonError(w, 500, "transition: "+err.Error())
		return
	}

	count, _ := store.PendingCount()
	if s.wsHub != nil {
		s.wsHub.broadcast(WSMessage{
			Type:    "notification_update",
			Payload: map[string]any{"id": id, "status": string(newStatus)},
		})
		s.wsHub.broadcast(WSMessage{
			Type:    "inbox_badge",
			Payload: map[string]any{"pending_count": count},
		})
	}
	// Push status change to HuginnCloud so the remote inbox updates in real-time.
	s.SendRelay(relay.Message{
		Type:    relay.MsgNotificationUpdate,
		Payload: map[string]any{"id": id, "status": string(newStatus), "pending_count": count},
	})

	jsonOK(w, map[string]any{"id": id, "status": string(newStatus), "pending_count": count})
}

func (s *Server) handleInboxSummary(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	store := s.notifStore
	s.mu.Unlock()
	if store == nil {
		jsonOK(w, map[string]int{"pending_count": 0, "urgent_count": 0})
		return
	}
	pending, err := store.ListPending()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	urgentCount := 0
	for _, n := range pending {
		if n.Severity == notification.SeverityUrgent {
			urgentCount++
		}
	}
	jsonOK(w, map[string]int{
		"pending_count": len(pending),
		"urgent_count":  urgentCount,
	})
}
