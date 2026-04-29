package proactivity

import (
	"strings"
	"testing"
	"time"
)

func TestHeartbeatDeliveryGate_NonHeartbeatBypassesPolicy(t *testing.T) {
	t.Parallel()
	policy := NewPolicy(Config{
		DailyBudget:  1,
		MinRelevance: 0.8,
		Cooldown:     30 * time.Minute,
	})
	allow, reason := EvaluateHeartbeatDeliveryGate(policy, DeliveryGateRequest{
		WorkflowID: "daily-digest",
		Schedule:   "0 */4 * * *",
		AgentName:  "Lead",
		User:       "dm:mel",
		Summary:    "[Daily Digest] no updates",
		Detail:     "nothing to report",
		CreatedAt:  time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	if !allow || reason != "" {
		t.Fatalf("expected non-heartbeat to bypass policy; allow=%v reason=%q", allow, reason)
	}
}

func TestHeartbeatDeliveryGate_HeartbeatPolicyEnforced(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy(Config{
		DailyBudget:  2,
		MinRelevance: 0.45,
		Cooldown:     30 * time.Minute,
	})
	req := DeliveryGateRequest{
		WorkflowID: "heartbeat-lead",
		Schedule:   "0 */4 * * *",
		AgentName:  "Lead",
		User:       "dm:mel",
		Summary:    "[Heartbeat: Lead] action required",
		Detail:     "build failed and needs your input",
	}

	allow1, reason1 := EvaluateHeartbeatDeliveryGate(policy, withTime(req, base))
	if !allow1 || reason1 != "" {
		t.Fatalf("first heartbeat should pass, allow=%v reason=%q", allow1, reason1)
	}

	allow2, reason2 := EvaluateHeartbeatDeliveryGate(policy, withTime(req, base.Add(10*time.Minute)))
	if allow2 {
		t.Fatalf("expected cooldown denial")
	}
	if !strings.Contains(strings.ToLower(reason2), "cooldown") {
		t.Fatalf("expected cooldown reason, got %q", reason2)
	}

	allow3, reason3 := EvaluateHeartbeatDeliveryGate(policy, withTime(req, base.Add(31*time.Minute)))
	if !allow3 || reason3 != "" {
		t.Fatalf("third heartbeat should pass after cooldown, allow=%v reason=%q", allow3, reason3)
	}

	allow4, reason4 := EvaluateHeartbeatDeliveryGate(policy, withTime(req, base.Add(62*time.Minute)))
	if allow4 {
		t.Fatalf("expected budget denial")
	}
	if !strings.Contains(strings.ToLower(reason4), "budget") {
		t.Fatalf("expected budget reason, got %q", reason4)
	}
}

func TestHeartbeatDeliveryGate_ScopeIsolationAndMissingScope(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 4, 29, 13, 0, 0, 0, time.UTC)
	policy := NewPolicy(Config{
		DailyBudget:  1,
		MinRelevance: 0.45,
		Cooldown:     5 * time.Minute,
	})
	template := DeliveryGateRequest{
		WorkflowID: "heartbeat-team",
		Schedule:   "0 */4 * * *",
		AgentName:  "Lead",
		Summary:    "[Heartbeat: Lead] action required",
		Detail:     "deadline risk",
	}

	allowA1, _ := EvaluateHeartbeatDeliveryGate(policy, withTime(setUser(template, "dm:alpha"), base))
	if !allowA1 {
		t.Fatal("expected first alpha heartbeat to pass")
	}
	allowA2, _ := EvaluateHeartbeatDeliveryGate(policy, withTime(setUser(template, "dm:alpha"), base.Add(10*time.Minute)))
	if allowA2 {
		t.Fatal("expected second alpha heartbeat to be budget denied")
	}

	allowB1, _ := EvaluateHeartbeatDeliveryGate(policy, withTime(setUser(template, "dm:beta"), base.Add(11*time.Minute)))
	if !allowB1 {
		t.Fatal("expected beta scope to have independent budget")
	}

	allowMissing, reasonMissing := EvaluateHeartbeatDeliveryGate(policy, withTime(setUser(template, ""), base.Add(12*time.Minute)))
	if !allowMissing || reasonMissing != "" {
		t.Fatalf("expected missing scope to fail-open; allow=%v reason=%q", allowMissing, reasonMissing)
	}
}

func TestHeartbeatDeliveryGate_NilPolicyAllows(t *testing.T) {
	t.Parallel()
	allow, reason := EvaluateHeartbeatDeliveryGate(nil, DeliveryGateRequest{
		WorkflowID: "heartbeat-lead",
		Schedule:   "0 */4 * * *",
		AgentName:  "Lead",
		User:       "dm:mel",
		Summary:    "[Heartbeat: Lead] action required",
		Detail:     "task blocked",
		CreatedAt:  time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	if !allow || reason != "" {
		t.Fatalf("expected nil policy to allow delivery; allow=%v reason=%q", allow, reason)
	}
}

func withTime(req DeliveryGateRequest, at time.Time) DeliveryGateRequest {
	req.CreatedAt = at
	return req
}

func setUser(req DeliveryGateRequest, user string) DeliveryGateRequest {
	req.User = user
	return req
}
