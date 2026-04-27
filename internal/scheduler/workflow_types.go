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
	// ModelOverride (Phase 7) lets a single step run against a different
	// model than the agent's default. Empty string means "use the agent's
	// configured model". The runner forwards this to the backend via
	// RunOptions.ModelOverride; agents.AgentDef.Model is never mutated.
	//
	// Use sparingly — the recommended UX is to clone the agent with a
	// different model. The override is the escape hatch for "Haiku for
	// classification, Sonnet for writing, Opus for review" pipelines that
	// don't need three full agents.
	ModelOverride string `yaml:"model_override,omitempty" json:"model_override,omitempty"`

	// When (Phase 8) is a conditional expression that gates step execution.
	// After all `{{run.scratch.K}}`, `{{prev.output}}` and `{{inputs.alias}}`
	// substitutions are applied, the runner trims the result and treats:
	//
	//   "", "false", "0", "no", "off" → skip the step
	//   anything else                 → run the step
	//
	// Skipped steps emit a `workflow_skipped` WS event and persist as a
	// WorkflowStepResult with Status="skipped". They DO NOT count as
	// failures and do not trigger on_failure handlers.
	When string `yaml:"when,omitempty" json:"when,omitempty"`

	// SubWorkflow (Phase 8) instructs the runner to invoke another workflow
	// (by ID) synchronously as the body of this step, in place of the agent
	// run. The sub-workflow's last-step output becomes this step's output;
	// its scratchpad is seeded from the parent run's scratchpad so the child
	// can read `{{run.scratch.KEY}}` at its first step.
	//
	// When SubWorkflow is set, Agent and Prompt are ignored. ModelOverride,
	// Connections and Vars do not propagate to the sub-workflow — those are
	// authored independently in the child's YAML.
	SubWorkflow string `yaml:"sub_workflow,omitempty" json:"sub_workflow,omitempty"`

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
	Type    string `yaml:"type"               json:"type"`               // "inbox" | "space" | "agent_dm" | "webhook" | "email"
	SpaceID string `yaml:"space_id,omitempty" json:"space_id,omitempty"` // for type=space

	// Phase 3: DM and authorship.
	// User is the recipient when type="agent_dm" (the user the agent should DM).
	// Empty means "the configured Huginn user" — bindings decide.
	User string `yaml:"user,omitempty" json:"user,omitempty"`
	// From is the author label that appears on the resulting message in DMs
	// and channels. When empty, the runner falls back to the step's Agent.
	From string `yaml:"from,omitempty" json:"from,omitempty"`

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

// NotifyMode describes the four notification states the user-facing radio
// button can render. The runner derives this from OnSuccess/OnFailure rather
// than persisting a separate field, so existing YAML stays valid.
type NotifyMode string

const (
	NotifyModeNone      NotifyMode = "none"       // both false: silent
	NotifyModeOnSuccess NotifyMode = "on_success" // only on success
	NotifyModeOnFailure NotifyMode = "on_failure" // only on failure
	NotifyModeAlways    NotifyMode = "always"     // both: always notify
)

// Mode returns the four-state notify mode derived from OnSuccess/OnFailure.
// nil receivers (no Notify config at all) collapse to NotifyModeNone so UI
// rendering and analytics treat "missing" and "explicitly silent" the same.
func (c *StepNotifyConfig) Mode() NotifyMode {
	if c == nil {
		return NotifyModeNone
	}
	switch {
	case c.OnSuccess && c.OnFailure:
		return NotifyModeAlways
	case c.OnSuccess:
		return NotifyModeOnSuccess
	case c.OnFailure:
		return NotifyModeOnFailure
	default:
		return NotifyModeNone
	}
}

// SetMode is the inverse of Mode: it normalises the OnSuccess/OnFailure
// booleans from a four-state radio. Useful for UI handlers that surface the
// modes as a single picker.
func (c *StepNotifyConfig) SetMode(mode NotifyMode) {
	if c == nil {
		return
	}
	switch mode {
	case NotifyModeAlways:
		c.OnSuccess, c.OnFailure = true, true
	case NotifyModeOnSuccess:
		c.OnSuccess, c.OnFailure = true, false
	case NotifyModeOnFailure:
		c.OnSuccess, c.OnFailure = false, true
	default:
		c.OnSuccess, c.OnFailure = false, false
	}
}

