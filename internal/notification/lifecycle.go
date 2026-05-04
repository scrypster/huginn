package notification

import (
	"fmt"
)

var validStatuses = map[Status]struct{}{
	StatusPending:   {},
	StatusSeen:      {},
	StatusDismissed: {},
	StatusApproved:  {},
	StatusExecuted:  {},
	StatusFailed:    {},
}

var allowedTransitions = map[Status]map[Status]struct{}{
	StatusPending: {
		StatusSeen:      {},
		StatusDismissed: {},
		StatusApproved:  {},
	},
	StatusSeen: {
		StatusDismissed: {},
		StatusApproved:  {},
	},
	StatusApproved: {
		StatusExecuted: {},
		StatusFailed:   {},
	},
}

// IsValidStatus reports whether st is a defined notification lifecycle state.
func IsValidStatus(st Status) bool {
	_, ok := validStatuses[st]
	return ok
}

// CanTransition reports whether transitioning from current to next is allowed.
func CanTransition(current, next Status) bool {
	if current == next {
		return true
	}
	nexts, ok := allowedTransitions[current]
	if !ok {
		return false
	}
	_, ok = nexts[next]
	return ok
}

// ValidateTransition returns an error when the lifecycle transition is invalid.
func ValidateTransition(current, next Status) error {
	if !IsValidStatus(current) {
		return fmt.Errorf("invalid current status %q", current)
	}
	if !IsValidStatus(next) {
		return fmt.Errorf("invalid target status %q", next)
	}
	if !CanTransition(current, next) {
		return fmt.Errorf("invalid transition %q -> %q", current, next)
	}
	return nil
}

// ResolveApproveAction validates the approve action selector against n.
//
// Rules:
//   - no proposed actions: reject
//   - one proposed action: empty actionID is accepted and resolves that action
//   - multiple proposed actions: actionID is required and must match one ID
func ResolveApproveAction(n *Notification, actionID string) (*ProposedAction, error) {
	if n == nil {
		return nil, fmt.Errorf("notification is nil")
	}
	if len(n.ProposedActions) == 0 {
		return nil, fmt.Errorf("notification has no proposed actions to approve")
	}
	if actionID == "" {
		if len(n.ProposedActions) == 1 {
			return &n.ProposedActions[0], nil
		}
		return nil, fmt.Errorf("proposed_action_id required when multiple actions exist")
	}
	for i := range n.ProposedActions {
		if n.ProposedActions[i].ID == actionID {
			return &n.ProposedActions[i], nil
		}
	}
	return nil, fmt.Errorf("unknown proposed_action_id %q", actionID)
}
