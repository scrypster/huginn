package threadmgr_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/threadmgr"
)

func TestSummaryCache_StoreAndGet(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 10, "Done the auth work")
	s, ok := c.Get("sess-1")
	if !ok {
		t.Fatal("expected summary, got nothing")
	}
	if s != "Done the auth work" {
		t.Errorf("got %q", s)
	}
}

func TestSummaryCache_GetMissingSession_ReturnsFalse(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	_, ok := c.Get("no-such-session")
	if ok {
		t.Error("expected miss for unknown session")
	}
}

func TestSummaryCache_LaterStoreOverwrites(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 5, "first")
	c.Store("sess-1", 10, "second")
	s, _ := c.Get("sess-1")
	if s != "second" {
		t.Errorf("expected 'second' (newer), got %q", s)
	}
}

func TestSummaryCache_EarlierStoreDoesNotOverwrite(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 10, "newer")
	c.Store("sess-1", 5, "older") // lower msgCount — should not overwrite
	s, _ := c.Get("sess-1")
	if s != "newer" {
		t.Errorf("expected 'newer', got %q", s)
	}
}

func TestSummaryCache_Invalidate(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 10, "summary")
	c.Invalidate("sess-1")
	_, ok := c.Get("sess-1")
	if ok {
		t.Error("expected miss after invalidate")
	}
}

func TestSummaryCache_MultipleSessionsIndependent(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 5, "summary-A")
	c.Store("sess-2", 5, "summary-B")
	a, _ := c.Get("sess-1")
	b, _ := c.Get("sess-2")
	if a != "summary-A" || b != "summary-B" {
		t.Errorf("sessions not independent: a=%q b=%q", a, b)
	}
}

func TestSummaryCache_Invalidate_NoOpOnMissing(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	// Should not panic
	c.Invalidate("not-here")
}

func TestSummaryCache_ConcurrentAccess(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(i int) {
			c.Store("sess-concurrent", i, "summary")
			_, _ = c.Get("sess-concurrent")
			c.Invalidate("sess-concurrent")
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
