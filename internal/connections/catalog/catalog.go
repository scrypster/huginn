// Package catalog provides the embedded, provider-agnostic credential catalog
// for Huginn's connection system.
//
// The catalog describes every API-key provider that Huginn supports: their
// display metadata, the ordered list of fields that must be collected from the
// user, and how each field should be stored (secret credentials vs plain
// metadata).  It is intentionally free of HTTP or validation logic — that
// lives in the server layer via the Validator/Registry types.
//
// Usage:
//
//	entry, ok := catalog.Global().Get("datadog")
//	if ok {
//	    for _, f := range entry.Fields {
//	        fmt.Println(f.Key, f.StoredIn)
//	    }
//	}
package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

//go:embed catalog.json
var catalogJSON []byte

// ── Types ─────────────────────────────────────────────────────────────────────

// Entry describes a single credential provider.
type Entry struct {
	// ID is the canonical provider identifier — must match the Provider constant
	// in internal/connections (e.g., "datadog", "splunk").
	ID string `json:"id"`

	// Name is the human-readable provider name shown in the UI (e.g., "Datadog").
	Name string `json:"name"`

	// Description is a short sentence shown as a sub-heading on the card.
	Description string `json:"description"`

	// Category groups the provider in the connections UI
	// (e.g., "observability", "productivity", "cloud").
	Category string `json:"category"`

	// Icon is a 1–3 character abbreviation used for the badge
	// (e.g., "DD", "SP", "NR").
	Icon string `json:"icon"`

	// IconColor is the hex background color for the icon badge (e.g., "#632ca6").
	IconColor string `json:"icon_color"`

	// Type classifies the authentication model for this provider.
	// Valid values: "credentials", "oauth", "system", "database", "coming_soon".
	// Drives both the frontend form routing and the server-side validation path.
	Type string `json:"type"`

	// DefaultLabel is the value pre-filled in the "Label" field of the
	// credential form (e.g., "Datadog").  Users can override it.
	DefaultLabel string `json:"default_label"`

	// MultiAccount indicates whether more than one credential set can be saved
	// for this provider simultaneously.
	MultiAccount bool `json:"multi_account"`

	// Fields is the ordered list of form fields that must be collected.
	Fields []FieldDef `json:"fields"`

	// Validation describes whether a live connectivity test is available and
	// what behavior to expect.
	Validation ValidationConfig `json:"validation"`
}

// FieldDef describes a single form field within an Entry.
type FieldDef struct {
	// Key is the JSON payload key used when POSTing credentials
	// (e.g., "api_key", "url").
	Key string `json:"key"`

	// Label is the human-readable field name shown as the form label.
	Label string `json:"label"`

	// Type controls the HTML input type rendered by the frontend.
	// Valid values: "text", "password", "url", "email", "select", "subdomain".
	Type string `json:"type"`

	// Required indicates that this field must be non-empty before saving.
	Required bool `json:"required"`

	// StoredIn determines which backend map this field's value is placed in.
	// "creds"    → stored in the encrypted SecretStore (API keys, tokens, passwords).
	// "metadata" → stored in the Connection.Metadata map (URLs, usernames, IDs).
	StoredIn string `json:"stored_in"`

	// Placeholder is an example value shown in the input when empty.
	Placeholder string `json:"placeholder,omitempty"`

	// HelpText is a short sentence shown below the input to guide the user.
	HelpText string `json:"help_text,omitempty"`

	// Default is the value pre-filled when the form is first opened.
	// Only meaningful for optional fields (e.g., a default API base URL).
	Default string `json:"default,omitempty"`

	// Options is the list of choices for "select" type fields.
	// If the last option has Value "__custom__", the frontend renders a
	// secondary free-form URL input when that option is chosen.
	Options []SelectOption `json:"options,omitempty"`
}

// SelectOption is a single choice within a "select" type field.
type SelectOption struct {
	// Label is the display text shown in the dropdown.
	Label string `json:"label"`

	// Value is the raw value sent in the POST payload.
	// The sentinel "__custom__" signals that a secondary free-text input
	// should be shown; the user's typed URL replaces "__custom__" before
	// the payload is submitted.
	Value string `json:"value"`
}

// ValidationConfig describes the live connectivity test for an entry.
type ValidationConfig struct {
	// Available indicates that the server can perform a live connectivity check
	// against this provider's API.
	Available bool `json:"available"`

	// Description is a short human-readable phrase shown next to the
	// "Test Connection" button (e.g., "Verifies API key against /api/v1/validate").
	Description string `json:"description,omitempty"`
}

// ── Catalog ───────────────────────────────────────────────────────────────────

// Catalog is the parsed, in-memory representation of catalog.json.
// All public methods are safe for concurrent use after initialisation.
type Catalog struct {
	entries []Entry
	byID    map[string]*Entry
}

// All returns a copy of the full catalog slice sorted alphabetically by name.
func (c *Catalog) All() []Entry {
	out := make([]Entry, len(c.entries))
	copy(out, c.entries)
	slices.SortFunc(out, func(a, b Entry) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

// Get returns the Entry for the given provider ID together with a boolean
// that reports whether the provider was found.
func (c *Catalog) Get(id string) (Entry, bool) {
	e, ok := c.byID[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// ── Singleton ─────────────────────────────────────────────────────────────────

var (
	globalOnce    sync.Once
	globalCatalog *Catalog
	globalErr     error
)

// Global returns the process-wide singleton Catalog, parsing catalog.json
// exactly once.  It panics on the first call if the embedded JSON is invalid —
// that would indicate a broken build, not a runtime error.
func Global() *Catalog {
	globalOnce.Do(func() {
		globalCatalog, globalErr = parse(catalogJSON)
	})
	if globalErr != nil {
		panic(fmt.Sprintf("catalog: failed to parse embedded catalog.json: %v", globalErr))
	}
	return globalCatalog
}

// parse unmarshals raw JSON into a Catalog and builds the by-ID index.
// Exported for testing with synthetic JSON.
func parse(data []byte) (*Catalog, error) {
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	byID := make(map[string]*Entry, len(entries))
	for i := range entries {
		e := &entries[i]
		if _, dup := byID[e.ID]; dup {
			return nil, fmt.Errorf("duplicate entry id %q", e.ID)
		}
		byID[e.ID] = e
	}
	return &Catalog{entries: entries, byID: byID}, nil
}
