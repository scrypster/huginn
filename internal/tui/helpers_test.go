package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/radar"
)

// ============================================================
// styles.go — WrapCode, WrapFilePath, clipString
// ============================================================

func TestWrapCode_ContainsContent(t *testing.T) {
	result := WrapCode("hello world")
	if !strings.Contains(result, "hello world") {
		t.Errorf("WrapCode should contain the content, got: %q", result)
	}
	if result == "" {
		t.Error("WrapCode should return non-empty string")
	}
}

func TestWrapCode_EmptyString(t *testing.T) {
	result := WrapCode("")
	// lipgloss renders the style even for empty content
	if result == "" {
		// acceptable — just ensure no panic
	}
}

func TestWrapFilePath_ContainsPath(t *testing.T) {
	result := WrapFilePath("/some/path/file.go")
	if !strings.Contains(result, "/some/path/file.go") {
		t.Errorf("WrapFilePath should contain the path, got: %q", result)
	}
	if result == "" {
		t.Error("WrapFilePath should return non-empty string")
	}
}

func TestWrapFilePath_NonEmpty(t *testing.T) {
	result := WrapFilePath("internal/tui/app.go")
	if result == "" {
		t.Error("WrapFilePath should return non-empty string")
	}
}

func TestClipString_ExactMaxRunes(t *testing.T) {
	s := "hello"
	result := clipString(s, 5)
	if result != "hello" {
		t.Errorf("clipString(5 chars, max=5) should return original, got %q", result)
	}
}

func TestClipString_UnderMaxRunes(t *testing.T) {
	s := "hi"
	result := clipString(s, 10)
	if result != "hi" {
		t.Errorf("clipString(2 chars, max=10) should return original, got %q", result)
	}
}

func TestClipString_OverMaxRunes(t *testing.T) {
	s := "hello world"
	result := clipString(s, 5)
	if result != "hello" {
		t.Errorf("clipString should truncate to max=5, got %q", result)
	}
	if len([]rune(result)) != 5 {
		t.Errorf("expected exactly 5 runes, got %d", len([]rune(result)))
	}
}

func TestClipString_Unicode(t *testing.T) {
	s := "héllo wörld" // multi-byte runes
	result := clipString(s, 5)
	runes := []rune(result)
	if len(runes) != 5 {
		t.Errorf("expected 5 runes in unicode clip, got %d", len(runes))
	}
	// First rune should be 'h'
	if runes[0] != 'h' {
		t.Errorf("expected first rune 'h', got %c", runes[0])
	}
}

func TestClipString_ZeroMax(t *testing.T) {
	result := clipString("hello", 0)
	if result != "" {
		t.Errorf("clipString with max=0 should return empty, got %q", result)
	}
}

// ============================================================
// app.go — parseModelCommandIfAny
// ============================================================

func TestParseModelCommandIfAny_ValidReasoningByPlanningAlias(t *testing.T) {
	// "planning" is no longer a valid alias; expect an error.
	_, err := parseModelCommandIfAny("use llama3 for planning")
	if err == nil {
		t.Error("expected error for 'planning' (removed alias), got nil")
	}
}

func TestParseModelCommandIfAny_ValidCodingByAlias(t *testing.T) {
	// "coding" is no longer a valid alias; expect an error.
	_, err := parseModelCommandIfAny("use codellama for coding")
	if err == nil {
		t.Error("expected error for 'coding' (removed alias), got nil")
	}
}

func TestParseModelCommandIfAny_ValidReasoningCommand(t *testing.T) {
	model, err := parseModelCommandIfAny("use deepseek for reasoning")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if model != "deepseek" {
		t.Errorf("expected model 'deepseek', got %q", model)
	}
}

func TestParseModelCommandIfAny_PlannerAlias(t *testing.T) {
	// "planner" is no longer a valid alias; expect an error.
	_, err := parseModelCommandIfAny("use llama3 for planner")
	if err == nil {
		t.Error("expected error for 'planner' (removed alias), got nil")
	}
}

func TestParseModelCommandIfAny_CoderAlias(t *testing.T) {
	// "coder" is no longer a valid alias; expect an error.
	_, err := parseModelCommandIfAny("use codellama for coder")
	if err == nil {
		t.Error("expected error for 'coder' (removed alias), got nil")
	}
}

func TestParseModelCommandIfAny_PlainTextReturnsError(t *testing.T) {
	_, err := parseModelCommandIfAny("just some plain text")
	if err == nil {
		t.Error("expected error for plain text, got nil")
	}
}

