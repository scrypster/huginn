package permissions

import (
	"testing"
	"github.com/scrypster/huginn/internal/tools"
)

func TestGate_WatchedProvider_BypassesSkipAll(t *testing.T) {
	prompted := false
	gate := NewGate(true, func(req PermissionRequest) Decision {
		prompted = true
		return Deny
	})
	gate.SetWatchedProviders(map[string]bool{"github": true})

	req := PermissionRequest{
		ToolName: "github_create_issue",
		Level:    tools.PermWrite,
		Provider: "github",
	}
	result := gate.Check(req)
	if result {
		t.Fatal("expected deny for watched write tool")
	}
	if !prompted {
		t.Fatal("expected prompt to be called for watched provider")
	}
}

func TestGate_UnwatchedProvider_SkipsAll(t *testing.T) {
	prompted := false
	gate := NewGate(true, func(req PermissionRequest) Decision {
		prompted = true
		return Deny
	})
	gate.SetWatchedProviders(map[string]bool{"github": true})

	req := PermissionRequest{
		ToolName: "slack_send_message",
		Level:    tools.PermWrite,
		Provider: "slack",
	}
	result := gate.Check(req)
	if !result {
		t.Fatal("expected allow for non-watched provider in skipAll mode")
	}
	if prompted {
		t.Fatal("expected no prompt for non-watched provider")
	}
}

func TestGate_WatchedProvider_ReadAlwaysAllowed(t *testing.T) {
	gate := NewGate(true, func(_ PermissionRequest) Decision { return Deny })
	gate.SetWatchedProviders(map[string]bool{"github": true})

	req := PermissionRequest{
		ToolName: "github_list_repos",
		Level:    tools.PermRead,
		Provider: "github",
	}
	if !gate.Check(req) {
		t.Fatal("read tools should always be allowed even for watched providers")
	}
}

func TestGate_SetWatchedProviders_ClearsOnNil(t *testing.T) {
	prompted := false
	gate := NewGate(true, func(_ PermissionRequest) Decision {
		prompted = true
		return Deny
	})
	gate.SetWatchedProviders(map[string]bool{"github": true})
	gate.SetWatchedProviders(nil) // clear

	req := PermissionRequest{
		ToolName: "github_create_issue",
		Level:    tools.PermWrite,
		Provider: "github",
	}
	result := gate.Check(req)
	if !result {
		t.Fatal("after clearing watched providers, skipAll should auto-allow")
	}
	if prompted {
		t.Fatal("prompt should not be called after clearing watched providers")
	}
}

func TestGate_WatchedProvider_AllowAll_SessionCaches(t *testing.T) {
	callCount := 0
	gate := NewGate(true, func(_ PermissionRequest) Decision {
		callCount++
		return AllowAll
	})
	gate.SetWatchedProviders(map[string]bool{"github": true})

	req := PermissionRequest{
		ToolName: "github_create_issue",
		Level:    tools.PermWrite,
		Provider: "github",
	}
	// First call prompts
	gate.Check(req)
	// Second call should use session cache
	gate.Check(req)
	if callCount != 1 {
		t.Fatalf("expected prompt called once (AllowAll caches), got %d", callCount)
	}
}
