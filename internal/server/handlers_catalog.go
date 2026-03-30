package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/connections/catalog"
)

// handleGetConnectionsCatalog serves the credential provider catalog.
// GET /api/v1/connections/catalog
// Response: JSON array of catalog.Entry objects.
func (s *Server) handleGetConnectionsCatalog(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, catalog.Global().All())
}

// handleSaveCredential is the generic credential save handler.
// POST /api/v1/credentials/{provider}
//
// Body: flat JSON object whose keys match the catalog FieldDef keys for the
// provider, plus an optional "label" key for the connection display name.
//
// The handler:
//  1. Looks up the provider in the catalog.
//  2. Decodes the request body into a flat map[string]string.
//  3. Applies defaults for optional fields that are absent or empty.
//  4. Validates required fields.
//  5. Calls the registered Validator (live connectivity check).
//  6. Splits fields into creds (SecretStore) vs metadata (Connection.Metadata).
//  7. Calls StoreAPIKeyConnection.
//
// Response: {"id":"<uuid>","provider":"<id>","account_label":"<label>"}
func (s *Server) handleSaveCredential(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	entry, ok := catalog.Global().Get(providerID)
	if !ok {
		jsonError(w, http.StatusBadRequest, "unknown provider: "+providerID)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var raw map[string]string
	if err := json.Unmarshal(body, &raw); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if raw == nil {
		raw = make(map[string]string)
	}

	// Apply catalog-defined defaults for optional absent/empty fields.
	for _, f := range entry.Fields {
		if f.Default != "" && strings.TrimSpace(raw[f.Key]) == "" {
			raw[f.Key] = f.Default
		}
	}

	// Validate required fields.
	for _, f := range entry.Fields {
		if f.Required && strings.TrimSpace(raw[f.Key]) == "" {
			jsonError(w, http.StatusBadRequest, f.Key+" is required")
			return
		}
	}

	// Determine the connection label.
	label := strings.TrimSpace(raw["label"])
	if label == "" {
		label = entry.DefaultLabel
	}

	// Only credentials and database providers support credential storage.
	if entry.Type != "credentials" && entry.Type != "database" {
		jsonError(w, http.StatusBadRequest, "provider does not support credential storage")
		return
	}

	// Live connectivity check via the registered validator.
	validator, hasValidator := s.credValidators.Get(providerID)
	if hasValidator {
		if err := validator.Validate(r.Context(), raw); err != nil {
			jsonError(w, http.StatusBadRequest, "credential validation failed: "+err.Error())
			return
		}
	}

	// Guard storage: connections must be configured.
	if s.connMgr == nil {
		jsonError(w, http.StatusServiceUnavailable, "connections not configured")
		return
	}

	// Split catalog fields into creds (secrets) vs metadata.
	creds := make(map[string]string)
	meta := make(map[string]string)
	for _, f := range entry.Fields {
		val := raw[f.Key]
		if val == "" {
			continue
		}
		switch f.StoredIn {
		case "creds":
			creds[f.Key] = val
		case "metadata":
			meta[f.Key] = val
		}
	}
	if len(meta) == 0 {
		meta = nil
	}

	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.Provider(providerID),
		label,
		meta,
		creds,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{
		"id":            conn.ID,
		"provider":      string(conn.Provider),
		"account_label": conn.AccountLabel,
	})
}

// handleTestCredential is the generic credential test handler.
// POST /api/v1/credentials/{provider}/test
//
// Body: same flat JSON as handleSaveCredential (without "label").
// Response: always HTTP 200 with {"ok":true} or {"ok":false,"error":"..."}.
// The caller must inspect the "ok" field — HTTP status is never used to
// convey validation failure so partial credentials don't trigger retries.
func (s *Server) handleTestCredential(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	entry, ok := catalog.Global().Get(providerID)
	if !ok {
		jsonOK(w, map[string]any{"ok": false, "error": "unknown provider: " + providerID})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "read body: " + err.Error()})
		return
	}
	var raw map[string]string
	if err := json.Unmarshal(body, &raw); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if raw == nil {
		raw = make(map[string]string)
	}

	// Apply defaults so optional fields with defaults work correctly in test mode.
	for _, f := range entry.Fields {
		if f.Default != "" && strings.TrimSpace(raw[f.Key]) == "" {
			raw[f.Key] = f.Default
		}
	}

	validator, ok := s.credValidators.Get(providerID)
	if !ok {
		jsonOK(w, map[string]any{"ok": false, "error": "no validator registered for provider: " + providerID})
		return
	}
	if err := validator.Validate(r.Context(), raw); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}
