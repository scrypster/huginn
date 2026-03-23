package agents

import (
	"strings"
	"testing"
	"time"
)

func TestSummaryKey_Format(t *testing.T) {
	key := SummaryKey("mymachine-01", "Mark", "sess-abc")
	if !strings.HasPrefix(key, "agent:summary:") {
		t.Errorf("expected agent:summary: prefix, got %q", key)
	}
	if !strings.Contains(key, "mymachine-01") || !strings.Contains(key, "Mark") || !strings.Contains(key, "sess-abc") {
		t.Errorf("key missing components: %q", key)
	}
}

func TestSummaryKey_Unique(t *testing.T) {
	k1 := SummaryKey("machine-01", "Mark", "sess-1")
	k2 := SummaryKey("machine-01", "Mark", "sess-2")
	k3 := SummaryKey("machine-01", "Chris", "sess-1")
	k4 := SummaryKey("machine-02", "Mark", "sess-1")
	seen := map[string]bool{}
	for _, k := range []string{k1, k2, k3, k4} {
		if seen[k] {
			t.Errorf("duplicate key: %q", k)
		}
		seen[k] = true
	}
}

func TestDelegationKey_Format(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	key := DelegationKey("mymachine-01", "Mark", "Chris", ts)
	if !strings.HasPrefix(key, "agent:delegation:") {
		t.Errorf("expected agent:delegation: prefix, got %q", key)
	}
	if !strings.Contains(key, "mymachine-01") || !strings.Contains(key, "Mark") || !strings.Contains(key, "Chris") {
		t.Errorf("key missing components: %q", key)
	}
}

func TestDelegationKey_Sortable(t *testing.T) {
	k1 := DelegationKey("m", "A", "B", time.Unix(1000, 0))
	k2 := DelegationKey("m", "A", "B", time.Unix(2000, 0))
	if k1 >= k2 {
		t.Errorf("expected k1 < k2: k1=%q k2=%q", k1, k2)
	}
}

func TestSessionSummary_Fields(t *testing.T) {
	s := SessionSummary{
		SessionID: "sess-1", MachineID: "m-01", AgentName: "Mark",
		Timestamp: time.Now(), Summary: "Did work",
		FilesTouched: []string{"a.go"}, Decisions: []string{"use pebble"}, OpenQuestions: []string{"add tests?"},
	}
	if s.AgentName != "Mark" {
		t.Errorf("AgentName: got %q", s.AgentName)
	}
	if len(s.FilesTouched) != 1 {
		t.Errorf("FilesTouched: got %d", len(s.FilesTouched))
	}
}

func TestDelegationEntry_Fields(t *testing.T) {
	d := DelegationEntry{From: "Mark", To: "Chris", Question: "q", Answer: "a", Timestamp: time.Now()}
	if d.From != "Mark" {
		t.Errorf("From: got %q", d.From)
	}
}
