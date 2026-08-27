package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

type mockCompanyVisibleChecker struct {
	members   []string
	deskPeers []string
	deskDM    bool
	companyID string
	roster    []string
	err       error
}

func (m *mockCompanyVisibleChecker) SpaceMembers(_ string) ([]string, error) {
	return m.members, m.err
}

func (m *mockCompanyVisibleChecker) DeskPeerNames() ([]string, error) {
	return m.deskPeers, m.err
}

func (m *mockCompanyVisibleChecker) SpaceIsDeskDM(_ string) (bool, error) {
	return m.deskDM, nil
}

func (m *mockCompanyVisibleChecker) SpaceCompanyID(_ string) (string, error) {
	return m.companyID, m.err
}

func (m *mockCompanyVisibleChecker) CompanyRoster(_ string) ([]string, error) {
	return m.roster, m.err
}

func labConsultRegistry() *AgentRegistry {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "Steve", SystemPrompt: "I am Steve."})
	reg.Register(&Agent{Name: "Winston", SystemPrompt: "I am Winston."})
	reg.Register(&Agent{Name: "Sam", SystemPrompt: "I am Sam."})
	return reg
}

func TestConsultDescription_LabCompany_OmitsSteve(t *testing.T) {
	reg := labConsultRegistry()
	tool := newTestConsultTool(reg, &mockBackendConsult{})
	tool.WithSpaceContext("lab", &mockCompanyVisibleChecker{
		members:   []string{"Winston"},
		companyID: "co-lab",
		roster:    []string{"Sam", "Winston"},
	})

	desc := tool.Description()
	if !strings.Contains(strings.ToLower(desc), "sam") {
		t.Fatalf("Lab consult list must include Sam: %s", desc)
	}
	if !strings.Contains(strings.ToLower(desc), "winston") {
		t.Fatalf("Lab consult list must include Winston: %s", desc)
	}
	if strings.Contains(strings.ToLower(desc), "steve") {
		t.Fatalf("Lab consult list must not include Steve: %s", desc)
	}
}

func TestConsultDescription_LabSpaceMembers_OmitsSteve(t *testing.T) {
	reg := labConsultRegistry()
	tool := newTestConsultTool(reg, &mockBackendConsult{})
	// No company roster interface — space members only.
	tool.WithSpaceContext("lab", &mockSpaceChecker{members: []string{"Sam", "Winston"}})

	desc := tool.Description()
	if strings.Contains(strings.ToLower(desc), "steve") {
		t.Fatalf("Lab space-member consult list must not include Steve: %s", desc)
	}
	if !strings.Contains(strings.ToLower(desc), "sam") || !strings.Contains(strings.ToLower(desc), "winston") {
		t.Fatalf("Lab space-member consult list want Sam+Winston: %s", desc)
	}
}

func TestConsultDescription_DeskDM_SeesDeskPeers(t *testing.T) {
	reg := labConsultRegistry()
	tool := newTestConsultTool(reg, &mockBackendConsult{})
	tool.WithSpaceContext("steve-dm", &mockDeskFloorChecker{
		members:   []string{"Steve"},
		deskPeers: []string{"Steve", "Winston"},
		deskDM:    true,
	})

	desc := tool.Description()
	if !strings.Contains(strings.ToLower(desc), "steve") {
		t.Fatalf("desk DM consult list must include Steve: %s", desc)
	}
	if !strings.Contains(strings.ToLower(desc), "winston") {
		t.Fatalf("desk DM consult list must include Winston: %s", desc)
	}
	if strings.Contains(strings.ToLower(desc), "sam") {
		t.Fatalf("desk DM consult list must not invent Lab-only Sam: %s", desc)
	}
}

func TestConsultDescription_NoSpaceContext_KeepsCatalog(t *testing.T) {
	reg := labConsultRegistry()
	tool := newTestConsultTool(reg, &mockBackendConsult{})
	desc := tool.Description()
	for _, name := range []string{"steve", "winston", "sam"} {
		if !strings.Contains(strings.ToLower(desc), name) {
			t.Fatalf("no-space consult catalog missing %s: %s", name, desc)
		}
	}
}

