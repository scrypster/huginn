package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/tools"
)

// echoSystemPromptBackend answers with the system-prompt text it actually
// received, so a test can assert on what the agent was told about its
// company/roster rather than guessing at prose the model would produce.
type echoSystemPromptBackend struct{}

func (b *echoSystemPromptBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	var sys string
	for _, m := range req.Messages {
		if m.Role == "system" {
			sys = m.Content
			break
		}
	}
	return &backend.ChatResponse{Content: sys, DoneReason: "stop"}, nil
}
func (b *echoSystemPromptBackend) Health(_ context.Context) error   { return nil }
func (b *echoSystemPromptBackend) Shutdown(_ context.Context) error { return nil }
func (b *echoSystemPromptBackend) ContextWindow() int               { return 8192 }

// TestRunSpaceThreadAgent_ThreadTurnGetsOwningSpaceCompanyContext is the TDD
// repro for the context-bleed defect: a drawer thread turn for a company-"Lab"
// space must see Lab's company name and Lab's roster in its system prompt,
// never another company's (e.g. Huginn) — even when the same agent (Winston)
// is seated in both companies.
func TestRunSpaceThreadAgent_ThreadTurnGetsOwningSpaceCompanyContext(t *testing.T) {
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatal(err)
	}
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := session.NewSQLiteSessionStore(db)

	// Two companies, both seating Winston — mirrors the live repro (Winston
	// serves both Lab and Huginn).
	huginnCo, err := spaceStore.CreateCompany("Huginn", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	labCo, err := spaceStore.CreateCompany("Lab", "", []string{"Winston", "Sam"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	huginnCh, err := spaceStore.CreateChannelForCompany("HuginnChan", "Steve", []string{"Winston"}, "", "", huginnCo.ID)
	if err != nil {
		t.Fatal(err)
	}
	labCh, err := spaceStore.CreateChannelForCompany("Lab", "Sam", []string{"Winston"}, "", "", labCo.ID)
	if err != nil {
		t.Fatal(err)
	}

	srv := testServer(t)
	srv.store = sessStore
	srv.SetSpaceStore(spaceStore)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Winston"}, {Name: "Sam"}, {Name: "Steve"},
		}}, nil
	}

	b := &echoSystemPromptBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	orch.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))
	agReg := agents.NewRegistry()
	agReg.Register(&agents.Agent{Name: "Winston", ModelID: "test-model"})
	agReg.Register(&agents.Agent{Name: "Sam", ModelID: "test-model"})
	agReg.Register(&agents.Agent{Name: "Steve", ModelID: "test-model"})
	orch.SetAgentRegistry(agReg)
	srv.orch = orch

	// Seed Winston's Huginn hallway/thread activity FIRST — the scenario the
	// task calls out: does a later Lab thread turn inherit Huginn context via
	// a reused/shared session or memory keyed only by agent name?
	huginnRoot, err := spaceStore.PostSpaceMessage(huginnCh.ID, "root in huginn", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RunSpaceThreadAgent(context.Background(), huginnCh.ID, huginnRoot.ID, "Winston", "@Winston hello"); err != nil {
		t.Fatalf("huginn warm-up RunSpaceThreadAgent: %v", err)
	}

	// Now the real repro turn: a Lab channel thread asking Winston to name
	// the company.
	labRoot, err := spaceStore.PostSpaceMessage(labCh.ID, "root in lab", "")
	if err != nil {
		t.Fatal(err)
	}
	speech, err := srv.RunSpaceThreadAgent(context.Background(), labCh.ID, labRoot.ID, "Winston",
		"@Winston in this thread please: name the company we are in, one word")
	if err != nil {
		t.Fatalf("RunSpaceThreadAgent: %v", err)
	}

	if !strings.Contains(speech, "Lab") {
		t.Fatalf("expected Lab's company context in system prompt, got:\n%s", speech)
	}
	if !strings.Contains(speech, "Sam") {
		t.Fatalf("expected Lab's roster (Sam) in system prompt, got:\n%s", speech)
	}
	if strings.Contains(speech, "Huginn") {
		t.Fatalf("Lab thread turn must never see Huginn's company context, got:\n%s", speech)
	}
}

