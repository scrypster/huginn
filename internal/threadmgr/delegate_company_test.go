package threadmgr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/spaces"
)

// stubCompanyGate implements CompanyGate for unit tests that do not need SQLite.
type stubCompanyGate struct {
	companyID string
	seated    []string
	lookupErr error
	inErr     error
}

func (s *stubCompanyGate) SpaceCompanyID(_ string) (string, error) {
	return s.companyID, s.lookupErr
}

func (s *stubCompanyGate) AgentInCompany(agent, _ string) (bool, error) {
	if s.inErr != nil {
		return false, s.inErr
	}
	for _, n := range s.seated {
		if strings.EqualFold(n, agent) {
			return true, nil
		}
	}
	return false, nil
}

func companyDelegateRegistry() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Winston", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Sam", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "coder", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "reviewer", ModelID: "claude-haiku-4"})
	return reg
}

func TestCreate_CompanyA_SpecialistOnlyInB_Rejected(t *testing.T) {
	tm := New()
	// Space roster would allow Sam (desk-style). Company A must still reject.
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston", "Sam"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston", "coder"}})

	_, err := tm.Create(CreateParams{
		SessionID: "sess-a",
		AgentID:   "Sam",
		Task:      "cross-company work",
		SpaceID:   "space-a",
	})
	if err == nil {
		t.Fatal("expected Sam (only in company B) to be rejected in company A space")
	}
	if !errors.Is(err, ErrAgentNotInCompany) {
		t.Errorf("expected ErrAgentNotInCompany, got: %v", err)
	}
	if strings.Contains(err.Error(), "DELEGATE_FAIL") {
		t.Errorf("company fail copy must be human, not DELEGATE_FAIL in speech: %v", err)
	}
	if !strings.Contains(err.Error(), "Sam isn't in this company.") {
		t.Errorf("expected teammate sentence, got: %v", err)
	}
}

func TestCreate_CompanyA_SpecialistInA_Allowed(t *testing.T) {
	tm := New()
	// coder is seated in A but not on this channel's space roster — company roster wins.
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston", "coder"}})

	th, err := tm.Create(CreateParams{
		SessionID: "sess-a",
		AgentID:   "coder",
		Task:      "in-company work",
		SpaceID:   "space-a",
	})
	if err != nil {
		t.Fatalf("specialist seated in A must be allowed, got: %v", err)
	}
	if th.AgentID != "coder" {
		t.Errorf("agent = %q, want coder", th.AgentID)
	}
}

func TestCreate_DeskEmptyCompanyID_RosterUnchanged(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston", "Sam"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "", seated: []string{"Winston"}})

	if _, err := tm.Create(CreateParams{
		SessionID: "desk",
		AgentID:   "Sam",
		Task:      "desk handoff",
		SpaceID:   "desk-space",
	}); err != nil {
		t.Fatalf("desk space must keep roster allow for Sam, got: %v", err)
	}

	_, err := tm.Create(CreateParams{
		SessionID: "desk",
		AgentID:   "reviewer",
		Task:      "outsider",
		SpaceID:   "desk-space",
	})
	if err == nil {
		t.Fatal("desk space must still deny reviewers not on the roster")
	}
	if !errors.Is(err, ErrAgentNotSpaceMember) {
		t.Errorf("expected ErrAgentNotSpaceMember on desk, got: %v", err)
	}
	if !strings.Contains(err.Error(), "DELEGATE_FAIL") {
		t.Errorf("desk not_in_roster should keep DELEGATE_FAIL hover token, got: %v", err)
	}
}

func TestCreate_CompanyGate_WinstonSeatedInCompanyAllowed(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"coder"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "spacex", seated: []string{"Winston", "coder"}})

	if _, err := tm.Create(CreateParams{
		SessionID: "sx",
		AgentID:   "Winston",
		Task:      "desk person in company",
		SpaceID:   "sx-eng",
	}); err != nil {
		t.Fatalf("Winston seated in the company must be targetable, got: %v", err)
	}
}

func TestCreate_CompanyGate_LookupErrorFailsClosed(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Sam"}})
	tm.SetCompanyGate(&stubCompanyGate{lookupErr: errors.New("db down")})

	_, err := tm.Create(CreateParams{
		SessionID: "sess",
		AgentID:   "Sam",
		Task:      "task",
		SpaceID:   "space",
	})
	if err == nil {
		t.Fatal("company lookup error must fail closed")
	}
	if errors.Is(err, ErrAgentNotInCompany) || errors.Is(err, ErrAgentNotSpaceMember) {
		t.Errorf("expected transient lookup error, got: %v", err)
	}
}

func TestDelegateToAgentTool_CompanyA_SpecialistOnlyInB_Rejected(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston", "Sam", "reviewer"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston", "coder"}})
	reg := companyDelegateRegistry()
	tool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("co-a-sess", "space-a", tm, reg)}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "reviewer",
		"task":  "review the PR",
	})
	if !result.IsError {
		t.Fatal("expected specialist only in B to fail")
	}
	if strings.Contains(result.Error, "DELEGATE_FAIL") {
		t.Errorf("tool error must be human, not DELEGATE_FAIL: %s", result.Error)
	}
	if !strings.Contains(result.Error, "reviewer isn't in this company.") {
		t.Errorf("expected teammate sentence, got: %s", result.Error)
	}
	if n := len(tm.ListBySession("co-a-sess")); n != 0 {
		t.Fatalf("must not spawn, got %d threads", n)
	}
}

func TestDelegateToAgentTool_CompanyA_SpecialistInA_Allowed(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston", "coder"}})
	reg := companyDelegateRegistry()
	tool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("co-a-sess", "space-a", tm, reg)}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "coder",
		"task":  "implement it",
	})
	if result.IsError {
		t.Fatalf("specialist in A must still delegate, got: %s", result.Error)
	}
	threads := tm.ListBySession("co-a-sess")
	if len(threads) != 1 || threads[0].AgentID != "coder" {
		t.Fatalf("expected coder thread, got %+v", threads)
	}
}

