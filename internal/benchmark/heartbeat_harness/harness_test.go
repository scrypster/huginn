package heartbeatharness

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultScenarios_RequiredCoverage(t *testing.T) {
	t.Parallel()
	scenarios := DefaultScenarios()
	if len(scenarios) != 5 {
		t.Fatalf("expected 5 default scenarios, got %d", len(scenarios))
	}
	want := map[string]bool{
		"normal_load":                   false,
		"restart_mid_interval":          false,
		"delayed_scheduler":             false,
		"queued_backlog":                false,
		"flaky_upstream_api_dependency": false,
	}
	for _, s := range scenarios {
		if _, ok := want[s.Name]; ok {
			want[s.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("expected required scenario %q", name)
		}
	}
}

func TestDefaultScenarios_SuiteDurationUnderEightMinutes(t *testing.T) {
	t.Parallel()
	total := SuiteDuration(DefaultScenarios())
	if total > 8*time.Minute {
		t.Fatalf("suite duration %s exceeds 8m target", total)
	}
}

func TestRunScenario_ComputesCoreRates(t *testing.T) {
	t.Parallel()
	sc := Scenario{
		Name:           "unit",
		Duration:       40 * time.Second,
		Interval:       10 * time.Second,
		DropSlots:      slotSet(1),
		DuplicateSlots: map[int]int{2: 1},
		FailureSlots:   slotSet(3),
	}
	result := RunScenario(time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC), sc)

	if result.KPI.ExpectedRuns != 4 {
		t.Fatalf("expected runs = %d, want 4", result.KPI.ExpectedRuns)
	}
	if result.KPI.MissedRuns != 1 {
		t.Fatalf("missed runs = %d, want 1", result.KPI.MissedRuns)
	}
	if result.KPI.DuplicateRuns != 1 {
		t.Fatalf("duplicate runs = %d, want 1", result.KPI.DuplicateRuns)
	}
	if result.KPI.MissedRunRate != 0.25 {
		t.Fatalf("missed run rate = %v, want 0.25", result.KPI.MissedRunRate)
	}
	if result.KPI.DuplicateRunRate != 0.25 {
		t.Fatalf("duplicate run rate = %v, want 0.25", result.KPI.DuplicateRunRate)
	}
	if result.KPI.RecoveryLatency <= 0 {
		t.Fatalf("expected positive recovery latency, got %s", result.KPI.RecoveryLatency)
	}
}

func TestBuildMarkdownReport(t *testing.T) {
	t.Parallel()
	results := RunDefaultSuite(time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC))
	report := BuildMarkdownReport(time.Date(2026, 4, 29, 12, 30, 0, 0, time.UTC), results)
	if !strings.Contains(report, "Heartbeat Benchmark Scorecard (Internal)") {
		t.Fatalf("expected title in report: %q", report)
	}
	if !strings.Contains(report, "normal_load") || !strings.Contains(report, "queued_backlog") {
		t.Fatalf("expected scenario rows in report: %q", report)
	}
	if !strings.Contains(report, "## Aggregate") {
		t.Fatalf("expected aggregate section in report: %q", report)
	}
}
