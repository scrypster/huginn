package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── Global / parse ─────────────────────────────────────────────────────────────

func TestGlobal_ReturnsNonNil(t *testing.T) {
	c := Global()
	if c == nil {
		t.Fatal("Global() returned nil")
	}
}

func TestGlobal_Singleton(t *testing.T) {
	a := Global()
	b := Global()
	if a != b {
		t.Error("Global() must return the same pointer on every call")
	}
}

// ── All ────────────────────────────────────────────────────────────────────────

func TestAll_NonEmpty(t *testing.T) {
	entries := Global().All()
	if len(entries) == 0 {
		t.Fatal("All() returned empty slice — catalog.json must have at least one entry")
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	c := Global()
	a := c.All()
	b := c.All()
	// Mutating the returned slice must not affect subsequent calls.
	if len(a) > 0 {
		a[0].Name = "MUTATED"
	}
	b2 := c.All()
	if len(b2) > 0 && b2[0].Name == "MUTATED" {
		t.Error("All() must return an independent copy, not a reference to the internal slice")
	}
	_ = b
}

func TestAll_ExpectedProviders(t *testing.T) {
	c := Global()
	want := []string{
		"datadog", "splunk", "pagerduty", "newrelic", "elastic", "grafana",
		"crowdstrike", "terraform", "servicenow", "notion", "airtable",
		"hubspot", "zendesk", "asana", "monday",
	}
	for _, id := range want {
		if _, ok := c.Get(id); !ok {
			t.Errorf("missing expected provider %q in catalog", id)
		}
	}
}

// ── Get ────────────────────────────────────────────────────────────────────────

func TestGet_KnownProvider(t *testing.T) {
	entry, ok := Global().Get("datadog")
	if !ok {
		t.Fatal("Get(\"datadog\") returned ok=false")
	}
	if entry.Name == "" {
		t.Error("entry.Name must not be empty")
	}
	if entry.Icon == "" {
		t.Error("entry.Icon must not be empty")
	}
	if entry.IconColor == "" {
		t.Error("entry.IconColor must not be empty")
	}
	if entry.DefaultLabel == "" {
		t.Error("entry.DefaultLabel must not be empty")
	}
}

func TestGet_UnknownProvider(t *testing.T) {
	_, ok := Global().Get("does_not_exist")
	if ok {
		t.Error("Get(\"does_not_exist\") returned ok=true — expected false")
	}
}

func TestGet_ReturnsCopy(t *testing.T) {
	c := Global()
	e1, _ := c.Get("datadog")
	e1.Name = "MUTATED"
	e2, _ := c.Get("datadog")
	if e2.Name == "MUTATED" {
		t.Error("Get() must return an independent copy, not a reference to the internal entry")
	}
}

// ── Field invariants ───────────────────────────────────────────────────────────

// TestFieldInvariants checks that every field across every entry satisfies the
// invariants required by the generic credential handlers:
//   - key is non-empty
//   - type is one of the supported values
//   - stored_in is either "creds" or "metadata"
func TestFieldInvariants(t *testing.T) {
	validTypes := map[string]bool{
		"text": true, "password": true, "url": true,
		"email": true, "select": true, "subdomain": true,
	}
	validStoredIn := map[string]bool{
		"creds": true, "metadata": true,
	}

	for _, entry := range Global().All() {
		if len(entry.Fields) == 0 {
			t.Errorf("provider %q: must have at least one field", entry.ID)
		}
		for _, f := range entry.Fields {
			if f.Key == "" {
				t.Errorf("provider %q: field has empty key", entry.ID)
			}
			if !validTypes[f.Type] {
				t.Errorf("provider %q field %q: invalid type %q (want one of: text, password, url, email, select, subdomain)",
					entry.ID, f.Key, f.Type)
			}
			if !validStoredIn[f.StoredIn] {
				t.Errorf("provider %q field %q: invalid stored_in %q (want \"creds\" or \"metadata\")",
					entry.ID, f.Key, f.StoredIn)
			}
			if f.Label == "" {
				t.Errorf("provider %q field %q: Label must not be empty", entry.ID, f.Key)
			}
			// Select fields must have at least one option.
			if f.Type == "select" && len(f.Options) == 0 {
				t.Errorf("provider %q field %q: type=select requires at least one option", entry.ID, f.Key)
			}
		}
	}
}

// TestRequiredFieldsHaveNonEmptyPlaceholderOrHelp verifies that required fields
// give users enough guidance to fill them in correctly.
func TestRequiredFieldsHaveNonEmptyPlaceholderOrHelp(t *testing.T) {
	for _, entry := range Global().All() {
		for _, f := range entry.Fields {
			if !f.Required {
				continue
			}
			// Select fields use options instead of placeholder — skip.
			if f.Type == "select" {
				continue
			}
			if f.Placeholder == "" && f.HelpText == "" {
				t.Errorf("provider %q field %q: required field must have placeholder or help_text",
					entry.ID, f.Key)
			}
		}
	}
}

// ── JSON validity ──────────────────────────────────────────────────────────────

// TestCatalogJSONValidity re-parses the embedded bytes independently of the
// global singleton to confirm the raw JSON is structurally valid.
func TestCatalogJSONValidity(t *testing.T) {
	if len(catalogJSON) == 0 {
		t.Fatal("embedded catalogJSON is empty")
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(catalogJSON, &raw); err != nil {
		t.Fatalf("catalog.json is not valid JSON: %v", err)
	}
	if len(raw) == 0 {
		t.Error("catalog.json contains an empty array")
	}
}

// TestNoDuplicateIDs ensures catalog.json has no two entries with the same id.
func TestNoDuplicateIDs(t *testing.T) {
	seen := map[string]int{}
	for _, entry := range Global().All() {
		seen[entry.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate catalog entry id %q (appears %d times)", id, count)
		}
	}
}

// ── StoredIn distribution ──────────────────────────────────────────────────────

// TestEachProviderHasCredsField verifies every provider stores at least one
// secret field so that StoreAPIKeyConnection always receives a non-empty creds map.
func TestEachProviderHasCredsField(t *testing.T) {
	for _, entry := range Global().All() {
		hasCreds := false
		for _, f := range entry.Fields {
			if f.StoredIn == "creds" {
				hasCreds = true
				break
			}
		}
		if !hasCreds {
			t.Errorf("provider %q: has no fields with stored_in=\"creds\" — every provider must store at least one secret",
				entry.ID)
		}
	}
}

// TestIconColorFormat verifies icon colors are hex strings beginning with '#'.
func TestIconColorFormat(t *testing.T) {
	for _, entry := range Global().All() {
		if !strings.HasPrefix(entry.IconColor, "#") {
			t.Errorf("provider %q: icon_color %q must start with '#'", entry.ID, entry.IconColor)
		}
		// 4-char (#rgb) or 7-char (#rrggbb) — both valid.
		l := len(entry.IconColor)
		if l != 4 && l != 7 {
			t.Errorf("provider %q: icon_color %q must be #rgb or #rrggbb format (got length %d)",
				entry.ID, entry.IconColor, l)
		}
	}
}

// ── parse error paths ──────────────────────────────────────────────────────────

func TestParse_InvalidJSON(t *testing.T) {
	_, err := parse([]byte(`not json`))
	if err == nil {
		t.Error("parse() should return an error for invalid JSON")
	}
}

func TestParse_DuplicateIDs(t *testing.T) {
	dupJSON := []byte(`[
		{"id":"dup","name":"A","fields":[{"key":"k","label":"K","type":"text","required":true,"stored_in":"creds"}]},
		{"id":"dup","name":"B","fields":[{"key":"k","label":"K","type":"text","required":true,"stored_in":"creds"}]}
	]`)
	_, err := parse(dupJSON)
	if err == nil {
		t.Error("parse() should return an error for duplicate IDs")
	}
}

func TestParse_EmptyArray(t *testing.T) {
	c, err := parse([]byte(`[]`))
	if err != nil {
		t.Fatalf("parse([]) unexpected error: %v", err)
	}
	if len(c.All()) != 0 {
		t.Error("empty array should yield empty catalog")
	}
}

// ── Validation config ──────────────────────────────────────────────────────────

// TestValidationAvailable ensures every provider in the catalog exposes a test
// endpoint — the catalog should only include providers that can be validated.
func TestValidationAvailable(t *testing.T) {
	for _, entry := range Global().All() {
		if !entry.Validation.Available {
			t.Errorf("provider %q: validation.available must be true (catalog only includes testable providers)",
				entry.ID)
		}
		if entry.Validation.Description == "" {
			t.Errorf("provider %q: validation.description must not be empty", entry.ID)
		}
	}
}
