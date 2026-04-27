// Package scheduler — Phase 8 step gating helpers.
//
// `Step.When` is a tiny templated boolean expression. We deliberately keep it
// stupid-simple: no operator parsing, no quoting, no external dependencies.
// After all `{{run.scratch.K}}` and `{{prev.output}}` substitutions are
// resolved, the residual string is interpreted as truthy/falsy with the
// rules documented in WorkflowStep.When.
package scheduler

import (
	"strings"
	"time"
)

// EvaluateWhen returns true when the resolved expression is truthy.
//
// Falsy values are: "", "false", "0", "no", "off" (case-insensitive, after
// TrimSpace). Anything else evaluates to true.
//
// Empty input is considered "no condition" and the step runs.
func EvaluateWhen(resolved string) bool {
	t := strings.ToLower(strings.TrimSpace(resolved))
	switch t {
	case "":
		return true // no condition → always run
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

// ApplyRetryDefaults mutates each step's MaxRetries / RetryDelay to inherit
// from the workflow's WorkflowRetryConfig when the step does NOT set its own.
// This is a one-shot pass invoked by the runner before stepping.
//
// Inheritance rules:
//   - step.MaxRetries == 0 AND wf.Retry != nil → step.MaxRetries = wf.Retry.MaxRetries
//   - step.RetryDelay == "" AND wf.Retry != nil AND wf.Retry.Delay != "" → step.RetryDelay = wf.Retry.Delay
//
// We only inherit when the step's value is the zero value, NOT when the
// step explicitly sets the same value as the default. (Go can't tell the
// difference between "unset" and "explicitly zero" without a *int — for
// this UX we accept that ambiguity.)
func ApplyRetryDefaults(steps []WorkflowStep, defaults *WorkflowRetryConfig) {
	if defaults == nil || (defaults.MaxRetries == 0 && strings.TrimSpace(defaults.Delay) == "") {
		return
	}
	for i := range steps {
		if steps[i].MaxRetries == 0 && defaults.MaxRetries > 0 {
			steps[i].MaxRetries = defaults.MaxRetries
		}
		if steps[i].RetryDelay == "" && strings.TrimSpace(defaults.Delay) != "" {
			steps[i].RetryDelay = defaults.Delay
			// Re-parse so the runner's RetryDelayDuration() reflects the
			// inherited value. Failure to parse is silently ignored —
			// validation happens at workflow-save time, so a malformed
			// inherited delay would already have surfaced earlier.
			if d, err := time.ParseDuration(defaults.Delay); err == nil {
				steps[i].retryDelayParsed = d
			}
		}
	}
}
