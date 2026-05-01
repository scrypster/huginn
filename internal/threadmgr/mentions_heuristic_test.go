package threadmgr

import (
	"strings"
	"testing"
)

// TestDetectBareAgentNames verifies that detectBareAgentNames finds agent names
// that appear in text without an @ sigil, near delegation-intent language.
func TestDetectBareAgentNames(t *testing.T) {
	known := []string{"Elena", "Sam", "Adam"}

	tests := []struct {
		name     string
		msg      string
		wantAny  []string // at least one of these must appear in result
		wantNone []string // none of these should appear
	}{
		{
			name:    "bare name with delegation verb",
			msg:     "Elena, please investigate the latency issue.",
			wantAny: []string{"Elena"},
		},
		{
			name:    "bare name with 'can you'",
			msg:     "Sam can you run the benchmarks?",
			wantAny: []string{"Sam"},
		},
		{
			name:     "already @mentioned — should not appear in heuristic",
			msg:      "@Elena please investigate",
			wantNone: []string{"Elena"},
		},
		{
			name:     "name in non-delegation context",
			msg:      "I was talking to Elena yesterday about the plan.",
			wantNone: []string{"Elena"},
		},
		{
			name:    "multiple bare names with delegation",
			msg:     "Sam, please run tests. Adam, please check the docs.",
			wantAny: []string{"Sam", "Adam"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectBareAgentNames(tt.msg, known)
			for _, want := range tt.wantAny {
				found := false
				for _, r := range result {
					if strings.EqualFold(r, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %q in result %v", want, result)
				}
			}
			for _, notwant := range tt.wantNone {
				for _, r := range result {
					if strings.EqualFold(r, notwant) {
						t.Errorf("expected %q NOT in result %v", notwant, result)
					}
				}
			}
		})
	}
}