// TestRunSpaceThreadAgent_DriftedToolSessionCorrectedToOwningSpace covers the
// defensive case directly: the tool session spaceThreadToolSession resolves
// for the Lab thread already has a *different* company's SpaceID persisted
// (simulating stale data, or an agent like Winston reused across companies).
// InjectSpaceContext (ws.go) derives company/roster from that session's
// stored SpaceID, so without a correction it would hand the Lab thread turn
// Huginn's context. ensureSpaceIDMatches must force it back in sync before
// InjectSpaceContext reads it.
func TestRunSpaceThreadAgent_DriftedToolSessionCorrectedToOwningSpace(t *testing.T) {
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatal(err)
	}
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := session.NewSQLiteSessionStore(db)

	huginnCo, err := spaceStore.CreateCompany("Huginn", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	labCo, err := spaceStore.CreateCompany("Lab", "", []string{"Winston", "Sam"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	huginnCh, err := spaceStore.CreateChannelForCompany("HuginnChan", "Steve", []string{"Winston"}, "", "", huginnCo.ID)
	if err != nil {
		t.Fatal(err)
	}
	labCh, err := spaceStore.CreateChannelForCompany("Lab", "Sam", []string{"Winston"}, "", "", labCo.ID)
	if err != nil {
		t.Fatal(err)
	}

	srv := testServer(t)
	srv.store = sessStore
	srv.SetSpaceStore(spaceStore)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Winston"}, {Name: "Sam"}, {Name: "Steve"},
		}}, nil
	}

	b := &echoSystemPromptBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	orch.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))
	agReg := agents.NewRegistry()
	agReg.Register(&agents.Agent{Name: "Winston", ModelID: "test-model"})
	agReg.Register(&agents.Agent{Name: "Sam", ModelID: "test-model"})
	agReg.Register(&agents.Agent{Name: "Steve", ModelID: "test-model"})
	orch.SetAgentRegistry(agReg)
	srv.orch = orch

	// No message has been posted to Lab yet, so spaces.ensureSpaceSession has
	// not created Lab's canonical session row: ListSessionsForSpace(labCh.ID)
	// is empty and spaceThreadToolSession falls back to the deterministic id
	// "space-thread-{parentID}-Winston". Pre-persist *that exact id* already
	// bound to Huginn's space — a session left over from a prior interaction,
	// a retried wake, or any other reuse of a delegate session for an agent
	// (Winston) seated in multiple companies. session.LoadForDelegate never
	// overwrites an already-set SpaceID, so ensureDelegateSession alone would
	// leave this row pinned to Huginn.
	parentID := "parent-drift-1"
	toolSID := srv.spaceThreadToolSession(labCh.ID, parentID, "Winston")
	if !strings.HasPrefix(toolSID, "space-thread-"+parentID+"-") {
		t.Fatalf("expected deterministic fallback id, got %q", toolSID)
	}
	now := time.Now().UTC()
	if err := sessStore.SaveManifest(&session.Session{
		ID: toolSID,
		Manifest: session.Manifest{
			ID: toolSID, SessionID: toolSID, SpaceID: huginnCh.ID,
			Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
	}); err != nil {
		t.Fatalf("seed drifted tool session: %v", err)
	}
	if got, _ := sessStore.Load(toolSID); got.SpaceID() != huginnCh.ID {
		t.Fatalf("setup: drift not applied, got %q", got.SpaceID())
	}

	speech, err := srv.RunSpaceThreadAgent(context.Background(), labCh.ID, parentID, "Winston",
		"@Winston in this thread please: name the company we are in, one word")
	if err != nil {
		t.Fatalf("RunSpaceThreadAgent: %v", err)
	}

	if !strings.Contains(speech, "Lab") {
		t.Fatalf("expected Lab's company context despite drifted session, got:\n%s", speech)
	}
	if strings.Contains(speech, "Huginn") {
		t.Fatalf("drifted tool session must not leak Huginn's context into the Lab thread turn, got:\n%s", speech)
	}
}
