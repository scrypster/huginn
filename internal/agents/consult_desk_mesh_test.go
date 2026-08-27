package agents

import (
	"context"
	"strings"
	"testing"
)

type mockDeskFloorChecker struct {
	members   []string
	deskPeers []string
	deskDM    bool
	err       error
}

func (m *mockDeskFloorChecker) SpaceMembers(_ string) ([]string, error) {
	return m.members, m.err
}

func (m *mockDeskFloorChecker) DeskPeerNames() ([]string, error) {
	return m.deskPeers, m.err
}

func (m *mockDeskFloorChecker) SpaceIsDeskDM(_ string) (bool, error) {
	return m.deskDM, nil
}

func TestConsultSpaceGuard_DeskDM_PeerAllowed(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "Winston", SystemPrompt: "I am Winston."})

	tool := newTestConsultTool(reg, &mockBackendConsult{response: "it is 3pm"})
	tool.WithSpaceContext("steve-dm", &mockDeskFloorChecker{
		members:   []string{"Steve"},
		deskPeers: []string{"Steve", "Winston"},
		deskDM:    true,
	})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Winston",
		"question":   "what time is it?",
	})
	if result.IsError {
		t.Fatalf("desk DM Steve must consult desk-peer Winston, got: %s", result.Error)
	}
}

func TestConsultSpaceGuard_DeskDM_NoDeskDMDenied(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "Reggie", SystemPrompt: "I am Reggie."})

	tool := newTestConsultTool(reg, &mockBackendConsult{response: "secret"})
	tool.WithSpaceContext("steve-dm", &mockDeskFloorChecker{
		members:   []string{"Steve"},
		deskPeers: []string{"Steve", "Winston"},
		deskDM:    true,
	})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Reggie",
		"question":   "lab secrets?",
	})
	if !result.IsError {
		t.Fatal("Lab-only agent with no desk DM must be denied")
	}
	if !strings.Contains(result.Error, "not a member") {
		t.Errorf("expected not-a-member, got: %s", result.Error)
	}
}

func TestConsultSpaceGuard_DeskChannel_NonMemberDenied(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "Winston", SystemPrompt: "I am Winston."})

	tool := newTestConsultTool(reg, &mockBackendConsult{response: "nope"})
	tool.WithSpaceContext("desk-channel", &mockDeskFloorChecker{
		members:   []string{"Steve"},
		deskPeers: []string{"Steve", "Winston"},
		deskDM:    false, // channel, not a desk DM
	})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Winston",
		"question":   "time?",
	})
	if !result.IsError {
		t.Fatal("desk channel must still deny non-member Winston")
	}
}
