package permissions

import (
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// --- NewGate / basic construction ---

func TestNewGate_InitializesCorrectly(t *testing.T) {
	g := NewGate(false, nil)
	if g == nil {
		t.Fatal("NewGate returned nil")
	}
	if g.skipAll {
		t.Error("expected skipAll=false")
	}
	if g.sessionAllowed == nil {
		t.Error("expected sessionAllowed map to be initialized")
	}
}

func TestNewGate_SkipAll(t *testing.T) {
	g := NewGate(true, nil)
	if !g.skipAll {
		t.Error("expected skipAll=true")
	}
}

// --- Gate.Check: PermRead is always allowed ---

func TestCheck_ReadAlwaysAllowed_NoSkip(t *testing.T) {
	g := NewGate(false, nil)
	req := PermissionRequest{ToolName: "read_file", Level: tools.PermRead}
	if !g.Check(req) {
		t.Error("expected PermRead to always be allowed")
	}
}

func TestCheck_ReadAlwaysAllowed_WithSkipAll(t *testing.T) {
	g := NewGate(true, nil)
	req := PermissionRequest{ToolName: "read_file", Level: tools.PermRead}
	if !g.Check(req) {
		t.Error("expected PermRead to always be allowed even with skipAll")
	}
}

func TestCheck_ReadAlwaysAllowed_WithNilPrompt(t *testing.T) {
	g := NewGate(false, nil) // no prompt func
	req := PermissionRequest{ToolName: "read_file", Level: tools.PermRead}
	if !g.Check(req) {
		t.Error("expected PermRead to be allowed regardless of promptFunc")
	}
}

// --- Gate.Check: skipAll bypasses everything ---

func TestCheck_SkipAll_Write(t *testing.T) {
	g := NewGate(true, nil)
	req := PermissionRequest{ToolName: "write_file", Level: tools.PermWrite}
	if !g.Check(req) {
		t.Error("expected PermWrite to be allowed when skipAll=true")
	}
}

func TestCheck_SkipAll_Exec(t *testing.T) {
	g := NewGate(true, nil)
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if !g.Check(req) {
		t.Error("expected PermExec to be allowed when skipAll=true")
	}
}

// --- Gate.Check: nil promptFunc denies non-read ---

func TestCheck_NilPromptDeniesWrite(t *testing.T) {
	g := NewGate(false, nil)
	req := PermissionRequest{ToolName: "write_file", Level: tools.PermWrite}
	if g.Check(req) {
		t.Error("expected denial when promptFunc is nil and level is PermWrite")
	}
}

func TestCheck_NilPromptDeniesExec(t *testing.T) {
	g := NewGate(false, nil)
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if g.Check(req) {
		t.Error("expected denial when promptFunc is nil and level is PermExec")
	}
}

// --- Gate.Check: Allow decisions ---

func TestCheck_AllowOnceDecision(t *testing.T) {
	g := NewGate(false, func(r PermissionRequest) Decision { return Allow })
	req := PermissionRequest{ToolName: "write_file", Level: tools.PermWrite}
	if !g.Check(req) {
		t.Error("expected Allow decision to return true")
	}
	// Allow (AllowOnce) must NOT add to sessionAllowed.
	g.mu.Lock()
	if g.sessionAllowed["write_file"] {
		t.Error("Allow should not persist in sessionAllowed")
	}
	g.mu.Unlock()
}

func TestCheck_AllowOnceAlias(t *testing.T) {
	g := NewGate(false, func(r PermissionRequest) Decision { return AllowOnce })
	req := PermissionRequest{ToolName: "edit_file", Level: tools.PermWrite}
	if !g.Check(req) {
		t.Error("expected AllowOnce decision to return true")
	}
}

func TestCheck_AllowAllDecision_PersistsInSession(t *testing.T) {
	callCount := 0
	g := NewGate(false, func(r PermissionRequest) Decision {
		callCount++
		return AllowAll
	})
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}

	// First call — prompt is invoked, decision AllowAll.
	if !g.Check(req) {
		t.Error("expected first AllowAll check to return true")
	}
	if callCount != 1 {
		t.Errorf("expected 1 prompt call, got %d", callCount)
	}

	// Second call — should short-circuit without calling promptFunc again.
	if !g.Check(req) {
		t.Error("expected second check to return true from sessionAllowed")
	}
	if callCount != 1 {
		t.Errorf("expected promptFunc to not be called again, got %d calls", callCount)
	}
}

