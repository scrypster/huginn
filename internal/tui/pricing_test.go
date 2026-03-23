package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/pricing"
)

func TestPricingTracker_FooterShowsCost(t *testing.T) {
	a := newTestApp()
	tracker := pricing.NewSessionTracker(pricing.DefaultTable)
	tracker.Add("claude-sonnet-4-6", 10000, 500)
	a.priceTracker = tracker

	footer := a.renderFooter()
	if !strings.Contains(footer, "~$") {
		t.Errorf("expected cost in footer, got:\n%s", footer)
	}
}

func TestPricingTracker_FooterEmptyWhenZeroCost(t *testing.T) {
	a := newTestApp()
	tracker := pricing.NewSessionTracker(pricing.DefaultTable)
	a.priceTracker = tracker

	footer := a.renderFooter()
	if strings.Contains(footer, "~$") {
		t.Errorf("expected no cost in footer for zero usage, got:\n%s", footer)
	}
}
