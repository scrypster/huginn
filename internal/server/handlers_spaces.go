package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/spaces"
)

func jsonCreated(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("jsonCreated: encode error", "err", err)
	}
}

func (s *Server) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	kind := r.URL.Query().Get("kind")
	includeArchived := r.URL.Query().Get("archived") == "true"
	cursor := r.URL.Query().Get("cursor")
	companyID := r.URL.Query().Get("company_id")
	res, err := s.spaceStore.ListSpaces(spaces.ListOpts{
		Kind: kind, IncludeArchived: includeArchived, Cursor: cursor, CompanyID: companyID,
	})
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	// Surface the next-page cursor in both the response header and body so
	// callers can choose whichever is more convenient.
	if res.NextCursor != "" {
		w.Header().Set("X-Next-Cursor", res.NextCursor)
	}
	jsonOK(w, res)
}

func (s *Server) handleGetSpace(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	id := r.PathValue("id")
	if err := validateSpaceID(id); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	sp, err := s.spaceStore.GetSpace(id)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	jsonOK(w, sp)
}

func (s *Server) handleGetOrCreateDM(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	agent := r.PathValue("agent")
	if err := validateSubdomain(agent); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	sp, err := s.spaceStore.OpenDM(agent)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	// Broadcast space_created so other connected clients add this DM to their
	// sidebar without a refresh. The frontend handler deduplicates by ID, so
	// broadcasting for both new and pre-existing DMs is safe.
	s.emitSpaceEvent("space_created", sp)
	jsonOK(w, sp)
}

