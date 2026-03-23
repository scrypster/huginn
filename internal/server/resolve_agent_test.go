package server

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/session"
)

// testAgentConfig builds a minimal AgentsConfig for use in tests.
func testAgentConfig(agentDefs ...agents.AgentDef) *agents.AgentsConfig {
	return &agents.AgentsConfig{Agents: agentDefs}
}

func testAgent(name, model, color, icon string, isDefault bool) agents.AgentDef {
	return agents.AgentDef{
		Name:      name,
		Model:     model,
		Color:     color,
		Icon:      icon,
		IsDefault: isDefault,
	}
}

// ── resolveAgent unit tests ───────────────────────────────────────────────────

func TestResolveAgent_LoaderError_ReturnsNil(t *testing.T) {
	s := &Server{
		agentLoader: func() (*agents.AgentsConfig, error) {
			return nil, errFakeLoaderFail
		},
	}
	ag := s.resolveAgent("any-session")
	if ag != nil {
		t.Errorf("expected nil on loader error, got %v", ag.Name)
	}
}

func TestResolveAgent_EmptyAgents_ReturnsNil(t *testing.T) {
	s := &Server{
		agentLoader: func() (*agents.AgentsConfig, error) {
			return &agents.AgentsConfig{Agents: nil}, nil
		},
	}
	ag := s.resolveAgent("any-session")
	if ag != nil {
		t.Errorf("expected nil for empty agents, got %v", ag.Name)
	}
}

func TestResolveAgent_NilStore_FallsBackToDefault(t *testing.T) {
	cfg := testAgentConfig(
		testAgent("Alice", "model-a", "#fff", "A", false),
		testAgent("Bob", "model-b", "#000", "B", true), // is_default
	)
	s := &Server{
		store: nil,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}
	ag := s.resolveAgent("any-session")
	if ag == nil {
		t.Fatal("expected non-nil agent from default fallback")
	}
	if ag.Name != "Bob" {
		t.Errorf("expected default agent Bob, got %q", ag.Name)
	}
}

func TestResolveAgent_EmptySessionID_FallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	cfg := testAgentConfig(
		testAgent("Chris", "model-c", "#58A6FF", "C", true),
		testAgent("Steve", "model-s", "#3FB950", "S", false),
	)
	s := &Server{
		store: store,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}
	ag := s.resolveAgent("") // empty sessionID
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}
	if ag.Name != "Chris" {
		t.Errorf("expected Chris (is_default), got %q", ag.Name)
	}
}

func TestResolveAgent_SessionHasPrimaryAgent_ReturnsThatAgent(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("test", "/workspace", "model")
	sess.SetPrimaryAgent("Steve")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("Chris", "qwen3:14b", "#58A6FF", "C", true),
		testAgent("Steve", "qwen2.5-coder:14b", "#3FB950", "S", false),
	)
	s := &Server{
		store: store,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}
	ag := s.resolveAgent(sess.ID)
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}
	if ag.Name != "Steve" {
		t.Errorf("expected Steve (primary agent), got %q", ag.Name)
	}
	if ag.GetModelID() != "qwen2.5-coder:14b" {
		t.Errorf("expected model qwen2.5-coder:14b, got %q", ag.GetModelID())
	}
}

func TestResolveAgent_ModelChange_PickedUpImmediately(t *testing.T) {
	// Verify that resolveAgent reads fresh from the loader on each call —
	// simulates a model update (user edits agent in UI, new model saved to disk).
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("test", "/workspace", "model")
	sess.SetPrimaryAgent("Mark")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	currentModel := "deepseek-r1:14b"
	s := &Server{
		store: store,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return testAgentConfig(
				testAgent("Mark", currentModel, "#D29922", "M", false),
			), nil
		},
	}

	// First call — original model
	ag1 := s.resolveAgent(sess.ID)
	if ag1 == nil {
		t.Fatal("expected non-nil agent (first call)")
	}
	if ag1.GetModelID() != "deepseek-r1:14b" {
		t.Errorf("first call: expected deepseek-r1:14b, got %q", ag1.GetModelID())
	}

	// Simulate user updating the model in the UI
	currentModel = "deepseek-r1:32b"

	// Second call — should pick up new model without restart
	ag2 := s.resolveAgent(sess.ID)
	if ag2 == nil {
		t.Fatal("expected non-nil agent (second call)")
	}
	if ag2.GetModelID() != "deepseek-r1:32b" {
		t.Errorf("second call: expected deepseek-r1:32b (updated model), got %q", ag2.GetModelID())
	}
}

func TestResolveAgent_PrimaryAgentNotInConfig_FallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("test", "/workspace", "model")
	sess.SetPrimaryAgent("DeletedAgent") // agent was removed from config
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("Chris", "model-c", "#58A6FF", "C", true),
	)
	s := &Server{
		store: store,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}
	ag := s.resolveAgent(sess.ID)
	if ag == nil {
		t.Fatal("expected fallback agent, got nil")
	}
	// Should fall back to default (Chris) since DeletedAgent is gone
	if ag.Name != "Chris" {
		t.Errorf("expected fallback to Chris, got %q", ag.Name)
	}
}

func TestResolveAgent_CaseInsensitiveMatch(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("test", "/workspace", "model")
	sess.SetPrimaryAgent("STEVE") // uppercase
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("Steve", "model-s", "#3FB950", "S", false),
	)
	s := &Server{
		store: store,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}
	ag := s.resolveAgent(sess.ID)
	if ag == nil {
		t.Fatal("expected case-insensitive match")
	}
	if !strings.EqualFold(ag.Name, "Steve") {
		t.Errorf("expected Steve (case-insensitive), got %q", ag.Name)
	}
}

func TestResolveAgent_NoDefaultFlagSet_ReturnsFirstAgent(t *testing.T) {
	cfg := testAgentConfig(
		testAgent("Alpha", "model-a", "#fff", "A", false),
		testAgent("Beta", "model-b", "#000", "B", false),
	)
	s := &Server{
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}
	ag := s.resolveAgent("no-session")
	if ag == nil {
		t.Fatal("expected first agent as last-resort fallback")
	}
	if ag.Name != "Alpha" {
		t.Errorf("expected Alpha (first agent), got %q", ag.Name)
	}
}

// errFakeLoaderFail is a sentinel error for test loader failures.
var errFakeLoaderFail = errFake("agent loader failed")

type errFake string

func (e errFake) Error() string { return string(e) }
