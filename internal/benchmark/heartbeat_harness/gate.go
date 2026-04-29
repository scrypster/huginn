package heartbeatharness

import (
	"fmt"
	"strings"
	"time"
)

// GateThresholds defines acceptable aggregate KPI limits for CI gating.
type GateThresholds struct {
	MaxScheduleAdherenceP95 time.Duration
	MaxScheduleAdherenceP99 time.Duration
	MaxMissedRunRate        float64
	MaxDuplicateRunRate     float64
	MaxRecoveryLatency      time.Duration
	MaxNuisanceScore        float64
	MinActionabilityScore   float64
}

// GateEvaluation is the pass/fail result for a benchmark run.
type GateEvaluation struct {
	Passed       bool
	Failures     []string
	Thresholds   GateThresholds
	AggregateKPI KPI
}

// DefaultGateThresholds returns baseline limits for the short-run harness.
func DefaultGateThresholds() GateThresholds {
	return GateThresholds{
		MaxScheduleAdherenceP95: 6 * time.Second,
		MaxScheduleAdherenceP99: 7 * time.Second,
		MaxMissedRunRate:        0.12,
		MaxDuplicateRunRate:     0.12,
		MaxRecoveryLatency:      70 * time.Second,
		MaxNuisanceScore:        65.0,
		MinActionabilityScore:   18.0,
	}
}

// EvaluateGate checks aggregate KPIs against threshold limits.
func EvaluateGate(results []Result, thresholds GateThresholds) GateEvaluation {
	kpi := AggregateKPI(results)
	failures := make([]string, 0, 8)

	if kpi.ScheduleAdherenceP95 > thresholds.MaxScheduleAdherenceP95 {
		failures = append(failures, fmt.Sprintf(
			"p95 adherence %s exceeds max %s",
			kpi.ScheduleAdherenceP95.Round(time.Millisecond),
			thresholds.MaxScheduleAdherenceP95.Round(time.Millisecond),
		))
	}
	if kpi.ScheduleAdherenceP99 > thresholds.MaxScheduleAdherenceP99 {
		failures = append(failures, fmt.Sprintf(
			"p99 adherence %s exceeds max %s",
			kpi.ScheduleAdherenceP99.Round(time.Millisecond),
			thresholds.MaxScheduleAdherenceP99.Round(time.Millisecond),
		))
	}
	if kpi.MissedRunRate > thresholds.MaxMissedRunRate {
		failures = append(failures, fmt.Sprintf(
			"missed run rate %.2f%% exceeds max %.2f%%",
			kpi.MissedRunRate*100,
			thresholds.MaxMissedRunRate*100,
		))
	}
	if kpi.DuplicateRunRate > thresholds.MaxDuplicateRunRate {
		failures = append(failures, fmt.Sprintf(
			"duplicate run rate %.2f%% exceeds max %.2f%%",
			kpi.DuplicateRunRate*100,
			thresholds.MaxDuplicateRunRate*100,
		))
	}
	if kpi.RecoveryLatency > thresholds.MaxRecoveryLatency {
		failures = append(failures, fmt.Sprintf(
			"recovery latency %s exceeds max %s",
			kpi.RecoveryLatency.Round(time.Millisecond),
			thresholds.MaxRecoveryLatency.Round(time.Millisecond),
		))
	}
	if kpi.NuisanceScore > thresholds.MaxNuisanceScore {
		failures = append(failures, fmt.Sprintf(
			"nuisance score %.1f exceeds max %.1f",
			kpi.NuisanceScore,
			thresholds.MaxNuisanceScore,
		))
	}
	if kpi.ActionabilityScore < thresholds.MinActionabilityScore {
		failures = append(failures, fmt.Sprintf(
			"actionability score %.1f below min %.1f",
			kpi.ActionabilityScore,
			thresholds.MinActionabilityScore,
		))
	}

	return GateEvaluation{
		Passed:       len(failures) == 0,
		Failures:     failures,
		Thresholds:   thresholds,
		AggregateKPI: kpi,
	}
}

// Summary renders a concise pass/fail text line for logs.
func (g GateEvaluation) Summary() string {
	if g.Passed {
		return "PASS: heartbeat benchmark quality gate satisfied"
	}
	return "FAIL: heartbeat benchmark quality gate failed"
}

// FailureText returns multiline details suitable for CI output.
func (g GateEvaluation) FailureText() string {
	if g.Passed || len(g.Failures) == 0 {
		return ""
	}
	return strings.Join(g.Failures, "\n")
}