func (s *Server) handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	var body struct {
		Name      string   `json:"name"`
		LeadAgent string   `json:"lead_agent"`
		Members   []string `json:"member_agents"`
		Icon      string   `json:"icon"`
		Color     string   `json:"color"`
		CompanyID string   `json:"company_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024) // 32 KB cap — generous for space metadata
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		jsonError(w, 400, "name is required")
		return
	}
	if len(body.Name) > 80 {
		jsonError(w, 400, "name must be 80 characters or fewer")
		return
	}
	// Prevent duplicate channel names inside the same company (or desk).
	// Two companies may both have "#eng".
	if body.LeadAgent == "" {
		jsonError(w, 400, "lead_agent is required")
		return
	}
	if err := validateSubdomain(body.LeadAgent); err != nil {
		jsonError(w, 400, "lead_agent: "+err.Error())
		return
	}
	if err := s.validateAgentExists(body.LeadAgent); err != nil {
		jsonError(w, 422, err.Error())
		return
	}
	if len(body.Members) > 20 {
		jsonError(w, 400, "too many member agents (max 20)")
		return
	}
	seenMembers := make(map[string]struct{}, len(body.Members))
	for _, m := range body.Members {
		if _, dup := seenMembers[strings.ToLower(m)]; dup {
			jsonError(w, 400, fmt.Sprintf("duplicate member_agent %q", m))
			return
		}
		seenMembers[strings.ToLower(m)] = struct{}{}
		if err := validateSubdomain(m); err != nil {
			jsonError(w, 400, fmt.Sprintf("member_agent %q: %s", m, err.Error()))
			return
		}
		if err := s.validateAgentExists(m); err != nil {
			jsonError(w, 422, fmt.Sprintf("member_agent %q: %s", m, err.Error()))
			return
		}
	}
	if cid := strings.TrimSpace(body.CompanyID); cid != "" {
		if err := validateSpaceID(cid); err != nil {
			jsonError(w, 400, "company_id: "+err.Error())
			return
		}
		cs := s.companyAPI()
		if cs == nil {
			jsonError(w, 503, "companies not configured")
			return
		}
		if _, err := cs.GetCompany(cid); err != nil {
			jsonSpaceError(w, err)
			return
		}
		body.CompanyID = cid
	}
	var (
		sp  *spaces.Space
		err error
	)
	if creator, ok := s.spaceStore.(interface {
		CreateChannelForCompany(name, leadAgent string, members []string, icon, color, companyID string) (*spaces.Space, error)
	}); ok {
		sp, err = creator.CreateChannelForCompany(body.Name, body.LeadAgent, body.Members, body.Icon, body.Color, body.CompanyID)
	} else {
		sp, err = s.spaceStore.CreateChannel(body.Name, body.LeadAgent, body.Members, body.Icon, body.Color)
		if err == nil && body.CompanyID != "" {
			cid := body.CompanyID
			sp, err = s.spaceStore.UpdateSpace(sp.ID, spaces.SpaceUpdates{CompanyID: &cid})
		}
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			jsonError(w, 409, fmt.Sprintf("a channel named %q already exists", body.Name))
			return
		}
		jsonSpaceError(w, err)
		return
	}
	s.emitSpaceEvent("space_created", sp)
	jsonCreated(w, sp)
}

func (s *Server) handleUpdateSpace(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	id := r.PathValue("id")
	if err := validateSpaceID(id); err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	// Fetch the space first so we can return 403 for DMs before body parsing.
	existing, err := s.spaceStore.GetSpace(id)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	if existing.Kind == spaces.KindDM {
		jsonError(w, 403, spaces.ErrImmutableDM.Message)
		return
	}

	var body struct {
		Name      *string   `json:"name"`
		Icon      *string   `json:"icon"`
		Color     *string   `json:"color"`
		Members   *[]string `json:"member_agents"`
		LeadAgent *string   `json:"lead_agent"`
		CompanyID *string   `json:"company_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 32*1024) // 32 KB cap
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
	}
	if body.Name != nil {
		if strings.TrimSpace(*body.Name) == "" {
			jsonError(w, 400, "name cannot be empty")
			return
		}
		if len(*body.Name) > 80 {
			jsonError(w, 400, "name must be 80 characters or fewer")
			return
		}
	}
	if body.Members != nil {
		if len(*body.Members) > 20 {
			jsonError(w, 400, "too many member agents (max 20)")
			return
		}
		seenMembers := make(map[string]struct{}, len(*body.Members))
		for _, m := range *body.Members {
			if _, dup := seenMembers[strings.ToLower(m)]; dup {
				jsonError(w, 400, fmt.Sprintf("duplicate member_agent %q", m))
				return
			}
			seenMembers[strings.ToLower(m)] = struct{}{}
			if err := validateSubdomain(m); err != nil {
				jsonError(w, 400, fmt.Sprintf("member_agent %q: %s", m, err.Error()))
				return
			}
			if err := s.validateAgentExists(m); err != nil {
				jsonError(w, 422, fmt.Sprintf("member_agent %q: %s", m, err.Error()))
				return
			}
		}
	}
	if body.LeadAgent != nil {
		if err := validateSubdomain(*body.LeadAgent); err != nil {
			jsonError(w, 400, "lead_agent: "+err.Error())
			return
		}
		if err := s.validateAgentExists(*body.LeadAgent); err != nil {
			jsonError(w, 422, err.Error())
			return
		}
	}
	if body.CompanyID != nil && strings.TrimSpace(*body.CompanyID) != "" {
		if err := validateSpaceID(*body.CompanyID); err != nil {
			jsonError(w, 400, "company_id: "+err.Error())
			return
		}
	}
	sp, err := s.spaceStore.UpdateSpace(id, spaces.SpaceUpdates{
		Name: body.Name, Icon: body.Icon, Color: body.Color, Members: body.Members, LeadAgent: body.LeadAgent, CompanyID: body.CompanyID,
	})
	if err != nil {
		jsonSpaceError(w, err)
		return
	}

	// Emit space_member_added / space_member_removed WS events when the roster changes.
	// Use existing.Members (pre-update) vs sp.Members (post-update) for an accurate diff
	// based on what the store actually persisted.
	if body.Members != nil {
		s.emitSpaceMemberEvents(id, existing.Members, sp.Members)
	}
	s.emitSpaceEvent("space_updated", sp)

	jsonOK(w, sp)
}

func (s *Server) handleDeleteSpace(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	id := r.PathValue("id")
	if err := validateSpaceID(id); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := s.spaceStore.ArchiveSpace(id); err != nil {
		jsonSpaceError(w, err)
		return
	}
	s.emitSpaceEvent("space_archived", &spaces.Space{ID: id})
	jsonOK(w, map[string]bool{"ok": true})
}

