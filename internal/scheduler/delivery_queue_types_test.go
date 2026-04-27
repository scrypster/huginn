package scheduler

import (
	"testing"
	"time"
)

func TestComputeRetryWindow(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		wantMin  int // seconds
		wantMax  int
	}{
		{"empty schedule = 1 hour default", "", 3600, 3600},
		{"every 10 min", "*/10 * * * *", 400, 500},         // 8 min = 480s ± jitter range
		{"every hour", "0 * * * *", 2700, 2900},            // 48 min = 2880s
		{"daily", "0 9 * * *", 60000, 70000},               // ~19hr
		{"weekly capped at 24h", "0 9 * * 1", 86390, 86401}, // 24h cap
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeRetryWindow(tc.schedule)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("ComputeRetryWindow(%q) = %d, want [%d, %d]", tc.schedule, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestNextRetryDelay(t *testing.T) {
	window := 480 // 480s window (10-min workflow, 0.8 × 600s)
	tests := []struct {
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{0, 0, 1 * time.Second},                     // immediate: ≈ 0
		{1, 18 * time.Second, 30 * time.Second},     // 0.05 × 480s = 24s ±10%
		{2, 55 * time.Second, 85 * time.Second},     // 0.15 × 480s = 72s ±10%
		{3, 170 * time.Second, 215 * time.Second},   // 0.40 × 480s = 192s ±10%
		{4, 345 * time.Second, 425 * time.Second},   // 0.80 × 480s = 384s ±10%
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			got := nextRetryDelay(window, tc.attempt)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("nextRetryDelay(attempt=%d) = %v, want [%v, %v]", tc.attempt, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}
