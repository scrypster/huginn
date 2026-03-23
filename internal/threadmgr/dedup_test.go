package threadmgr

import (
	"strings"
	"testing"
)

func TestDedupMentions_NoOverlap(t *testing.T) {
	// User mentions @Tom, assistant mentions @Sam — @Sam must be preserved.
	result := DedupMentions("@Tom start the analysis", "I'll ask @Sam to handle the data layer")
	if !strings.Contains(result, "@Sam") {
		t.Errorf("@Sam should be preserved (not in userMsg), got %q", result)
	}
}

func TestDedupMentions_SameAgentInBoth(t *testing.T) {
	// User says @Sam and assistant reply also says @Sam — strip from reply.
	result := DedupMentions("@Sam fix the bug", "I will ask @Sam to handle this")
	if strings.Contains(result, "@Sam") {
		t.Errorf("@Sam should be stripped from reply (was in userMsg), got %q", result)
	}
}

func TestDedupMentions_CaseInsensitive(t *testing.T) {
	// @sam (lowercase) in user message should strip @Sam (title-case) from reply.
	result := DedupMentions("@sam fix it", "Delegating to @Sam now")
	if strings.Contains(result, "@Sam") {
		t.Errorf("@Sam should be stripped case-insensitively, got %q", result)
	}
}

func TestDedupMentions_EmptyUserMsg(t *testing.T) {
	// No user @mentions — all reply mentions preserved.
	result := DedupMentions("", "Please ask @Sam to help")
	if !strings.Contains(result, "@Sam") {
		t.Errorf("@Sam should be preserved when userMsg has no @mentions, got %q", result)
	}
}

func TestDedupMentions_EmptyReply(t *testing.T) {
	result := DedupMentions("@Sam do it", "")
	if result != "" {
		t.Errorf("empty reply should stay empty, got %q", result)
	}
}

func TestDedupMentions_BothEmpty(t *testing.T) {
	result := DedupMentions("", "")
	if result != "" {
		t.Errorf("both empty should produce empty string, got %q", result)
	}
}

func TestDedupMentions_MultipleAgents_OnlyOverlapStripped(t *testing.T) {
	// User @mentions Tom; assistant reply mentions @Tom and @Sam.
	// Only @Tom should be stripped; @Sam should be preserved.
	result := DedupMentions("@Tom start", "@Tom route to @Sam for the data work")
	if strings.Contains(result, "@Tom") {
		t.Errorf("@Tom should be stripped (was in userMsg), got %q", result)
	}
	if !strings.Contains(result, "@Sam") {
		t.Errorf("@Sam should be preserved (was NOT in userMsg), got %q", result)
	}
}

func TestDedupMentions_HyphenatedName_Stripped(t *testing.T) {
	// Hyphenated agent names supported by MentionRe `[\w-]+`.
	result := DedupMentions("@Sam-Johnson fix it", "I'll delegate to @Sam-Johnson")
	if strings.Contains(result, "@Sam-Johnson") {
		t.Errorf("@Sam-Johnson should be stripped from reply, got %q", result)
	}
}

func TestDedupMentions_HyphenatedName_Preserved(t *testing.T) {
	// Hyphenated agent not in userMsg should be preserved.
	result := DedupMentions("@Tom start", "Route to @Sam-Johnson for infra")
	if !strings.Contains(result, "@Sam-Johnson") {
		t.Errorf("@Sam-Johnson should be preserved (not in userMsg), got %q", result)
	}
}

func TestDedupMentions_LeadingSeparatorPreserved(t *testing.T) {
	// After stripping @Name, the leading space/punct should remain.
	result := DedupMentions("@Sam fix it", "Please ask @Sam to handle this")
	// "@Sam" → "Sam" but the preceding space should still be present.
	if !strings.Contains(result, " Sam") {
		t.Errorf("leading space should be preserved after stripping @, got %q", result)
	}
}

func TestDedupMentions_ReplyWithNoMentions(t *testing.T) {
	// Reply has no @mentions at all — should pass through unchanged.
	reply := "Sure, I will look into this right away."
	result := DedupMentions("@Sam do it", reply)
	if result != reply {
		t.Errorf("reply without mentions should be unchanged, got %q", result)
	}
}

func TestDedupMentions_DuplicateInUserMsg(t *testing.T) {
	// @Sam appears twice in userMsg — dedup should still strip @Sam from reply.
	result := DedupMentions("@Sam and @Sam again", "Will ask @Sam to handle this")
	if strings.Contains(result, "@Sam") {
		t.Errorf("@Sam should be stripped even with duplicate in userMsg, got %q", result)
	}
}
