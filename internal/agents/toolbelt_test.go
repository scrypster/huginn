package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolbeltProviders_Empty(t *testing.T) {
	var tb []ToolbeltEntry
	got := ToolbeltProviders(tb)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestToolbeltProviders_Deduplicates(t *testing.T) {
	tb := []ToolbeltEntry{
		{ConnectionID: "c1", Provider: "github"},
		{ConnectionID: "c2", Provider: "github"},
		{ConnectionID: "c3", Provider: "slack"},
	}
	got := ToolbeltProviders(tb)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(got), got)
	}
}

func TestToolbeltProviders_AllUnique(t *testing.T) {
	tb := []ToolbeltEntry{
		{ConnectionID: "c1", Provider: "github"},
		{ConnectionID: "c2", Provider: "slack"},
		{ConnectionID: "c3", Provider: "jira"},
	}
	got := ToolbeltProviders(tb)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(got), got)
	}
}

func TestWatchedProviders_Empty(t *testing.T) {
	got := WatchedProviders(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestWatchedProviders_OnlyApprovalGate(t *testing.T) {
	tb := []ToolbeltEntry{
		{ConnectionID: "c1", Provider: "github", ApprovalGate: false},
		{ConnectionID: "c2", Provider: "slack",  ApprovalGate: true},
	}
	got := WatchedProviders(tb)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if !got["slack"] {
		t.Fatal("expected slack in watched providers")
	}
}

func TestWatchedProviders_MultipleGated(t *testing.T) {
	tb := []ToolbeltEntry{
		{ConnectionID: "c1", Provider: "github", ApprovalGate: true},
		{ConnectionID: "c2", Provider: "slack",  ApprovalGate: true},
		{ConnectionID: "c3", Provider: "jira",   ApprovalGate: false},
	}
	got := WatchedProviders(tb)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestToolbeltEntry_ProfileField(t *testing.T) {
	entry := ToolbeltEntry{
		ConnectionID: "abc",
		Provider:     "aws",
		Profile:      "staging",
		ApprovalGate: false,
	}
	if entry.Profile != "staging" {
		t.Fatalf("expected Profile=staging, got %q", entry.Profile)
	}
	// Verify JSON round-trip
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var out ToolbeltEntry
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if out.Profile != "staging" {
		t.Fatalf("JSON round-trip: expected staging, got %q", out.Profile)
	}
}

func TestToolbeltEntry_ProfileOmitEmpty(t *testing.T) {
	entry := ToolbeltEntry{ConnectionID: "abc", Provider: "github"}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(b), "profile") {
		t.Fatalf("empty Profile should be omitted from JSON, got: %s", b)
	}
}

func TestAllowedProviders_NilReturnsNil(t *testing.T) {
	got := AllowedProviders(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAllowedProviders_EmptyReturnsNil(t *testing.T) {
	got := AllowedProviders([]ToolbeltEntry{})
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAllowedProviders_AllProvidersIncluded(t *testing.T) {
	tb := []ToolbeltEntry{
		{Provider: "github"},
		{Provider: "slack"},
	}
	got := AllowedProviders(tb)
	if !got["github"] || !got["slack"] {
		t.Errorf("expected both providers, got %v", got)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
}

func TestAllowedProviders_IncludesNonGated(t *testing.T) {
	// Unlike WatchedProviders, AllowedProviders includes entries with ApprovalGate: false
	tb := []ToolbeltEntry{
		{Provider: "github", ApprovalGate: false},
	}
	got := AllowedProviders(tb)
	if !got["github"] {
		t.Errorf("expected github in AllowedProviders, got %v", got)
	}
}
