package heartbeatharness

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluateGate_DefaultThresholdsPass(t *testing.T) {
	t.Parallel()
	results := RunDefaultSuite(time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC))
	eval := EvaluateGate(results, DefaultGateThresholds())
	if !eval.Passed {
		t.Fatalf("expected gate to pass, failures: %v", eval.Failures)
	}
	if got := eval.Summary(); !strings.Contains(got, "PASS") {
		t.Fatalf("expected pass summary, got %q", got)
	}
}

func TestEvaluateGate_FailsWhenThresholdsTooStrict(t *testing.T) {
	t.Parallel()
	results := RunDefaultSuite(time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC))
	thresholds := GateThresholds{
		MaxScheduleAdherenceP95: 1 * time.Second,
		MaxScheduleAdherenceP99: 2 * time.Second,
		MaxMissedRunRate:        0.01,
		MaxDuplicateRunRate:     0.01,
		MaxRecoveryLatency:      15 * time.Second,
		MaxNuisanceScore:        30,
		MinActionabilityScore:   50,
	}
	eval := EvaluateGate(results, thresholds)
	if eval.Passed {
		t.Fatalf("expected gate to fail")
	}
	if text := eval.FailureText(); !strings.Contains(text, "missed run rate") {
		t.Fatalf("expected failure details, got: %q", text)
	}
	if got := eval.Summary(); !strings.Contains(got, "FAIL") {
		t.Fatalf("expected fail summary, got %q", got)
	}
}
