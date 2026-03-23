package modelconfig

import "testing"

func TestParseModelCommand(t *testing.T) {
	name, err := ParseModelCommand("use llama3:8b for reasoning")
	if err != nil {
		t.Fatal(err)
	}
	if name != "llama3:8b" {
		t.Errorf("name: want llama3:8b, got %s", name)
	}
}

func TestRegistryDefaults(t *testing.T) {
	reg := NewRegistry(DefaultModels())
	if reg == nil {
		t.Error("expected non-nil registry")
	}
}

// TestParseModelCommand_AllRoles verifies that all supported role aliases parse correctly.
func TestParseModelCommand_AllRoles(t *testing.T) {
	cases := []string{
		"use model-x for reasoning",
		"use model-x for reasoner",
	}
	for _, input := range cases {
		name, err := ParseModelCommand(input)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", input, err)
			continue
		}
		if name != "model-x" {
			t.Errorf("%q: expected name=model-x, got %q", input, name)
		}
	}
}

// TestParseModelCommand_CaseInsensitive verifies parsing is case-insensitive for roles.
func TestParseModelCommand_CaseInsensitive(t *testing.T) {
	_, err := ParseModelCommand("use mymodel for REASONING")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParseModelCommand_InvalidInput verifies that unparseable input returns an error.
func TestParseModelCommand_InvalidInput(t *testing.T) {
	cases := []string{
		"set model to llama3",
		"",
		"use for coding", // missing model name
		"just some random text",
	}
	for _, input := range cases {
		_, err := ParseModelCommand(input)
		if err == nil {
			t.Errorf("expected error for input %q, got nil", input)
		}
	}
}

// TestModels_GetAndSet verifies that the Reasoner field is set correctly.
func TestModels_GetAndSet(t *testing.T) {
	m := &Models{
		Reasoner: "reasoner-model",
	}
	if m.Reasoner != "reasoner-model" {
		t.Errorf("expected reasoner-model, got %q", m.Reasoner)
	}

	m.Reasoner = "new-reasoner"
	if m.Reasoner != "new-reasoner" {
		t.Errorf("expected new-reasoner after assignment, got %q", m.Reasoner)
	}
}

// TestDefaultModels verifies DefaultModels returns a non-empty Reasoner.
func TestDefaultModels(t *testing.T) {
	m := DefaultModels()
	if m.Reasoner == "" {
		t.Error("expected non-empty default model name for Reasoner")
	}
}

// TestRegistry_SlotContextWindow verifies ModelContextWindow lookup works.
func TestRegistry_SlotContextWindow(t *testing.T) {
	models := DefaultModels()
	reg := NewRegistry(models)
	reg.Available = []ModelInfo{
		{Name: models.Reasoner, ContextWindow: 32768, SupportsTools: true},
	}

	cw := reg.ModelContextWindow(models.Reasoner)
	if cw != 32768 {
		t.Errorf("expected context window 32768, got %d", cw)
	}
}

// TestRegistry_SlotSupportsTools_ExplicitFalse verifies that a model with
// SupportsTools=false returns false.
func TestRegistry_SlotSupportsTools_ExplicitFalse(t *testing.T) {
	models := DefaultModels()
	reg := NewRegistry(models)
	reg.Available = []ModelInfo{
		{Name: models.Reasoner, ContextWindow: 4096, SupportsTools: false},
	}

	if reg.ModelSupportsTools(models.Reasoner) {
		t.Error("expected SupportsTools=false for reasoner model")
	}
}

// TestRegistry_SlotSupportsTools_UnknownModel verifies optimistic default.
func TestRegistry_SlotSupportsTools_UnknownSlot(t *testing.T) {
	reg := &ModelRegistry{
		Available: nil,
	}
	// Unknown model should return true (optimistic).
	if !reg.ModelSupportsTools("unknown-model") {
		t.Error("expected optimistic true for unregistered model")
	}
}
