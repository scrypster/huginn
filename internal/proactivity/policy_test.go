package proactivity

import (
	"testing"
	"time"
)

func TestPolicy_BudgetExhaustedSuppressesProactiveOutput(t *testing.T) {
	t.Parallel()
	p := NewPolicy(Config{
		DailyBudget:  2,
		MinRelevance: 0.4,
		Cooldown:     time.Second,
	})
	base := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)

	first := p.Allow(Event{
		AgentName:  "Lead",
		SpaceID:    "dm:mel",
		Relevance:  0.9,
		OccurredAt: base,
	})
	if !first.Allowed {
		t.Fatalf("first event denied: %+v", first)
	}

	second := p.Allow(Event{
		AgentName:  "Lead",
		SpaceID:    "dm:mel",
		Relevance:  0.9,
		OccurredAt: base.Add(2 * time.Second),
	})
	if !second.Allowed {
		t.Fatalf("second event denied: %+v", second)
	}

	third := p.Allow(Event{
		AgentName:  "Lead",
		SpaceID:    "dm:mel",
		Relevance:  0.9,
		OccurredAt: base.Add(4 * time.Second),
	})
	if third.Allowed {
		t.Fatalf("expected third event to be denied by budget, got %+v", third)
	}
	if third.ReasonCode != ReasonBudgetExhausted {
		t.Fatalf("reason code = %q, want %q", third.ReasonCode, ReasonBudgetExhausted)
	}
}

func TestPolicy_LowRelevanceSuppressed(t *testing.T) {
	t.Parallel()
	p := NewPolicy(Config{
		DailyBudget:  4,
		MinRelevance: 0.7,
		Cooldown:     5 * time.Minute,
	})

	decision := p.Allow(Event{
		AgentName:  "Lead",
		SpaceID:    "dm:mel",
		Relevance:  0.35,
		OccurredAt: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	if decision.Allowed {
		t.Fatalf("expected low-relevance event to be denied, got %+v", decision)
	}
	if decision.ReasonCode != ReasonLowRelevance {
		t.Fatalf("reason code = %q, want %q", decision.ReasonCode, ReasonLowRelevance)
	}
}

func TestPolicy_CooldownSuppressesBurst(t *testing.T) {
	t.Parallel()
	p := NewPolicy(Config{
		DailyBudget:  5,
		MinRelevance: 0.4,
		Cooldown:     30 * time.Minute,
	})
	base := time.Date(2026, 4, 29, 13, 0, 0, 0, time.UTC)

	first := p.Allow(Event{
		AgentName:  "Lead",
		SpaceID:    "dm:mel",
		Relevance:  0.8,
		OccurredAt: base,
	})
	if !first.Allowed {
		t.Fatalf("first event denied: %+v", first)
	}

	second := p.Allow(Event{
		AgentName:  "Lead",
		SpaceID:    "dm:mel",
		Relevance:  0.85,
		OccurredAt: base.Add(10 * time.Minute),
	})
	if second.Allowed {
		t.Fatalf("expected cooldown denial, got %+v", second)
	}
	if second.ReasonCode != ReasonCooldownActive {
		t.Fatalf("reason code = %q, want %q", second.ReasonCode, ReasonCooldownActive)
	}
	if second.RetryAfter <= 0 {
		t.Fatalf("expected positive retry_after, got %s", second.RetryAfter)
	}
}

func TestPolicy_EligibleEventAllowed(t *testing.T) {
	t.Parallel()
	p := NewPolicy(Config{
		DailyBudget:  3,
		MinRelevance: 0.4,
		Cooldown:     10 * time.Minute,
	})

	decision := p.Allow(Event{
		AgentName:  "Lead",
		SpaceID:    "dm:mel",
		Relevance:  0.75,
		OccurredAt: time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC),
	})
	if !decision.Allowed {
		t.Fatalf("expected eligible event to be allowed, got %+v", decision)
	}
	if decision.ReasonCode != ReasonAllowed {
		t.Fatalf("reason code = %q, want %q", decision.ReasonCode, ReasonAllowed)
	}
	if decision.RemainingBudget != 2 {
		t.Fatalf("remaining budget = %d, want 2", decision.RemainingBudget)
	}
}

func TestScoreRelevance(t *testing.T) {
	t.Parallel()
	if got := ScoreRelevance("All clear", "nothing to report"); got >= 0.4 {
		t.Fatalf("expected low relevance, got %.2f", got)
	}
	if got := ScoreRelevance("Build failed", "action required to fix CI before release"); got < 0.8 {
		t.Fatalf("expected high relevance, got %.2f", got)
	}
}

func TestIsHeartbeatEvent(t *testing.T) {
	t.Parallel()
	if !IsHeartbeatEvent("heartbeat-assistant", "[Any] details") {
		t.Fatal("expected heartbeat id to match")
	}
	if !IsHeartbeatEvent("", "[Heartbeat: Lead] pending review") {
		t.Fatal("expected heartbeat summary marker to match")
	}
	if IsHeartbeatEvent("daily-report", "[Ops] all clear") {
		t.Fatal("did not expect non-heartbeat payload to match")
	}
}
