// internal/notification/types.go
package notification

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID generates a time-sortable random notification ID.
// Format: 13-hex-digit millisecond timestamp + 16-hex-digit random suffix.
// IDs sort lexicographically in creation order.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%013x", time.Now().UnixMilli()) + hex.EncodeToString(b[:])
}

// Severity indicates urgency level.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityUrgent  Severity = "urgent"
)

// Status tracks notification lifecycle state.
type Status string

const (
	StatusPending   Status = "pending"   // new, unread
	StatusSeen      Status = "seen"      // user opened it
	StatusDismissed Status = "dismissed" // user dismissed without acting
	StatusApproved  Status = "approved"  // proposed action approved, awaiting execution
	StatusExecuted  Status = "executed"  // proposed action ran successfully
	StatusFailed    Status = "failed"    // proposed action failed
)

// ProposedAction is a deterministic tool call to execute on user approval.
// Parameters are set at notification-creation time — not re-prompted at execution time.
type ProposedAction struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`       // button label, e.g. "Merge PR #42"
	ToolName    string         `json:"tool_name"`   // e.g. "bash", "write_file"
	ToolParams  map[string]any `json:"tool_params"` // literal parameters
	Destructive bool           `json:"destructive"` // show extra confirmation if true
}

// DeliveryRecord is an audit entry for an external notification delivery attempt.
type DeliveryRecord struct {
	Type   string    `json:"type"`             // "inbox" | "space"
	Target string    `json:"target"`           // spaceID for space delivery
	Status string    `json:"status"`           // "sent" | "failed"
	Error  string    `json:"error,omitempty"`
	SentAt time.Time `json:"sent_at"`
}

// Notification is a structured output from a Routine run.
type Notification struct {
	// Identity
	ID        string `json:"id"`
	RoutineID string `json:"routine_id"`
	RunID     string `json:"run_id"`

	// Phase 1: always empty; Phase 2: populated by HuginnCloud
	SatelliteID string `json:"satellite_id,omitempty"`

	// Workflow linkage (optional; set when triggered by a workflow run)
	WorkflowID    string `json:"workflow_id,omitempty"`
	WorkflowRunID string `json:"workflow_run_id,omitempty"`
	StepPosition  *int   `json:"step_position,omitempty"`
	StepName      string `json:"step_name,omitempty"`

	// Content
	Summary  string   `json:"summary"` // one-line for inbox card (≤120 chars)
	Detail   string   `json:"detail"`  // full markdown analysis
	Severity Severity `json:"severity"`

	// State
	Status    Status `json:"status"`
	SessionID string `json:"session_id,omitempty"` // set when user clicks "Chat"

	// Optional proposed actions (deterministic tool calls)
	ProposedActions []ProposedAction `json:"proposed_actions,omitempty"`

	// Delivery audit records
	Deliveries []DeliveryRecord `json:"deliveries,omitempty"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // set when superseded by newer run
}
