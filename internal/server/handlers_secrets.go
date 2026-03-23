package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/secrets"
)

// handleGetSecrets returns the set/unset status of all known secret slots.
// It never returns actual key values.
//
// GET /api/v1/secrets
func (s *Server) handleGetSecrets(w http.ResponseWriter, r *http.Request) {
	result := secrets.Default().List(secrets.KnownSlots)
	jsonOK(w, result)
}

// handleSetSecret stores a secret value for the given slot, promotes it to
// secure storage, and saves the "keyring:huginn:<slot>" reference to config.json.
//
// PUT /api/v1/secrets/{slot}
// Body: {"value": "sk-ant-..."}
func (s *Server) handleSetSecret(w http.ResponseWriter, r *http.Request) {
	slot := r.PathValue("slot")
	if !isKnownSlot(slot) {
		jsonError(w, 400, fmt.Sprintf("unknown secret slot %q; valid slots: %v", slot, secrets.KnownSlots))
		return
	}

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if body.Value == "" {
		jsonError(w, 400, "value must not be empty; use DELETE to clear a secret")
		return
	}

	ref, err := s.storeAPIKey(slot, body.Value)
	if err != nil {
		jsonError(w, 500, "store secret: "+err.Error())
		return
	}

	// Update the in-memory config reference and persist to disk.
	s.mu.Lock()
	applySecretRef(&s.cfg, slot, ref)
	s.mu.Unlock()

	if err := s.saveConfig(&s.cfg); err != nil {
		jsonError(w, 500, "save config: "+err.Error())
		return
	}

	// For LLM provider slots, update the live BackendCache immediately so
	// in-flight and future agent requests use the new key without a restart.
	if slot == "anthropic" || slot == "openai" || slot == "openrouter" {
		if s.orch != nil {
			s.orch.UpdateFallbackAPIKey(ref)
		}
	}

	jsonOK(w, map[string]any{
		"slot":    slot,
		"storage": secrets.Default().Status(slot).Storage,
	})
}

// handleDeleteSecret removes a secret from all storage backends and clears
// the reference from config.json.
//
// DELETE /api/v1/secrets/{slot}
func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	slot := r.PathValue("slot")
	if !isKnownSlot(slot) {
		jsonError(w, 400, fmt.Sprintf("unknown secret slot %q", slot))
		return
	}

	if err := secrets.Delete(slot); err != nil {
		jsonError(w, 500, "delete secret: "+err.Error())
		return
	}

	s.mu.Lock()
	applySecretRef(&s.cfg, slot, "")
	s.mu.Unlock()

	if err := s.saveConfig(&s.cfg); err != nil {
		jsonError(w, 500, "save config: "+err.Error())
		return
	}

	jsonOK(w, map[string]any{"cleared": true, "slot": slot})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func isKnownSlot(slot string) bool {
	for _, s := range secrets.KnownSlots {
		if s == slot {
			return true
		}
	}
	return false
}

// applySecretRef writes ref into the correct field of cfg for the given slot.
// Called under s.mu to keep in-memory state consistent with what gets saved.
func applySecretRef(cfg *config.Config, slot, ref string) {
	switch slot {
	case "anthropic", "openai", "openrouter":
		cfg.Backend.APIKey = ref
	case "brave":
		cfg.BraveAPIKey = ref
	case "google":
		cfg.Integrations.Google.ClientSecret = ref
	case "github":
		cfg.Integrations.GitHub.ClientSecret = ref
	case "slack":
		cfg.Integrations.Slack.ClientSecret = ref
	case "jira":
		cfg.Integrations.Jira.ClientSecret = ref
	case "bitbucket":
		cfg.Integrations.Bitbucket.ClientSecret = ref
	}
}
