package agent

import (
	"fmt"
	"testing"
	"time"
)

// TestPrefetchCache_TTLEviction verifies that entries are evicted after maxAge.
func TestPrefetchCache_TTLEviction(t *testing.T) {
	c := newPrefetchCache(50*time.Millisecond, 100)

	c.set("key1", "value1", 50*time.Millisecond)

	// Should be present immediately.
	if got := c.get("key1"); got != "value1" {
		t.Fatalf("expected 'value1', got %q", got)
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	// get() evicts expired entries; key1 should be gone.
	if got := c.get("key1"); got != "" {
		t.Errorf("expected empty after TTL expiry, got %q", got)
	}
}

// TestPrefetchCache_SizeCapEviction verifies that the cache never exceeds maxSize.
func TestPrefetchCache_SizeCapEviction(t *testing.T) {
	const maxSize = 10
	c := newPrefetchCache(30*time.Minute, maxSize)

	// Insert maxSize+5 entries.
	for i := 0; i < maxSize+5; i++ {
		c.set(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i), 30*time.Minute)
	}

	if len(c.entries) > maxSize {
		t.Errorf("cache has %d entries, exceeds maxSize of %d", len(c.entries), maxSize)
	}
}

// TestPrefetchCache_GetUpdateLastAccess verifies that Get updates the LRU timestamp.
func TestPrefetchCache_GetUpdateLastAccess(t *testing.T) {
	const maxSize = 3
	c := newPrefetchCache(30*time.Minute, maxSize)

	// Fill the cache.
	for _, k := range []string{"a", "b", "c"} {
		c.set(k, k, 30*time.Minute)
		time.Sleep(1 * time.Millisecond) // ensure different lastAccess times
	}

	// Access "a" to make it recently used.
	_ = c.get("a")
	time.Sleep(1 * time.Millisecond)

	// Adding a 4th entry should evict the LRU (which should be "b", not "a").
	c.set("d", "d", 30*time.Minute)

	if len(c.entries) > maxSize {
		t.Errorf("cache has %d entries after eviction, expected <= %d", len(c.entries), maxSize)
	}

	// "a" should still be present because we accessed it recently.
	if got := c.get("a"); got == "" {
		t.Error("expected 'a' to still be cached after recent access")
	}
}
