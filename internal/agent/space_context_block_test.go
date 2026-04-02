package agent

import (
	"strings"
	"testing"
)

func TestBuildSpaceContextBlock_NonChannelReturnsEmpty(t *testing.T) {
	result := BuildSpaceContextBlock("myspace", "dm", "Tom", "Tom", []SpaceMember{{Name: "Sam"}})
	if result != "" {
		t.Errorf("expected empty string for non-channel space, got %q", result)
	}
}

func TestBuildSpaceContextBlock_NoMembersReturnsEmpty(t *testing.T) {
	result := BuildSpaceContextBlock("myspace", "channel", "Tom", "Tom", []SpaceMember{})
	if result != "" {
		t.Errorf("expected empty string when no members, got %q", result)
	}
}

func TestBuildSpaceContextBlock_LeadAgentBlock_ContainsConditionalSilenceRule(t *testing.T) {
	result := BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", []SpaceMember{
		{Name: "Sam", Description: "backend specialist"},
	})

	// Must contain the conditional silence directive
	if !strings.Contains(result, "speak only when additive") {
		t.Error("lead agent block missing conditional silence rule")
	}
	if !strings.Contains(result, "synthesized recommendation") {
		t.Error("lead agent block missing synthesis condition")
	}
	if !strings.Contains(result, "Do NOT summarize or narrate") {
		t.Error("lead agent block missing no-recap directive")
	}
}

func TestBuildSpaceContextBlock_LeadAgentBlock_ContainsDelegationProtocol(t *testing.T) {
	result := BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", []SpaceMember{
		{Name: "Sam", Description: "backend specialist"},
	})

	if !strings.Contains(result, "Delegation protocol") {
		t.Error("lead agent block missing delegation protocol section")
	}
	if !strings.Contains(result, "@mentions") {
		t.Error("lead agent block missing @mention example")
	}
}

func TestBuildSpaceContextBlock_LeadAgentBlock_ListsMembers(t *testing.T) {
	result := BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", []SpaceMember{
		{Name: "Sam", Description: "backend specialist"},
		{Name: "Jordan", Description: ""},
	})

	if !strings.Contains(result, "Sam") {
		t.Error("lead agent block missing member Sam")
	}
	// Empty description falls back to "specialist agent"
	if !strings.Contains(result, "specialist agent") {
		t.Error("empty description should fall back to 'specialist agent'")
	}
}

func TestBuildSpaceContextBlock_MemberBlock_ContainsChannelAndLead(t *testing.T) {
	result := BuildSpaceContextBlock("Engineering", "channel", "Sam", "Tom", []SpaceMember{
		{Name: "Sam"}, {Name: "Jordan"},
	})

	if !strings.Contains(result, "Engineering") {
		t.Error("member block should contain channel name")
	}
	if !strings.Contains(result, "Tom") {
		t.Error("member block should contain lead agent name")
	}
}

func TestBuildSpaceContextBlock_MemberBlock_NoConditionalSilence(t *testing.T) {
	// Non-lead agents should NOT receive the conditional silence rule
	result := BuildSpaceContextBlock("Engineering", "channel", "Sam", "Tom", []SpaceMember{
		{Name: "Sam"}, {Name: "Jordan"},
	})

	if strings.Contains(result, "speak only when additive") {
		t.Error("non-lead agent block should NOT contain conditional silence rule")
	}
}

func TestBuildSpaceContextBlock_CaseInsensitiveLeadMatch(t *testing.T) {
	// selfName "TOM" should match leadAgent "Tom" (case-insensitive)
	result := BuildSpaceContextBlock("Engineering", "channel", "TOM", "Tom", []SpaceMember{
		{Name: "Sam"},
	})

	if !strings.Contains(result, "speak only when additive") {
		t.Error("case-insensitive lead match should produce lead agent block with silence rule")
	}
}
