package heartbeatharness

import (
	"math"
	"sort"
	"time"
)

const (
	defaultScenarioDuration = 90 * time.Second
	defaultScenarioInterval = 15 * time.Second
)

// Scenario defines one deterministic benchmark simulation.
type Scenario struct {
	Name        string
	Description string

	Duration time.Duration
	Interval time.Duration

	BaseDelay      time.Duration
	DelayBySlot    map[int]time.Duration
	DropSlots      map[int]bool
	DuplicateSlots map[int]int
	FailureSlots   map[int]bool
	SignalBySlot   map[int]float64
}

// RunEvent records one simulated heartbeat execution.
type RunEvent struct {
	Slot        int
	ScheduledAt time.Time
	ExecutedAt  time.Time
	Success     bool
	Relevance   float64
	Duplicate   bool
}

// KPI captures the core benchmark metrics.
type KPI struct {
	ExpectedRuns  int
	ExecutedRuns  int
	MissedRuns    int
	DuplicateRuns int

	ScheduleAdherenceP95 time.Duration
	ScheduleAdherenceP99 time.Duration
	RecoveryLatency      time.Duration

	MissedRunRate    float64 // 0..1
	DuplicateRunRate float64 // 0..1

	NuisanceScore      float64 // 0..100 (higher = noisier)
	ActionabilityScore float64 // 0..100 (higher = more actionable)
}

// Result contains one scenario's execution output and metrics.
type Result struct {
	Scenario Scenario
	KPI      KPI
	Events   []RunEvent
}

// DefaultScenarios returns the required low-cost heartbeat benchmark suite.
func DefaultScenarios() []Scenario {
	return []Scenario{
		{
			Name:        "normal_load",
			Description: "Baseline heartbeat cadence with minor scheduler jitter.",
			Duration:    defaultScenarioDuration,
			Interval:    defaultScenarioInterval,
			DelayBySlot: map[int]time.Duration{
				1: 200 * time.Millisecond,
				3: 350 * time.Millisecond,
			},
		},
		{
			Name:        "restart_mid_interval",
			Description: "Runtime restarts in the middle of a heartbeat cadence.",
			Duration:    defaultScenarioDuration,
			Interval:    defaultScenarioInterval,
			DropSlots:   slotSet(2, 3),
			DelayBySlot: map[int]time.Duration{
				4: 5 * time.Second,
			},
		},
		{
			Name:        "delayed_scheduler",
			Description: "Scheduler remains alive but drifts and executes late.",
			Duration:    defaultScenarioDuration,
			Interval:    defaultScenarioInterval,
			DelayBySlot: map[int]time.Duration{
				1: 3 * time.Second,
				2: 5 * time.Second,
				4: 6 * time.Second,
				5: 2 * time.Second,
			},
		},
		{
			Name:        "queued_backlog",
			Description: "Backlog catch-up emits delayed and duplicate executions.",
			Duration:    defaultScenarioDuration,
			Interval:    defaultScenarioInterval,
			DelayBySlot: map[int]time.Duration{
				2: 2 * time.Second,
				3: 4 * time.Second,
			},
			DuplicateSlots: map[int]int{
				2: 1,
				3: 2,
			},
		},
		{
			Name:         "flaky_upstream_api_dependency",
			Description:  "Heartbeat runner executes but upstream dependency fails intermittently.",
			Duration:     defaultScenarioDuration,
			Interval:     defaultScenarioInterval,
			FailureSlots: slotSet(1, 2, 4),
			DropSlots:    slotSet(3),
			DelayBySlot: map[int]time.Duration{
				5: 3 * time.Second,
			},
		},
	}
}

// SuiteDuration returns the sum of scenario durations.
func SuiteDuration(scenarios []Scenario) time.Duration {
	var total time.Duration
	for _, s := range scenarios {
		total += scenarioDuration(s)
	}
	return total
}

// RunSuite executes scenarios in deterministic order.
func RunSuite(start time.Time, scenarios []Scenario) []Result {
	results := make([]Result, 0, len(scenarios))
	now := start
	for _, s := range scenarios {
		r := RunScenario(now, s)
		results = append(results, r)
		now = now.Add(scenarioDuration(s))
	}
	return results
}

// RunDefaultSuite executes the built-in five-scenario benchmark set.
func RunDefaultSuite(start time.Time) []Result {
	return RunSuite(start, DefaultScenarios())
}

// AggregateKPI computes weighted aggregate metrics for a suite.
func AggregateKPI(results []Result) KPI {
	var (
		totalExpected    int
		totalExecuted    int
		totalMissed      int
		totalDuplicates  int
		adherence        []time.Duration
		recoveries       []time.Duration
		nuisanceWeighted float64
		actionWeighted   float64
	)
	for _, r := range results {
		k := r.KPI
		totalExpected += k.ExpectedRuns
		totalExecuted += k.ExecutedRuns
		totalMissed += k.MissedRuns
		totalDuplicates += k.DuplicateRuns
		nuisanceWeighted += k.NuisanceScore * float64(k.ExecutedRuns)
		actionWeighted += k.ActionabilityScore * float64(k.ExecutedRuns)

		adherence = append(adherence, sampleAdherence(r.Events)...)
		if k.RecoveryLatency > 0 {
			recoveries = append(recoveries, k.RecoveryLatency)
		}
	}
	var nuisance, action float64
	if totalExecuted > 0 {
		nuisance = nuisanceWeighted / float64(totalExecuted)
		action = actionWeighted / float64(totalExecuted)
	}
	return KPI{
		ExpectedRuns:         totalExpected,
		ExecutedRuns:         totalExecuted,
		MissedRuns:           totalMissed,
		DuplicateRuns:        totalDuplicates,
		ScheduleAdherenceP95: percentileDuration(adherence, 0.95),
		ScheduleAdherenceP99: percentileDuration(adherence, 0.99),
		RecoveryLatency:      percentileDuration(recoveries, 0.95),
		MissedRunRate:        ratio(totalMissed, totalExpected),
		DuplicateRunRate:     ratio(totalDuplicates, totalExpected),
		NuisanceScore:        nuisance,
		ActionabilityScore:   action,
	}
}

