package backend

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter_Empty(t *testing.T) {
	h := http.Header{}
	if d := parseRetryAfter(h); d != 0 {
		t.Errorf("expected 0 for missing header, got %v", d)
	}
}

func TestParseRetryAfter_IntegerSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	d := parseRetryAfter(h)
	if d != 30*time.Second {
		t.Errorf("expected 30s, got %v", d)
	}
}

func TestParseRetryAfter_IntegerSeconds_Capped(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "300") // 5 minutes — exceeds cap
	d := parseRetryAfter(h)
	if d != maxRetryAfter {
		t.Errorf("expected cap of %v, got %v", maxRetryAfter, d)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	// Use a date 10 seconds in the future.
	future := time.Now().Add(10 * time.Second).UTC()
	h := http.Header{}
	h.Set("Retry-After", future.Format(http.TimeFormat))
	d := parseRetryAfter(h)
	// Allow ±2s of clock skew.
	if d < 8*time.Second || d > 12*time.Second {
		t.Errorf("expected ~10s from HTTP-date, got %v", d)
	}
}

func TestParseRetryAfter_PastDate(t *testing.T) {
	// A date in the past should return 0 (not negative).
	past := time.Now().Add(-10 * time.Second).UTC()
	h := http.Header{}
	h.Set("Retry-After", past.Format(http.TimeFormat))
	d := parseRetryAfter(h)
	if d != 0 {
		t.Errorf("expected 0 for past date, got %v", d)
	}
}

func TestParseRetryAfter_Garbage(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "not-a-number-or-date")
	d := parseRetryAfter(h)
	if d != 0 {
		t.Errorf("expected 0 for garbage value, got %v", d)
	}
}

func TestParseRetryAfter_Zero(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "0")
	d := parseRetryAfter(h)
	if d != 0 {
		t.Errorf("expected 0 for '0' seconds, got %v", d)
	}
}

func TestRateLimitError_CarriesRetryAfter(t *testing.T) {
	e := &RateLimitError{Body: "rate limited", RetryAfter: 45 * time.Second}
	if e.RetryAfter != 45*time.Second {
		t.Errorf("expected 45s RetryAfter on error, got %v", e.RetryAfter)
	}
	if e.Error() == "" {
		t.Error("expected non-empty Error() string")
	}
}
