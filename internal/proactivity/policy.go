package proactivity

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// ReasonAllowed means the proactive message passed all gates.
	ReasonAllowed = "allowed"
	// ReasonBudgetExhausted means the per-day initiative budget is depleted.
	ReasonBudgetExhausted = "budget_exhausted"
	// ReasonLowRelevance means the candidate did not meet the relevance threshold.
	ReasonLowRelevance = "low_relevance"
	// ReasonCooldownActive means the anti-spam cooldown has not elapsed yet.
	ReasonCooldownActive = "cooldown_active"
	// ReasonInvalidScope means agent/space scope could not be derived.
	ReasonInvalidScope = "invalid_scope"
)

const (
	DefaultDailyBudget  = 4
	DefaultMinRelevance = 0.45
	DefaultCooldown     = 30 * time.Minute
)

// Config controls initiative guardrails for proactive messages.
type Config struct {
	// DailyBudget is the maximum proactive messages allowed per agent+space+day.
	// Values <= 0 default to DefaultDailyBudget.
	DailyBudget int
	// MinRelevance is the minimum [0..1] relevance score required to send.
	// Values <= 0 default to DefaultMinRelevance.
	MinRelevance float64
	// Cooldown is the minimum delay between two proactive sends in the same scope.
	// Values <= 0 default to DefaultCooldown.
	Cooldown time.Duration
}

// Event describes one candidate proactive message.
type Event struct {
	AgentName string
	SpaceID   string
	Summary   string
	Detail    string
	// Relevance is optional. When <= 0, ScoreRelevance(summary, detail) is used.
	Relevance float64
	// OccurredAt defaults to policy clock when zero.
	OccurredAt time.Time
}

// Decision is the policy outcome.
type Decision struct {
	Allowed         bool
	ReasonCode      string
	Reason          string
	RemainingBudget int
	RetryAfter      time.Duration
}

type scopeState struct {
	dayKey        string
	usedToday     int
	lastAllowedAt time.Time
}

// Policy applies initiative budget, relevance, and cooldown checks.
type Policy struct {
	mu    sync.Mutex
	cfg   Config
	nowFn func() time.Time
	state map[string]scopeState
}

// Option configures Policy behavior.
type Option func(*Policy)

// WithClock injects a deterministic clock (used by tests).
func WithClock(nowFn func() time.Time) Option {
	return func(p *Policy) {
		if nowFn != nil {
			p.nowFn = nowFn
		}
	}
}

// NewPolicy constructs a policy with sensible defaults.
func NewPolicy(cfg Config, opts ...Option) *Policy {
	p := &Policy{
		cfg: Config{
			DailyBudget:  cfg.DailyBudget,
			MinRelevance: cfg.MinRelevance,
			Cooldown:     cfg.Cooldown,
		},
		nowFn: time.Now,
		state: map[string]scopeState{},
	}
	if p.cfg.DailyBudget <= 0 {
		p.cfg.DailyBudget = DefaultDailyBudget
	}
	if p.cfg.MinRelevance <= 0 {
		p.cfg.MinRelevance = DefaultMinRelevance
	}
	if p.cfg.MinRelevance > 1 {
		p.cfg.MinRelevance = 1
	}
	if p.cfg.Cooldown <= 0 {
		p.cfg.Cooldown = DefaultCooldown
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Allow evaluates the event and records usage when allowed.
func (p *Policy) Allow(e Event) Decision {
	agent := strings.TrimSpace(strings.ToLower(e.AgentName))
	space := strings.TrimSpace(strings.ToLower(e.SpaceID))
	if agent == "" || space == "" {
		return Decision{
			Allowed:    false,
			ReasonCode: ReasonInvalidScope,
			Reason:     "cannot evaluate proactivity policy without agent and space scope",
		}
	}

	at := e.OccurredAt
	if at.IsZero() {
		at = p.nowFn().UTC()
	} else {
		at = at.UTC()
	}
	relevance := e.Relevance
	if relevance <= 0 {
		relevance = ScoreRelevance(e.Summary, e.Detail)
	}
	if relevance < p.cfg.MinRelevance {
		return Decision{
			Allowed:         false,
			ReasonCode:      ReasonLowRelevance,
			Reason:          fmt.Sprintf("relevance %.2f is below threshold %.2f", relevance, p.cfg.MinRelevance),
			RemainingBudget: p.cfg.DailyBudget,
		}
	}

	key := agent + "::" + space
	dayKey := at.Format("2006-01-02")

	p.mu.Lock()
	defer p.mu.Unlock()

	st := p.state[key]
	if st.dayKey != dayKey {
		st.dayKey = dayKey
		st.usedToday = 0
		st.lastAllowedAt = time.Time{}
	}
	if st.usedToday >= p.cfg.DailyBudget {
		return Decision{
			Allowed:         false,
			ReasonCode:      ReasonBudgetExhausted,
			Reason:          fmt.Sprintf("daily initiative budget exhausted (%d/%d)", st.usedToday, p.cfg.DailyBudget),
			RemainingBudget: 0,
		}
	}
	if !st.lastAllowedAt.IsZero() {
		if elapsed := at.Sub(st.lastAllowedAt); elapsed < p.cfg.Cooldown {
			retry := p.cfg.Cooldown - elapsed
			if retry < 0 {
				retry = 0
			}
			return Decision{
				Allowed:         false,
				ReasonCode:      ReasonCooldownActive,
				Reason:          fmt.Sprintf("cooldown active (%s remaining)", retry.Round(time.Second)),
				RetryAfter:      retry,
				RemainingBudget: p.cfg.DailyBudget - st.usedToday,
			}
		}
	}

	st.usedToday++
	st.lastAllowedAt = at
	p.state[key] = st
	return Decision{
		Allowed:         true,
		ReasonCode:      ReasonAllowed,
		Reason:          "eligible for proactive delivery",
		RemainingBudget: p.cfg.DailyBudget - st.usedToday,
	}
}

var lowSignalPhrases = []string{
	"nothing to report",
	"nothing unusual",
	"no updates",
	"no action needed",
	"all clear",
}

var highSignalPhrases = []string{
	"action required",
	"needs your input",
	"blocked",
	"failed",
	"error",
	"urgent",
	"deadline",
	"overdue",
	"attention",
}

// ScoreRelevance maps proactive content to a deterministic relevance score [0..1].
func ScoreRelevance(summary, detail string) float64 {
	text := strings.ToLower(strings.TrimSpace(summary + "\n" + detail))
	if text == "" {
		return 0
	}
	for _, phrase := range lowSignalPhrases {
		if strings.Contains(text, phrase) {
			return 0.15
		}
	}
	for _, phrase := range highSignalPhrases {
		if strings.Contains(text, phrase) {
			return 0.9
		}
	}
	if len(text) < 48 {
		return 0.4
	}
	return 0.65
}

// IsHeartbeatEvent returns true for the managed heartbeat workflow path.
func IsHeartbeatEvent(workflowID, summary string) bool {
	id := strings.ToLower(strings.TrimSpace(workflowID))
	if strings.HasPrefix(id, "heartbeat-") {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(summary))
	return strings.HasPrefix(s, "[heartbeat:")
}