// RunScenario simulates one heartbeat scenario and computes KPIs.
func RunScenario(start time.Time, scenario Scenario) Result {
	expected := expectedRuns(scenario)
	events := make([]RunEvent, 0, expected)
	adherence := make([]time.Duration, 0, expected)
	recoveries := make([]time.Duration, 0, 2)

	missed := 0
	duplicates := 0
	lowSignal := 0
	highSignal := 0

	disruptionActive := false
	var disruptionStart time.Time

	for slot := 0; slot < expected; slot++ {
		scheduledAt := start.Add(time.Duration(slot) * scenarioInterval(scenario))
		dropped := scenario.DropSlots != nil && scenario.DropSlots[slot]
		failed := scenario.FailureSlots != nil && scenario.FailureSlots[slot]
		slotIsDisrupted := dropped || failed

		if slotIsDisrupted && !disruptionActive {
			disruptionActive = true
			disruptionStart = scheduledAt
		}

		if dropped {
			missed++
			continue
		}

		baseDelay := scenario.BaseDelay
		if scenario.DelayBySlot != nil {
			baseDelay += scenario.DelayBySlot[slot]
		}
		adherence = append(adherence, baseDelay)

		repeats := 1
		if scenario.DuplicateSlots != nil {
			repeats += maxInt(0, scenario.DuplicateSlots[slot])
		}
		duplicates += maxInt(0, repeats-1)

		firstSuccessTime := time.Time{}
		for i := 0; i < repeats; i++ {
			execAt := scheduledAt.Add(baseDelay).Add(time.Duration(i) * 500 * time.Millisecond)
			relevance := scenarioSignal(scenario, slot, failed, baseDelay, i > 0)
			if relevance < 0.45 {
				lowSignal++
			}
			if relevance >= 0.65 {
				highSignal++
			}
			event := RunEvent{
				Slot:        slot,
				ScheduledAt: scheduledAt,
				ExecutedAt:  execAt,
				Success:     !failed,
				Relevance:   relevance,
				Duplicate:   i > 0,
			}
			if event.Success && firstSuccessTime.IsZero() {
				firstSuccessTime = execAt
			}
			events = append(events, event)
		}

		if disruptionActive && !firstSuccessTime.IsZero() && !slotIsDisrupted {
			recoveries = append(recoveries, firstSuccessTime.Sub(disruptionStart))
			disruptionActive = false
		}
	}

	executed := len(events)
	kpi := KPI{
		ExpectedRuns:         expected,
		ExecutedRuns:         executed,
		MissedRuns:           missed,
		DuplicateRuns:        duplicates,
		ScheduleAdherenceP95: percentileDuration(adherence, 0.95),
		ScheduleAdherenceP99: percentileDuration(adherence, 0.99),
		RecoveryLatency:      percentileDuration(recoveries, 0.95),
		MissedRunRate:        ratio(missed, expected),
		DuplicateRunRate:     ratio(duplicates, expected),
		NuisanceScore:        ratio(lowSignal, executed) * 100,
		ActionabilityScore:   ratio(highSignal, executed) * 100,
	}
	return Result{
		Scenario: scenario,
		KPI:      kpi,
		Events:   events,
	}
}

func expectedRuns(s Scenario) int {
	d := scenarioDuration(s)
	i := scenarioInterval(s)
	if d <= 0 || i <= 0 {
		return 1
	}
	n := int(d / i)
	if n <= 0 {
		return 1
	}
	return n
}

func scenarioDuration(s Scenario) time.Duration {
	if s.Duration <= 0 {
		return defaultScenarioDuration
	}
	return s.Duration
}

func scenarioInterval(s Scenario) time.Duration {
	if s.Interval <= 0 {
		return defaultScenarioInterval
	}
	return s.Interval
}

func scenarioSignal(s Scenario, slot int, failed bool, delay time.Duration, duplicate bool) float64 {
	if s.SignalBySlot != nil {
		if v, ok := s.SignalBySlot[slot]; ok {
			return clamp01(v)
		}
	}
	switch {
	case failed:
		return 0.9
	case duplicate:
		return 0.7
	case delay > scenarioInterval(s)/2:
		return 0.75
	case delay > 2*time.Second:
		return 0.6
	default:
		return 0.25
	}
}

func percentileDuration(values []time.Duration, q float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	if q <= 0 {
		return values[0]
	}
	if q >= 1 {
		q = 1
	}
	cp := make([]time.Duration, len(values))
	copy(cp, values)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(math.Ceil(float64(len(cp))*q)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func sampleAdherence(events []RunEvent) []time.Duration {
	if len(events) == 0 {
		return nil
	}
	bySlot := map[int]time.Duration{}
	for _, e := range events {
		if e.Duplicate {
			continue
		}
		bySlot[e.Slot] = e.ExecutedAt.Sub(e.ScheduledAt)
	}
	out := make([]time.Duration, 0, len(bySlot))
	for _, d := range bySlot {
		out = append(out, d)
	}
	return out
}

func ratio(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func slotSet(slots ...int) map[int]bool {
	m := make(map[int]bool, len(slots))
	for _, s := range slots {
		m[s] = true
	}
	return m
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
