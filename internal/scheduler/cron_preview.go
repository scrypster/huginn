// Package scheduler — cron preview (Phase 4).
//
// CronPreview lets the workflow editor UI render the next-N upcoming run
// times for a cron expression without spinning up a full Scheduler. It's a
// pure function of (expression, count) which keeps the implementation
// trivially testable and free of side effects.
package scheduler

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/robfig/cron/v3"
)

// cronPreviewParser is the same parser the live Scheduler uses so the preview
// can never disagree with what gets scheduled at runtime. Both the standard
// cron set and the @hourly / @daily / etc descriptors are supported.
var cronPreviewParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// CronPreview returns the next `count` UTC instants at which the cron
// expression would fire, starting from `from`. count is clamped to [1,50].
// from is optional — pass time.Time{} to use time.Now().
//
// Returns an error when the expression is syntactically invalid; the UI
// surfaces this as inline validation feedback while the user is typing.
func CronPreview(expr string, count int, from time.Time) ([]time.Time, error) {
	if count <= 0 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	sched, err := cronPreviewParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("scheduler: invalid cron expression %q: %w", expr, err)
	}
	if from.IsZero() {
		from = time.Now().UTC()
	} else {
		from = from.UTC()
	}
	out := make([]time.Time, 0, count)
	cursor := from
	for i := 0; i < count; i++ {
		next := sched.Next(cursor)
		if next.IsZero() {
			break // expression has no further occurrences (extremely unusual)
		}
		out = append(out, next)
		cursor = next
	}
	return out, nil
}

// ComputeRetryWindow returns the retry window in seconds for a workflow's
// delivery queue. Derived as min(cron_interval × 0.8, 86400).
// Empty schedule (ad-hoc runs) returns 3600 (1 hour).
// For irregular expressions the minimum gap between the next 6 fire times is used.
func ComputeRetryWindow(schedule string) int {
	const maxWindowS = 86400 // 24 hours
	if schedule == "" {
		return 3600
	}
	times, err := CronPreview(schedule, 6, time.Now())
	if err != nil || len(times) < 2 {
		return 3600
	}
	minGap := times[1].Sub(times[0])
	for i := 2; i < len(times); i++ {
		if d := times[i].Sub(times[i-1]); d < minGap {
			minGap = d
		}
	}
	window := time.Duration(float64(minGap) * 0.8)
	if int(window.Seconds()) > maxWindowS {
		return maxWindowS
	}
	return int(window.Seconds())
}

// nextRetryDelay returns the delay before attempt number attemptCount
// (0-indexed) using exponential spacing within the retry window.
// Attempt 0 returns 0 (fire immediately on next poll tick).
// Each delay gets ±10% jitter.
func nextRetryDelay(retryWindowS, attemptCount int) time.Duration {
	ratios := []float64{0.0, 0.05, 0.15, 0.40, 0.80}
	if attemptCount >= len(ratios) {
		attemptCount = len(ratios) - 1
	}
	base := time.Duration(float64(retryWindowS) * ratios[attemptCount] * float64(time.Second))
	if base == 0 {
		return 0
	}
	jitterRange := float64(base) * 0.10
	offset := time.Duration(rand.Int63n(int64(2*jitterRange+1)) - int64(jitterRange)) //nolint:gosec
	return base + offset
}