func TestConsultVisibleNames_UnknownAgentError_LabOmitsSteve(t *testing.T) {
	reg := labConsultRegistry()
	tool := newTestConsultTool(reg, &mockBackendConsult{})
	tool.WithSpaceContext("lab", &mockCompanyVisibleChecker{
		members:   []string{"Winston"},
		companyID: "co-lab",
		roster:    []string{"Sam", "Winston"},
	})
	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "nobody",
		"question":   "hi",
	})
	// unknown agent happens after space guard; nobody is not a member so space guard fires first.
	// Use a registered-but-unseated name so we hit the available-list path? Steve is registered
	// but not in company — space/company guard denies membership first.
	// Assert Description list; also assert visibleConsultNames directly.
	names := tool.visibleConsultNames()
	joined := strings.ToLower(strings.Join(names, ","))
	if strings.Contains(joined, "steve") {
		t.Fatalf("Lab visible names include Steve: %v", names)
	}
	if !strings.Contains(joined, "sam") || !strings.Contains(joined, "winston") {
		t.Fatalf("Lab visible names want Sam+Winston: %v", names)
	}
	if !result.IsError {
		t.Fatal("expected error for unknown/unseated nobody")
	}
}

func TestConsultExecute_LabCompany_SteveDenied_CompanyWall(t *testing.T) {
	reg := labConsultRegistry()
	backendCalled := false
	tool := newTestConsultTool(reg, &mockBackendConsult{
		response: "should never run",
	})
	// Wrap to detect a consult completion if the mock is invoked.
	tool.backend = &countingConsultBackend{called: &backendCalled}
	tool.WithSpaceContext("lab", &mockCompanyVisibleChecker{
		members:   []string{"Winston"},
		companyID: "co-lab",
		roster:    []string{"Sam", "Winston"},
	})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Steve",
		"question":   "what is the hostname?",
	})
	if !result.IsError {
		t.Fatal("expected company-wall deny for Steve in Lab")
	}
	if !strings.Contains(result.Error, "Steve isn't in this company.") {
		t.Fatalf("want company wall, got %q", result.Error)
	}
	if strings.Contains(result.Error, "not a member") {
		t.Fatalf("company miss must not use space-member copy: %q", result.Error)
	}
	if backendCalled {
		t.Fatal("company-wall deny must not consult")
	}
}

func TestConsultExecute_LabCompany_SamAllowedDespiteSpaceRoster(t *testing.T) {
	reg := labConsultRegistry()
	tool := newTestConsultTool(reg, &mockBackendConsult{response: "sam-ok"})
	tool.WithSpaceContext("lab", &mockCompanyVisibleChecker{
		members:   []string{"Winston"},
		companyID: "co-lab",
		roster:    []string{"Sam", "Winston"},
	})
	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Sam",
		"question":   "hostname?",
	})
	if result.IsError {
		t.Fatalf("Lab Sam must consult even when hallway roster is Winston-only: %s", result.Error)
	}
}

type countingConsultBackend struct {
	called *bool
	mockBackendConsult
}

func (b *countingConsultBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	*b.called = true
	return b.mockBackendConsult.ChatCompletion(ctx, req)
}

func TestConsultExecute_LabWinstonCannotReachSteveViaSam(t *testing.T) {
	reg := labConsultRegistry()
	backendCalled := false
	tool := newTestConsultTool(reg, &countingConsultBackend{called: &backendCalled})
	tool.WithSpaceContext("lab", &mockCompanyVisibleChecker{
		members:   []string{"Winston"},
		companyID: "co-lab",
		roster:    []string{"Sam", "Winston"},
	})

	ok := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Sam",
		"question":   "ask Steve the hostname",
	})
	if ok.IsError {
		t.Fatalf("Lab Winston → Sam must consult: %s", ok.Error)
	}

	backendCalled = false
	via := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Steve",
		"question":   "hostname?",
	})
	if !via.IsError {
		t.Fatal("Lab must not reach Huginn Steve via Sam hop")
	}
	if !strings.Contains(via.Error, "Steve isn't in this company.") {
		t.Fatalf("want company wall on Steve via Sam, got %q", via.Error)
	}
	if backendCalled {
		t.Fatal("company-wall hop must not consult Steve")
	}
}
