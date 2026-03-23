package agent

// tool_parallelism_spec_test.go — Behavior specs for isIndependentTool.
//
// isIndependentTool classifies whether a tool call can be executed in parallel
// with others in the same turn.  This is a critical scheduling decision: wrong
// classifications cause either unnecessary serialization (slow) or unsafe
// concurrent mutations (data corruption).
//
// Each test documents the REASON a tool is serial or independent so that the
// classification rules are readable as specification.

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func makeCall(name, filePath string) backend.ToolCall {
	args := map[string]any{}
	if filePath != "" {
		args["file_path"] = filePath
	}
	return backend.ToolCall{
		Function: backend.ToolCallFunction{Name: name, Arguments: args},
	}
}

// ── Read-only tools: always independent ──────────────────────────────────────

func TestIsIndependentTool_ReadFile_IsIndependent(t *testing.T) {
	call := makeCall("read_file", "/a.go")
	if !isIndependentTool("read_file", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("read_file should be independent (read-only)")
	}
}

func TestIsIndependentTool_Grep_IsIndependent(t *testing.T) {
	call := makeCall("grep", "")
	if !isIndependentTool("grep", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("grep should be independent (read-only)")
	}
}

func TestIsIndependentTool_GitStatus_IsIndependent(t *testing.T) {
	call := makeCall("git_status", "")
	if !isIndependentTool("git_status", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("git_status should be independent (read-only git)")
	}
}

func TestIsIndependentTool_GitLog_IsIndependent(t *testing.T) {
	call := makeCall("git_log", "")
	if !isIndependentTool("git_log", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("git_log should be independent (read-only git)")
	}
}

func TestIsIndependentTool_WebSearch_IsIndependent(t *testing.T) {
	call := makeCall("web_search", "")
	if !isIndependentTool("web_search", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("web_search should be independent (network read-only)")
	}
}

// ── Bash: always serial ───────────────────────────────────────────────────────

func TestIsIndependentTool_Bash_IsSerial(t *testing.T) {
	call := makeCall("bash", "")
	if isIndependentTool("bash", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("bash should be serial (may have side effects)")
	}
}

// Multiple bash calls should each be serial.
func TestIsIndependentTool_TwoBashCalls_BothSerial(t *testing.T) {
	c1, c2 := makeCall("bash", ""), makeCall("bash", "")
	batch := []backend.ToolCall{c1, c2}
	if isIndependentTool("bash", c1.Function.Arguments, batch) {
		t.Error("first bash in batch should be serial")
	}
	if isIndependentTool("bash", c2.Function.Arguments, batch) {
		t.Error("second bash in batch should be serial")
	}
}

// ── Git write operations: always serial ──────────────────────────────────────

func TestIsIndependentTool_GitCommit_IsSerial(t *testing.T) {
	call := makeCall("git_commit", "")
	if isIndependentTool("git_commit", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("git_commit should be serial (mutates git state)")
	}
}

func TestIsIndependentTool_GitStash_IsSerial(t *testing.T) {
	call := makeCall("git_stash", "")
	if isIndependentTool("git_stash", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("git_stash should be serial (mutates git state)")
	}
}

// ── Write/edit to distinct paths: independent ────────────────────────────────

func TestIsIndependentTool_WriteFile_DistinctPaths_IsIndependent(t *testing.T) {
	c1 := makeCall("write_file", "/a.go")
	c2 := makeCall("write_file", "/b.go")
	batch := []backend.ToolCall{c1, c2}
	if !isIndependentTool("write_file", c1.Function.Arguments, batch) {
		t.Error("write_file to distinct path should be independent")
	}
	if !isIndependentTool("write_file", c2.Function.Arguments, batch) {
		t.Error("write_file to distinct path should be independent")
	}
}

func TestIsIndependentTool_EditFile_DistinctPaths_IsIndependent(t *testing.T) {
	c1 := makeCall("edit_file", "/x.go")
	c2 := makeCall("edit_file", "/y.go")
	batch := []backend.ToolCall{c1, c2}
	if !isIndependentTool("edit_file", c1.Function.Arguments, batch) {
		t.Error("edit_file to distinct path should be independent")
	}
}

// ── Write/edit to the SAME path: serial ──────────────────────────────────────

func TestIsIndependentTool_WriteFile_SamePath_IsSerial(t *testing.T) {
	c1 := makeCall("write_file", "/shared.go")
	c2 := makeCall("write_file", "/shared.go")
	batch := []backend.ToolCall{c1, c2}
	if isIndependentTool("write_file", c1.Function.Arguments, batch) {
		t.Error("write_file to same path as another call should be serial (last-write-wins race)")
	}
}

func TestIsIndependentTool_EditFile_SamePath_IsSerial(t *testing.T) {
	c1 := makeCall("edit_file", "/shared.go")
	c2 := makeCall("edit_file", "/shared.go")
	batch := []backend.ToolCall{c1, c2}
	if isIndependentTool("edit_file", c1.Function.Arguments, batch) {
		t.Error("edit_file to same path as another call should be serial")
	}
}

// ── Write with missing path: serial (safe default) ───────────────────────────

func TestIsIndependentTool_WriteFile_NoPath_IsSerial(t *testing.T) {
	call := makeCall("write_file", "") // no file_path arg
	if isIndependentTool("write_file", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("write_file with no path should be serial (can't deduplicate)")
	}
}

// ── MCP tools: always serial ──────────────────────────────────────────────────

func TestIsIndependentTool_MCPTool_IsSerial(t *testing.T) {
	call := makeCall("mcp_slack_send_message", "")
	if isIndependentTool("mcp_slack_send_message", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("MCP tool (mcp_ prefix) should be serial (state-dependent side effects)")
	}
}

func TestIsIndependentTool_MCPPrefix_AnyName_IsSerial(t *testing.T) {
	for _, name := range []string{"mcp_github_create_pr", "mcp_jira_create_issue", "mcp_custom_tool"} {
		call := makeCall(name, "")
		if isIndependentTool(name, call.Function.Arguments, []backend.ToolCall{call}) {
			t.Errorf("MCP tool %q should be serial", name)
		}
	}
}

// ── Unknown tools: serial (safe default) ─────────────────────────────────────

func TestIsIndependentTool_Unknown_IsSerial(t *testing.T) {
	call := makeCall("some_future_tool", "")
	if isIndependentTool("some_future_tool", call.Function.Arguments, []backend.ToolCall{call}) {
		t.Error("unknown tool should default to serial (conservative safe default)")
	}
}
