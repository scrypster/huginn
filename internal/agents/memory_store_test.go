package agents

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/storage"
)

func openTestMemoryStore(t *testing.T, machineID string) *MemoryStore {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewMemoryStore(s, machineID)
}

func TestMemoryStore_SaveAndLoadSummary(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	summary := SessionSummary{SessionID: "sess-1", MachineID: "test-machine", AgentName: "Mark", Timestamp: time.Now(), Summary: "Did some work"}
	if err := ms.SaveSummary(ctx, summary); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}
	results, err := ms.LoadRecentSummaries(ctx, "Mark", 5)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(results))
	}
	if results[0].Summary != "Did some work" {
		t.Errorf("Summary: got %q", results[0].Summary)
	}
}

func TestMemoryStore_LoadRecentSummaries_LimitRespected(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 7; i++ {
		s := SessionSummary{SessionID: fmt.Sprintf("sess-%d", i), MachineID: "test-machine", AgentName: "Mark", Timestamp: base.Add(time.Duration(i) * time.Second), Summary: fmt.Sprintf("session %d", i)}
		if err := ms.SaveSummary(ctx, s); err != nil {
			t.Fatalf("SaveSummary[%d]: %v", i, err)
		}
	}
	results, err := ms.LoadRecentSummaries(ctx, "Mark", 3)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestMemoryStore_LoadRecentSummaries_SortedDescending(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 5; i++ {
		s := SessionSummary{SessionID: fmt.Sprintf("sess-%d", i), MachineID: "test-machine", AgentName: "Chris", Timestamp: base.Add(time.Duration(i) * time.Minute), Summary: fmt.Sprintf("session %d", i)}
		_ = ms.SaveSummary(ctx, s)
	}
	results, _ := ms.LoadRecentSummaries(ctx, "Chris", 10)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if !sort.SliceIsSorted(results, func(i, j int) bool { return results[i].Timestamp.After(results[j].Timestamp) }) {
		t.Error("results not sorted descending")
	}
	if results[0].SessionID != "sess-4" {
		t.Errorf("expected most recent to be sess-4, got %q", results[0].SessionID)
	}
}

func TestMemoryStore_LoadRecentSummaries_AgentIsolation(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	_ = ms.SaveSummary(ctx, SessionSummary{SessionID: "s1", MachineID: "test-machine", AgentName: "Mark", Timestamp: time.Now(), Summary: "mark"})
	_ = ms.SaveSummary(ctx, SessionSummary{SessionID: "s2", MachineID: "test-machine", AgentName: "Chris", Timestamp: time.Now(), Summary: "chris"})
	markResults, _ := ms.LoadRecentSummaries(ctx, "Mark", 10)
	for _, r := range markResults {
		if r.AgentName != "Mark" {
			t.Errorf("expected only Mark summaries, got %q", r.AgentName)
		}
	}
}

func TestMemoryStore_AppendAndLoadDelegation(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	entry := DelegationEntry{From: "Mark", To: "Chris", Question: "how does pebble scan work?", Answer: "use NewIter", Timestamp: time.Now()}
	if err := ms.AppendDelegation(ctx, entry); err != nil {
		t.Fatalf("AppendDelegation: %v", err)
	}
	results, err := ms.LoadRecentDelegations(ctx, "Mark", "Chris", 5)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 delegation, got %d", len(results))
	}
	if results[0].Question != "how does pebble scan work?" {
		t.Errorf("Question: got %q", results[0].Question)
	}
}

func TestMemoryStore_Delegation_BoundedTo10(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 15; i++ {
		entry := DelegationEntry{From: "Mark", To: "Chris", Question: fmt.Sprintf("q%d", i), Answer: fmt.Sprintf("a%d", i), Timestamp: base.Add(time.Duration(i) * time.Millisecond)}
		if err := ms.AppendDelegation(ctx, entry); err != nil {
			t.Fatalf("AppendDelegation[%d]: %v", i, err)
		}
	}
	results, err := ms.LoadRecentDelegations(ctx, "Mark", "Chris", 20)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	if len(results) > 10 {
		t.Errorf("expected at most 10, got %d", len(results))
	}
}

func TestMemoryStore_Delegation_OldestDropped(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 12; i++ {
		entry := DelegationEntry{From: "Mark", To: "Chris", Question: fmt.Sprintf("question %d", i), Answer: fmt.Sprintf("answer %d", i), Timestamp: base.Add(time.Duration(i) * time.Millisecond)}
		_ = ms.AppendDelegation(ctx, entry)
	}
	results, _ := ms.LoadRecentDelegations(ctx, "Mark", "Chris", 20)
	for _, r := range results {
		if r.Question == "question 0" || r.Question == "question 1" {
			t.Errorf("expected oldest dropped, found %q", r.Question)
		}
	}
}

func TestMemoryStore_MachineIDIsolation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	msA := NewMemoryStore(s, "machine-A")
	msB := NewMemoryStore(s, "machine-B")

	// Write a summary for machine-A.
	_ = msA.SaveSummary(ctx, SessionSummary{
		SessionID: "s1", MachineID: "machine-A", AgentName: "Mark",
		Timestamp: time.Now(), Summary: "machine A work",
	})

	// machine-B should not see machine-A's data.
	resultsB, err := msB.LoadRecentSummaries(ctx, "Mark", 10)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(resultsB) != 0 {
		t.Errorf("machine-B should not see machine-A data, got %d results", len(resultsB))
	}

	// machine-A should see its own data.
	resultsA, err := msA.LoadRecentSummaries(ctx, "Mark", 10)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(resultsA) != 1 {
		t.Errorf("machine-A should see its own data, got %d results", len(resultsA))
	}
}

func TestMemoryStore_Delegation_PairIsolation(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	ctx := context.Background()
	_ = ms.AppendDelegation(ctx, DelegationEntry{From: "Mark", To: "Chris", Question: "q1", Answer: "a1", Timestamp: time.Now()})
	_ = ms.AppendDelegation(ctx, DelegationEntry{From: "Mark", To: "Odin", Question: "q2", Answer: "a2", Timestamp: time.Now()})
	results, _ := ms.LoadRecentDelegations(ctx, "Mark", "Chris", 10)
	for _, r := range results {
		if r.To != "Chris" {
			t.Errorf("expected Mark->Chris only, got To=%q", r.To)
		}
	}
}
