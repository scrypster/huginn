package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleReplicationStatusNoDB(t *testing.T) {
	_, ts := newTestServer(t)
	// No DB wired — should return 200 with zeroed counts, not 500.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/memory/replication-status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"pending", "failed", "dead", "connected"} {
		if _, ok := body[field]; !ok {
			t.Errorf("response missing %q field", field)
		}
	}
	if connected, _ := body["connected"].(bool); connected {
		t.Error("expected connected=false when no DB wired")
	}
}
