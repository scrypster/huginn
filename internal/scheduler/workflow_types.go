// internal/scheduler/workflow_types.go
package scheduler

import (
	"fmt"
	"time"
)

// maxStepRetries caps MaxRetries to prevent runaway retry loops.
const maxStepRetries = 10

// StepInput references another step's output for use in this step's prompt.
type StepInput struct {
	FromStep string `yaml:"from_step" json:"from_step"`
	As       string `yaml:"as"        json:"as"`
}

// WorkflowStep is one step in a Workflow.
// Either set Routine (legacy: reference to an external Routine by slug)
// OR set inline fields (Name, Agent, Prompt, etc.) — the unified model.
type WorkflowStep struct {
	// Legacy: reference to an external Routine by slug (kept for migration compatibility)
	Routine string `yaml:"routine,omitempty" json:"routine,omitempty"`
	// Inline step fields (new unified model)
	Name        string            `yaml:"name,omitempty"         json:"name,omitempty"`
	Agent       string            `yaml:"agent,omitempty"        json:"agent,omitempty"`
	Prompt      string            `yaml:"prompt,omitempty"       json:"prompt,omitempty"`
	Connections map[string]string `yaml:"connections,omitempty"  json:"connections,omitempty"`
	Vars        map[string]string `yaml:"vars,omitempty"         json:"vars,omitempty"`
	Inputs      []StepInput       `yaml:"inputs,omitempty"       json:"inputs,omitempty"`
	Position    int               `yaml:"position"               json:"position"`
	OnFailure   string            `yaml:"on_failure,omitempty"   json:"on_failure,omitempty"`
	MaxRetries  int               `yaml:"max_retries,omitempty"  json:"max_retries,omitempty"`
	RetryDelay  string            `yaml:"retry_delay,omitempty"  json:"retry_delay,omitempty"`
	// Timeout is the maximum duration a single step execution may run (e.g. "30m", "2h").
	// Empty string means no per-step limit; the workflow-level timeout still applies.
	// Valid range: 1s–24h. Shorter value wins when both step and workflow timeouts are set.
	Timeout     string            `yaml:"timeout,omitempty"      json:"timeout,omitempty"`
	Notify      *StepNotifyConfig `yaml:"notify,omitempty"       json:"notify,omitempty"`

	// retryDelayParsed and timeoutParsed are populated by Validate; not serialised.
	retryDelayParsed time.Duration
	timeoutParsed    time.Duration
}

// Validate validates the step fields and pre-parses RetryDelay.
// Returns an error if any field value is invalid.
func (s *WorkflowStep) Validate() error {
	if s.MaxRetries < 0 {
		return fmt.Errorf("step position %d: max_retries must be >= 0", s.Position)
	}
	if s.MaxRetries > maxStepRetries {
		return fmt.Errorf("step position %d: max_retries %d exceeds maximum %d", s.Position, s.MaxRetries, maxStepRetries)
	}
	if s.RetryDelay != "" {
		d, err := time.ParseDuration(s.RetryDelay)
		if err != nil {
			return fmt.Errorf("step position %d: invalid retry_delay %q: %w", s.Position, s.RetryDelay, err)
		}
		s.retryDelayParsed = d
	}
	if s.Timeout != "" {
		d, err := time.ParseDuration(s.Timeout)
		if err != nil {
			return fmt.Errorf("step position %d: invalid timeout %q: %w", s.Position, s.Timeout, err)
		}
		if d < time.Second {
			return fmt.Errorf("step position %d: timeout %s is below 1s minimum", s.Position, s.Timeout)
		}
		if d > 24*time.Hour {
			return fmt.Errorf("step position %d: timeout %s exceeds 24h maximum", s.Position, s.Timeout)
		}
		s.timeoutParsed = d
	}
	return nil
}

// RetryDelayDuration returns the pre-parsed retry delay (0 if not set).
func (s WorkflowStep) RetryDelayDuration() time.Duration {
	return s.retryDelayParsed
}

// TimeoutDuration returns the pre-parsed step timeout (0 if not set, meaning no step-level limit).
// Defensively clamps to 24h even if Validate was not called.
func (s WorkflowStep) TimeoutDuration() time.Duration {
	if s.timeoutParsed <= 0 {
		return 0
	}
	if s.timeoutParsed > 24*time.Hour {
		return 24 * time.Hour
	}
	return s.timeoutParsed
}

// EffectiveOnFailure returns "stop" if OnFailure is empty, else the configured value.
func (s WorkflowStep) EffectiveOnFailure() string {
	if s.OnFailure == "" {
		return "stop"
	}
	return s.OnFailure
}