func (s *Server) handleMarkSpaceRead(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	spaceID := r.PathValue("id")
	if err := validateSpaceID(spaceID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	// Verify the space exists so we don't create orphan rows in space_read_positions.
	if _, err := s.spaceStore.GetSpace(spaceID); err != nil {
		jsonSpaceError(w, err)
		return
	}
	if err := s.spaceStore.MarkRead(spaceID); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (s *Server) handleListSpaceSessions(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	spaceID := r.PathValue("id")
	if err := validateSpaceID(spaceID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	// Verify the space exists before listing sessions so callers get 404 on
	// unknown IDs rather than a silent empty list.
	if _, err := s.spaceStore.GetSpace(spaceID); err != nil {
		jsonSpaceError(w, err)
		return
	}
	sessions, err := s.spaceStore.ListSessionsForSpace(spaceID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, sessions)
}

// handleListSpaceMessages serves GET /api/v1/space-messages/{id}.
// It returns messages from all sessions in the space in chronological order,
// supporting keyset cursor pagination for infinite scroll upward.
//
// Query parameters:
//   - before: opaque cursor token from a previous response (scroll upward)
//   - limit:  page size (default 20, clamped to [1, 100])
func (s *Server) handleListSpaceMessages(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	spaceID := r.PathValue("id")
	if err := validateSpaceID(spaceID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if n, err := strconv.Atoi(lStr); err == nil {
			limit = n
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var cursor *spaces.SpaceMsgCursor
	if c := r.URL.Query().Get("before"); c != "" {
		dc, err := spaces.DecodeSpaceMsgCursor(c)
		if err != nil {
			jsonError(w, 400, "invalid cursor")
			return
		}
		cursor = &dc
	}
	result, err := s.spaceStore.ListSpaceMessages(spaceID, cursor, limit)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, result)
}

// handleListSpaceReplies serves GET /api/v1/space-messages/{id}/replies?parent_id=.
func (s *Server) handleListSpaceReplies(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	spaceID := r.PathValue("id")
	if err := validateSpaceID(spaceID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	parentID := strings.TrimSpace(r.URL.Query().Get("parent_id"))
	replies, err := s.spaceStore.ListSpaceReplies(spaceID, parentID)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	participant, _ := s.spaceStore.HasThreadParticipation(spaceID, parentID, spaces.LocalViewer)
	unseen := 0
	if participant {
		unseen, _ = s.spaceStore.ThreadUnseenForViewer(spaceID, parentID, spaces.LocalViewer)
	}
	jsonOK(w, map[string]any{
		"messages":    replies,
		"participant": participant,
		"unseen":      unseen,
	})
}

// handlePostSpaceMessage serves POST /api/v1/space-messages/{id}.
// Body: {"content":"...","parent_id":"..."}. Persists a human message only —
// does not start an agent run.
func (s *Server) handlePostSpaceMessage(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	spaceID := r.PathValue("id")
	if err := validateSpaceID(spaceID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	var body struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		ParentID string `json:"parent_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 70*1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if strings.TrimSpace(body.ID) != "" && strings.TrimSpace(body.ID) == strings.TrimSpace(body.ParentID) {
		jsonError(w, 400, "message cannot be its own parent")
		return
	}
	msg, err := s.spaceStore.PostSpaceMessage(spaceID, body.Content, body.ParentID)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	if msg.ParentID != "" {
		resp := s.afterSpaceReplyPersisted(spaceID, msg, body.Content)
		jsonCreated(w, resp)
		return
	}
	jsonCreated(w, msg)
}

// handleDeleteSpaceMessage serves DELETE /api/v1/space-messages/{id}/{msgID}.
// Deletes that one message and, if it is a hallway root, its thread replies.
func (s *Server) handleDeleteSpaceMessage(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	spaceID := r.PathValue("id")
	if err := validateSpaceID(spaceID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	msgID := r.PathValue("msgID")
	if err := validateSpaceID(msgID); err != nil {
		jsonError(w, 400, "invalid message ID")
		return
	}
	if err := s.spaceStore.DeleteSpaceMessage(spaceID, msgID); err != nil {
		jsonSpaceError(w, err)
		return
	}
	if s.wsHub != nil {
		s.BroadcastWS(WSMessage{
			Type: "space_message_deleted",
			Payload: map[string]any{
				"space_id":   spaceID,
				"message_id": msgID,
			},
		})
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// validateSpaceID rejects IDs that could enable path traversal or SQLite injection.
// Space IDs are UUIDs or short alphanumeric slugs — they must never contain slashes,
// dots-dot sequences, or characters outside the allowed set.
func validateSpaceID(id string) error {
	if id == "" {
		return fmt.Errorf("space ID is required")
	}
	if len(id) > 128 {
		return fmt.Errorf("space ID too long")
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("space ID contains invalid character %q", c)
		}
	}
	return nil
}

// validateAgentExists checks that the named agent exists in the current agent config.
// Returns nil if the agent is found or if the config cannot be loaded (best-effort).
// Returns a non-nil error if the config loaded successfully but the agent is absent.
func (s *Server) validateAgentExists(name string) error {
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
	if err != nil || cfg == nil {
		return nil // best-effort: don't block on config load failure
	}
	for _, def := range cfg.Agents {
		if strings.EqualFold(def.Name, name) {
			return nil
		}
	}
	return fmt.Errorf("agent %q not found in agent config", name)
}

// emitSpaceMemberEvents diffs the old and new member lists and broadcasts
// space_member_added / space_member_removed events to all sessions in the space.
func (s *Server) emitSpaceMemberEvents(spaceID string, oldMembers, newMembers []string) {
	oldSet := make(map[string]struct{}, len(oldMembers))
	for _, m := range oldMembers {
		oldSet[m] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(newMembers))
	for _, m := range newMembers {
		newSet[m] = struct{}{}
	}

	// Collect session IDs that should receive the events.
	var sessionIDs []string
	if s.spaceStore != nil {
		if refs, err := s.spaceStore.ListSessionsForSpace(spaceID); err == nil {
			for _, r := range refs {
				sessionIDs = append(sessionIDs, r.ID)
			}
		}
	}

	broadcast := func(eventType, agent string) {
		payload := map[string]any{"space_id": spaceID, "agent": agent}
		for _, sid := range sessionIDs {
			s.BroadcastToSession(sid, eventType, payload)
		}
		// Also broadcast globally so listeners without a specific session
		// still receive roster events.
		s.BroadcastWS(WSMessage{Type: eventType, Payload: payload})
	}

	for _, m := range newMembers {
		if _, existed := oldSet[m]; !existed {
			broadcast("space_member_added", m)
		}
	}
	for _, m := range oldMembers {
		if _, exists := newSet[m]; !exists {
			broadcast("space_member_removed", m)
		}
	}
}

// emitSpaceEvent broadcasts a space lifecycle event (space_created, space_updated,
// space_archived) to all connected WS clients. sp may be nil for archive events
// where only the ID is known.
func (s *Server) emitSpaceEvent(eventType string, sp *spaces.Space) {
	if s.wsHub == nil {
		return
	}
	payload := map[string]any{}
	if sp != nil {
		payload["space"] = sp
		payload["space_id"] = sp.ID
	}
	s.BroadcastWS(WSMessage{Type: eventType, Payload: payload})
}

// emitSpaceActivity broadcasts a space_activity event with the current unseen
// session count for spaceID. It is called after a chat response completes so
// that all connected browser tabs can update their unseen-badge counters without
// polling. Also SendRelay the same type + payload for a remote puppet on the
// HuginnCloud satellite. Failures are logged and swallowed — badge accuracy is
// best-effort. Nil satellite / disconnected hub must not panic.
func (s *Server) emitSpaceActivity(spaceID string) {
	if s.spaceStore == nil || spaceID == "" {
		return
	}
	count, err := s.spaceStore.UnseenCount(spaceID)
	if err != nil {
		slog.Warn("server: unseen count failed", "space_id", spaceID, "err", err)
		return
	}
	payload := map[string]any{
		"space_id":     spaceID,
		"unseen_count": count,
	}
	msg := WSMessage{Type: "space_activity", Payload: payload}
	if s.onSpaceWS != nil {
		s.onSpaceWS(msg)
	}
	s.BroadcastWS(msg)
	s.SendRelay(relay.Message{
		Type:    relay.MessageType("space_activity"),
		SpaceID: spaceID,
		Payload: payload,
	})
}

// isUniqueConstraintError returns true if err is a SQLite UNIQUE constraint violation.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed")
}

func jsonSpaceError(w http.ResponseWriter, err error) {
	var se *spaces.SpaceError
	if errors.As(err, &se) {
		switch se.Code {
		case "space_not_found", "company_not_found", "parent_not_found":
			jsonError(w, 404, se.Message)
			return
		case "dm_immutable", "not_in_company", "not_in_roster":
			jsonError(w, 403, se.Message)
			return
		case "invalid_name", "invalid_agent", "invalid_content", "invalid_parent", "parent_wrong_space", "lead_not_seated":
			jsonError(w, 400, se.Message)
			return
		case "company_name_taken", "channel_name_taken", "company_has_spaces", "company_reserved":
			jsonError(w, 409, se.Message)
			return
		}
	}
	jsonError(w, 500, err.Error())
}
