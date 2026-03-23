package agent

import (
	"testing"

	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
)

func TestNewSession_RecordsMetric(t *testing.T) {
	t.Parallel()
	reg := stats.NewRegistry()
	sc := reg.Collector()

	orch, err := NewOrchestrator(obsBackendNoTools{}, modelconfig.DefaultModels(), nil, nil, sc, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = orch.NewSession("")
	if err != nil {
		t.Fatal(err)
	}

	snap := reg.Snapshot()
	found := false
	for _, r := range snap.Records {
		if r.Metric == "sessions.created" && r.Value == 1 {
			found = true
		}
	}
	if !found {
		t.Error("sessions.created metric not recorded after NewSession")
	}
}
