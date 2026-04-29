package threadmgr

import (
	"testing"
)

func TestThreadManager_SetHelpResolver(t *testing.T) {
	tm := New()
	if tm.helpResolver != nil {
		t.Fatal("expected nil helpResolver on new ThreadManager")
	}
	r := &AutoHelpResolver{}
	tm.SetHelpResolver(r)
	if tm.helpResolver != r {
		t.Fatal("SetHelpResolver did not set the resolver")
	}
}

func TestThreadManager_SetCompletionNotifier(t *testing.T) {
	tm := New()
	if tm.completionNotifier != nil {
		t.Fatal("expected nil completionNotifier on new ThreadManager")
	}
	n := &CompletionNotifier{}
	tm.SetCompletionNotifier(n)
	if tm.completionNotifier != n {
		t.Fatal("SetCompletionNotifier did not set the notifier")
	}
}

func TestThreadManager_SetThreadBus(t *testing.T) {
	tm := New()
	if tm.threadBus == nil {
		t.Fatal("expected default threadBus on new ThreadManager")
	}
	bus := NewThreadBus(4)
	tm.SetThreadBus(bus)
	if tm.threadBus != bus {
		t.Fatal("SetThreadBus did not set bus")
	}
	tm.SetThreadBus(nil)
	if tm.threadBus != nil {
		t.Fatal("SetThreadBus(nil) should disable bus")
	}
}
