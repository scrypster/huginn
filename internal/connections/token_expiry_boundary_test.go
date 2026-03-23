package connections

import (
	"testing"
	"time"
)

// TestTokenNeedsRefresh_ExpiringIn29s verifies that a token expiring in 29 seconds
// is considered as needing refresh (within the 30s buffer).
func TestTokenNeedsRefresh_ExpiringIn29s(t *testing.T) {
	expiry := time.Now().Add(29 * time.Second)
	if !tokenNeedsRefresh(expiry) {
		t.Error("expected tokenNeedsRefresh=true for token expiring in 29s (within 30s buffer)")
	}
}

// TestTokenNeedsRefresh_ExpiringIn31s verifies that a token expiring in 31 seconds
// is NOT considered as needing refresh (outside the 30s buffer).
func TestTokenNeedsRefresh_ExpiringIn31s(t *testing.T) {
	expiry := time.Now().Add(31 * time.Second)
	if tokenNeedsRefresh(expiry) {
		t.Error("expected tokenNeedsRefresh=false for token expiring in 31s (outside 30s buffer)")
	}
}

// TestTokenNeedsRefresh_AlreadyExpired verifies that an already-expired token
// is considered as needing refresh.
func TestTokenNeedsRefresh_AlreadyExpired(t *testing.T) {
	expiry := time.Now().Add(-1 * time.Second)
	if !tokenNeedsRefresh(expiry) {
		t.Error("expected tokenNeedsRefresh=true for already-expired token")
	}
}

// TestTokenNeedsRefresh_ZeroExpiry verifies that a zero expiry (provider omits
// expiry, e.g. GitHub PATs) is NOT considered as needing refresh.
func TestTokenNeedsRefresh_ZeroExpiry(t *testing.T) {
	var expiry time.Time // zero value
	if tokenNeedsRefresh(expiry) {
		t.Error("expected tokenNeedsRefresh=false for zero expiry (no expiry set by provider)")
	}
}

// TestTokenNeedsRefresh_ExpiryExactlyNow verifies the boundary condition where
// a token expires at exactly this instant is considered as needing refresh.
// Previously `time.Until(expiry) <= 0` with == 0 being skipped could be ambiguous;
// the 30s buffer ensures this is always treated as needing refresh.
func TestTokenNeedsRefresh_ExpiryExactlyNow(t *testing.T) {
	// Simulate "exactly now" by using a tiny past time.
	expiry := time.Now()
	if !tokenNeedsRefresh(expiry) {
		t.Error("expected tokenNeedsRefresh=true for token expiring at exactly now")
	}
}

// TestTokenNeedsRefresh_ExpiringIn30sExact verifies the exact boundary (30s).
// A token expiring in exactly 30 seconds should NOT be considered needing refresh
// (time.Until < 30s is the condition, so exactly 30s is not yet within buffer).
func TestTokenNeedsRefresh_ExpiringIn30sExact(t *testing.T) {
	// Add a small epsilon to ensure we're just at or above the 30s mark.
	expiry := time.Now().Add(30*time.Second + 5*time.Millisecond)
	if tokenNeedsRefresh(expiry) {
		t.Errorf("expected tokenNeedsRefresh=false for token expiring in just over 30s")
	}
}

// TestTokenNeedsRefresh_FarFuture verifies that a token with a long remaining
// lifetime is not considered as needing refresh.
func TestTokenNeedsRefresh_FarFuture(t *testing.T) {
	expiry := time.Now().Add(24 * time.Hour)
	if tokenNeedsRefresh(expiry) {
		t.Error("expected tokenNeedsRefresh=false for token expiring in 24h")
	}
}
