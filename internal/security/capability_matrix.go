package security

import (
	"fmt"
	"slices"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/connections/catalog"
)

const (
	ReasonMissingConnectionID       = "missing_connection_id"
	ReasonDuplicateConnectionID     = "duplicate_connection_id"
	ReasonUnknownConnectionID       = "unknown_connection_id"
	ReasonWildcardProviderForbidden = "wildcard_provider_forbidden"
	ReasonProviderMismatch          = "provider_mismatch"
	ReasonSingleAccountProvider     = "single_account_provider"
)

type ProviderCapability struct {
	Provider     string `json:"provider"`
	DisplayName  string `json:"display_name,omitempty"`
	Category     string `json:"category,omitempty"`
	Type         string `json:"type,omitempty"`
	MultiAccount bool   `json:"multi_account"`
}

type ConnectionCapability struct {
	ConnectionID string `json:"connection_id"`
	Provider     string `json:"provider"`
	AccountLabel string `json:"account_label,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
}

type CapabilityMatrix struct {
	Connections []ConnectionCapability `json:"connections"`
	Providers   []ProviderCapability   `json:"providers"`

	connByID        map[string]connections.Connection
	providerByName  map[string]ProviderCapability
	connectionByKey map[string]ConnectionCapability
}

type ToolbeltDecision struct {
	Entry            agents.ToolbeltEntry `json:"entry"`
	Allowed          bool                 `json:"allowed"`
	ReasonCode       string               `json:"reason_code,omitempty"`
	Reason           string               `json:"reason,omitempty"`
	ResolvedProvider string               `json:"resolved_provider,omitempty"`
}

type ValidationResult struct {
	Valid     bool               `json:"valid"`
	Decisions []ToolbeltDecision `json:"decisions"`
}

func (r ValidationResult) FirstDenied() (ToolbeltDecision, bool) {
	for _, d := range r.Decisions {
		if !d.Allowed {
			return d, true
		}
	}
	return ToolbeltDecision{}, false
}

type providerCatalog interface {
	Get(id string) (catalog.Entry, bool)
}

func NewCapabilityMatrix(conns []connections.Connection) CapabilityMatrix {
	return NewCapabilityMatrixWithCatalog(conns, catalog.Global())
}

func NewCapabilityMatrixWithCatalog(conns []connections.Connection, cat providerCatalog) CapabilityMatrix {
	connByID := make(map[string]connections.Connection, len(conns))
	connectionByKey := make(map[string]ConnectionCapability, len(conns))
	providerByName := map[string]ProviderCapability{}

	connectionsOut := make([]ConnectionCapability, 0, len(conns))
	for _, conn := range conns {
		id := strings.TrimSpace(conn.ID)
		if id == "" {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(string(conn.Provider)))
		if provider == "" {
			continue
		}
		connByID[id] = conn
		capConn := ConnectionCapability{
			ConnectionID: id,
			Provider:     provider,
			AccountLabel: conn.AccountLabel,
			AccountID:    conn.AccountID,
		}
		connectionByKey[id] = capConn
		connectionsOut = append(connectionsOut, capConn)

		if _, exists := providerByName[provider]; !exists {
			policy := ProviderCapability{
				Provider:     provider,
				MultiAccount: true,
			}
			if cat != nil {
				if entry, ok := cat.Get(provider); ok {
					policy.DisplayName = entry.Name
					policy.Category = entry.Category
					policy.Type = entry.Type
					policy.MultiAccount = entry.MultiAccount
				}
			}
			providerByName[provider] = policy
		}
	}

	slices.SortFunc(connectionsOut, func(a, b ConnectionCapability) int {
		if c := strings.Compare(a.Provider, b.Provider); c != 0 {
			return c
		}
		if c := strings.Compare(strings.ToLower(a.AccountLabel), strings.ToLower(b.AccountLabel)); c != 0 {
			return c
		}
		return strings.Compare(a.ConnectionID, b.ConnectionID)
	})

	providersOut := make([]ProviderCapability, 0, len(providerByName))
	for _, p := range providerByName {
		providersOut = append(providersOut, p)
	}
	slices.SortFunc(providersOut, func(a, b ProviderCapability) int {
		return strings.Compare(a.Provider, b.Provider)
	})

	return CapabilityMatrix{
		Connections:     connectionsOut,
		Providers:       providersOut,
		connByID:        connByID,
		providerByName:  providerByName,
		connectionByKey: connectionByKey,
	}
}

func (m CapabilityMatrix) ValidateToolbelt(tb []agents.ToolbeltEntry) ValidationResult {
	if len(tb) == 0 {
		return ValidationResult{Valid: true, Decisions: []ToolbeltDecision{}}
	}

	seenEntries := map[string]bool{}
	seenProviders := map[string]int{}
	decisions := make([]ToolbeltDecision, 0, len(tb))
	valid := true

	for _, entry := range tb {
		decision := ToolbeltDecision{
			Entry: entry,
		}
		connID := strings.TrimSpace(entry.ConnectionID)
		if connID == "" {
			valid = false
			decision.ReasonCode = ReasonMissingConnectionID
			decision.Reason = "toolbelt entry requires connection_id"
			decisions = append(decisions, decision)
			continue
		}
		profile := strings.TrimSpace(entry.Profile)
		entryKey := connID + "::" + profile
		if seenEntries[entryKey] {
			valid = false
			decision.ReasonCode = ReasonDuplicateConnectionID
			decision.Reason = fmt.Sprintf("connection_id/profile %q is assigned multiple times", entryKey)
			decisions = append(decisions, decision)
			continue
		}
		seenEntries[entryKey] = true
		provider := strings.ToLower(strings.TrimSpace(entry.Provider))
		if provider == "*" {
			valid = false
			decision.ReasonCode = ReasonWildcardProviderForbidden
			decision.Reason = "wildcard provider is not allowed in toolbelt assignments"
			decisions = append(decisions, decision)
			continue
		}

		// System tool entries (system:<tool>) are not backed by connStore and may
		// legitimately appear multiple times with distinct profiles.
		if strings.HasPrefix(connID, "system:") {
			if provider == "" {
				provider = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(connID, "system:")))
			}
			decision.ResolvedProvider = provider
			decision.Allowed = true
			decisions = append(decisions, decision)
			continue
		}

		conn, ok := m.connByID[connID]
		if !ok {
			valid = false
			decision.ReasonCode = ReasonUnknownConnectionID
			decision.Reason = fmt.Sprintf("connection_id %q is not available", connID)
			decisions = append(decisions, decision)
			continue
		}

		resolvedProvider := strings.ToLower(strings.TrimSpace(string(conn.Provider)))
		decision.ResolvedProvider = resolvedProvider
		if provider == "" {
			provider = resolvedProvider
		}
		if provider != resolvedProvider {
			valid = false
			decision.ReasonCode = ReasonProviderMismatch
			decision.Reason = fmt.Sprintf("provider %q does not match connection provider %q", provider, resolvedProvider)
			decisions = append(decisions, decision)
			continue
		}

		if policy, ok := m.providerByName[resolvedProvider]; ok && !policy.MultiAccount && seenProviders[resolvedProvider] > 0 {
			valid = false
			decision.ReasonCode = ReasonSingleAccountProvider
			decision.Reason = fmt.Sprintf("provider %q allows only one assigned connection", resolvedProvider)
			decisions = append(decisions, decision)
			continue
		}
		seenProviders[resolvedProvider]++
		decision.Allowed = true
		decisions = append(decisions, decision)
	}

	return ValidationResult{Valid: valid, Decisions: decisions}
}