func TestParseModelCommandIfAny_EmptyStringReturnsError(t *testing.T) {
	_, err := parseModelCommandIfAny("")
	if err == nil {
		t.Error("expected error for empty string, got nil")
	}
}

func TestParseModelCommandIfAny_CaseInsensitive(t *testing.T) {
	model, err := parseModelCommandIfAny("USE llama3 FOR REASONING")
	if err != nil {
		t.Fatalf("expected case-insensitive match, got error: %v", err)
	}
	if model != "llama3" {
		t.Errorf("expected 'llama3', got %q", model)
	}
}

// ============================================================
// app.go — formatDuration
// ============================================================

func TestFormatDuration_ZeroMs(t *testing.T) {
	result := formatDuration(0)
	if result != "0ms" {
		t.Errorf("expected '0ms', got %q", result)
	}
}

func TestFormatDuration_500ms(t *testing.T) {
	result := formatDuration(500 * time.Millisecond)
	if result != "500ms" {
		t.Errorf("expected '500ms', got %q", result)
	}
}

func TestFormatDuration_999ms(t *testing.T) {
	result := formatDuration(999 * time.Millisecond)
	if result != "999ms" {
		t.Errorf("expected '999ms', got %q", result)
	}
}

func TestFormatDuration_1Second(t *testing.T) {
	result := formatDuration(1 * time.Second)
	if result != "1.0s" {
		t.Errorf("expected '1.0s', got %q", result)
	}
}

func TestFormatDuration_1point5Seconds(t *testing.T) {
	result := formatDuration(1500 * time.Millisecond)
	if result != "1.5s" {
		t.Errorf("expected '1.5s', got %q", result)
	}
}

func TestFormatDuration_65Seconds(t *testing.T) {
	result := formatDuration(65 * time.Second)
	if result != "65.0s" {
		t.Errorf("expected '65.0s', got %q", result)
	}
}

func TestFormatDuration_SubSecondVsSecond(t *testing.T) {
	sub := formatDuration(999 * time.Millisecond)
	sec := formatDuration(1000 * time.Millisecond)
	if strings.Contains(sub, "s") && !strings.Contains(sub, "ms") {
		t.Errorf("sub-second should use 'ms', got: %q", sub)
	}
	if !strings.Contains(sec, "s") {
		t.Errorf("1 second should use 's', got: %q", sec)
	}
}

// ============================================================
// app.go — fmtToolCallPreview
// ============================================================

func TestFmtToolCallPreview_Bash(t *testing.T) {
	result := fmtToolCallPreview("bash", map[string]any{"command": "ls -la"})
	if !strings.Contains(result, "bash") {
		t.Errorf("expected 'bash' in result, got %q", result)
	}
	if !strings.Contains(result, "ls -la") {
		t.Errorf("expected command in result, got %q", result)
	}
}

func TestFmtToolCallPreview_BashLongCommand(t *testing.T) {
	longCmd := strings.Repeat("x", 100)
	result := fmtToolCallPreview("bash", map[string]any{"command": longCmd})
	// Should be truncated to 80 chars + "…"
	if !strings.Contains(result, "…") {
		t.Errorf("expected truncation for long command, got %q", result)
	}
}

func TestFmtToolCallPreview_BashNewlines(t *testing.T) {
	result := fmtToolCallPreview("bash", map[string]any{"command": "echo foo\necho bar"})
	if strings.Contains(result, "\n") {
		t.Errorf("expected newlines replaced in bash preview, got %q", result)
	}
}

func TestFmtToolCallPreview_ReadFile(t *testing.T) {
	result := fmtToolCallPreview("read_file", map[string]any{"file_path": "/src/main.go"})
	if !strings.Contains(result, "read_file") {
		t.Errorf("expected 'read_file' in result, got %q", result)
	}
	if !strings.Contains(result, "/src/main.go") {
		t.Errorf("expected path in result, got %q", result)
	}
}

func TestFmtToolCallPreview_WriteFile(t *testing.T) {
	result := fmtToolCallPreview("write_file", map[string]any{"file_path": "output.txt"})
	if !strings.Contains(result, "write_file") {
		t.Errorf("expected 'write_file' in result, got %q", result)
	}
}

func TestFmtToolCallPreview_EditFile(t *testing.T) {
	result := fmtToolCallPreview("edit_file", map[string]any{"file_path": "app.go"})
	if !strings.Contains(result, "edit_file") {
		t.Errorf("expected 'edit_file' in result, got %q", result)
	}
}

