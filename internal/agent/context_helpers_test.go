package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/workforce"
)

// TestWithDelegationContext_RoundTrip verifies that a DelegationContext
// stored in the context via WithDelegationContext is retrievable via
// GetDelegationContext.
func TestWithDelegationContext_RoundTrip(t *testing.T) {
	dc := workforce.NewDelegationContext("session-1", "alice", 3)
	ctx := WithDelegationContext(context.Background(), &dc)
	got := GetDelegationContext(ctx)
	if got == nil {
		t.Fatal("expected non-nil DelegationContext")
	}
}

// TestGetDelegationContext_NilWhenAbsent verifies that GetDelegationContext
// returns nil when no context has been attached.
func TestGetDelegationContext_NilWhenAbsent(t *testing.T) {
	if got := GetDelegationContext(context.Background()); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestGetDelegationContext_NilContext verifies that GetDelegationContext
// handles a nil context gracefully.
func TestGetDelegationContext_NilContext(t *testing.T) {
	if got := GetDelegationContext(nil); got != nil {
		t.Errorf("expected nil for nil context, got %+v", got)
	}
}

// TestSetSessionID_RoundTrip verifies that SetSessionID / GetSessionID
// are consistent.
func TestSetSessionID_RoundTrip(t *testing.T) {
	ctx := SetSessionID(context.Background(), "test-session-42")
	got := GetSessionID(ctx)
	if got != "test-session-42" {
		t.Errorf("got %q, want %q", got, "test-session-42")
	}
}

// TestGetSessionID_EmptyWhenAbsent verifies GetSessionID returns empty string
// when no session has been attached.
func TestGetSessionID_EmptyWhenAbsent(t *testing.T) {
	if got := GetSessionID(context.Background()); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
