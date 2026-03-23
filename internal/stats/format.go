package stats

import (
	"fmt"
	"sort"
	"strings"
)

// FormatTable returns a human-readable table of stats for the /stats TUI command.
// Shows the last recorded value per metric (most recent wins).
func FormatTable(snap Stats) string {
	// Collect last value per metric from records
	lastRecord := make(map[string]metricEntry)
	for _, e := range snap.Records {
		lastRecord[e.Metric] = e
	}

	// Collect last value per metric from histograms
	lastHist := make(map[string]metricEntry)
	for _, e := range snap.Histograms {
		lastHist[e.Metric] = e
	}

	if len(lastRecord) == 0 && len(lastHist) == 0 {
		return "  no stats collected yet\n"
	}

	var sb strings.Builder
	sb.WriteString("  METRIC                          VALUE\n")
	sb.WriteString("  ─────────────────────────────── ─────────────\n")

	// Sort keys for deterministic output
	var keys []string
	for k := range lastRecord {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := lastRecord[k]
		sb.WriteString(fmt.Sprintf("  %-31s %v\n", k, e.Value))
	}

	var histKeys []string
	for k := range lastHist {
		histKeys = append(histKeys, k)
	}
	sort.Strings(histKeys)
	for _, k := range histKeys {
		e := lastHist[k]
		sb.WriteString(fmt.Sprintf("  %-31s %vms\n", k+" (p99)", e.Value))
	}

	return sb.String()
}
