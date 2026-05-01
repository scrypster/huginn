package agent

import (
	"strings"
	"testing"
)

// TestBuildDMCrossSpaceContextBlock_EmptyChannels_ReturnsEmpty verifies that
// an empty channels slice returns an empty string.
func TestBuildDMCrossSpaceContextBlock_EmptyChannels_ReturnsEmpty(t *testing.T) {
	result := BuildDMCrossSpaceContextBlock("Alice", []ChannelRoster{})
	if result != "" {
		t.Errorf("expected empty string for empty channels, got: %q", result)
	}
}

// TestBuildDMCrossSpaceContextBlock_SingleChannel_ContainsChannelName verifies
// that a single channel with a name, lead, and members is properly formatted
// with the channel name in the output.
func TestBuildDMCrossSpaceContextBlock_SingleChannel_ContainsChannelName(t *testing.T) {
	channels := []ChannelRoster{
		{
			Name:      "Engineering",
			LeadAgent: "Tom",
			Members: []SpaceMember{
				{Name: "Sam", Description: "backend engineer"},
				{Name: "Adam", Description: "frontend engineer"},
			},
		},
	}

	result := BuildDMCrossSpaceContextBlock("Alice", channels)

	if !strings.Contains(result, "#Engineering") {
		t.Errorf("expected output to contain '#Engineering', got: %s", result)
	}
}

// TestBuildDMCrossSpaceContextBlock_SelfIsLead_ContainsLeadIndicator verifies
// that when selfName matches the channel's LeadAgent, the output contains
// "(you are the lead)" marker.
func TestBuildDMCrossSpaceContextBlock_SelfIsLead_ContainsLeadIndicator(t *testing.T) {
	channels := []ChannelRoster{
		{
			Name:      "Engineering",
			LeadAgent: "Tom",
			Members: []SpaceMember{
				{Name: "Sam", Description: "backend engineer"},
				{Name: "Adam", Description: "frontend engineer"},
			},
		},
	}

	result := BuildDMCrossSpaceContextBlock("Tom", channels)

	if !strings.Contains(result, "(you are the lead)") {
		t.Errorf("expected output to contain '(you are the lead)', got: %s", result)
	}
}

// TestBuildDMCrossSpaceContextBlock_SelfIsNotLead_NoLeadIndicator verifies
// that when selfName does NOT match the channel's LeadAgent, the output does
// NOT contain "(you are the lead)".
func TestBuildDMCrossSpaceContextBlock_SelfIsNotLead_NoLeadIndicator(t *testing.T) {
	channels := []ChannelRoster{
		{
			Name:      "Engineering",
			LeadAgent: "Tom",
			Members: []SpaceMember{
				{Name: "Sam", Description: "backend engineer"},
				{Name: "Adam", Description: "frontend engineer"},
			},
		},
	}

	result := BuildDMCrossSpaceContextBlock("Alice", channels)

	if strings.Contains(result, "(you are the lead)") {
		t.Errorf("expected output to NOT contain '(you are the lead)', got: %s", result)
	}
}

// TestBuildDMCrossSpaceContextBlock_MultipleChannels_AllListed verifies that
// when multiple channels are provided, all of them appear in the output.
func TestBuildDMCrossSpaceContextBlock_MultipleChannels_AllListed(t *testing.T) {
	channels := []ChannelRoster{
		{
			Name:      "Engineering",
			LeadAgent: "Tom",
			Members: []SpaceMember{
				{Name: "Sam", Description: "backend engineer"},
			},
		},
		{
			Name:      "Design",
			LeadAgent: "Carol",
			Members: []SpaceMember{
				{Name: "Dave", Description: "UI designer"},
			},
		},
	}

	result := BuildDMCrossSpaceContextBlock("Alice", channels)

	if !strings.Contains(result, "#Engineering") {
		t.Errorf("expected output to contain '#Engineering', got: %s", result)
	}
	if !strings.Contains(result, "#Design") {
		t.Errorf("expected output to contain '#Design', got: %s", result)
	}
}

// TestBuildDMCrossSpaceContextBlock_MemberDescriptions_Included verifies that
// members with descriptions include those descriptions in the output, and that
// members without descriptions show the "specialist agent" default text.
func TestBuildDMCrossSpaceContextBlock_MemberDescriptions_Included(t *testing.T) {
	channels := []ChannelRoster{
		{
			Name:      "Engineering",
			LeadAgent: "Tom",
			Members: []SpaceMember{
				{Name: "Sam", Description: "backend expert"},
				{Name: "Adam", Description: ""}, // No description
			},
		},
	}

	result := BuildDMCrossSpaceContextBlock("Alice", channels)

	if !strings.Contains(result, "backend expert") {
		t.Errorf("expected output to contain 'backend expert', got: %s", result)
	}
	if !strings.Contains(result, "specialist agent") {
		t.Errorf("expected output to contain 'specialist agent' for member without description, got: %s", result)
	}
}

// TestBuildDMCrossSpaceContextBlock_ContainsDelegateInstruction verifies that
// the output contains delegate_to_agent-first delegation instructions.
func TestBuildDMCrossSpaceContextBlock_ContainsDelegateInstruction(t *testing.T) {
	channels := []ChannelRoster{
		{
			Name:      "Engineering",
			LeadAgent: "Tom",
			Members: []SpaceMember{
				{Name: "Sam", Description: "backend engineer"},
			},
		},
	}

	result := BuildDMCrossSpaceContextBlock("Alice", channels)

	if !strings.Contains(result, "delegate_to_agent") {
		t.Errorf("expected output to contain delegate_to_agent delegation instructions, got: %s", result)
	}
	if strings.Contains(result, "@mention") {
		t.Errorf("did not expect @mention fallback guidance in DM cross-space context, got: %s", result)
	}
}
