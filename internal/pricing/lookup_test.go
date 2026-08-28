package pricing

import "testing"

// Lookup falls back to a longest-substring-ish match for dated/regional
// model variants, but the 2-char OpenAI keys ("o1", "o3") must not mis-fire
// on names that merely contain those two characters mid-token.

func TestLookup_ExactMatch(t *testing.T) {
	entry, ok := Lookup(DefaultTable, "o3")
	if !ok {
		t.Fatal("expected exact match for o3")
	}
	if entry != DefaultTable["o3"] {
		t.Errorf("got %+v, want %+v", entry, DefaultTable["o3"])
	}
}

func TestLookup_O3Mini_MatchesO3Mini_NotO3(t *testing.T) {
	// Longest-match precedence: "o3-mini" (7 chars) must win over "o3" (2 chars).
	entry, ok := Lookup(DefaultTable, "o3-mini")
	if !ok {
		t.Fatal("expected match for o3-mini")
	}
	if entry != DefaultTable["o3-mini"] {
		t.Errorf("got %+v, want o3-mini's entry %+v", entry, DefaultTable["o3-mini"])
	}
}

func TestLookup_DatedVariant_BoundaryMatch(t *testing.T) {
	// A dated/regional variant like "o3-2026-01-01" should still resolve
	// against "o3" via a boundary-flanked substring match.
	entry, ok := Lookup(DefaultTable, "o3-2026-01-01")
	if !ok {
		t.Fatal("expected boundary match for dated o3 variant")
	}
	if entry != DefaultTable["o3"] {
		t.Errorf("got %+v, want o3's entry %+v", entry, DefaultTable["o3"])
	}
}

func TestLookup_O1MidToken_DoesNotMatch(t *testing.T) {
	// "gpto1-custom" merely CONTAINS "o1" mid-token (flanked by 't' on the
	// left) — must NOT resolve against the OpenAI o1 pricing entry.
	_, ok := Lookup(DefaultTable, "gpto1-custom")
	if ok {
		t.Error("expected no match: 'o1' appears mid-token, not as its own token")
	}
}

func TestLookup_O3MidToken_DoesNotMatch(t *testing.T) {
	// "foo3bar" contains "o3" flanked by 'o'/'3' on the left... actually
	// flanked by alnum on both sides — must not match.
	_, ok := Lookup(DefaultTable, "foo3bar")
	if ok {
		t.Error("expected no match: 'o3' appears mid-token in 'foo3bar'")
	}
}

func TestLookup_O1PrecededByAlnum_DoesNotMatch(t *testing.T) {
	_, ok := Lookup(DefaultTable, "zoo1model")
	if ok {
		t.Error("expected no match: 'o1' preceded by alnum 'o' with no boundary")
	}
}

func TestLookup_O1WithBoundaryDash_Matches(t *testing.T) {
	// "chat-o1-preview" flanks "o1" with '-' on both sides — a legitimate
	// boundary match.
	entry, ok := Lookup(DefaultTable, "chat-o1-preview")
	if !ok {
		t.Fatal("expected boundary match for chat-o1-preview")
	}
	if entry != DefaultTable["o1"] {
		t.Errorf("got %+v, want o1's entry %+v", entry, DefaultTable["o1"])
	}
}

func TestLookup_ClaudeFamily_LongestMatchPrecedencePreserved(t *testing.T) {
	// claude-opus-4-6 and claude-sonnet-4-6 must still resolve correctly for
	// their own dated variants — the boundary-match rewrite must not break
	// the long, multi-hyphen claude keys.
	tests := []struct {
		model string
		want  string
	}{
		{"claude-opus-4-6-20260615", "claude-opus-4-6"},
		{"claude-sonnet-4-6-20260615", "claude-sonnet-4-6"},
		{"claude-opus-4-5-us", "claude-opus-4-5"},
		{"claude-haiku-4-5-20251001-eu", "claude-haiku-4-5-20251001"},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			entry, ok := Lookup(DefaultTable, tc.model)
			if !ok {
				t.Fatalf("expected match for %q", tc.model)
			}
			if entry != DefaultTable[tc.want] {
				t.Errorf("got %+v, want %q's entry %+v", entry, tc.want, DefaultTable[tc.want])
			}
		})
	}
}

func TestLookup_UnknownModel_NoMatch(t *testing.T) {
	_, ok := Lookup(DefaultTable, "llama3-local")
	if ok {
		t.Error("expected no match for an unrelated local model name")
	}
}

func TestLookup_EmptyModel_NoMatch(t *testing.T) {
	_, ok := Lookup(DefaultTable, "")
	if ok {
		t.Error("expected no match for empty model string")
	}
}
