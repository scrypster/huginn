package scheduler

import (
	"testing"
	"time"
)

func TestCronPreview_StandardExpression(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	got, err := CronPreview("0 9 * * *", 3, from)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d runs, want 3", len(got))
	}
	// Each subsequent run should be exactly 24h apart at 09:00 UTC.
	for i, ts := range got {
		if ts.Hour() != 9 || ts.Minute() != 0 {
			t.Errorf("run %d at %s, want 09:00 UTC", i, ts)
		}
		if i > 0 {
			diff := ts.Sub(got[i-1])
			if diff != 24*time.Hour {
				t.Errorf("gap between runs %d and %d = %s, want 24h", i-1, i, diff)
			}
		}
	}
}

func TestCronPreview_Descriptor(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	got, err := CronPreview("@hourly", 5, from)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d runs, want 5", len(got))
	}
	if got[0] != time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC) {
		t.Errorf("first run = %s, want 13:00", got[0])
	}
}

func TestCronPreview_InvalidExpr(t *testing.T) {
	t.Parallel()
	if _, err := CronPreview("this is not cron", 3, time.Time{}); err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestCronPreview_Clamps(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	// count = 0 → clamps to 1.
	got, err := CronPreview("@hourly", 0, from)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("count=0 → %d runs, want 1", len(got))
	}
	// count = 9999 → clamps to 50.
	got, _ = CronPreview("@hourly", 9999, from)
	if len(got) != 50 {
		t.Errorf("count=9999 → %d runs, want 50", len(got))
	}
}

func TestCronPreview_FromZeroDefaultsToNow(t *testing.T) {
	t.Parallel()
	got, err := CronPreview("@hourly", 1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	// Should fire within the next hour.
	if got[0].Sub(time.Now()) > time.Hour+time.Minute {
		t.Errorf("first run too far in the future: %s", got[0])
	}
}
