// Package scheduler — cron preview (Phase 4).
//
// CronPreview lets the workflow editor UI render the next-N upcoming run
// times for a cron expression without spinning up a full Scheduler. It's a
// pure function of (expression, count) which keeps the implementation
// trivially testable and free of side effects.
package scheduler

import (
	"fmt"
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