func TestCheck_DenyDecision(t *testing.T) {
	g := NewGate(false, func(r PermissionRequest) Decision { return Deny })
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if g.Check(req) {
		t.Error("expected Deny decision to return false")
	}
}

// --- sessionAllowed pre-populated ---

func TestCheck_SessionAllowed_SkipsPrompt(t *testing.T) {
	prompted := false
	g := NewGate(false, func(r PermissionRequest) Decision {
		prompted = true
		return Deny // would deny if prompted
	})
	g.mu.Lock()
	g.sessionAllowed["bash"] = true
	g.mu.Unlock()

	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if !g.Check(req) {
		t.Error("expected sessionAllowed to bypass prompt and return true")
	}
	if prompted {
		t.Error("promptFunc must not be called when tool is in sessionAllowed")
	}
}

// --- Concurrent access ---

func TestCheck_ConcurrentAccess(t *testing.T) {
	var mu sync.Mutex
	decisions := []Decision{Allow, AllowAll, Deny, AllowOnce}
	idx := 0

	g := NewGate(false, func(r PermissionRequest) Decision {
		mu.Lock()
		d := decisions[idx%len(decisions)]
		idx++
		mu.Unlock()
		return d
	})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
			_ = g.Check(req) // must not race / panic
		}()
	}
	wg.Wait()
}

// --- FormatRequest ---

func TestFormatRequest_UseSummaryWhenPresent(t *testing.T) {
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Summary:  "run the build script",
	}
	result := FormatRequest(req)
	if result != "run the build script" {
		t.Errorf("expected summary to be returned verbatim, got %q", result)
	}
}

func TestFormatRequest_Bash_WithCommand(t *testing.T) {
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Args:     map[string]any{"command": "echo hello"},
	}
	result := FormatRequest(req)
	if result != "bash: echo hello" {
		t.Errorf("expected 'bash: echo hello', got %q", result)
	}
}

func TestFormatRequest_Bash_TruncatesLongCommand(t *testing.T) {
	longCmd := ""
	for i := 0; i < 100; i++ {
		longCmd += "x"
	}
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Args:     map[string]any{"command": longCmd},
	}
	result := FormatRequest(req)
	// 80 chars + "…" suffix
	if len([]rune(result)) > len("bash: ")+80+2 {
		t.Errorf("expected truncation, got length %d: %q", len(result), result)
	}
	if len([]rune(result)) < len("bash: ") {
		t.Errorf("result too short: %q", result)
	}
}

func TestFormatRequest_Bash_ReplacesNewlines(t *testing.T) {
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Args:     map[string]any{"command": "echo a\necho b"},
	}
	result := FormatRequest(req)
	if result != "bash: echo a echo b" {
		t.Errorf("expected newlines replaced with spaces, got %q", result)
	}
}

func TestFormatRequest_Bash_MissingCommandKey(t *testing.T) {
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Args:     map[string]any{"other": "value"},
	}
	result := FormatRequest(req)
	// Falls through to generic format
	if result == "" {
		t.Error("expected non-empty fallback result")
	}
}

func TestFormatRequest_WriteFile_WithPath(t *testing.T) {
	req := PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
		Args:     map[string]any{"file_path": "/tmp/foo.txt", "content": "hello"},
	}
	result := FormatRequest(req)
	expected := "write_file: /tmp/foo.txt (5 bytes)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFormatRequest_WriteFile_MissingPath(t *testing.T) {
	req := PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
		Args:     map[string]any{"content": "hello"},
	}
	result := FormatRequest(req)
	if result == "" {
		t.Error("expected non-empty fallback for write_file without path")
	}
}

