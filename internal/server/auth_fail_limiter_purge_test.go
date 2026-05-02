package server

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClock is a controllable clock for testing authFailLimiter without real sleeps.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestAuthFailLimiter_OneShotIPPurgedAfterWindow verifies that an IP which fails
// exactly once and never returns is removed from the window map after the sweep
// that follows authFailWindow expiry.
func TestAuthFailLimiter_OneShotIPPurgedAfterWindow(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	lim := newAuthFailLimiterWithClock(clk.Now)

	oneShotIP := "10.0.0.1"

	// One-shot IP fails once.
	lim.recordFailure(oneShotIP)

	lim.mu.Lock()
	if _, ok := lim.window[oneShotIP]; !ok {
		lim.mu.Unlock()
		t.Fatal("window entry should exist immediately after recordFailure")
	}
	lim.mu.Unlock()

	// Advance past authFailWindow so the entry is stale.
	clk.Advance(authFailWindow + time.Second)

	// Trigger a sweep by recording a failure from a different IP after the window.
	// The sweep fires when now > lastSweep + authFailWindow, which is true here
	// since lastSweep is zero.
	lim.recordFailure("10.0.0.2")

	lim.mu.Lock()
	_, stillPresent := lim.window[oneShotIP]
	lim.mu.Unlock()

	if stillPresent {
		t.Error("stale one-shot IP entry should have been purged by periodic sweep")
	}
}

// TestAuthFailLimiter_MapDoesNotGrowUnboundedlyWithOneShotIPs verifies that after
// the sweep window, stale entries for many one-shot IPs are removed.
func TestAuthFailLimiter_MapDoesNotGrowUnboundedlyWithOneShotIPs(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	lim := newAuthFailLimiterWithClock(clk.Now)

	// Simulate 500 one-shot IPs all failing within the same window.
	for i := 0; i < 500; i++ {
		lim.recordFailure(fmt.Sprintf("192.168.1.%d", i%256))
	}

	lim.mu.Lock()
	sizeBefore := len(lim.window)
	lim.mu.Unlock()

	if sizeBefore == 0 {
		t.Fatal("map should be non-empty before window expires")
	}

	// Advance past authFailWindow so all entries are stale.
	clk.Advance(authFailWindow + time.Second)

	// Trigger a sweep by recording a failure from a fresh IP.
	lim.recordFailure("1.2.3.4")

	lim.mu.Lock()
	sizeAfter := len(lim.window)
	lim.mu.Unlock()

	// After the sweep only the freshly-recorded IP should remain.
	if sizeAfter > 1 {
		t.Errorf("expected at most 1 entry after sweep, got %d (stale entries not purged)", sizeAfter)
	}
}

// TestAuthFailLimiter_BannedIPMovedOutOfWindowMap verifies that once an IP crosses
// the ban threshold its slice is removed from the window map and the ban is tracked
// in the compact bannedUntil map instead.
func TestAuthFailLimiter_BannedIPMovedOutOfWindowMap(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	lim := newAuthFailLimiterWithClock(clk.Now)

	ip := "172.16.0.1"

	// Push the IP over the threshold.
	for i := 0; i <= authFailMaxPerMinute; i++ {
		lim.recordFailure(ip)
	}

	lim.mu.Lock()
	_, inWindow := lim.window[ip]
	_, inBan := lim.bannedUntil[ip]
	lim.mu.Unlock()

	if inWindow {
		t.Error("banned IP slice should be removed from window map on ban")
	}
	if !inBan {
		t.Error("banned IP should appear in bannedUntil map")
	}
}

// TestAuthFailLimiter_BanExpiresAfterWindow verifies that a banned IP is unblocked
// and its bannedUntil entry cleaned up after authFailWindow has elapsed.
func TestAuthFailLimiter_BanExpiresAfterWindow(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	lim := newAuthFailLimiterWithClock(clk.Now)

	ip := "172.16.0.2"

	// Ban the IP.
	for i := 0; i <= authFailMaxPerMinute; i++ {
		lim.recordFailure(ip)
	}

	if !lim.isBlocked(ip) {
		t.Fatal("IP should be blocked immediately after ban")
	}

	// Advance past the ban window.
	clk.Advance(authFailWindow + time.Second)

	if lim.isBlocked(ip) {
		t.Error("IP should be unblocked after ban window expires")
	}

	lim.mu.Lock()
	_, stillBanned := lim.bannedUntil[ip]
	lim.mu.Unlock()

	if stillBanned {
		t.Error("expired ban entry should be cleaned up from bannedUntil map")
	}
}

// TestAuthFailLimiter_SweepCleansBannedUntilMap verifies that expired bans are
// removed from bannedUntil during a periodic sweep.
func TestAuthFailLimiter_SweepCleansBannedUntilMap(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	lim := newAuthFailLimiterWithClock(clk.Now)

	// Ban several IPs.
	for i := 0; i < 10; i++ {
		ip := fmt.Sprintf("10.10.10.%d", i)
		for j := 0; j <= authFailMaxPerMinute; j++ {
			lim.recordFailure(ip)
		}
	}

	lim.mu.Lock()
	bansBefore := len(lim.bannedUntil)
	lim.mu.Unlock()

	if bansBefore == 0 {
		t.Fatal("bannedUntil should have entries after banning IPs")
	}

	// Advance past ban expiry.
	clk.Advance(authFailWindow + time.Second)

	// Trigger a sweep via a new recordFailure call.
	lim.recordFailure("99.99.99.99")

	lim.mu.Lock()
	bansAfter := len(lim.bannedUntil)
	lim.mu.Unlock()

	// Only the freshly-banned check (if it would cross threshold) should remain.
	// The freshly-recorded IP only has 1 failure so it won't be in bannedUntil.
	if bansAfter != 0 {
		t.Errorf("expected 0 expired ban entries after sweep, got %d", bansAfter)
	}
}

// TestAuthFailLimiter_RecordFailureReturnsCorrectBool verifies the return value
// semantics are preserved: false until threshold crossed, true from that point on.
func TestAuthFailLimiter_RecordFailureReturnsCorrectBool(t *testing.T) {
	clk := newFakeClock(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	lim := newAuthFailLimiterWithClock(clk.Now)

	ip := "192.0.2.1"

	for i := 0; i < authFailMaxPerMinute; i++ {
		if lim.recordFailure(ip) {
			t.Errorf("failure %d should not yet trigger ban (threshold is %d)", i+1, authFailMaxPerMinute)
		}
	}

	// The (authFailMaxPerMinute+1)-th failure should trigger the ban.
	if !lim.recordFailure(ip) {
		t.Errorf("failure %d should trigger ban", authFailMaxPerMinute+1)
	}
}