func TestDelegateToAgentTool_DeskEmptyCompanyID_RosterUnchanged(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston", "Sam"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "", seated: nil})
	reg := companyDelegateRegistry()
	tool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("desk", "desk-space", tm, reg)}

	ok := tool.Execute(context.Background(), map[string]any{"agent": "Sam", "task": "help"})
	if ok.IsError {
		t.Fatalf("desk roster member Sam must still delegate, got: %s", ok.Error)
	}

	denied := tool.Execute(context.Background(), map[string]any{"agent": "reviewer", "task": "nope"})
	if !denied.IsError {
		t.Fatal("desk non-member reviewer must fail")
	}
	if !strings.Contains(denied.Error, "DELEGATE_FAIL") {
		t.Errorf("desk not_in_roster keeps DELEGATE_FAIL, got: %s", denied.Error)
	}
}

func TestDelegateToAgentTool_LiveStore_CompanyIsolation(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)

	coA, err := store.CreateCompany("Company A", "", []string{"Winston", "coder"}, "", "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := store.CreateCompany("Company B", "", []string{"Winston", "reviewer"}, "", ""); err != nil {
		t.Fatalf("create B: %v", err)
	}

	companySpace, err := store.CreateChannel("acme-eng", "Winston", []string{"Sam"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel company: %v", err)
	}
	if _, err := store.UpdateSpace(companySpace.ID, spaces.SpaceUpdates{CompanyID: &coA.ID}); err != nil {
		t.Fatalf("assign company_id: %v", err)
	}

	desk, err := store.CreateChannel("desk-channel", "Winston", []string{"Sam"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel desk: %v", err)
	}
	if desk.CompanyID != "" {
		t.Fatalf("desk channel must have empty company_id, got %q", desk.CompanyID)
	}

	tm := New()
	tm.SetMembershipChecker(store)
	tm.SetCompanyGate(store)
	reg := companyDelegateRegistry()

	// Company A space: reviewer is only in B (and not needed on space roster).
	aTool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("co-a", companySpace.ID, tm, reg)}
	denied := aTool.Execute(context.Background(), map[string]any{"agent": "reviewer", "task": "cross"})
	if !denied.IsError || !strings.Contains(denied.Error, "reviewer isn't in this company.") {
		t.Fatalf("company A must reject reviewer-only-in-B, got isError=%v err=%q", denied.IsError, denied.Error)
	}
	if strings.Contains(denied.Error, "DELEGATE_FAIL") {
		t.Errorf("live company fail must be human: %s", denied.Error)
	}
	if n := len(tm.ListBySession("co-a")); n != 0 {
		t.Fatalf("company A spawned %d threads for outsider", n)
	}

	// Specialist seated in A is allowed even if not on the channel roster (Sam is; coder is not).
	allowed := aTool.Execute(context.Background(), map[string]any{"agent": "coder", "task": "ship"})
	if allowed.IsError {
		t.Fatalf("coder in A must be allowed, got: %s", allowed.Error)
	}

	// Desk: empty company_id keeps space-roster behavior (Sam yes, reviewer no).
	deskTool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("desk", desk.ID, tm, reg)}
	deskOK := deskTool.Execute(context.Background(), map[string]any{"agent": "Sam", "task": "desk help"})
	if deskOK.IsError {
		t.Fatalf("desk roster Sam must still delegate, got: %s", deskOK.Error)
	}
	deskNo := deskTool.Execute(context.Background(), map[string]any{"agent": "reviewer", "task": "desk no"})
	if !deskNo.IsError || !strings.Contains(deskNo.Error, "DELEGATE_FAIL") {
		t.Fatalf("desk must keep not_in_roster for reviewer, got isError=%v err=%q", deskNo.IsError, deskNo.Error)
	}
}

func TestDelegateToAgentTool_SameNameSpacesStayIsolated(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	coA, err := store.CreateCompany("SpaceX", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	coB, err := store.CreateCompany("Tesla", "", []string{"Winston", "Reggie"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	engA, err := store.CreateChannelForCompany("eng", "Winston", []string{"Steve"}, "", "", coA.ID)
	if err != nil {
		t.Fatal(err)
	}
	engB, err := store.CreateChannelForCompany("eng", "Winston", []string{"Reggie"}, "", "", coB.ID)
	if err != nil {
		t.Fatal(err)
	}
	tm := New()
	tm.SetMembershipChecker(store)
	tm.SetCompanyGate(store)
	reg := companyDelegateRegistry()
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Reggie", ModelID: "claude-haiku-4"})

	aTool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("sx", engA.ID, tm, reg)}
	denied := aTool.Execute(context.Background(), map[string]any{"agent": "Reggie", "task": "cross"})
	if !denied.IsError || !strings.Contains(denied.Error, "isn't in this company") {
		t.Fatalf("SpaceX #eng must reject Tesla-only Reggie, got %q", denied.Error)
	}
	ok := aTool.Execute(context.Background(), map[string]any{"agent": "Steve", "task": "ship"})
	if ok.IsError {
		t.Fatalf("Steve in SpaceX must delegate, got %s", ok.Error)
	}

	bTool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("tsla", engB.ID, tm, reg)}
	deniedB := bTool.Execute(context.Background(), map[string]any{"agent": "Steve", "task": "cross"})
	if !deniedB.IsError || !strings.Contains(deniedB.Error, "isn't in this company") {
		t.Fatalf("Tesla #eng must reject SpaceX-only Steve, got %q", deniedB.Error)
	}
}
