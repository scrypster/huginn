package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/scrypster/huginn/internal/stats"
)

// promState holds the per-server Prometheus registry and collector.
// Initialised on SetStatsRegistry; nil if metrics are not configured.
type promState struct {
	collector *stats.PrometheusCollector
	registry  *prometheus.Registry
}

// initPromState creates a new Prometheus registry with the stats collector
// registered. Safe to call from SetStatsRegistry after the stats.Registry is
// wired. Uses a fresh prometheus.Registry (not DefaultRegisterer) so test
// runs never conflict with each other or with the process default.
func initPromState(reg *stats.Registry) *promState {
	collector := stats.NewPrometheusCollector(reg)
	promReg := prometheus.NewRegistry()
	promReg.MustRegister(collector)
	return &promState{
		collector: collector,
		registry:  promReg,
	}
}

// handlePrometheusMetrics serves collected metrics in the Prometheus text
// exposition format (version 0.0.4).
// GET /api/v1/metrics/prometheus
func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	ps := s.prometheusSt
	s.mu.Unlock()

	if ps == nil {
		http.Error(w, "metrics not configured", http.StatusServiceUnavailable)
		return
	}

	// Gather all metric families from our registry.
	mfs, err := ps.registry.Gather()
	if err != nil {
		http.Error(w, fmt.Sprintf("gather: %v", err), http.StatusInternalServerError)
		return
	}

	// Sort metric families for deterministic output.
	sort.Slice(mfs, func(i, j int) bool {
		return mfs[i].GetName() < mfs[j].GetName()
	})

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	var sb strings.Builder
	for _, mf := range mfs {
		name := mf.GetName()
		help := mf.GetHelp()
		if help != "" {
			fmt.Fprintf(&sb, "# HELP %s %s\n", name, help)
		}

		metricType := strings.ToLower(mf.GetType().String())
		fmt.Fprintf(&sb, "# TYPE %s %s\n", name, metricType)

		for _, m := range mf.GetMetric() {
			labels := formatLabels(m.GetLabel())

			switch mf.GetType() {
			case dto.MetricType_GAUGE:
				fmt.Fprintf(&sb, "%s%s %g\n", name, labels, m.GetGauge().GetValue())
			case dto.MetricType_COUNTER:
				fmt.Fprintf(&sb, "%s%s %g\n", name, labels, m.GetCounter().GetValue())
			case dto.MetricType_HISTOGRAM:
				h := m.GetHistogram()
				for _, b := range h.GetBucket() {
					fmt.Fprintf(&sb, "%s_bucket{le=\"%g\"%s} %d\n",
						name, b.GetUpperBound(), labelsInner(m.GetLabel()), b.GetCumulativeCount())
				}
				fmt.Fprintf(&sb, "%s_bucket{le=\"+Inf\"%s} %d\n",
					name, labelsInner(m.GetLabel()), h.GetSampleCount())
				fmt.Fprintf(&sb, "%s_sum%s %g\n", name, labels, h.GetSampleSum())
				fmt.Fprintf(&sb, "%s_count%s %d\n", name, labels, h.GetSampleCount())
			}
		}
	}

	_, _ = fmt.Fprint(w, sb.String())
}

// formatLabels returns a Prometheus label string like {key="val",...} or "".
func formatLabels(pairs []*dto.LabelPair) string {
	if len(pairs) == 0 {
		return ""
	}
	return "{" + labelsInner(pairs) + "}"
}

// labelsInner returns the inner part of a Prometheus label set without braces.
func labelsInner(pairs []*dto.LabelPair) string {
	if len(pairs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s=%q", p.GetName(), p.GetValue()))
	}
	return strings.Join(parts, ",")
}
