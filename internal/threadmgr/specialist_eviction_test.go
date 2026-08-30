package threadmgr

import (
	"sync"
	"testing"
	"time"
)

// TestSpecialistEviction_OnTerminalStatus verifies that a specialist thread
// registered via RegisterSpecialistThread is evicted (specialistEvictFn
// called, specialistCompany cleared) the moment its thread lands terminal —
// no roster pollution, no dangling fixed-company record (S5 + S12 cleanup).
func TestSpecialistEviction_OnTerminalStatus(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston"}})
	tm.SetSpecialistCompany("Rust Audit Specialist", "co-a")

	var mu sync.Mutex
	var evicted []string
	tm.SetSpecialistEvictor(func(name, threadID string) {
		mu.Lock()
		evicted = append(evicted, name)
		mu.Unlock()
	})

	th, err := tm.Create(CreateParams{
		SessionID: "sess-a",
		AgentID:   "Rust Audit Specialist",
		Task:      "audit",
		SpaceID:   "space-a",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.RegisterSpecialistThread(th.ID, "Rust Audit Specialist")

	tm.Cancel(th.ID) // any terminal transition should trigger eviction

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(evicted)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(evicted) != 1 || evicted[0] != "rust audit specialist" {
		t.Fatalf("expected eviction of rust audit specialist, got %v", evicted)
	}

	// Company record must also be cleared — a fresh Create() call for the
	// same name should be rejected as an unknown specialist now.
	_, err = tm.Create(CreateParams{
		SessionID: "sess-a2",
		AgentID:   "Rust Audit Specialist",
		Task:      "after eviction",
		SpaceID:   "space-a",
	})
	if err == nil {
		t.Fatal("expected specialistCompany cleared after eviction")
	}
}

func TestEvictStaleSpecialists_TTLSweepFallback(t *testing.T) {
	tm := New()
	tm.SetSpecialistCompany("Stuck Specialist", "co-a")

	var mu sync.Mutex
	var evicted []string
	tm.SetSpecialistEvictor(func(name, threadID string) {
		mu.Lock()
		evicted = append(evicted, name)
		mu.Unlock()
	})

	tm.mu.Lock()
	tm.specialistThreads["stuck-thread"] = specialistThreadEntry{
		agentName:    "stuck specialist",
		registeredAt: time.Now().Add(-2 * time.Hour),
	}
	tm.mu.Unlock()

	stale := tm.EvictStaleSpecialists(1 * time.Hour)
	if len(stale) != 1 || stale[0] != "stuck specialist" {
		t.Fatalf("expected stale sweep to evict stuck specialist, got %v", stale)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(evicted) != 1 || evicted[0] != "stuck specialist" {
		t.Fatalf("expected evictor called for stuck specialist, got %v", evicted)
	}
}

func TestEvictStaleSpecialists_KeepsFreshEntries(t *testing.T) {
	tm := New()
	tm.mu.Lock()
	tm.specialistThreads["fresh-thread"] = specialistThreadEntry{
		agentName:    "fresh specialist",
		registeredAt: time.Now(),
	}
	tm.mu.Unlock()

	stale := tm.EvictStaleSpecialists(1 * time.Hour)
	if len(stale) != 0 {
		t.Fatalf("expected fresh entry to survive sweep, evicted: %v", stale)
	}
}
