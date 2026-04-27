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
		{"invalid expression fallback", "not-a-cron", 3600, 3600},
		{"whitespace schedule = 1 hour default", "   ", 3600, 3600},
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
		name    string
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{"attempt_0_immediate", 0, 0, 1 * time.Second},                     // immediate: ≈ 0
		{"attempt_1", 1, 18 * time.Second, 30 * time.Second},               // 0.05 × 480s = 24s ±10%
		{"attempt_2", 2, 55 * time.Second, 85 * time.Second},               // 0.15 × 480s = 72s ±10%
		{"attempt_3", 3, 170 * time.Second, 215 * time.Second},             // 0.40 × 480s = 192s ±10%
		{"attempt_4", 4, 345 * time.Second, 425 * time.Second},             // 0.80 × 480s = 384s ±10%
		{"attempt_5_clamped_to_4", 5, 345 * time.Second, 425 * time.Second}, // clamped: same as attempt 4
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nextRetryDelay(window, tc.attempt)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("nextRetryDelay(attempt=%d) = %v, want [%v, %v]", tc.attempt, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestEndpointKey(t *testing.T) {
	tests := []struct {
		target NotificationDelivery
		want   string
	}{
		{
			NotificationDelivery{Type: "webhook", To: "https://hooks.slack.com/abc"},
			"https://hooks.slack.com/abc",
		},
		{
			NotificationDelivery{Type: "email", SMTPUser: "bot", SMTPHost: "smtp.gmail.com"},
			"smtp://bot@smtp.gmail.com",
		},
		{
			NotificationDelivery{Type: "email", Connection: "my-gmail"},
			"smtp-connection://my-gmail",
		},
	}
	for _, tc := range tests {
		got := endpointKey(tc.target)
		if got != tc.want {
			t.Errorf("endpointKey(%+v) = %q, want %q", tc.target, got, tc.want)
		}
	}
}