func TestFmtToolCallPreview_ListDir(t *testing.T) {
	result := fmtToolCallPreview("list_dir", map[string]any{"path": "/tmp"})
	if !strings.Contains(result, "list_dir") {
		t.Errorf("expected 'list_dir' in result, got %q", result)
	}
	if !strings.Contains(result, "/tmp") {
		t.Errorf("expected path in result, got %q", result)
	}
}

func TestFmtToolCallPreview_SearchFiles(t *testing.T) {
	result := fmtToolCallPreview("search_files", map[string]any{"pattern": "*.go"})
	if !strings.Contains(result, "search_files") {
		t.Errorf("expected 'search_files' in result, got %q", result)
	}
}

func TestFmtToolCallPreview_Grep(t *testing.T) {
	result := fmtToolCallPreview("grep", map[string]any{"pattern": "TODO"})
	if !strings.Contains(result, "grep") {
		t.Errorf("expected 'grep' in result, got %q", result)
	}
	if !strings.Contains(result, "TODO") {
		t.Errorf("expected pattern in result, got %q", result)
	}
}

func TestFmtToolCallPreview_UnknownTool(t *testing.T) {
	result := fmtToolCallPreview("some_unknown_tool", map[string]any{})
	if !strings.Contains(result, "some_unknown_tool") {
		t.Errorf("expected tool name in fallback, got %q", result)
	}
	if !strings.Contains(result, "(…)") {
		t.Errorf("expected fallback format '…', got %q", result)
	}
}

func TestFmtToolCallPreview_BashMissingArg(t *testing.T) {
	result := fmtToolCallPreview("bash", map[string]any{})
	// No "command" arg — should fall through to default format
	if !strings.Contains(result, "bash") {
		t.Errorf("expected tool name in result, got %q", result)
	}
}

// ============================================================
// app.go — helpText
// ============================================================

func TestHelpText_NonEmpty(t *testing.T) {
	result := helpText()
	if result == "" {
		t.Error("helpText() should return non-empty string")
	}
}

func TestHelpText_ContainsKeyBindings(t *testing.T) {
	result := helpText()
	keywords := []string{"ctrl+c", "/reason"}
	for _, kw := range keywords {
		if !strings.Contains(result, kw) {
			t.Errorf("helpText() should contain %q", kw)
		}
	}
}

// ============================================================
// app.go — formatRadarFindings
// ============================================================

func TestFormatRadarFindings_Nil(t *testing.T) {
	result := formatRadarFindings(nil)
	if result == "" {
		t.Error("formatRadarFindings(nil) should return non-empty string")
	}
	if !strings.Contains(result, "0") {
		t.Errorf("expected '0 finding(s)' in output, got %q", result)
	}
}

func TestFormatRadarFindings_Empty(t *testing.T) {
	result := formatRadarFindings([]radar.Finding{})
	if !strings.Contains(result, "0") {
		t.Errorf("expected '0 finding(s)' for empty slice, got %q", result)
	}
}

func TestFormatRadarFindings_SingleFinding(t *testing.T) {
	findings := []radar.Finding{
		{
			Title:       "High churn file modified",
			Description: "File changed frequently",
			Severity:    radar.SeverityHigh,
		},
	}
	result := formatRadarFindings(findings)
	if !strings.Contains(result, "High churn file modified") {
		t.Errorf("expected title in output, got %q", result)
	}
	if !strings.Contains(result, "HIGH") {
		t.Errorf("expected severity in output, got %q", result)
	}
	if !strings.Contains(result, "File changed frequently") {
		t.Errorf("expected description in output, got %q", result)
	}
}

func TestFormatRadarFindings_MultipleSeverities(t *testing.T) {
	findings := []radar.Finding{
		{Title: "Critical finding", Severity: radar.SeverityCritical},
		{Title: "Medium finding", Severity: radar.SeverityMedium},
		{Title: "Info finding", Severity: radar.SeverityInfo},
	}
	result := formatRadarFindings(findings)
	if !strings.Contains(result, "CRITICAL") {
		t.Errorf("expected CRITICAL in output, got %q", result)
	}
	if !strings.Contains(result, "MEDIUM") {
		t.Errorf("expected MEDIUM in output, got %q", result)
	}
	if !strings.Contains(result, "INFO") {
		t.Errorf("expected INFO in output, got %q", result)
	}
	if !strings.Contains(result, "3") {
		t.Errorf("expected '3 finding(s)' in output, got %q", result)
	}
}

func TestFormatRadarFindings_NoDescription(t *testing.T) {
	findings := []radar.Finding{
		{Title: "Simple finding", Severity: radar.SeverityLow},
	}
	result := formatRadarFindings(findings)
	if !strings.Contains(result, "Simple finding") {
		t.Errorf("expected title without description, got %q", result)
	}
}
