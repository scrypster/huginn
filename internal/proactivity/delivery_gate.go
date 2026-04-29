package proactivity

import (
	"strings"
	"time"
)

// DeliveryGateRequest contains the fields used by heartbeat DM gating.
type DeliveryGateRequest struct {
	WorkflowID string
	Schedule   string
	AgentName  string
	User       string
	Summary    string
	Detail     string
	CreatedAt  time.Time
}

// EvaluateHeartbeatDeliveryGate applies heartbeat-only proactivity gating.
//
// Behavior is intentionally fail-open for non-heartbeat or underspecified events:
// - non-scheduled and non-heartbeat notifications are always allowed
// - missing agent/user scope is allowed to avoid accidental suppression
// - nil policy allows delivery
func EvaluateHeartbeatDeliveryGate(policy *Policy, req DeliveryGateRequest) (bool, string) {
	if strings.TrimSpace(req.Schedule) == "" || !IsHeartbeatEvent(req.WorkflowID, req.Summary) {
		return true, ""
	}
	if policy == nil {
		return true, ""
	}
	if strings.TrimSpace(req.AgentName) == "" || strings.TrimSpace(req.User) == "" {
		return true, ""
	}

	decision := policy.Allow(Event{
		AgentName:  req.AgentName,
		SpaceID:    req.User,
		Summary:    req.Summary,
		Detail:     req.Detail,
		OccurredAt: req.CreatedAt,
	})
	if decision.Allowed {
		return true, ""
	}
	if decision.Reason != "" {
		return false, decision.Reason
	}
	return false, decision.ReasonCode
}