// NotificationDelivery configures a delivery target for workflow notifications.
type NotificationDelivery struct {
	Type    string `yaml:"type"               json:"type"`               // "inbox" | "space" | "webhook" | "email"
	SpaceID string `yaml:"space_id,omitempty" json:"space_id,omitempty"` // for type=space

	// Webhook / email common fields.
	To         string `yaml:"to,omitempty"         json:"to,omitempty"`         // webhook URL or email recipient
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty"` // named connection for credentials

	// Email-specific SMTP fields (used when Connection is empty).
	SMTPHost string `yaml:"smtp_host,omitempty" json:"smtp_host,omitempty"`
	SMTPPort string `yaml:"smtp_port,omitempty" json:"smtp_port,omitempty"`
	SMTPFrom string `yaml:"smtp_from,omitempty" json:"smtp_from,omitempty"`
	SMTPUser string `yaml:"smtp_user,omitempty" json:"smtp_user,omitempty"`
	// SMTPPass holds the SMTP password inline in the workflow config.
	// Deprecated: inline SMTP credentials are insecure (plaintext on disk).
	// Prefer setting Connection to a named Huginn SMTP/Gmail connection.
	// SMTPPass is preserved for backward compatibility but will be removed in a future release.
	SMTPPass string `yaml:"smtp_pass,omitempty" json:"smtp_pass,omitempty"`
	// SMTPPassDeprecated is set to true at validation time when SMTPPass is
	// populated without a named Connection. Use this flag to surface warnings
	// without breaking existing workflows.
	SMTPPassDeprecated bool `yaml:"-" json:"-"`
}

// SMTPPassObfuscated returns "[REDACTED]" when SMTPPass is non-empty, or ""
// when it is unset. Use this in all log messages to avoid leaking credentials.
func (d NotificationDelivery) SMTPPassObfuscated() string {
	if d.SMTPPass != "" {
		return "[REDACTED]"
	}
	return ""
}

// StepNotifyConfig controls per-step notification behavior.
type StepNotifyConfig struct {
	OnSuccess bool                   `yaml:"on_success,omitempty" json:"on_success,omitempty"`
	OnFailure bool                   `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
	DeliverTo []NotificationDelivery `yaml:"deliver_to,omitempty" json:"deliver_to,omitempty"`
}

// WorkflowNotificationConfig controls whether the Workflow emits a Notification on completion.
type WorkflowNotificationConfig struct {
	OnSuccess bool                   `yaml:"on_success,omitempty" json:"on_success,omitempty"`
	OnFailure bool                   `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
	Severity  string                 `yaml:"severity,omitempty"   json:"severity,omitempty"`
	DeliverTo []NotificationDelivery `yaml:"deliver_to,omitempty" json:"deliver_to,omitempty"`
}

// Workflow is the parsed, in-memory representation of a workflow config.
type Workflow struct {
	ID           string                     `yaml:"id"                     json:"id"`
	Slug         string                     `yaml:"slug,omitempty"         json:"slug,omitempty"`
	Name         string                     `yaml:"name"                   json:"name"`
	Description  string                     `yaml:"description,omitempty"  json:"description,omitempty"`
	Tags         []string                   `yaml:"tags,omitempty"         json:"tags,omitempty"`
	Enabled      bool                       `yaml:"enabled"                json:"enabled"`
	Schedule     string                     `yaml:"schedule"               json:"schedule"`
	Steps        []WorkflowStep             `yaml:"steps"                  json:"steps"`
	Notification WorkflowNotificationConfig `yaml:"notification,omitempty" json:"notification,omitempty"`
	FilePath     string                     `yaml:"-"                      json:"file_path,omitempty"`
	CreatedAt    time.Time                  `yaml:"created_at,omitempty"   json:"created_at,omitempty"`
	UpdatedAt    time.Time                  `yaml:"updated_at,omitempty"   json:"updated_at,omitempty"`
	// Version is an optimistic-locking counter. It is incremented by SaveWorkflow
	// on every successful write. PUT /api/v1/workflows/{id} rejects requests whose
	// submitted Version does not match the stored Version (HTTP 409 Conflict).
	// Version 0 in a submitted payload is treated as "skip version check" for
	// backward-compatibility with clients that do not yet send a version.
	Version uint64 `yaml:"version,omitempty" json:"version,omitempty"`

	// TimeoutMinutes caps how long a single workflow run may execute.
	// 0 means use the default (30 minutes). Valid range: [1, 1440] (max 24 h).
	// Server-side validation clamps out-of-range values before persisting.
	TimeoutMinutes int `yaml:"timeout_minutes,omitempty" json:"timeout_minutes,omitempty"`
}

// WorkflowRunStatus is the lifecycle state of a workflow run.
type WorkflowRunStatus string

const (
	WorkflowRunStatusRunning  WorkflowRunStatus = "running"
	WorkflowRunStatusComplete WorkflowRunStatus = "complete"
	// WorkflowRunStatusPartial means the run finished but at least one step
	// failed with on_failure: continue. All steps were attempted.
	WorkflowRunStatusPartial   WorkflowRunStatus = "partial"
	WorkflowRunStatusFailed    WorkflowRunStatus = "failed"
	WorkflowRunStatusCancelled WorkflowRunStatus = "cancelled"
)

// WorkflowStepResult holds the outcome of a single step execution.
type WorkflowStepResult struct {
	Position  int    `json:"position"`
	Slug      string `json:"slug"`
	RoutineID string `json:"routine_id"`
	SessionID string `json:"session_id,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Output    string `json:"output,omitempty"`
}

// WorkflowRun is a single execution record for a Workflow.
type WorkflowRun struct {
	ID          string               `json:"id"`
	WorkflowID  string               `json:"workflow_id"`
	Status      WorkflowRunStatus    `json:"status"`
	Steps       []WorkflowStepResult `json:"steps"`
	StartedAt   time.Time            `json:"started_at"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Error       string               `json:"error,omitempty"`
}
