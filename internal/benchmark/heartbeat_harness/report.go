package heartbeatharness

import (
	"fmt"
	"strings"
	"time"
)

// BuildMarkdownReport renders a private benchmark scorecard.
func BuildMarkdownReport(generatedAt time.Time, results []Result) string {
	var b strings.Builder
	b.WriteString("# Heartbeat Benchmark Scorecard (Internal)\n\n")
	b.WriteString(fmt.Sprintf("- Generated: `%s`\n", generatedAt.UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Scenarios: `%d`\n", len(results)))
	b.WriteString(fmt.Sprintf("- Simulated suite duration: `%s`\n\n", SuiteDuration(extractScenarios(results)).String()))
	b.WriteString("| Scenario | p95 adherence | p99 adherence | Missed rate | Duplicate rate | Recovery latency | Nuisance score | Actionability score |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range results {
		k := r.KPI
		b.WriteString(fmt.Sprintf(
			"| %s | %s | %s | %.1f%% | %.1f%% | %s | %.1f | %.1f |\n",
			r.Scenario.Name,
			k.ScheduleAdherenceP95.Round(time.Millisecond),
			k.ScheduleAdherenceP99.Round(time.Millisecond),
			k.MissedRunRate*100,
			k.DuplicateRunRate*100,
			k.RecoveryLatency.Round(time.Millisecond),
			k.NuisanceScore,
			k.ActionabilityScore,
		))
	}
	agg := AggregateKPI(results)
	gate := EvaluateGate(results, DefaultGateThresholds())
	b.WriteString("\n## Aggregate\n\n")
	b.WriteString(fmt.Sprintf(
		"- Runs: expected `%d`, executed `%d`, missed `%d`, duplicates `%d`\n",
		agg.ExpectedRuns, agg.ExecutedRuns, agg.MissedRuns, agg.DuplicateRuns,
	))
	b.WriteString(fmt.Sprintf("- Adherence: p95 `%s`, p99 `%s`\n",
		agg.ScheduleAdherenceP95.Round(time.Millisecond),
		agg.ScheduleAdherenceP99.Round(time.Millisecond),
	))
	b.WriteString(fmt.Sprintf("- Missed run rate: `%.2f%%`\n", agg.MissedRunRate*100))
	b.WriteString(fmt.Sprintf("- Duplicate run rate: `%.2f%%`\n", agg.DuplicateRunRate*100))
	b.WriteString(fmt.Sprintf("- Recovery latency (p95): `%s`\n", agg.RecoveryLatency.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("- Nuisance score (0-100): `%.1f`\n", agg.NuisanceScore))
	b.WriteString(fmt.Sprintf("- Actionability score (0-100): `%.1f`\n", agg.ActionabilityScore))
	b.WriteString("\n## Quality Gate\n\n")
	b.WriteString(fmt.Sprintf("- Status: `%s`\n", boolToStatus(gate.Passed)))
	b.WriteString(fmt.Sprintf("- Max p95 adherence: `%s`\n", gate.Thresholds.MaxScheduleAdherenceP95))
	b.WriteString(fmt.Sprintf("- Max p99 adherence: `%s`\n", gate.Thresholds.MaxScheduleAdherenceP99))
	b.WriteString(fmt.Sprintf("- Max missed run rate: `%.2f%%`\n", gate.Thresholds.MaxMissedRunRate*100))
	b.WriteString(fmt.Sprintf("- Max duplicate run rate: `%.2f%%`\n", gate.Thresholds.MaxDuplicateRunRate*100))
	b.WriteString(fmt.Sprintf("- Max recovery latency: `%s`\n", gate.Thresholds.MaxRecoveryLatency))
	b.WriteString(fmt.Sprintf("- Max nuisance score: `%.1f`\n", gate.Thresholds.MaxNuisanceScore))
	b.WriteString(fmt.Sprintf("- Min actionability score: `%.1f`\n", gate.Thresholds.MinActionabilityScore))
	if !gate.Passed && len(gate.Failures) > 0 {
		b.WriteString("- Failures:\n")
		for _, failure := range gate.Failures {
			b.WriteString(fmt.Sprintf("  - `%s`\n", failure))
		}
	}
	return b.String()
}

func extractScenarios(results []Result) []Scenario {
	out := make([]Scenario, 0, len(results))
	for _, r := range results {
		out = append(out, r.Scenario)
	}
	return out
}

func boolToStatus(v bool) string {
	if v {
		return "pass"
	}
	return "fail"
}
