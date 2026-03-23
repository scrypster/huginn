package permissions

import (
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// TestCheck_NilPromptFunc_DeniesWriteTools verifies that when no promptFunc
// is set, write-level tool calls are denied by default.
func TestCheck_NilPromptFunc_DeniesWriteTools(t *testing.T) {
	g := NewGate(false, nil) // no promptFunc
	req := PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
	}
	if g.Check(req) {
		t.Error("expected Check to deny when promptFunc is nil")
	}
}

// TestCheck_NilPromptFunc_DeniesExecTools verifies nil promptFunc denies exec-level tools.
func TestCheck_NilPromptFunc_DeniesExecTools(t *testing.T) {
	g := NewGate(false, nil)
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
	}
	if g.Check(req) {
		t.Error("expected Check to deny exec tool when promptFunc is nil")
	}
}

// TestCheck_PermRead_AlwaysAllowed verifies that PermRead tools bypass all
// gates regardless of skipAll setting or sessionAllowed state.
func TestCheck_PermRead_AlwaysAllowedWithoutSkipAll(t *testing.T) {
	promptCalled := false
	g := NewGate(false, func(req PermissionRequest) Decision {
		promptCalled = true
		return Deny
	})
	req := PermissionRequest{
		ToolName: "read_file",
		Level:    tools.PermRead,
	}
	if !g.Check(req) {
		t.Error("expected PermRead to always be allowed")
	}
	if promptCalled {
		t.Error("expected promptFunc NOT to be called for PermRead tools")
	}
}

// TestCheck_SkipAll_AllowsExecWithoutPrompt verifies that skipAll=true bypasses
// the promptFunc even for high-permission tools.
func TestCheck_SkipAll_AllowsExecWithoutPrompt(t *testing.T) {
	promptCalled := false
	g := NewGate(true, func(req PermissionRequest) Decision {
		promptCalled = true
		return Deny
	})
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Args:     map[string]any{"command": "rm -rf /"},
	}
	if !g.Check(req) {
		t.Error("expected skipAll=true to allow all tools")
	}
	if promptCalled {
		t.Error("expected promptFunc NOT to be called when skipAll=true")
	}
}

// TestCheck_AllowAll_PersistsConcurrently verifies that concurrent AllowAll
// decisions for the same tool are handled safely and idempotently.
func TestCheck_AllowAll_ConcurrentSameToolName(t *testing.T) {
	const n = 50
	g := NewGate(false, func(req PermissionRequest) Decision {
		return AllowAll
	})

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Check(PermissionRequest{
				ToolName: "shared_tool",
				Level:    tools.PermWrite,
			})
		}()
	}
	wg.Wait()

	// After concurrent AllowAll calls, sessionAllowed should have the tool.
	g.mu.Lock()
	allowed := g.sessionAllowed["shared_tool"]
	g.mu.Unlock()
	if !allowed {
		t.Error("expected shared_tool to be sessionAllowed after AllowAll")
	}
}

// TestFormatRequest_EditFile_MissingPath verifies FormatRequest fallback for
// edit_file without file_path.
func TestFormatRequest_EditFileNoPath(t *testing.T) {
	req := PermissionRequest{
		ToolName: "edit_file",
		Args:     map[string]any{},
	}
	got := FormatRequest(req)
	// Should fall through to default format since no file_path.
	if got == "" {
		t.Error("expected non-empty result from FormatRequest")
	}
}

// TestFormatRequest_LargeArgs verifies that FormatRequest handles requests with
// many args without panic.
func TestFormatRequest_LargeArgs(t *testing.T) {
	args := make(map[string]any)
	for i := 0; i < 100; i++ {
		args[string(rune('a'+i%26))+string(rune('0'+i%10))] = i
	}
	req := PermissionRequest{
		ToolName: "custom_tool",
		Level:    tools.PermExec,
		Args:     args,
	}
	// Must not panic.
	got := FormatRequest(req)
	if got == "" {
		t.Error("expected non-empty result")
	}
}

// TestNewRelayRequestID_IsHex verifies that NewRelayRequestID returns a
// 32-character lowercase hex string.
func TestNewRelayRequestID_IsHex(t *testing.T) {
	id, err := NewRelayRequestID()
	if err != nil {
		t.Fatalf("NewRelayRequestID: %v", err)
	}
	if len(id) != 32 {
		t.Errorf("expected length 32, got %d: %q", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("expected lowercase hex, got char %q in %q", c, id)
			break
		}
	}
}

// TestNewRelayRequestID_UniqueAcrossCalls verifies that two consecutive calls
// produce different IDs.
func TestNewRelayRequestID_UniqueAcrossCalls(t *testing.T) {
	id1, _ := NewRelayRequestID()
	id2, _ := NewRelayRequestID()
	if id1 == id2 {
		t.Errorf("expected unique IDs, got same: %q", id1)
	}
}

// TestCheck_DenyPersistsOnlyForCurrentCall verifies that a Deny decision does
// not add to sessionAllowed, so subsequent calls still prompt.
func TestCheck_Deny_DoesNotPersist(t *testing.T) {
	callCount := 0
	g := NewGate(false, func(req PermissionRequest) Decision {
		callCount++
		return Deny
	})

	req := PermissionRequest{ToolName: "some_tool", Level: tools.PermWrite}
	g.Check(req)
	g.Check(req)

	if callCount != 2 {
		t.Errorf("expected promptFunc called twice (deny should not persist), got %d", callCount)
	}
}

// TestGate_SessionAllowed_AfterAllowAll_SkipsPrompt verifies the session cache
// is consulted before promptFunc on subsequent calls.
func TestGate_SessionAllowed_AfterAllowAll_SkipsPrompt(t *testing.T) {
	callCount := 0
	g := NewGate(false, func(req PermissionRequest) Decision {
		callCount++
		return AllowAll
	})

	req := PermissionRequest{ToolName: "the_tool", Level: tools.PermWrite}

	// First call: promptFunc called, AllowAll stored.
	if !g.Check(req) {
		t.Fatal("expected first check to allow")
	}
	if callCount != 1 {
		t.Fatalf("expected promptFunc called once, got %d", callCount)
	}

	// Second call: sessionAllowed should be hit, promptFunc skipped.
	if !g.Check(req) {
		t.Fatal("expected second check to allow (sessionAllowed)")
	}
	if callCount != 1 {
		t.Errorf("expected promptFunc not called on second check, got total %d calls", callCount)
	}
}

// TestFormatRequest_WriteFile_LargeContent verifies truncation doesn't panic
// for write_file with large content.
func TestFormatRequest_WriteFile_LargeContent(t *testing.T) {
	largeContent := make([]byte, 1_000_000)
	req := PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
		Args: map[string]any{
			"file_path": "/tmp/big.bin",
			"content":   string(largeContent),
		},
	}
	got := FormatRequest(req)
	if got == "" {
		t.Error("expected non-empty result for large write_file")
	}
}
