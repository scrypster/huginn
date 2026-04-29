package security

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/connections/catalog"
)

type fakeCatalog struct {
	entries map[string]catalog.Entry
}

func (f fakeCatalog) Get(id string) (catalog.Entry, bool) {
	e, ok := f.entries[id]
	return e, ok
}

func TestNewCapabilityMatrix_SortsDeterministically(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog([]connections.Connection{
		{ID: "conn-b", Provider: connections.ProviderSlack, AccountLabel: "Z Team"},
		{ID: "conn-a", Provider: connections.ProviderGitHub, AccountLabel: "Repo Bot"},
	}, nil)

	if len(matrix.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(matrix.Connections))
	}
	if got := matrix.Connections[0].ConnectionID; got != "conn-a" {
		t.Fatalf("first connection = %q, want conn-a", got)
	}
	if got := matrix.Connections[1].ConnectionID; got != "conn-b" {
		t.Fatalf("second connection = %q, want conn-b", got)
	}
}

func TestValidateToolbelt_DeniesUnknownConnectionID(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog(nil, nil)
	result := matrix.ValidateToolbelt([]agents.ToolbeltEntry{
		{ConnectionID: "stale-conn", Provider: "github"},
	})
	if result.Valid {
		t.Fatal("expected invalid result for unknown connection id")
	}
	denied, ok := result.FirstDenied()
	if !ok {
		t.Fatal("expected denied decision")
	}
	if denied.ReasonCode != ReasonUnknownConnectionID {
		t.Fatalf("reason_code = %q, want %q", denied.ReasonCode, ReasonUnknownConnectionID)
	}
}

func TestValidateToolbelt_DeniesProviderMismatch(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog([]connections.Connection{
		{ID: "conn-gh", Provider: connections.ProviderGitHub},
	}, nil)
	result := matrix.ValidateToolbelt([]agents.ToolbeltEntry{
		{ConnectionID: "conn-gh", Provider: "slack"},
	})
	if result.Valid {
		t.Fatal("expected invalid result for provider mismatch")
	}
	denied, _ := result.FirstDenied()
	if denied.ReasonCode != ReasonProviderMismatch {
		t.Fatalf("reason_code = %q, want %q", denied.ReasonCode, ReasonProviderMismatch)
	}
}

func TestValidateToolbelt_DeniesDuplicateConnectionID(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog([]connections.Connection{
		{ID: "conn-gh", Provider: connections.ProviderGitHub},
	}, nil)
	result := matrix.ValidateToolbelt([]agents.ToolbeltEntry{
		{ConnectionID: "conn-gh", Provider: "github"},
		{ConnectionID: "conn-gh", Provider: "github"},
	})
	if result.Valid {
		t.Fatal("expected invalid result for duplicate connection id")
	}
	if len(result.Decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(result.Decisions))
	}
	if !result.Decisions[0].Allowed {
		t.Fatal("first decision should be allowed")
	}
	if result.Decisions[1].ReasonCode != ReasonDuplicateConnectionID {
		t.Fatalf("second reason_code = %q, want %q", result.Decisions[1].ReasonCode, ReasonDuplicateConnectionID)
	}
}

func TestValidateToolbelt_AllowsSystemEntries(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog(nil, nil)
	result := matrix.ValidateToolbelt([]agents.ToolbeltEntry{
		{ConnectionID: "system:github", Provider: "github_cli"},
	})
	if !result.Valid {
		t.Fatalf("expected valid result, got %+v", result)
	}
	if len(result.Decisions) != 1 || !result.Decisions[0].Allowed {
		t.Fatalf("expected allowed decision, got %+v", result.Decisions)
	}
}

func TestValidateToolbelt_AllowsSystemEntriesWithDistinctProfiles(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog(nil, nil)
	result := matrix.ValidateToolbelt([]agents.ToolbeltEntry{
		{ConnectionID: "system:aws", Provider: "aws", Profile: "prod"},
		{ConnectionID: "system:aws", Provider: "aws", Profile: "staging"},
	})
	if !result.Valid {
		t.Fatalf("expected valid result, got %+v", result)
	}
	if len(result.Decisions) != 2 || !result.Decisions[0].Allowed || !result.Decisions[1].Allowed {
		t.Fatalf("expected both decisions allowed, got %+v", result.Decisions)
	}
}

func TestValidateToolbelt_DeniesSingleAccountProviderMultipleAssignments(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog([]connections.Connection{
		{ID: "conn-gh-1", Provider: connections.ProviderGitHub},
		{ID: "conn-gh-2", Provider: connections.ProviderGitHub},
	}, fakeCatalog{
		entries: map[string]catalog.Entry{
			"github": {
				ID:           "github",
				Name:         "GitHub",
				MultiAccount: false,
			},
		},
	})
	result := matrix.ValidateToolbelt([]agents.ToolbeltEntry{
		{ConnectionID: "conn-gh-1", Provider: "github"},
		{ConnectionID: "conn-gh-2", Provider: "github"},
	})
	if result.Valid {
		t.Fatal("expected invalid result for single-account provider")
	}
	if len(result.Decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(result.Decisions))
	}
	if !result.Decisions[0].Allowed {
		t.Fatal("first decision should be allowed")
	}
	if result.Decisions[1].ReasonCode != ReasonSingleAccountProvider {
		t.Fatalf("second reason_code = %q, want %q", result.Decisions[1].ReasonCode, ReasonSingleAccountProvider)
	}
}

func TestValidateToolbelt_AllowsMatchingProvider(t *testing.T) {
	matrix := NewCapabilityMatrixWithCatalog([]connections.Connection{
		{ID: "conn-gh", Provider: connections.ProviderGitHub},
	}, nil)
	result := matrix.ValidateToolbelt([]agents.ToolbeltEntry{
		{ConnectionID: "conn-gh", Provider: "github"},
	})
	if !result.Valid {
		t.Fatal("expected valid result")
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(result.Decisions))
	}
	if !result.Decisions[0].Allowed {
		t.Fatal("expected decision to be allowed")
	}
}
