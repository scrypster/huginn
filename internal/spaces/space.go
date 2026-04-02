package spaces

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

const (
	KindDM      = "dm"
	KindChannel = "channel"
)

// Space represents a DM or Channel conversation space.
type Space struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	LeadAgent   string     `json:"lead_agent"`
	Members     []string   `json:"member_agents"`
	Icon        string     `json:"icon"`
	Color       string     `json:"color"`
	TeamID      string     `json:"team_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
	UnseenCount int        `json:"unseen_count"`
}

// ListOpts controls filtering and pagination for ListSpaces.
type ListOpts struct {
	Kind            string
	IncludeArchived bool
	Limit           int
	// Cursor is an opaque base64-encoded token returned by a prior ListSpaces call.
	// When set, results are fetched from the position after the last item in the
	// previous page. Empty string means start from the beginning.
	Cursor string
}

// ListSpacesResult is the return type for ListSpaces. It bundles the page of
// spaces with an opaque cursor for fetching the next page.
type ListSpacesResult struct {
	Spaces []*Space
	// NextCursor is empty when there are no more results, otherwise it encodes
	// the position of the last item in this page and can be passed as
	// ListOpts.Cursor in the next call.
	NextCursor string
}

// cursorPayload is the JSON structure encoded inside an opaque cursor token.
type cursorPayload struct {
	UpdatedAt string `json:"ua"` // RFC3339Nano of the last item's updated_at
	ID        string `json:"id"` // ID of the last item
}

// EncodeCursor returns a base64url-encoded opaque cursor token for the given
// updated_at timestamp and space ID. Exported so tests can create known cursors.
func EncodeCursor(updatedAt time.Time, id string) string {
	p := cursorPayload{UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano), ID: id}
	b, _ := json.Marshal(p)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeCursor decodes an opaque cursor token back into its component parts.
// Returns an error if the token is malformed or missing required fields.
func DecodeCursor(cursor string) (updatedAt time.Time, id string, err error) {
	b, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("spaces: invalid cursor encoding: %w", err)
	}
	var p cursorPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return time.Time{}, "", fmt.Errorf("spaces: invalid cursor payload: %w", err)
	}
	if p.ID == "" || p.UpdatedAt == "" {
		return time.Time{}, "", fmt.Errorf("spaces: cursor missing required fields")
	}
	t, err := time.Parse(time.RFC3339Nano, p.UpdatedAt)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("spaces: invalid cursor timestamp: %w", err)
	}
	return t, p.ID, nil
}

// SpaceUpdates carries optional fields to update on a Space.
// A nil pointer means "leave unchanged".
type SpaceUpdates struct {
	Name      *string
	Icon      *string
	Color     *string
	Members   *[]string
	LeadAgent *string
}

// SessionRef is a lightweight session summary returned by ListSessionsForSpace.
type SessionRef struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	SpaceID   string `json:"space_id"`
}

// SpaceMessageToolCall is a single tool invocation included in a SpaceMessage.
type SpaceMessageToolCall struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
}

// SpaceMessage is a single message returned by ListSpaceMessages.
type SpaceMessage struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
	// Ts is assigned by SQLite (strftime default); lexicographic string comparison
	// is safe for cursor pagination because SQLite is single-writer.
	Ts        string                 `json:"ts"`
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Agent     string                 `json:"agent"`
	ToolCalls []SpaceMessageToolCall `json:"toolCalls,omitempty"`
}

// SpaceMessagesResult is the paginated result of ListSpaceMessages.
type SpaceMessagesResult struct {
	Messages []SpaceMessage `json:"messages"`
	// NextCursor is empty when there are no older messages to load.
	// Pass it as the "before" query parameter to fetch the previous page.
	NextCursor string `json:"next_cursor"`
}

// SpaceMsgCursor encodes a position for keyset pagination of space messages.
// It points to the oldest message in the previously returned page.
type SpaceMsgCursor struct {
	Ts string `json:"ts"`
	ID string `json:"id"`
}

// EncodeSpaceMsgCursor serialises a cursor to an opaque base64url token.
func EncodeSpaceMsgCursor(ts, id string) string {
	c := SpaceMsgCursor{Ts: ts, ID: id}
	b, _ := json.Marshal(c)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeSpaceMsgCursor parses an opaque cursor token produced by EncodeSpaceMsgCursor.
func DecodeSpaceMsgCursor(token string) (SpaceMsgCursor, error) {
	b, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return SpaceMsgCursor{}, fmt.Errorf("spaces: invalid message cursor encoding: %w", err)
	}
	var c SpaceMsgCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return SpaceMsgCursor{}, fmt.Errorf("spaces: invalid message cursor payload: %w", err)
	}
	if c.ID == "" || c.Ts == "" {
		return SpaceMsgCursor{}, fmt.Errorf("spaces: message cursor missing required fields")
	}
	return c, nil
}

// SpaceCascadeResult carries the IDs of spaces affected by RemoveAgentFromAllSpaces
// so callers can emit appropriate WS events for each side effect.
type SpaceCascadeResult struct {
	ArchivedSpaceIDs []string // DMs and channels that were archived
	UpdatedSpaceIDs  []string // channels where the agent was a member but not lead (roster updated)
}

// HasChanges returns true if any spaces were modified.
func (r *SpaceCascadeResult) HasChanges() bool {
	return len(r.ArchivedSpaceIDs) > 0 || len(r.UpdatedSpaceIDs) > 0
}

// StoreInterface is the full contract for a space store implementation.
type StoreInterface interface {
	OpenDM(agentName string) (*Space, error)
	CreateChannel(name, leadAgent string, members []string, icon, color string) (*Space, error)
	GetSpace(id string) (*Space, error)
	ListSpaces(opts ListOpts) (ListSpacesResult, error)
	UpdateSpace(id string, updates SpaceUpdates) (*Space, error)
	ArchiveSpace(id string) error
	MarkRead(spaceID string) error
	UnseenCount(spaceID string) (int, error)
	ListSessionsForSpace(spaceID string) ([]SessionRef, error)
	// GetChannelsForAgent returns all non-archived channel spaces where the agent
	// is either the lead or a member. Used for DM cross-space awareness.
	GetChannelsForAgent(agentName string) ([]*Space, error)
	// SpacesByLeadAgent returns all non-archived spaces where agentName is the lead agent.
	// Used to prevent deletion of agents that are assigned as channel/space leads.
	SpacesByLeadAgent(agentName string) ([]*Space, error)
	// RemoveAgentFromAllSpaces removes an agent from all space membership lists,
	// archives any DMs for that agent, and archives channels where that agent
	// was the lead. Returns a SpaceCascadeResult describing the side effects.
	RemoveAgentFromAllSpaces(agentName string) (*SpaceCascadeResult, error)
	// ListSpaceMessages returns messages from all sessions in the space in
	// chronological order (oldest first). before is nil for the initial load
	// (returns the newest messages), or a cursor pointing to the oldest message
	// already loaded — enabling infinite scroll upward.
	// limit is clamped to [1, 100]; defaults to 20 when 0.
	ListSpaceMessages(spaceID string, before *SpaceMsgCursor, limit int) (SpaceMessagesResult, error)
}

// ErrImmutableDM is returned when attempting a mutating operation on a DM space
// that is not permitted (e.g. archiving, renaming).
var ErrImmutableDM = &SpaceError{Code: "dm_immutable", Message: "DM spaces are immutable"}

// SpaceError is a structured domain error returned by store operations.
type SpaceError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *SpaceError) Error() string { return e.Message }
