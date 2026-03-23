package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestConnectAgentVault_EmptyPath_ReturnsWarning verifies that when the
// orchestrator has no muninnCfgPath configured, connectAgentVault returns a
// non-empty warning string and a valid (degraded) sessionReg. The session
// continues without memory rather than failing hard.
func TestConnectAgentVault_EmptyPath_ReturnsWarning(t *testing.T) {
	// &Orchestrator{} leaves muninnCfgPath="" which triggers the failure path.
	o := &Orchestrator{}
	sharedReg := tools.NewRegistry()
	ag := &agents.Agent{Name: "test-agent", MemoryEnabled: true, VaultName: "test-vault"}

	vr := o.connectAgentVault(context.Background(), ag, sharedReg)
	defer vr.cancel()

	if vr.warning == "" {
		t.Fatal("expected warning when muninnCfgPath is empty, got empty string")
	}
	if !strings.Contains(vr.warning, "muninn config path not set") {
		t.Errorf("unexpected warning text: %q, want to contain 'muninn config path not set'", vr.warning)
	}
	// sessionReg must be valid for graceful degradation — session continues without memory.
	if vr.sessionReg == nil {
		t.Error("sessionReg must not be nil even on vault failure (graceful degradation)")
	}
	// No vault tools registered — the LLM must not be told to use tools that aren't there.
	if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
		t.Error("muninn_recall must not be in sessionReg when vault failed")
	}
}

// TestConnectAgentVault_MemoryDisabled_NoWarning verifies that when the agent
// has MemoryEnabled=false, connectAgentVault returns an empty warning — no
// degraded-mode notification is shown for agents that never had memory enabled.
func TestConnectAgentVault_MemoryDisabled_NoWarning(t *testing.T) {
	o := &Orchestrator{}
	sharedReg := tools.NewRegistry()
	ag := &agents.Agent{Name: "no-memory-agent", MemoryEnabled: false}

	vr := o.connectAgentVault(context.Background(), ag, sharedReg)
	defer vr.cancel()

	if vr.warning != "" {
		t.Errorf("expected empty warning when MemoryEnabled=false, got %q", vr.warning)
	}
	if vr.sessionReg == nil {
		t.Error("sessionReg must not be nil")
	}
}

// TestConnectAgentVault_NilAgent_NoWarning verifies that a nil agent does not
// trigger a vault connection attempt or a warning — the orchestrator degrades
// silently with no memory.
func TestConnectAgentVault_NilAgent_NoWarning(t *testing.T) {
	o := &Orchestrator{}
	sharedReg := tools.NewRegistry()

	vr := o.connectAgentVault(context.Background(), nil, sharedReg)
	defer vr.cancel()

	if vr.warning != "" {
		t.Errorf("expected empty warning for nil agent, got %q", vr.warning)
	}
}

// TestStreamWarning_EmissionGuard_FiresOnVaultFailure documents and verifies
// the emission guard pattern used in CodeWithAgent, ChatWithAgent, and AgentChat:
//
//	if vr.warning != "" && onEvent != nil {
//	    onEvent(StreamEvent{Type: StreamWarning, Content: "⚠️ ..."})
//	}
//
// This test confirms the full path: vault failure → non-empty warning → onEvent
// called with StreamWarning type and content that includes the failure reason.
func TestStreamWarning_EmissionGuard_FiresOnVaultFailure(t *testing.T) {
	o := &Orchestrator{}
	sharedReg := tools.NewRegistry()
	ag := &agents.Agent{Name: "test-agent", MemoryEnabled: true, VaultName: "test-vault"}

	vr := o.connectAgentVault(context.Background(), ag, sharedReg)
	defer vr.cancel()

	if vr.warning == "" {
		t.Fatal("precondition failed: expected non-empty warning")
	}

	var received []backend.StreamEvent
	onEvent := func(ev backend.StreamEvent) {
		received = append(received, ev)
	}

	// Reproduce the guard that CodeWithAgent / ChatWithAgent / AgentChat use.
	if vr.warning != "" && onEvent != nil {
		onEvent(backend.StreamEvent{
			Type:    backend.StreamWarning,
			Content: fmt.Sprintf("⚠️ Memory vault unavailable: %s. Memory features are disabled for this session.", vr.warning),
		})
	}

	if len(received) != 1 {
		t.Fatalf("expected 1 StreamWarning event, got %d", len(received))
	}
	if received[0].Type != backend.StreamWarning {
		t.Errorf("event type = %v, want backend.StreamWarning", received[0].Type)
	}
	if !strings.Contains(received[0].Content, "muninn config path not set") {
		t.Errorf("event content = %q, want to contain vault failure reason", received[0].Content)
	}
	if !strings.Contains(received[0].Content, "Memory features are disabled") {
		t.Errorf("event content = %q, want to contain degraded-mode message", received[0].Content)
	}
}

// TestStreamWarning_EmissionGuard_SkippedWhenVaultSucceeds verifies the negative
// case: when the agent has no memory configured, vr.warning is empty, and the
// emission guard does NOT call onEvent with StreamWarning.
func TestStreamWarning_EmissionGuard_SkippedWhenVaultSucceeds(t *testing.T) {
	o := &Orchestrator{}
	sharedReg := tools.NewRegistry()
	ag := &agents.Agent{Name: "no-memory-agent", MemoryEnabled: false}

	vr := o.connectAgentVault(context.Background(), ag, sharedReg)
	defer vr.cancel()

	warnCalled := false
	onEvent := func(ev backend.StreamEvent) {
		if ev.Type == backend.StreamWarning {
			warnCalled = true
		}
	}

	// Emission guard — must NOT fire when vr.warning is empty.
	if vr.warning != "" && onEvent != nil {
		onEvent(backend.StreamEvent{Type: backend.StreamWarning})
	}

	if warnCalled {
		t.Error("StreamWarning must NOT be emitted when memory is disabled (vr.warning is empty)")
	}
}

// TestStreamWarning_EmissionGuard_SafeWithNilCallback verifies that passing a
// nil onEvent callback does not panic. All emission sites guard with
// `onEvent != nil` before calling, so this should always be safe.
func TestStreamWarning_EmissionGuard_SafeWithNilCallback(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emission guard panicked with nil onEvent: %v", r)
		}
	}()

	o := &Orchestrator{}
	sharedReg := tools.NewRegistry()
	ag := &agents.Agent{Name: "test-agent", MemoryEnabled: true, VaultName: "test-vault"}

	vr := o.connectAgentVault(context.Background(), ag, sharedReg)
	defer vr.cancel()

	var onEvent func(backend.StreamEvent) // intentionally nil
	if vr.warning != "" && onEvent != nil {
		onEvent(backend.StreamEvent{Type: backend.StreamWarning})
	}
	// No panic = pass
}
