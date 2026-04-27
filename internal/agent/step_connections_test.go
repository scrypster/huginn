package agent

import (
	"context"
	"strings"
	"testing"
)

// TestWithStepConnections_RoundTrip verifies the per-step connection map
// survives a context.WithValue round-trip and that the stored value is a
// defensive copy (mutating the source map after WithStepConnections must not
// affect what StepConnections returns).
func TestWithStepConnections_RoundTrip(t *testing.T) {
	t.Parallel()
	src := map[string]string{"github": "personal", "slack": "work"}
	ctx := WithStepConnections(context.Background(), src)

	got := StepConnections(ctx)
	if got["github"] != "personal" || got["slack"] != "work" {
		t.Fatalf("got %v, want round-trip", got)
	}

	// Mutating src must not poison the stored value.
	src["github"] = "evil"
	if StepConnections(ctx)["github"] != "personal" {
		t.Fatalf("StepConnections leaked source map mutation")
	}
}

// TestWithStepConnections_EmptyMap_NoOp verifies that supplying nil/empty maps
// is a true no-op (returns the input ctx) so callers don't pay the
// context-derive cost in the common no-connections case.
func TestWithStepConnections_EmptyMap_NoOp(t *testing.T) {
	t.Parallel()
	parent := context.Background()
	if WithStepConnections(parent, nil) != parent {
		t.Error("nil map should return parent ctx unchanged")
	}
	if WithStepConnections(parent, map[string]string{}) != parent {
		t.Error("empty map should return parent ctx unchanged")
	}
	if got := StepConnections(parent); got != nil {
		t.Errorf("StepConnections on bare ctx = %v, want nil", got)
	}
}

// TestStepConnectionsAddendum_DeterministicOrder verifies the rendered prompt
// addendum is alphabetised by provider so the system prompt is stable across
// runs (important for prompt caching and golden-file testing).
func TestStepConnectionsAddendum_DeterministicOrder(t *testing.T) {
	t.Parallel()
	ctx := WithStepConnections(context.Background(), map[string]string{
		"slack":  "work",
		"github": "personal",
		"jira":   "team",
	})
	got := stepConnectionsAddendum(ctx)
	// Expected ordering: github, jira, slack (alphabetical).
	gh := strings.Index(got, "github")
	jr := strings.Index(got, "jira")
	sl := strings.Index(got, "slack")
	if gh < 0 || jr < 0 || sl < 0 {
		t.Fatalf("missing provider in addendum:\n%s", got)
	}
	if !(gh < jr && jr < sl) {
		t.Fatalf("addendum providers not alphabetical:\n%s", got)
	}
	if !strings.Contains(got, `"personal"`) || !strings.Contains(got, `"team"`) || !strings.Contains(got, `"work"`) {
		t.Fatalf("addendum missing account labels:\n%s", got)
	}
}

// TestStepConnectionsAddendum_EmptyContext_ReturnsBlank verifies the addendum
// is the empty string when no connections are set, so the caller can safely
// concatenate it without producing a stray heading.
func TestStepConnectionsAddendum_EmptyContext_ReturnsBlank(t *testing.T) {
	t.Parallel()
	if got := stepConnectionsAddendum(context.Background()); got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}
