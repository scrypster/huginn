package agents_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestFromDef_UnrecognizedPersonality_NormalizedToDefault fails without the
// fix: LoadAgentsFromBase unmarshals AgentDef files straight off disk
// without ever calling Validate, so FromDef used to carry a garbage
// personality string (e.g. one left over from a renamed preset, or a
// hand-edited file) straight through onto the runtime Agent — and that
// string renders verbatim in the web UI's personality badge. The fix
// normalizes any unrecognized value to "" in FromDef itself and logs a
// warning, since FromDef is the single conversion point every load path
// goes through.
func TestFromDef_UnrecognizedPersonality_NormalizedToDefault(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(orig) })

	def := agents.AgentDef{Name: "Rogue", Model: "m", Personality: "<script>garbage-preset-9000</script>"}
	a := agents.FromDef(def)

	if a.Personality != "" {
		t.Errorf("Personality = %q, want normalized to empty string", a.Personality)
	}
	logged := buf.String()
	if !strings.Contains(logged, "unrecognized personality") {
		t.Errorf("expected a warning about the unrecognized personality, got log: %q", logged)
	}
}

// TestFromDef_ValidPersonality_PassesThroughUnchanged guards against an
// over-eager normalization that clobbers legitimate presets.
func TestFromDef_ValidPersonality_PassesThroughUnchanged(t *testing.T) {
	def := agents.AgentDef{Name: "Reviewer", Model: "m", Personality: agents.PersonalityStrictReviewer}
	a := agents.FromDef(def)
	if a.Personality != agents.PersonalityStrictReviewer {
		t.Errorf("Personality = %q, want %q preserved", a.Personality, agents.PersonalityStrictReviewer)
	}
}
