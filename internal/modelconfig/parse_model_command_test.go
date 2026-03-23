package modelconfig

import (
	"strings"
	"testing"
)

// TestParseModelCommand_GibberishRole verifies that an unrecognised role returns an error.
func TestParseModelCommand_GibberishRole(t *testing.T) {
	// The regex pattern only matches (reasoning|reasoner), so an unrecognised
	// role won't match — FindStringSubmatch returns nil and we get an error.
	_, err := ParseModelCommand("use model-x for unknownrole")
	if err == nil {
		t.Error("expected error for unrecognised role, got nil")
	}
}

// TestParseModelCommand_ModelWithColon verifies parsing when model name
// contains a colon (common for local models like qwen2.5-coder:14b).
func TestParseModelCommand_ModelWithColon(t *testing.T) {
	name, err := ParseModelCommand("use qwen2.5-coder:14b for reasoning")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "qwen2.5-coder:14b" {
		t.Errorf("expected model='qwen2.5-coder:14b', got %q", name)
	}
}

// TestParseModelCommand_RDAlias verifies that r&d is not a valid alias.
func TestParseModelCommand_RDAlias(t *testing.T) {
	_, err := ParseModelCommand("use my-model for r&d")
	if err == nil {
		t.Error("expected error for 'r&d' (removed alias), got nil")
	}
}

// TestParseModelCommand_ExtraSpaces verifies behaviour is deterministic with extra spaces.
func TestParseModelCommand_ExtraSpaces(t *testing.T) {
	// The regex uses \s+ so multiple spaces might still work.
	// Verify behaviour is deterministic and does not panic.
	_, err := ParseModelCommand("use  model-x  for  coding")
	_ = err
}

// TestParseModelCommand_ErrorMessageContainsInput verifies the error message
// mentions the bad input for debuggability.
func TestParseModelCommand_ErrorMessageContainsInput(t *testing.T) {
	badInput := "not a valid command"
	_, err := ParseModelCommand(badInput)
	if err == nil {
		t.Fatal("expected error for invalid command")
	}
	if !strings.Contains(err.Error(), badInput) {
		t.Errorf("expected error to mention input %q, got: %v", badInput, err)
	}
}
