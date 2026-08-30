package tui

import (
	"strings"
	"testing"
	"time"
)

func TestFormatRelativeTime(t *testing.T) {
	now := time.Date(2026, 8, 27, 15, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	cases := []struct {
		ts   string
		want string
	}{
		{"", ""},
		{"nope", ""},
		{now.Add(-20 * time.Second).Format(time.RFC3339Nano), "just now"},
		{now.Add(-2 * time.Minute).Format(time.RFC3339Nano), "2m"},
		{now.Add(-time.Hour).Format(time.RFC3339Nano), "1h"},
		{now.Add(-26 * time.Hour).Format(time.RFC3339Nano), "yesterday"},
	}
	for _, tc := range cases {
		got := FormatRelativeTime(tc.ts, now)
		if got != tc.want {
			t.Errorf("FormatRelativeTime(%q) = %q want %q", tc.ts, got, tc.want)
		}
	}
	weekday := FormatRelativeTime(now.Add(-3*24*time.Hour).Format(time.RFC3339Nano), now)
	if !strings.EqualFold(weekday, "Monday") {
		t.Errorf("weekday got %q want Monday", weekday)
	}
}
