package proactivity

import (
	"strings"
	"testing"
)

func TestAssembleContinuityPack_DeterministicIsTaskScoped(t *testing.T) {
	t.Parallel()
	pack := AssembleContinuityPack(ContinuityPackInput{
		Mode:               ContinuityModeDeterministic,
		UserMessage:        "please fix CI token failures in the pipeline",
		WhereLeftOffOutput: "Last chat was about weekend planning.",
		RecallOutput: strings.Join([]string{
			"Pipeline run failed on deploy stage.",
			"Need to rotate CI token before next run.",
			"Diet note: drink more water.",
		}, "\n"),
	})

	if !strings.Contains(pack, "## Continuity Pack") {
		t.Fatalf("expected continuity heading, got %q", pack)
	}
	if strings.Contains(pack, "### Recent Orientation") {
		t.Fatalf("did not expect conversational orientation block in deterministic mode: %q", pack)
	}
	if !strings.Contains(pack, "### Task Scope") {
		t.Fatalf("expected task scope block, got %q", pack)
	}
	if !strings.Contains(pack, "Pipeline run failed") {
		t.Fatalf("expected task-scoped line to be included, got %q", pack)
	}
	if !strings.Contains(pack, "### Open Commitments") {
		t.Fatalf("expected commitments section, got %q", pack)
	}
}

func TestAssembleContinuityPack_ConversationalIncludesOrientation(t *testing.T) {
	t.Parallel()
	pack := AssembleContinuityPack(ContinuityPackInput{
		Mode:               ContinuityModeConversational,
		WhereLeftOffOutput: "You were reviewing delivery retries yesterday.",
		RecallOutput:       "Action required: confirm fallback endpoint.",
	})

	if !strings.Contains(pack, "### Recent Orientation") {
		t.Fatalf("expected orientation section in conversational mode, got %q", pack)
	}
	if !strings.Contains(pack, "### Relevant Memory") {
		t.Fatalf("expected relevant memory section in conversational mode, got %q", pack)
	}
}

func TestExtractCommitments(t *testing.T) {
	t.Parallel()
	out := ExtractCommitments(3,
		"- [ ] Follow up with Ops on webhook retries",
		"All clear",
		"Need to rotate CI token",
		"TODO: confirm release window",
	)
	if len(out) != 3 {
		t.Fatalf("expected 3 commitments, got %d (%v)", len(out), out)
	}
	if !strings.Contains(strings.ToLower(strings.Join(out, " | ")), "rotate ci token") {
		t.Fatalf("expected rotate token commitment in %v", out)
	}
}
