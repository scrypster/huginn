package scheduler

import (
	"testing"

	"github.com/scrypster/huginn/internal/notification"
)

// ── resolveInlineVars ────────────────────────────────────────────────────────

func TestResolveInlineVars_SubstitutesPlaceholders(t *testing.T) {
	vars := map[string]string{"date": "2026-03-19", "user": "alice"}
	got := resolveInlineVars("Hello {{user}} on {{date}}", vars)
	want := "Hello alice on 2026-03-19"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveInlineVars_MissingKeyLeftUnchanged(t *testing.T) {
	got := resolveInlineVars("Hello {{missing}}", map[string]string{"other": "x"})
	if got != "Hello {{missing}}" {
		t.Errorf("expected unchanged placeholder, got %q", got)
	}
}

// ── resolveRuntimeVars ───────────────────────────────────────────────────────

func TestResolveRuntimeVars_PrevOutput(t *testing.T) {
	got := resolveRuntimeVars("Use: {{prev.output}}", nil, nil, "step-1-result")
	if got != "Use: step-1-result" {
		t.Errorf("got %q", got)
	}
}

func TestResolveRuntimeVars_NamedInput(t *testing.T) {
	outputs := map[string]string{"step-1": "summary text"}
	inputs := []StepInput{{FromStep: "step-1", As: "prev_summary"}}
	got := resolveRuntimeVars("Summarise: {{inputs.prev_summary}}", inputs, outputs, "")
	if got != "Summarise: summary text" {
		t.Errorf("got %q", got)
	}
}

func TestResolveRuntimeVars_MissingFromStep_PlaceholderUnchanged(t *testing.T) {
	inputs := []StepInput{{FromStep: "nonexistent", As: "x"}}
	got := resolveRuntimeVars("{{inputs.x}}", inputs, map[string]string{}, "")
	if got != "{{inputs.x}}" {
		t.Errorf("expected unchanged placeholder, got %q", got)
	}
}

func TestResolveRuntimeVars_EmptyAs_Skipped(t *testing.T) {
	inputs := []StepInput{{FromStep: "step-1", As: ""}}
	got := resolveRuntimeVars("{{inputs.}}", inputs, map[string]string{"step-1": "value"}, "")
	// With empty As, the replacement key would be "{{inputs.}}" — we expect no substitution.
	if got != "{{inputs.}}" {
		t.Errorf("expected unchanged placeholder, got %q", got)
	}
}

// ── parseOutput ──────────────────────────────────────────────────────────────

func TestParseOutput_JSONBlock(t *testing.T) {
	output := "{\"summary\": \"All good\"}\nDetails below."
	summary, detail := parseOutput(output)
	if summary != "All good" {
		t.Errorf("summary: got %q", summary)
	}
	_ = detail
}

func TestParseOutput_Fallback_FirstLine(t *testing.T) {
	output := "First line as summary\nrest of output"
	summary, detail := parseOutput(output)
	if summary != "First line as summary" {
		t.Errorf("summary: got %q", summary)
	}
	if detail != "rest of output" {
		t.Errorf("detail: got %q", detail)
	}
}

func TestParseOutput_EmptyOutput(t *testing.T) {
	summary, _ := parseOutput("")
	if summary != "(no output)" {
		t.Errorf("expected '(no output)', got %q", summary)
	}
}

func TestParseOutput_LongLineTruncated(t *testing.T) {
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	summary, _ := parseOutput(string(long))
	if len([]rune(summary)) > 120 {
		t.Errorf("expected summary truncated to 120 runes, got len=%d", len([]rune(summary)))
	}
}

// ── parseSeverity ────────────────────────────────────────────────────────────

func TestParseSeverity_Info(t *testing.T) {
	if parseSeverity("info") != notification.SeverityInfo {
		t.Error("expected SeverityInfo for 'info'")
	}
}

func TestParseSeverity_Warning(t *testing.T) {
	if parseSeverity("warning") != notification.SeverityWarning {
		t.Error("expected SeverityWarning for 'warning'")
	}
}

func TestParseSeverity_Urgent(t *testing.T) {
	if parseSeverity("urgent") != notification.SeverityUrgent {
		t.Error("expected SeverityUrgent for 'urgent'")
	}
}

func TestParseSeverity_Unknown_DefaultsInfo(t *testing.T) {
	if parseSeverity("UNKNOWN_VAL") != notification.SeverityInfo {
		t.Error("expected SeverityInfo for unknown value")
	}
}

func TestParseSeverity_CaseInsensitive(t *testing.T) {
	if parseSeverity("WARNING") != notification.SeverityWarning {
		t.Error("expected SeverityWarning for 'WARNING'")
	}
}

// ── truncate ─────────────────────────────────────────────────────────────────

func TestTruncate_UnderLimit(t *testing.T) {
	s := "hello"
	if truncate(s, 10) != s {
		t.Errorf("expected unchanged, got %q", truncate(s, 10))
	}
}

func TestTruncate_AtLimit(t *testing.T) {
	s := "exactly10x"
	if truncate(s, 10) != s {
		t.Errorf("expected unchanged at limit, got %q", truncate(s, 10))
	}
}

func TestTruncate_OverLimit(t *testing.T) {
	s := "this is a longer string than the limit"
	result := truncate(s, 10)
	r := []rune(result)
	if len(r) != 10 {
		t.Errorf("expected 10 runes, got %d: %q", len(r), result)
	}
	// Last rune should be the ellipsis.
	if r[9] != '…' {
		t.Errorf("expected ellipsis at end, got %q", string(r[9]))
	}
}

func TestTruncate_Empty(t *testing.T) {
	if truncate("", 10) != "" {
		t.Error("expected empty string unchanged")
	}
}
