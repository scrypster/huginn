package tui

import (
	"testing"
)

// ============================================================
// indeterminatePct
// ============================================================

func TestIndeterminatePct_ZeroTick(t *testing.T) {
	pct := indeterminatePct(0)
	if pct < 0.0 || pct > 1.0 {
		t.Errorf("indeterminatePct(0) should be in [0,1], got %f", pct)
	}
	if pct != 0.0 {
		t.Errorf("expected 0.0 for tick=0, got %f", pct)
	}
}

func TestIndeterminatePct_MidCycle(t *testing.T) {
	pct := indeterminatePct(50)
	if pct < 0.0 || pct > 1.0 {
		t.Errorf("indeterminatePct(50) should be in [0,1], got %f", pct)
	}
	if pct != 0.5 {
		t.Errorf("expected 0.5 for tick=50, got %f", pct)
	}
}

func TestIndeterminatePct_EndOfCycle(t *testing.T) {
	pct := indeterminatePct(99)
	if pct < 0.0 || pct > 1.0 {
		t.Errorf("indeterminatePct(99) should be in [0,1], got %f", pct)
	}
	if pct != 0.99 {
		t.Errorf("expected 0.99 for tick=99, got %f", pct)
	}
}

func TestIndeterminatePct_WrapAround(t *testing.T) {
	// At tick=100, it wraps back to 0 (100 % 100 == 0)
	pct := indeterminatePct(100)
	if pct != 0.0 {
		t.Errorf("expected 0.0 after full cycle (tick=100), got %f", pct)
	}
}

func TestIndeterminatePct_LargeTickWraps(t *testing.T) {
	// tick=150 → 150%100=50 → 0.5
	pct := indeterminatePct(150)
	if pct != 0.5 {
		t.Errorf("expected 0.5 for tick=150 (150%%100=50), got %f", pct)
	}
}

func TestIndeterminatePct_AlwaysInRange(t *testing.T) {
	for i := 0; i <= 500; i++ {
		pct := indeterminatePct(i)
		if pct < 0.0 || pct > 1.0 {
			t.Errorf("indeterminatePct(%d) = %f, out of [0,1] range", i, pct)
		}
	}
}