func TestFormatRequest_WriteFile_EmptyContent(t *testing.T) {
	req := PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
		Args:     map[string]any{"file_path": "/tmp/empty.txt"},
	}
	result := FormatRequest(req)
	expected := "write_file: /tmp/empty.txt (0 bytes)"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFormatRequest_EditFile(t *testing.T) {
	req := PermissionRequest{
		ToolName: "edit_file",
		Level:    tools.PermWrite,
		Args:     map[string]any{"file_path": "/tmp/bar.txt"},
	}
	result := FormatRequest(req)
	expected := "edit_file: /tmp/bar.txt"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFormatRequest_EditFile_MissingPath(t *testing.T) {
	req := PermissionRequest{
		ToolName: "edit_file",
		Level:    tools.PermWrite,
		Args:     map[string]any{},
	}
	result := FormatRequest(req)
	if result == "" {
		t.Error("expected non-empty fallback for edit_file without path")
	}
}

func TestFormatRequest_UnknownTool_EmptyArgs(t *testing.T) {
	req := PermissionRequest{
		ToolName: "some_tool",
		Level:    tools.PermExec,
		Args:     map[string]any{},
	}
	result := FormatRequest(req)
	if result == "" {
		t.Error("expected non-empty generic format")
	}
}

// TestFormatRequest_BrowserTool_RendersURLReadably is V9: a browser_*/MCP
// tool call must render its url argument readably ("browser_navigate:
// https://...") instead of falling through to the generic Go map dump
// ("browser_navigate: map[url:https://...]").
func TestFormatRequest_BrowserTool_RendersURLReadably(t *testing.T) {
	req := PermissionRequest{
		ToolName: "browser_navigate",
		Provider: "playwright",
		Level:    tools.PermWrite,
		Args:     map[string]any{"url": "https://example.com/path"},
	}
	result := FormatRequest(req)
	want := "browser_navigate: https://example.com/path"
	if result != want {
		t.Errorf("FormatRequest = %q, want %q", result, want)
	}
	if strings.Contains(result, "map[") {
		t.Errorf("expected no Go map dump in output, got: %q", result)
	}
}

// TestFormatRequest_MCPTool_RendersArgsReadably verifies a provider-tagged
// tool with no url/selector/text/query arg still renders key=value pairs
// instead of a Go map dump.
func TestFormatRequest_MCPTool_RendersArgsReadably(t *testing.T) {
	req := PermissionRequest{
		ToolName: "browser_wait_for",
		Provider: "playwright",
		Level:    tools.PermWrite,
		Args:     map[string]any{"timeout": 30},
	}
	result := FormatRequest(req)
	want := "browser_wait_for: timeout=30"
	if result != want {
		t.Errorf("FormatRequest = %q, want %q", result, want)
	}
	if strings.Contains(result, "map[") {
		t.Errorf("expected no Go map dump in output, got: %q", result)
	}
}

func TestFormatRequest_EmptyToolName(t *testing.T) {
	req := PermissionRequest{
		ToolName: "",
		Level:    tools.PermRead,
		Args:     map[string]any{"key": "val"},
	}
	result := FormatRequest(req)
	if result == "" {
		t.Error("expected non-empty result even with empty tool name")
	}
}

// --- FormatPromptOptions ---

func TestFormatPromptOptions_ContainsAllKeys(t *testing.T) {
	opts := strings.ToLower(FormatPromptOptions())
	// The prompt options string uses bracketed key hints like [a]llow and [d]eny.
	// We check for the meaningful substrings rather than the exact key letter.
	for _, fragment := range []string{"llow", "eny", "session"} {
		if !strings.Contains(opts, fragment) {
			t.Errorf("expected fragment %q in FormatPromptOptions output: %q", fragment, opts)
		}
	}
}

func TestFormatPromptOptions_NotEmpty(t *testing.T) {
	if FormatPromptOptions() == "" {
		t.Error("FormatPromptOptions must not return empty string")
	}
}

// --- truncateLine (exercised via FormatRequest) ---

func TestTruncateLine_ShortString_Unchanged(t *testing.T) {
	result := truncateLine("short", 80)
	if result != "short" {
		t.Errorf("expected 'short', got %q", result)
	}
}

func TestTruncateLine_ExactlyMax_Unchanged(t *testing.T) {
	s := "12345"
	result := truncateLine(s, 5)
	if result != s {
		t.Errorf("expected unchanged string of exactly max length, got %q", result)
	}
}

func TestTruncateLine_LongString_Truncated(t *testing.T) {
	s := "abcdefghij"
	result := truncateLine(s, 5)
	if len([]rune(result)) > 5+1 { // "…" is one rune
		t.Errorf("expected truncation, got %q (len %d)", result, len(result))
	}
	// Should end with ellipsis rune
	runes := []rune(result)
	if runes[len(runes)-1] != '…' {
		t.Errorf("expected truncated string to end with '…', got %q", result)
	}
}

func TestTruncateLine_NewlinesReplaced(t *testing.T) {
	result := truncateLine("line1\nline2\nline3", 80)
	for _, c := range result {
		if c == '\n' {
			t.Error("truncateLine must replace all newlines with spaces")
		}
	}
}

// --- Decision constants ---

func TestDecisionConstants_Distinct(t *testing.T) {
	if Allow == AllowAll {
		t.Error("Allow and AllowAll should be distinct constants")
	}
	if Allow == Deny {
		t.Error("Allow and Deny should be distinct constants")
	}
	if AllowAll == Deny {
		t.Error("AllowAll and Deny should be distinct constants")
	}
}

// --- Gate.SetAllowedProviders ---

func TestGate_AllowedProviders_NilAllowsAll(t *testing.T) {
	g := NewGate(false, func(r PermissionRequest) Decision { return Allow })
	// nil allowedProviders — any provider allowed (legacy / unscoped gate)
	g.SetAllowedProviders(nil)
	req := PermissionRequest{ToolName: "slack_post", Level: tools.PermRead, Provider: "slack"}
	if !g.Check(req) {
		t.Error("expected allowed when allowedProviders is nil")
	}
}

func TestGate_AllowedProviders_EmptyMapDeniesExternal(t *testing.T) {
	// Empty map is fail-closed. skipAll auto-approves prompts; it must not
	// grant every provider just because the toolbelt was empty.
	g := NewGate(true, nil)
	g.SetAllowedProviders(map[string]bool{})
	req := PermissionRequest{ToolName: "aws_ec2_terminate_instance", Level: tools.PermWrite, Provider: "aws"}
	if g.Check(req) {
		t.Error("expected empty AllowedProviders to deny aws even when skipAll=true")
	}
}

func TestGate_AllowedProviders_WildcardAllowsAll(t *testing.T) {
	g := NewGate(true, nil)
	g.SetAllowedProviders(map[string]bool{"*": true})
	req := PermissionRequest{ToolName: "aws_ec2_terminate_instance", Level: tools.PermWrite, Provider: "aws"}
	if !g.Check(req) {
		t.Error("expected provider \"*\" to allow aws")
	}
}

func TestGate_AllowedProviders_RejectsUnauthorizedProvider(t *testing.T) {
	g := NewGate(false, func(r PermissionRequest) Decision { return Allow })
	g.SetAllowedProviders(map[string]bool{"github": true})
	req := PermissionRequest{ToolName: "slack_post", Level: tools.PermRead, Provider: "slack"}
	if g.Check(req) {
		t.Error("expected rejection for provider not in allowed set")
	}
}

func TestGate_AllowedProviders_AllowsAuthorizedProvider(t *testing.T) {
	g := NewGate(false, func(r PermissionRequest) Decision { return Allow })
	g.SetAllowedProviders(map[string]bool{"github": true})
	req := PermissionRequest{ToolName: "github_list_prs", Level: tools.PermRead, Provider: "github"}
	if !g.Check(req) {
		t.Error("expected allowed for provider in allowed set")
	}
}

func TestGate_AllowedProviders_InternalToolsUnaffected(t *testing.T) {
	// Tools with no provider tag (internal tools like bash, write_file) are never blocked
	g := NewGate(false, func(r PermissionRequest) Decision { return Allow })
	g.SetAllowedProviders(map[string]bool{"github": true})
	req := PermissionRequest{ToolName: "bash", Level: tools.PermRead, Provider: ""}
	if !g.Check(req) {
		t.Error("expected internal tools (empty provider) to be unaffected by AllowedProviders")
	}
}

func TestGate_AllowedProviders_SkipAllRespected(t *testing.T) {
	// Even with skipAll=true, provider restriction still applies
	g := NewGate(true, nil)
	g.SetAllowedProviders(map[string]bool{"github": true})
	req := PermissionRequest{ToolName: "slack_post", Level: tools.PermRead, Provider: "slack"}
	if g.Check(req) {
		t.Error("expected rejection even with skipAll when provider not in allowed set")
	}
}

// TestAllowAndAllowOnce_BehaviorEquivalent verifies that both Allow and AllowOnce
// result in the tool call being permitted (even though their iota values differ).
func TestAllowAndAllowOnce_BehaviorEquivalent(t *testing.T) {
	for _, dec := range []Decision{Allow, AllowOnce} {
		g := NewGate(false, func(r PermissionRequest) Decision { return dec })
		req := PermissionRequest{ToolName: "write_file", Level: tools.PermWrite}
		if !g.Check(req) {
			t.Errorf("expected decision %d to allow the tool call", dec)
		}
	}
}
