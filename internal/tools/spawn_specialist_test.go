package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSpawnSpecialist_RequiresAllFields(t *testing.T) {
	tool := &SpawnSpecialistTool{Deps: SpawnSpecialistDeps{}}

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing name", map[string]any{"task": "t", "model": "m", "rationale": "r"}, "name"},
		{"missing task", map[string]any{"name": "X Specialist", "model": "m", "rationale": "r"}, "do"},
		{"missing model", map[string]any{"name": "X Specialist", "task": "t", "rationale": "r"}, "model"},
		{"missing rationale", map[string]any{"name": "X Specialist", "task": "t", "model": "m"}, "why"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := tool.Execute(context.Background(), c.args)
			if !res.IsError {
				t.Fatalf("expected error for %s, got %+v", c.name, res)
			}
			if !strings.Contains(res.Error, c.want) {
				t.Errorf("expected error mentioning %q, got %q", c.want, res.Error)
			}
		})
	}
}

func TestSpawnSpecialist_EmptyRationaleIsToolError(t *testing.T) {
	spawned := false
	tool := &SpawnSpecialistTool{Deps: SpawnSpecialistDeps{
		Spawn: func(ctx context.Context, req SpawnSpecialistRequest) (string, error) {
			spawned = true
			return "thread-1", nil
		},
	}}
	res := tool.Execute(context.Background(), map[string]any{
		"name": "Rust Audit Specialist", "task": "audit the crate", "model": "claude-opus-4-6", "rationale": "",
	})
	if !res.IsError {
		t.Fatal("expected error for empty rationale")
	}
	if spawned {
		t.Fatal("Spawn must not be called when rationale is empty")
	}
}

// Colons are now SANITIZED (auto-cleaned) rather than rejected, so a small
// model proposing "rust:audit" spawns as "rust audit" instead of looping.
func TestSpawnSpecialist_NameColonsSanitizedNotRejected(t *testing.T) {
	var seen string
	tool := &SpawnSpecialistTool{Deps: SpawnSpecialistDeps{
		ValidateName: func(name string) error {
			seen = name
			if strings.Contains(name, ":") {
				return errors.New("invalid character")
			}
			return nil
		},
		NameTaken:    func(string) bool { return false },
		ResolveModel: func(string) (ModelChoice, bool) { return ModelChoice{}, true },
		Spawn:        func(context.Context, SpawnSpecialistRequest) (string, error) { return "t1", nil },
	}}
	res := tool.Execute(context.Background(), map[string]any{
		"name": "rust:audit", "task": "t", "model": "m", "rationale": "r",
	})
	if res.IsError {
		t.Fatalf("colon name should sanitize and proceed, got error: %q", res.Error)
	}
	if strings.Contains(seen, ":") {
		t.Errorf("ValidateName saw an unsanitized name: %q", seen)
	}
}

func TestSpawnSpecialist_NameTakenRejected(t *testing.T) {
	tool := &SpawnSpecialistTool{Deps: SpawnSpecialistDeps{
		NameTaken: func(name string) bool { return true },
	}}
	res := tool.Execute(context.Background(), map[string]any{
		"name": "Winston", "task": "t", "model": "m", "rationale": "r",
	})
	if !res.IsError {
		t.Fatal("expected error for name collision")
	}
	if !strings.Contains(res.Error, "already") {
		t.Errorf("expected 'already' in collision error, got %q", res.Error)
	}
}

func TestSpawnSpecialist_UnknownModelRejected(t *testing.T) {
	tool := &SpawnSpecialistTool{Deps: SpawnSpecialistDeps{
		ResolveModel: func(model string) (ModelChoice, bool) { return ModelChoice{}, false },
	}}
	res := tool.Execute(context.Background(), map[string]any{
		"name": "Rust Audit Specialist", "task": "t", "model": "unknown-model-xyz", "rationale": "r",
	})
	if !res.IsError {
		t.Fatal("expected error for unrecognized model")
	}
	if !strings.Contains(res.Error, "available models") {
		t.Errorf("expected pointer to available models list, got %q", res.Error)
	}
}

