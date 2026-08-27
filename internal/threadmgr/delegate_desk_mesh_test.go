package threadmgr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/spaces"
)

func deskMeshRegistry() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Winston", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Reggie", ModelID: "claude-haiku-4"})
	return reg
}

func TestCreate_DeskDM_SteveToWinston_Succeeds(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	steveDM, err := store.OpenDM("Steve")
	if err != nil {
		t.Fatalf("OpenDM Steve: %v", err)
	}
	if _, err := store.OpenDM("Winston"); err != nil {
		t.Fatalf("OpenDM Winston: %v", err)
	}

	tm := New()
	tm.SetMembershipChecker(store)
	tm.SetCompanyGate(store)

	th, err := tm.Create(CreateParams{
		SessionID: "steve-dm",
		AgentID:   "Winston",
		Task:      "what time is it",
		SpaceID:   steveDM.ID,
	})
	if err != nil {
		t.Fatalf("desk DM Steve → Winston must succeed, got: %v", err)
	}
	if th.AgentID != "Winston" {
		t.Errorf("agent = %q, want Winston", th.AgentID)
	}
}

func TestCreate_DeskDM_LabOnlyNoDeskDM_Denied(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	steveDM, err := store.OpenDM("Steve")
	if err != nil {
		t.Fatalf("OpenDM Steve: %v", err)
	}
	if _, err := store.OpenDM("Winston"); err != nil {
		t.Fatalf("OpenDM Winston: %v", err)
	}
	lab, err := store.CreateCompany("Lab", "", []string{"Reggie"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany Lab: %v", err)
	}
	// Reggie is seated only in Lab and has no desk DM.
	_ = lab

	tm := New()
	tm.SetMembershipChecker(store)
	tm.SetCompanyGate(store)

	_, err = tm.Create(CreateParams{
		SessionID: "steve-dm",
		AgentID:   "Reggie",
		Task:      "lab work",
		SpaceID:   steveDM.ID,
	})
	if err == nil {
		t.Fatal("Lab-only agent with no desk DM must be denied from Steve's desk DM")
	}
	if !errors.Is(err, ErrAgentNotSpaceMember) {
		t.Errorf("expected ErrAgentNotSpaceMember, got: %v", err)
	}
	if !strings.Contains(err.Error(), "DELEGATE_FAIL") {
		t.Errorf("expected DELEGATE_FAIL, got: %v", err)
	}
}

func TestCreate_CompanyHuginn_LabSpecialistDenied(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	huginn, err := store.CreateCompany("Huginn", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany Huginn: %v", err)
	}
	if _, err := store.CreateCompany("Lab", "", []string{"Reggie"}, "", ""); err != nil {
		t.Fatalf("CreateCompany Lab: %v", err)
	}
	huginnSpace, err := store.CreateChannelForCompany("eng", "Winston", []string{"Steve"}, "", "", huginn.ID)
	if err != nil {
		t.Fatalf("Huginn channel: %v", err)
	}

	tm := New()
	tm.SetMembershipChecker(store)
	tm.SetCompanyGate(store)

	_, err = tm.Create(CreateParams{
		SessionID: "huginn-eng",
		AgentID:   "Reggie",
		Task:      "cross company",
		SpaceID:   huginnSpace.ID,
	})
	if err == nil {
		t.Fatal("Huginn space must deny Lab-only Reggie")
	}
	if !errors.Is(err, ErrAgentNotInCompany) {
		t.Errorf("expected ErrAgentNotInCompany, got: %v", err)
	}
}

func TestDelegateToAgentTool_DeskDM_SteveToWinston(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	steveDM, err := store.OpenDM("Steve")
	if err != nil {
		t.Fatalf("OpenDM Steve: %v", err)
	}
	if _, err := store.OpenDM("Winston"); err != nil {
		t.Fatalf("OpenDM Winston: %v", err)
	}

	tm := New()
	tm.SetMembershipChecker(store)
	tm.SetCompanyGate(store)
	reg := deskMeshRegistry()
	tool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("steve-dm", steveDM.ID, tm, reg)}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "Winston",
		"task":  "what time is it",
	})
	if result.IsError {
		t.Fatalf("Steve desk DM → Winston must succeed, got: %s", result.Error)
	}
}

func TestCreate_DeskChannel_NonMemberStillDenied(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	if _, err := store.OpenDM("Winston"); err != nil {
		t.Fatalf("OpenDM Winston: %v", err)
	}
	ch, err := store.CreateChannel("desk-hall", "Steve", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	tm := New()
	tm.SetMembershipChecker(store)
	tm.SetCompanyGate(store)

	_, err = tm.Create(CreateParams{
		SessionID: "desk-ch",
		AgentID:   "Winston",
		Task:      "should fail",
		SpaceID:   ch.ID,
	})
	if err == nil {
		t.Fatal("desk channel must still deny non-member Winston")
	}
	if !errors.Is(err, ErrAgentNotSpaceMember) {
		t.Errorf("expected ErrAgentNotSpaceMember, got: %v", err)
	}
}
