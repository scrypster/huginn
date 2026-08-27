package tui

import (
	"fmt"
	"strings"
	"time"
)

// FormatRelativeTime returns a quiet hallway/thread age: just now / 2m / 1h / yesterday / weekday.
func FormatRelativeTime(ts string, now time.Time) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return ""
		}
	}
	if now.IsZero() {
		now = time.Now()
	}
	tLocal := t.In(now.Location())
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	msgDay := time.Date(tLocal.Year(), tLocal.Month(), tLocal.Day(), 0, 0, 0, 0, now.Location())
	days := int(nowDay.Sub(msgDay).Hours() / 24)
	if days == 1 {
		return "yesterday"
	}
	if days > 1 && days < 7 {
		return tLocal.Weekday().String()
	}
	if days >= 7 {
		return tLocal.Format("Mon, Jan 2")
	}
	diff := now.Sub(t)
	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	}
	hrs := int(diff.Hours())
	if hrs < 1 {
		hrs = 1
	}
	return fmt.Sprintf("%dh", hrs)
}
