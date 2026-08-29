package server

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestSpecialistPromotion_FirstAndSecondSpawn_NoRecommendation(t *testing.T) {
	tr := NewSpecialistPromotionTracker(t.TempDir())

	mustRecord(t, tr, "co1", "Rust Audit", "claude-sonnet-4", "thread-1")
	rec, err := tr.ShouldRecommendHire("Rust Audit")
	if err != nil {
		t.Fatalf("ShouldRecommendHire: %v", err)
	}
	if rec {
		t.Error("expected no recommendation after 1st spawn")
	}

	mustRecord(t, tr, "co1", "Rust Audit", "claude-sonnet-4", "thread-2")
	rec, err = tr.ShouldRecommendHire("Rust Audit")
	if err != nil {
		t.Fatalf("ShouldRecommendHire: %v", err)
	}
	if rec {
		t.Error("expected no recommendation after 2nd spawn")
	}
}

func TestSpecialistPromotion_ThirdSpawnInWindow_Triggers(t *testing.T) {
	tr := NewSpecialistPromotionTracker(t.TempDir())

	mustRecord(t, tr, "co1", "Rust Audit", "claude-sonnet-4", "thread-1")
	mustRecord(t, tr, "co1", "Rust Audit", "claude-sonnet-4", "thread-2")
	mustRecord(t, tr, "co1", "rust audit", "claude-sonnet-4", "thread-3") // case-insensitive match

	rec, err := tr.ShouldRecommendHire("Rust Audit")
	if err != nil {
		t.Fatalf("ShouldRecommendHire: %v", err)
	}
	if !rec {
		t.Error("expected recommendation on 3rd spawn of the same label within the window")
	}
}

func TestSpecialistPromotion_OutsideWindow_DoesNotCount(t *testing.T) {
	tr := NewSpecialistPromotionTracker(t.TempDir())

	// Two spawns 15 days ago (outside the 14-day window), one just now.
	stale := time.Now().Add(-15 * 24 * time.Hour)
	mustRecordAt(t, tr, "co1", "Rust Audit", "m", "thread-1", stale)
	mustRecordAt(t, tr, "co1", "Rust Audit", "m", "thread-2", stale)
	mustRecord(t, tr, "co1", "Rust Audit", "m", "thread-3")

	rec, err := tr.ShouldRecommendHire("Rust Audit")
	if err != nil {
		t.Fatalf("ShouldRecommendHire: %v", err)
	}
	if rec {
		t.Error("expected no recommendation: only 1 spawn falls inside the 14-day window")
	}
}

func TestSpecialistPromotion_DifferentLabel_DoesNotCrossTrigger(t *testing.T) {
	tr := NewSpecialistPromotionTracker(t.TempDir())

	mustRecord(t, tr, "co1", "Rust Audit", "m", "thread-1")
	mustRecord(t, tr, "co1", "Rust Audit", "m", "thread-2")
	// A related-but-distinct label — must NOT be folded into "Rust Audit"'s
	// count. Undercount over false clustering (S14 synthesis decision).
	mustRecord(t, tr, "co1", "Rust Security Review", "m", "thread-3")

	rec, err := tr.ShouldRecommendHire("Rust Audit")
	if err != nil {
		t.Fatalf("ShouldRecommendHire: %v", err)
	}
	if rec {
		t.Error("expected no recommendation: the 3rd spawn was a different label")
	}

	rec2, err := tr.ShouldRecommendHire("Rust Security Review")
	if err != nil {
		t.Fatalf("ShouldRecommendHire: %v", err)
	}
	if rec2 {
		t.Error("expected no recommendation: Rust Security Review has only 1 spawn")
	}
}

func TestNormalizeCapabilityLabel(t *testing.T) {
	cases := map[string]string{
		"Rust Audit":           "rust audit",
		"  rust   audit  ":     "rust audit",
		"RUST AUDIT":           "rust audit",
		"Rust Security Review": "rust security review",
	}
	for in, want := range cases {
		if got := NormalizeCapabilityLabel(in); got != want {
			t.Errorf("NormalizeCapabilityLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustRecord(t *testing.T, tr *SpecialistPromotionTracker, company, label, model, threadID string) {
	t.Helper()
	if err := tr.RecordSpawn(company, label, model, threadID); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
}

// mustRecordAt writes a record with an explicit timestamp by calling
// RecordSpawn then rewriting the just-written line's ts field — the public
// API only ever stamps "now", but the promotion counter's window logic
// needs to be exercised against records from the past.
func mustRecordAt(t *testing.T, tr *SpecialistPromotionTracker, company, label, model, threadID string, ts time.Time) {
	t.Helper()
	mustRecord(t, tr, company, label, model, threadID)

	data, err := os.ReadFile(tr.path)
	if err != nil {
		t.Fatalf("read %s: %v", tr.path, err)
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	last := lines[len(lines)-1]
	var rec specialistSpawnRecord
	if err := json.Unmarshal(last, &rec); err != nil {
		t.Fatalf("unmarshal last record: %v", err)
	}
	rec.TS = ts.UTC()
	rewritten, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal backdated record: %v", err)
	}
	lines[len(lines)-1] = rewritten
	out := bytes.Join(lines, []byte("\n"))
	out = append(out, '\n')
	if err := os.WriteFile(tr.path, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", tr.path, err)
	}
}

// Opus vet 2026-08-29: the recommendation must fire EXACTLY ONCE per label
// per window, not once per completion after the threshold, and not zero
// times if the count jumps past 3. The check-and-set marker enforces this.
func TestSpecialistPromotion_FiresExactlyOncePerWindow(t *testing.T) {
	dir := t.TempDir()
	tr := NewSpecialistPromotionTracker(dir)
	for i := 0; i < 3; i++ {
		if err := tr.RecordSpawn("Acme", "Rust Audit", "m", "t"); err != nil {
			t.Fatal(err)
		}
	}
	// Three completions all call ShouldRecommendHire — only the first fires.
	fires := 0
	for i := 0; i < 3; i++ {
		ok, err := tr.ShouldRecommendHire("Rust Audit")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			fires++
		}
	}
	if fires != 1 {
		t.Fatalf("recommendation fired %d times, want exactly 1", fires)
	}
	// A 4th spawn+completion still stays silent (already recommended this window).
	_ = tr.RecordSpawn("Acme", "Rust Audit", "m", "t")
	ok, _ := tr.ShouldRecommendHire("Rust Audit")
	if ok {
		t.Fatal("recommendation fired again after already recommending this window")
	}
}

// The jump-past-3 case the vet flagged: if the count reaches 3 via any path,
// the first ShouldRecommendHire at >=3 fires (never zero).
func TestSpecialistPromotion_JumpPastThreeStillFires(t *testing.T) {
	dir := t.TempDir()
	tr := NewSpecialistPromotionTracker(dir)
	for i := 0; i < 5; i++ {
		_ = tr.RecordSpawn("Acme", "COBOL", "m", "t")
	}
	ok, err := tr.ShouldRecommendHire("COBOL")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("count of 5 should still fire the (once) recommendation")
	}
}
