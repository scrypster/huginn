package pricing_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/pricing"
)

func TestSessionTracker_AddAndTotal(t *testing.T) {
	tbl := pricing.DefaultTable
	tracker := pricing.NewSessionTracker(tbl)
	tracker.Add("gpt-4o", 1000, 500)
	tracker.Add("gpt-4o", 2000, 800)
	total := tracker.SessionCost()
	expected := pricing.CalculateCost(tbl, "gpt-4o", 3000, 1300)
	// Use approximate comparison to handle floating point precision
	if diff := total - expected; diff < -1e-10 || diff > 1e-10 {
		t.Errorf("SessionCost = %v, want %v (diff: %v)", total, expected, diff)
	}
}

func TestSessionTracker_FormatCost(t *testing.T) {
	tests := []struct {
		cost float64
		want string
	}{
		{0.0023, "~$0.0023"},
		{0.0, "$0.00"},
		{1.234567, "~$1.2346"},
		{0.00001, "~$0.0000"},
	}
	for _, tt := range tests {
		got := pricing.FormatCost(tt.cost)
		if got != tt.want {
			t.Errorf("FormatCost(%v) = %q, want %q", tt.cost, got, tt.want)
		}
	}
}

func TestSessionTracker_IsCloudModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-4o", true},
		{"claude-sonnet-4-6", true},
		{"gpt-4o-mini", true},
		{"qwen2.5-coder:14b", false},
		{"llama3.2:3b", false},
		{"deepseek-r1:14b", false},
	}
	for _, tt := range tests {
		got := pricing.IsCloudModel(pricing.DefaultTable, tt.model)
		if got != tt.want {
			t.Errorf("IsCloudModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestSessionTracker_Reset(t *testing.T) {
	tbl := pricing.DefaultTable
	tracker := pricing.NewSessionTracker(tbl)
	tracker.Add("gpt-4o", 1000, 500)
	tracker.Reset()
	if cost := tracker.SessionCost(); cost != 0 {
		t.Errorf("SessionCost after Reset = %v, want 0", cost)
	}
}

func TestMonthlyKey(t *testing.T) {
	ts := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	got := pricing.MonthlyKey(ts)
	want := "stats:cost:2026-03"
	if got != want {
		t.Errorf("MonthlyKey = %q, want %q", got, want)
	}
}

func TestSessionTracker_StatusBarText_Cloud(t *testing.T) {
	tbl := pricing.DefaultTable
	tracker := pricing.NewSessionTracker(tbl)
	tracker.Add("gpt-4o", 1_000_000, 500_000)
	text := tracker.StatusBarText()
	if text == "" {
		t.Error("expected non-empty status bar text for cloud model")
	}
	if len(text) < 3 {
		t.Errorf("status bar text too short: %q", text)
	}
}

func TestSessionTracker_StatusBarText_Ollama_HiddenWhenZero(t *testing.T) {
	tbl := pricing.DefaultTable
	tracker := pricing.NewSessionTracker(tbl)
	tracker.Add("qwen2.5-coder:14b", 1_000_000, 500_000)
	text := tracker.StatusBarText()
	if text != "" && text != "$0.00" {
		t.Errorf("expected empty or $0.00 for ollama, got %q", text)
	}
}

func TestSessionTracker_BreakdownByModel(t *testing.T) {
	tbl := pricing.DefaultTable
	tracker := pricing.NewSessionTracker(tbl)
	tracker.Add("gpt-4o", 1000, 500)
	tracker.Add("claude-sonnet-4-6", 2000, 800)
	bd := tracker.Breakdown()
	if len(bd) != 2 {
		t.Errorf("Breakdown length = %d, want 2", len(bd))
	}
	found4o := false
	for _, entry := range bd {
		if entry.Model == "gpt-4o" {
			found4o = true
			if entry.PromptTokens != 1000 {
				t.Errorf("gpt-4o PromptTokens = %d, want 1000", entry.PromptTokens)
			}
		}
	}
	if !found4o {
		t.Error("expected gpt-4o in breakdown")
	}
}

func TestFormatCost_EdgeCases(t *testing.T) {
	got := pricing.FormatCost(-0.001)
	if got == "" {
		t.Error("FormatCost(-0.001) returned empty string")
	}
	_ = fmt.Sprintf("got: %s", got)
}
