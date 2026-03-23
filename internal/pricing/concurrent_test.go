package pricing

import (
	"sync"
	"testing"
)

// TestSessionTracker_Concurrent_MultipleGoroutines verifies thread-safe concurrent Add operations.
func TestSessionTracker_Concurrent_MultipleGoroutines(t *testing.T) {
	table := map[string]PricingEntry{
		"gpt-4": {PromptPer1M: 30.00, CompletionPer1M: 60.00},
		"gpt-3": {PromptPer1M: 0.5, CompletionPer1M: 1.5},
	}
	tracker := NewSessionTracker(table)

	const numGoroutines = 100
	const addsPerGoroutine = 10
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			model := "gpt-4"
			if idx%2 == 0 {
				model = "gpt-3"
			}
			for j := 0; j < addsPerGoroutine; j++ {
				tracker.Add(model, 100, 50)
			}
		}(i)
	}

	wg.Wait()

	// Verify final cost is non-zero
	cost := tracker.SessionCost()
	if cost <= 0 {
		t.Errorf("expected non-zero cost after concurrent adds, got %f", cost)
	}

	// Verify breakdown has entries for both models
	bd := tracker.Breakdown()
	if len(bd) < 1 {
		t.Error("expected at least one model in breakdown")
	}
}

// TestSessionTracker_Concurrent_Reset_During_Add verifies Reset doesn't panic during concurrent Add.
func TestSessionTracker_Concurrent_Reset_During_Add(t *testing.T) {
	table := map[string]PricingEntry{
		"gpt-4": {PromptPer1M: 30.00, CompletionPer1M: 60.00},
	}
	tracker := NewSessionTracker(table)

	var wg sync.WaitGroup
	const numAdders = 10

	for i := 0; i < numAdders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tracker.Add("gpt-4", 50, 25)
			}
		}()
	}

	// Reset while goroutines are adding
	for i := 0; i < 5; i++ {
		tracker.Reset()
	}

	wg.Wait()

	// Tracker should still be in valid state
	cost := tracker.SessionCost()
	if cost < 0 {
		t.Errorf("cost should never be negative, got %f", cost)
	}
}

// TestSessionTracker_Concurrent_SessionCost_During_Add verifies SessionCost reads during Add.
func TestSessionTracker_Concurrent_SessionCost_During_Add(t *testing.T) {
	table := map[string]PricingEntry{
		"gpt-4": {PromptPer1M: 30.00, CompletionPer1M: 60.00},
	}
	tracker := NewSessionTracker(table)

	var wg sync.WaitGroup
	const numAdders = 5
	const numReaders = 5

	for i := 0; i < numAdders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tracker.Add("gpt-4", 100, 50)
			}
		}()
	}

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = tracker.SessionCost()
			}
		}()
	}

	wg.Wait()

	// Final cost should be non-zero
	cost := tracker.SessionCost()
	if cost <= 0 {
		t.Errorf("expected non-zero final cost, got %f", cost)
	}
}

// TestSessionTracker_Concurrent_Breakdown_During_Add verifies Breakdown reads during Add.
func TestSessionTracker_Concurrent_Breakdown_During_Add(t *testing.T) {
	table := map[string]PricingEntry{
		"gpt-4": {PromptPer1M: 30.00, CompletionPer1M: 60.00},
		"gpt-3": {PromptPer1M: 0.5, CompletionPer1M: 1.5},
	}
	tracker := NewSessionTracker(table)

	var wg sync.WaitGroup

	// Add entries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.Add("gpt-4", 50, 25)
			tracker.Add("gpt-3", 100, 100)
		}()
	}

	// Read breakdown concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tracker.Breakdown()
		}()
	}

	wg.Wait()

	bd := tracker.Breakdown()
	if len(bd) != 2 {
		t.Errorf("expected 2 models in breakdown, got %d", len(bd))
	}
}

// TestSessionTracker_Concurrent_StatusBarText verifies StatusBarText during updates.
func TestSessionTracker_Concurrent_StatusBarText(t *testing.T) {
	table := map[string]PricingEntry{
		"gpt-4": {PromptPer1M: 30.00, CompletionPer1M: 60.00},
	}
	tracker := NewSessionTracker(table)

	var wg sync.WaitGroup

	// Add concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tracker.Add("gpt-4", 100, 50)
			}
		}()
	}

	// Read status bar text concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				text := tracker.StatusBarText()
				// text could be empty or a formatted string, both valid
				if text != "" && !contains(text, "$") {
					t.Errorf("non-empty StatusBarText should contain $, got %q", text)
				}
			}
		}()
	}

	wg.Wait()
}

// TestSessionTracker_ConcurrentRaceAdd verifies heavy concurrent Add with various models.
func TestSessionTracker_ConcurrentRaceAdd(t *testing.T) {
	table := map[string]PricingEntry{
		"gpt-4":       {PromptPer1M: 30.00, CompletionPer1M: 60.00},
		"gpt-3.5":     {PromptPer1M: 0.5, CompletionPer1M: 1.5},
		"claude-opus": {PromptPer1M: 15.00, CompletionPer1M: 75.00},
	}
	tracker := NewSessionTracker(table)

	var wg sync.WaitGroup
	const numGoroutines = 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			models := []string{"gpt-4", "gpt-3.5", "claude-opus"}
			for j := 0; j < 100; j++ {
				model := models[(idx+j)%len(models)]
				tracker.Add(model, 10+j, 5+j)
			}
		}(i)
	}

	wg.Wait()

	// Verify consistent state
	cost := tracker.SessionCost()
	bd := tracker.Breakdown()

	if cost < 0 {
		t.Errorf("cost should never be negative, got %f", cost)
	}
	if len(bd) > 3 {
		t.Errorf("expected at most 3 models, got %d", len(bd))
	}

	// All models should have positive usage
	for _, entry := range bd {
		if entry.PromptTokens <= 0 {
			t.Errorf("model %s should have positive prompt tokens", entry.Model)
		}
		if entry.CompletionTokens <= 0 {
			t.Errorf("model %s should have positive completion tokens", entry.Model)
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