// WorkflowChainConfig configures a "trigger downstream workflow on completion"
// link. The downstream workflow runs with seeded scratchpad inputs derived
// from the upstream run.
type WorkflowChainConfig struct {
	// Next is the workflow ID to trigger when the upstream run finishes.
	Next string `yaml:"next" json:"next"`
	// OnSuccess (default true) chains when the upstream run is `complete`.
	OnSuccess bool `yaml:"on_success,omitempty" json:"on_success,omitempty"`
	// OnFailure (default false) chains when the upstream run is `failed` or
	// `partial`. Set true to build alert pipelines that always fire.
	OnFailure bool `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
}

// WorkflowRetryConfig declares workflow-level retry defaults. Any step that
// does NOT set its own MaxRetries / RetryDelay inherits these values. Steps
// MAY override either field individually; setting MaxRetries=0 explicitly
// on a step disables inheritance for that step.
type WorkflowRetryConfig struct {
	// MaxRetries is the default retry count for steps without an override.
	// Values outside [0, maxStepRetries] are clamped at validation time.
	MaxRetries int `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	// Delay is the default retry-delay duration string (e.g. "30s", "2m")
	// for steps without an override. Empty means "no delay".
	Delay string `yaml:"delay,omitempty" json:"delay,omitempty"`
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
	// Phase 5: workflow chaining. When set, the scheduler will automatically
	// trigger the next workflow on this run's terminal status (complete,
	// failed, or both depending on Chain.OnSuccess / Chain.OnFailure). The
	// downstream workflow receives the upstream run's last-step output as
	// `{{run.scratch.upstream_output}}`.
	Chain *WorkflowChainConfig `yaml:"chain,omitempty" json:"chain,omitempty"`
	// Phase 8: workflow-level retry defaults. When set, the runner uses
	// these as the implicit MaxRetries / RetryDelay for any step that does
	// NOT set its own — so users can author "retry every step three times"
	// once at the top of the YAML.
	Retry *WorkflowRetryConfig `yaml:"retry,omitempty" json:"retry,omitempty"`
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
//
// Phase 4 (observability): Latency is the wall-clock duration of the step
// from the moment the agent run started to when it returned (or errored). The
// runner populates it for every step regardless of outcome so per-run
// dashboards can chart slow steps without conditionally filtering. TokensIn /
// TokensOut are placeholders for backend token-usage reporting; populated
// when the underlying backend exposes usage. Cost is derived from tokens and
// the model's pricing — left at 0 when pricing isn't configured.
type WorkflowStepResult struct {
	Position    int        `json:"position"`
	Slug        string     `json:"slug"`
	RoutineID   string     `json:"routine_id"`
	SessionID   string     `json:"session_id,omitempty"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	Output      string     `json:"output,omitempty"`
	StartedAt   time.Time  `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	LatencyMs   int64      `json:"latency_ms,omitempty"`
	TokensIn    int        `json:"tokens_in,omitempty"`
	TokensOut   int        `json:"tokens_out,omitempty"`
	CostUSD     float64    `json:"cost_usd,omitempty"`
	// SkipReason is set when Status=="skipped" (e.g. "when_false").
	SkipReason string `json:"skip_reason,omitempty"`
	// WhenResolved is the post-substitution When expression when SkipReason is "when_false".
	WhenResolved string `json:"when_resolved,omitempty"`
}

// WorkflowRun is a single execution record for a Workflow.
//
// Phase 6 (run analytics) added two fields:
//   - TriggerInputs records the seed inputs used to start the run, so a
//     downstream caller can replay or fork with identical inputs.
//   - WorkflowSnapshot captures the workflow definition AT THE TIME the run
//     started. Replays run against the snapshot so a later YAML edit cannot
//     "rewrite history". Forks default to the snapshot but the API permits
//     overriding to use the current definition instead.
type WorkflowRun struct {
	ID          string               `json:"id"`
	WorkflowID  string               `json:"workflow_id"`
	Status      WorkflowRunStatus    `json:"status"`
	Steps       []WorkflowStepResult `json:"steps"`
	StartedAt   time.Time            `json:"started_at"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	Error       string               `json:"error,omitempty"`

	// Phase 6: replay/fork support.
	TriggerInputs    map[string]string `json:"trigger_inputs,omitempty"`
	WorkflowSnapshot *Workflow         `json:"workflow_snapshot,omitempty"`
}