func TestSpawnSpecialist_SpeechContract(t *testing.T) {
	var gotReq SpawnSpecialistRequest
	tool := &SpawnSpecialistTool{Deps: SpawnSpecialistDeps{
		ResolveModel: func(model string) (ModelChoice, bool) {
			return ModelChoice{DisplayName: "Claude Opus 4.6", InputCostPerMTok: 5, OutputCostPerMTok: 25}, true
		},
		Spawn: func(ctx context.Context, req SpawnSpecialistRequest) (string, error) {
			gotReq = req
			return "thread-1", nil
		},
	}}
	res := tool.Execute(context.Background(), map[string]any{
		"name":      "Rust Audit Specialist",
		"task":      "audit the crypto crate for memory safety",
		"model":     "claude-opus-4-6",
		"rationale": "Winston is the nearest fit but has no Rust experience",
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	speech := res.Output
	if !strings.Contains(speech, "Nobody on the roster covers Rust Audit") {
		t.Errorf("expected roster-miss opener, got %q", speech)
	}
	if !strings.Contains(speech, "Winston is the nearest fit but has no Rust experience") {
		t.Errorf("expected rationale embedded verbatim, got %q", speech)
	}
	if !strings.Contains(speech, "Claude Opus 4.6") {
		t.Errorf("expected model display name in speech, got %q", speech)
	}
	if !strings.Contains(speech, "one-off") || !strings.Contains(speech, "won't be added to the team") {
		t.Errorf("expected 'one-off' / 'not a hire' framing, got %q", speech)
	}
	if gotReq.Name != "Rust Audit Specialist" || gotReq.Task == "" || gotReq.Model != "claude-opus-4-6" {
		t.Errorf("unexpected request passed to Spawn: %+v", gotReq)
	}
}

func TestSpawnSpecialist_SpawnErrorSurfaced(t *testing.T) {
	tool := &SpawnSpecialistTool{Deps: SpawnSpecialistDeps{
		ResolveModel: func(model string) (ModelChoice, bool) { return ModelChoice{}, true },
		Spawn: func(ctx context.Context, req SpawnSpecialistRequest) (string, error) {
			return "", errors.New("preview denied")
		},
	}}
	res := tool.Execute(context.Background(), map[string]any{
		"name": "Rust Audit Specialist", "task": "t", "model": "m", "rationale": "r",
	})
	if !res.IsError {
		t.Fatal("expected error when Spawn fails")
	}
	if !strings.Contains(res.Error, "not") && !strings.Contains(res.Error, "n't") {
		t.Errorf("expected a human denial message, got %q", res.Error)
	}
}

func TestSpecialistFinishSpeech(t *testing.T) {
	got := SpecialistFinishSpeech("Rust Audit Specialist")
	want := "Rust Audit Specialist is done and gone."
	if got != want {
		t.Errorf("SpecialistFinishSpeech = %q, want %q", got, want)
	}
}

func TestStripHireGrant_StripsBothHiringTools(t *testing.T) {
	got := stripHireGrant([]string{"bash", CreateAgentName, "read_file", SpawnSpecialistName})
	for _, g := range got {
		if g == CreateAgentName || g == SpawnSpecialistName {
			t.Errorf("expected both hiring tools stripped, got %v", got)
		}
	}
	if len(got) != 2 {
		t.Errorf("expected 2 tools remaining, got %v", got)
	}
}

func TestSanitizeSpecialistName(t *testing.T) {
	cases := map[string]string{
		"Specialist: COBOL":   "Specialist COBOL",
		"COBOL/Security":      "COBOL Security",
		":::leading":          "leading",
		"  Rust  Audit  ":     "Rust Audit",
		"@#$":                 "Domain Specialist",
	}
	for in, want := range cases {
		if got := sanitizeSpecialistName(in); got != want {
			t.Errorf("sanitizeSpecialistName(%q) = %q, want %q", in, got, want)
		}
	}
}
