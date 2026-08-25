package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/threadmgr"
)

func (s *Server) handleGetToken(w http.ResponseWriter, r *http.Request) {
	slog.Info("token requested", "remote_addr", r.RemoteAddr, "request_id", r.Header.Get("X-Request-ID"))
	// Safe: server only binds to 127.0.0.1
	jsonOK(w, map[string]string{"token": s.token})
}

func (s *Server) handleCloudStatus(w http.ResponseWriter, r *http.Request) {
	sat := s.Satellite()
	if sat == nil {
		// Server started without satellite wiring (e.g. test mode).
		jsonOK(w, map[string]any{
			"registered": false,
			"connected":  false,
		})
		return
	}
	jsonOK(w, sat.Status())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	sat := s.Satellite()
	satConnected := sat != nil && sat.Status().Connected

	relayInfo := map[string]any{
		"connected":        satConnected,
		"circuit_breaker":  "closed",
		"outbox_depth":     0,
		"dropped_messages": 0,
	}

	if sat != nil {
		relayInfo["circuit_breaker"] = sat.CircuitBreakerState()
	}

	if s.outbox != nil {
		// Len() performs a Pebble iterator scan — O(n) in outbox size.
		// Acceptable while outbox remains small (typical <10 items).
		// If monitoring polls at high frequency and outbox grows large,
		// consider caching this value with a short TTL.
		if n, err := s.outbox.Len(); err == nil {
			relayInfo["outbox_depth"] = n
		}
	}

	s.mu.Lock()
	hub := s.wsHub
	s.mu.Unlock()
	if hub != nil {
		relayInfo["dropped_messages"] = hub.WSDroppedMessages()
	}

	s.mu.Lock()
	ver := s.version
	s.mu.Unlock()
	if ver == "" {
		ver = "dev"
	}

	stale := false
	if s.staleWatcher != nil {
		stale = s.staleWatcher.IsStale()
	}
	health := map[string]any{
		"status":              "ok",
		"version":             ver,
		"stale":               stale,
		"satellite_connected": satConnected, // preserved for backward compatibility
		"relay":               relayInfo,
	}
	if s.orch != nil {
		if searchHealth, ok := s.orch.SearchHealth(); ok {
			health["search"] = searchHealth
		}
	}
	jsonOK(w, health)
}

// handleRestart handles POST /api/v1/restart.
// It replaces the current process in-place with the on-disk binary via
// syscall.Exec (Unix only). On Windows it returns 503.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !execSupported {
		jsonError(w, http.StatusServiceUnavailable,
			"in-place restart is not supported on this platform; restart huginn manually")
		return
	}
	exePath, err := currentExePath()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "could not resolve binary path: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"restarting"}` + "\n")) //nolint:errcheck
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	if err := platformExec(exePath, os.Args, os.Environ()); err != nil {
		slog.Error("restart: exec failed", "err", err)
	}
}

// handleSearchSessions handles GET /api/v1/sessions/search?q=<query>.
// It queries the FTS5 sessions_fts index and returns matching manifests (max 50).
// The query is sanitised: null bytes stripped, wildcards removed, wrapped in
// double-quotes so user input is treated as a phrase match rather than raw FTS5 syntax.
func (s *Server) handleSearchSessions(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		jsonError(w, 400, "missing 'q' parameter")
		return
	}
	if len(q) > 200 {
		jsonError(w, 400, "query too long (max 200 characters)")
		return
	}
	// Sanitise for FTS5: strip null bytes and wildcards, escape double-quotes,
	// wrap in double-quotes so the input is a phrase match (not raw FTS5 syntax).
	q = strings.ReplaceAll(q, "\x00", "")
	q = strings.ReplaceAll(q, "*", "")
	q = `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
	results, err := s.store.SearchSessions(q)
	if err != nil {
		slog.Warn("session: search failed", "err", err)
		jsonError(w, 500, "search failed")
		return
	}
	jsonOK(w, results)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	all, err := s.store.ListFiltered(session.SessionFilter{IncludeArchived: includeArchived})
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	includeRoutine := r.URL.Query().Get("include_routine_sessions") == "true"
	filtered := all[:0]
	for _, m := range all {
		if m.Source == "routine" && !includeRoutine {
			continue
		}
		filtered = append(filtered, m)
	}
	if filtered == nil {
		filtered = []session.Manifest{}
	}
	jsonOK(w, filtered)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SpaceID string `json:"space_id"`
	}
	// Ignore parse errors — body is optional.
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	sess, err := s.orch.NewSession("")
	if err != nil {
		jsonError(w, 500, "failed to create session: "+err.Error())
		return
	}
	if body.SpaceID != "" {
		// The orchestrator session (sess) is only in-memory at this point —
		// s.orch.NewSession never writes to the store. Construct the manifest
		// directly and upsert it so ListSpaceMessages can find it via the
		// sessions.space_id foreign key.
		now := time.Now().UTC()
		storedSess := &session.Session{
			ID: sess.ID,
			Manifest: session.Manifest{
				ID:        sess.ID,
				SessionID: sess.ID,
				Status:    "active",
				Version:   1,
				SpaceID:   body.SpaceID,
				CreatedAt: now,
				UpdatedAt: now,
			},
		}
		// Stamp the space's lead agent onto the session manifest so that
		// resolveAgent selects the correct agent (e.g. "Mark" for a DM with
		// Mark) rather than falling through to the default/first agent.
		// Without this, a DM to Mark would be answered by whoever is first
		// in agents.yaml — a security/correctness issue (issue #33).
		if s.spaceStore != nil {
			if sp, spErr := s.spaceStore.GetSpace(body.SpaceID); spErr == nil && sp.LeadAgent != "" {
				storedSess.Manifest.Agent = sp.LeadAgent
			} else if spErr != nil {
				slog.Warn("handleCreateSession: space lookup failed; agent not stamped",
					"space_id", body.SpaceID, "err", spErr)
			}
		}
		if s.store != nil {
			if saveErr := s.store.SaveManifest(storedSess); saveErr != nil {
				slog.Error("handleCreateSession: failed to persist space_id", "session_id", sess.ID, "space_id", body.SpaceID, "err", saveErr)
			}
		}
	}
	jsonOK(w, map[string]string{"session_id": sess.ID})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, ok := s.orch.GetSession(id)
	if !ok {
		jsonError(w, 404, "session not found")
		return
	}
	jsonOK(w, sess)
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "id is required")
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	const maxTitleLen = 512
	if len(body.Title) > maxTitleLen {
		jsonError(w, 400, fmt.Sprintf("title too long: max %d characters", maxTitleLen))
		return
	}
	sess, err := s.store.Load(id)
	if err != nil {
		jsonError(w, 404, "session not found")
		return
	}
	sess.Manifest.Title = body.Title
	if err := s.store.SaveManifest(sess); err != nil {
		jsonError(w, 500, "save manifest: "+err.Error())
		return
	}
	jsonOK(w, sess.Manifest)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "id is required")
		return
	}
	if !s.store.Exists(id) {
		jsonError(w, 404, "session not found")
		return
	}
	// Default behaviour: hard-delete. Pass ?archive=true to soft-delete
	// (preserve data but hide from normal listing).
	if r.URL.Query().Get("archive") == "true" {
		if err := s.store.ArchiveSession(id); err != nil {
			jsonError(w, 500, "archive session: "+err.Error())
			return
		}
		jsonOK(w, map[string]any{"deleted": true, "permanent": false, "archived": true})
		return
	}
	// Cancel and remove any in-flight threads for this session before
	// deleting the session record. Guarded for nil to support minimal
	// server configurations that don't wire up multi-agent.
	if s.tm != nil {
		s.tm.CleanupSession(id)
	}
	if err := s.store.Delete(id); err != nil {
		jsonError(w, 500, "delete session: "+err.Error())
		return
	}
	// Stop any in-flight chat run and evict the seq counter + replay buffer
	// so a recycled session ID starts fresh.
	s.cancelChatRun(id)
	s.wsHub.DeleteSessionSeq(id)
	jsonOK(w, map[string]any{"deleted": true})
}

// redactAgentDef returns a copy of the AgentDef with APIKey masked and
// the transient MemoryType derived from canonical fields for API responses.
func redactAgentDef(a agents.AgentDef) agents.AgentDef {
	if a.APIKey != "" {
		a.APIKey = "[REDACTED]"
	}
	a.DeriveMemoryType()
	// Normalize nil to empty slice to avoid null in JSON response.
	if a.LocalTools == nil {
		a.LocalTools = []string{}
	}
	// Legacy YAML often has no description key. Fill a display fallback from
	// the system prompt so list/GET never surface an empty description when a
	// role blurb can be derived. Disk is unchanged until the next save.
	if a.Description == "" {
		a.Description = agents.ExtractRoleBlurb(a.SystemPrompt, "")
	}
	return a
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agentsCfg, err := agents.LoadAgents()
	if err != nil {
		agentsCfg = agents.DefaultAgentsConfig()
	}
	redacted := make([]agents.AgentDef, len(agentsCfg.Agents))
	for i, a := range agentsCfg.Agents {
		redacted[i] = redactAgentDef(a)
	}
	jsonOK(w, redacted)
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	agentsCfg, err := agents.LoadAgents()
	if err != nil {
		agentsCfg = agents.DefaultAgentsConfig()
	}
	for _, a := range agentsCfg.Agents {
		if strings.EqualFold(a.Name, name) {
			jsonOK(w, redactAgentDef(a))
			return
		}
	}
	jsonError(w, 404, "agent not found")
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, 400, "agent name is required")
		return
	}
	var incoming agents.AgentDef
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	// Ensure the name matches the URL path
	if incoming.Name == "" {
		incoming.Name = name
	}

	// Load existing agent config once for use in multiple checks below.
	existingCfg, _ := agents.LoadAgents()
	if existingCfg == nil {
		existingCfg = agents.DefaultAgentsConfig()
	}

	// Find the existing agent record (case-insensitive match on URL name).
	var existingAgent *agents.AgentDef
	for i := range existingCfg.Agents {
		if strings.EqualFold(existingCfg.Agents[i].Name, name) {
			existingAgent = &existingCfg.Agents[i]
			break
		}
	}

	// If the incoming APIKey is the [REDACTED] sentinel, preserve the real key
	// from the existing agent record (GET → PUT round-trip safety).
	if incoming.APIKey == "[REDACTED]" && existingAgent != nil {
		incoming.APIKey = existingAgent.APIKey
	}

	// Optimistic locking: if the client sends Version > 0, it must match the
	// stored version. Version == 0 means "skip check" (last-writer-wins).
	if incoming.Version > 0 && existingAgent != nil && incoming.Version != existingAgent.Version {
		jsonError(w, 409, fmt.Sprintf("agent version conflict: stored=%d, submitted=%d — reload and retry",
			existingAgent.Version, incoming.Version))
		return
	}

	// Guard against rename collisions: if the name is changing, reject if target already exists.
	// Also guard against duplicate names when creating a NEW agent (existingAgent is nil).
	isRename := !strings.EqualFold(incoming.Name, name)
	isCreation := existingAgent == nil
	if isRename || isCreation {
		for _, a := range existingCfg.Agents {
			if strings.EqualFold(a.Name, incoming.Name) {
				// Skip self when checking for rename (allow non-changing saves).
				if isRename && strings.EqualFold(a.Name, name) {
					continue
				}
				jsonError(w, 409, fmt.Sprintf("agent %q already exists", incoming.Name))
				return
			}
		}
	}

	// Vault name collision check: reject if the incoming VaultName is already
	// claimed by a different agent. We skip the agent currently being updated
	// (identified by the URL name) to allow non-changing saves.
	if err := agents.CheckVaultNameCollision(incoming, name, "", existingCfg.Agents); err != nil {
		jsonError(w, 422, err.Error())
		return
	}

	// Infer provider from model name when the client doesn't supply one.
	if incoming.Provider == "" && incoming.Model != "" {
		incoming.Provider = agents.InferProvider(incoming.Model)
	}

	// Translate frontend memory_type enum to canonical fields before validation/save.
	if err := incoming.ApplyMemoryType(); err != nil {
		jsonError(w, 400, "invalid memory_type: "+err.Error())
		return
	}
	// Persist a one-line description when the client omitted one so list rows
	// never have to render a "No description" placeholder.
	if incoming.Description == "" {
		incoming.Description = agents.ExtractRoleBlurb(incoming.SystemPrompt, "")
	}
	if err := incoming.Validate(); err != nil {
		jsonError(w, 422, "invalid agent: "+err.Error())
		return
	}
	toolbeltResult, err := s.evaluateToolbelt(incoming.Toolbelt)
	if err != nil {
		jsonError(w, 500, "validate toolbelt: "+err.Error())
		return
	}
	if !toolbeltResult.Valid {
		if denied, ok := toolbeltResult.FirstDenied(); ok {
			jsonError(w, 422, fmt.Sprintf("invalid toolbelt: %s (%s)", denied.Reason, denied.ReasonCode))
			return
		}
		jsonError(w, 422, "invalid toolbelt")
		return
	}
	if err := agents.SaveAgentDefault(incoming); err != nil {
		jsonError(w, 500, "save agent: "+err.Error())
		return
	}
	// If this was a rename, delete the old agent file.
	if isRename {
		_ = agents.DeleteAgentDefault(name) // best effort; ignore error
	}
	// Heartbeat lifecycle: sync or remove the managed workflow YAML.
	if isRename {
		_ = agents.RenameHeartbeatYAMLDefault(name, incoming) // best effort
	} else {
		_ = agents.SyncHeartbeatYAMLDefault(incoming) // best effort
	}
	// Refresh the live agent registry so the new/updated agent is immediately
	// visible to delegation, mention parsing, and space rosters (issue #124).
	s.notifyAgentsChanged()
	// Broadcast so all connected frontends refresh their agent list.
	action := "updated"
	if existingAgent == nil {
		action = "created"
	}
	s.BroadcastWS(WSMessage{
		Type: "agent_changed",
		Payload: map[string]any{
			"name":   incoming.Name,
			"action": action,
		},
	})
	jsonOK(w, map[string]string{"saved": incoming.Name})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	// Return the configured models from the config
	jsonOK(w, map[string]string{
		"reasoner": s.cfg.ReasonerModel,
	})
}

func (s *Server) handleListAvailableModels(w http.ResponseWriter, r *http.Request) {
	var ollamaModels []any
	ollamaErr := ""

	baseURL := s.cfg.OllamaBaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	ollamaReq, ollamaReqErr := http.NewRequestWithContext(r.Context(), http.MethodGet, baseURL+"/api/tags", nil)
	if ollamaReqErr != nil {
		ollamaErr = "build request: " + ollamaReqErr.Error()
	} else if resp, doErr := http.DefaultClient.Do(ollamaReq); doErr != nil {
		ollamaErr = "Ollama not reachable: " + doErr.Error()
	} else {
		defer resp.Body.Close()
		var result struct {
			Models []any `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			ollamaErr = "decode error: " + err.Error()
		} else {
			ollamaModels = result.Models
		}
	}

	// Built-in llama.cpp managed models.
	type builtinModel struct {
		Name      string `json:"name"`
		Source    string `json:"source"`
		SizeBytes int64  `json:"size_bytes,omitempty"`
	}
	var builtinModels []builtinModel
	if s.modelStore != nil {
		if installed, err := s.modelStore.Installed(); err == nil {
			for name, entry := range installed {
				builtinModels = append(builtinModels, builtinModel{
					Name:      name,
					Source:    "built-in",
					SizeBytes: entry.SizeBytes,
				})
			}
		}
	}

	// Cloud provider models — show every cloud provider that has credentials
	// configured so the agent model picker covers all available backends, not
	// just the one currently set as cfg.Backend.Provider. This lets a user
	// configure both Vertex AI and (e.g.) Anthropic side by side and pick
	// per-agent without re-saving the global config.
	var cloudModels []any
	addProvider := func(name string, models []providerModel) {
		for _, m := range models {
			cloudModels = append(cloudModels, map[string]any{"name": m.ID, "source": name})
		}
	}

	// --- Vertex AI: project + credentials reference signal that it's wired. ---
	vProject := s.cfg.Backend.Project
	if vProject == "" {
		vProject = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	vLocation := s.cfg.Backend.Location
	if vLocation == "" {
		vLocation = os.Getenv("GOOGLE_CLOUD_LOCATION")
	}
	// CredentialsPath supports literal path / "$ENV" / "keyring:..." / "".
	// Treat "" as configured only when GOOGLE_APPLICATION_CREDENTIALS env is
	// set (matches the gcloud convention that ResolveVertexCredentials uses).
	vCreds := s.cfg.Backend.CredentialsPath
	vCredsConfigured := vCreds != "" || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != ""
	if vProject != "" && vCredsConfigured {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		live, listErr := backend.ListVertexPublisherModels(ctx, vProject, vLocation, vCreds)
		cancel()
		if listErr == nil && len(live) > 0 {
			fetched := make([]providerModel, 0, len(live))
			for _, m := range live {
				fetched = append(fetched, vertexPublisherToProviderModel(m))
			}
			writeProviderModelsCache("vertex", fetched)
			addProvider("vertex", fetched)
		} else {
			if cached, cacheErr := readProviderModelsCache("vertex"); cacheErr == nil && len(cached) > 0 {
				addProvider("vertex", cached)
			} else {
				addProvider("vertex", vertexKnownModels)
			}
		}
	}

	// --- Anthropic / OpenAI / OpenRouter / Google AI Studio: API key required.
	//     Key lives on cfg.Backend.APIKey and applies to whichever provider is
	//     configured globally. (Per-provider API-key storage is a future feature.)
	if s.cfg.Backend.Provider != "" && s.cfg.Backend.Provider != "ollama" && s.cfg.Backend.Provider != "vertex" {
		provider := s.cfg.Backend.Provider
		apiKey, _ := backend.ResolveAPIKey(s.cfg.Backend.APIKey)
		if apiKey != "" {
			endpoint := s.cfg.Backend.Endpoint
			var fetched []providerModel
			var fetchErr error
			switch provider {
			case "anthropic":
				if endpoint == "" {
					endpoint = "https://api.anthropic.com"
				}
				fetched, fetchErr = fetchAnthropicModels(strings.TrimSuffix(endpoint, "/"), apiKey)
			case "openai":
				if endpoint == "" {
					endpoint = "https://api.openai.com/v1"
				}
				fetched, fetchErr = fetchOpenAIModels(strings.TrimSuffix(endpoint, "/"), apiKey)
			case "openrouter":
				if endpoint == "" {
					endpoint = "https://openrouter.ai/api/v1"
				}
				fetched, fetchErr = fetchOpenRouterModels(strings.TrimSuffix(endpoint, "/"), apiKey)
			case "google":
				live, liveErr := fetchGoogleAIModels(apiKey)
				if liveErr != nil || len(live) == 0 {
					fetched = googleAIKnownModels
				} else {
					fetched = live
				}
			}
			if fetchErr != nil {
				if cached, cacheErr := readProviderModelsCache(provider); cacheErr == nil {
					fetched = cached
				} else if provider == "anthropic" {
					fetched = anthropicKnownModels
				}
			} else if fetched != nil {
				writeProviderModelsCache(provider, fetched)
			}
			addProvider(provider, fetched)
		}
	}

	out := map[string]any{
		"models":          ollamaModels,
		"builtin_models":  builtinModels,
		"provider_models": cloudModels,
	}
	if ollamaErr != "" {
		out["error"] = ollamaErr
	}
	jsonOK(w, out)
}

// handleListThreads returns all threads for the given session from the ThreadManager.
// Returns an empty array if thread management is not enabled or no threads exist.
func (s *Server) handleListThreads(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		jsonError(w, 400, "session id is required")
		return
	}
	if s.tm == nil {
		jsonOK(w, []struct{}{})
		return
	}
	if err := s.tm.LoadFromStore(r.Context(), sessionID); err != nil {
		// Non-fatal: return whatever in-memory state exists, but log so operators
		// can diagnose persistence regressions.
		logger.Warn("threads: failed to load from durable store",
			"session_id", sessionID, "err", err)
	}
	threads := s.tm.ListBySession(sessionID)
	if threads == nil {
		threads = []*threadmgr.Thread{}
	}
	type threadListItem struct {
		ID              string                   `json:"ID"`
		SessionID       string                   `json:"SessionID"`
		AgentID         string                   `json:"AgentID"`
		Task            string                   `json:"Task"`
		Status          threadmgr.ThreadStatus   `json:"Status"`
		StartedAt       time.Time                `json:"StartedAt"`
		CompletedAt     time.Time                `json:"CompletedAt"`
		Summary         *threadmgr.FinishSummary `json:"Summary,omitempty"`
		TokensUsed      int                      `json:"TokensUsed"`
		TokenBudget     int                      `json:"TokenBudget"`
		ParentMessageID string                   `json:"ParentMessageID,omitempty"`
	}
	out := make([]threadListItem, 0, len(threads))
	for _, t := range threads {
		if t == nil {
			continue
		}
		out = append(out, threadListItem{
			ID:              t.ID,
			SessionID:       t.SessionID,
			AgentID:         t.AgentID,
			Task:            t.Task,
			Status:          t.Status,
			StartedAt:       t.StartedAt,
			CompletedAt:     t.CompletedAt,
			Summary:         t.Summary,
			TokensUsed:      t.TokensUsed,
			TokenBudget:     t.TokenBudget,
			ParentMessageID: t.ParentMessageID,
		})
	}
	jsonOK(w, out)
}

// handleGetMessages returns the last N messages for the given session.
// Query params:
//   - limit=N  (default 50, max 500)
//   - before_seq=N  if set, returns only messages with seq < N (for reverse pagination)
func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "session id is required")
		return
	}
	limit := 50
	if qs := r.URL.Query().Get("limit"); qs != "" {
		if parsed, err := strconv.Atoi(qs); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 500 {
		limit = 500
	}
	var beforeSeq int64
	if qs := r.URL.Query().Get("before_seq"); qs != "" {
		if parsed, err := strconv.ParseInt(qs, 10, 64); err == nil && parsed > 0 {
			beforeSeq = parsed
		}
	}
	var (
		msgs []session.SessionMessage
		err  error
	)
	if beforeSeq > 0 {
		msgs, err = s.store.TailMessagesBefore(id, limit, beforeSeq)
	} else {
		msgs, err = s.store.TailMessages(id, limit)
	}
	if err != nil {
		jsonError(w, 500, "load messages: "+err.Error())
		return
	}
	if msgs == nil {
		msgs = []session.SessionMessage{}
	}
	jsonOK(w, msgs)
}

// handleSendMessage accepts a user message via REST and returns the full assistant
// reply synchronously. This mirrors the WebSocket "chat" message type and provides
// REST/CLI parity for non-streaming clients.
//
//	POST /api/v1/sessions/{id}/messages
//	{"content": "your message"}
//
// Response: {"content": "<assistant reply>"}
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "session id is required")
		return
	}
	if s.orch == nil {
		jsonError(w, 503, "orchestrator not initialized")
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if body.Content == "" {
		jsonError(w, 400, "content is required")
		return
	}

	userMsgID := session.NewID()
	userPersisted := s.persistInboundUserMessage(id, userMsgID, body.Content)

	var buf strings.Builder
	err := s.orch.ChatForSession(r.Context(), id, body.Content,
		func(token string) { buf.WriteString(token) },
		nil,
	)
	if err != nil {
		jsonError(w, 500, "chat error: "+err.Error())
		return
	}
	// Persist assistant (user row was written at accept so mid-turn Appends
	// cannot win seq). Fallback-persist the user row if accept write failed.
	if s.store != nil {
		if sess, loadErr := s.store.Load(id); loadErr == nil {
			ag := s.resolveAgent(id)
			agentName := ""
			if ag != nil {
				agentName = ag.Name
			}
			if !userPersisted {
				if appendErr := s.store.Append(sess, session.SessionMessage{
					ID: userMsgID, Role: "user", Content: body.Content, Ts: time.Now().UTC(),
				}); appendErr != nil {
					slog.Error("handleSendMessage: failed to persist user message", "session_id", id, "err", appendErr)
				}
			}
			if buf.Len() > 0 {
				assistantMsg := session.SessionMessage{
					ID: session.NewID(), Role: "assistant", Content: buf.String(), Agent: agentName, Ts: time.Now().UTC(),
				}
				s.applyKnownUsage(&assistantMsg, id, persistModelName(sess, ag))
				if appendErr := s.store.Append(sess, assistantMsg); appendErr != nil {
					slog.Error("handleSendMessage: failed to persist assistant message", "session_id", id, "err", appendErr)
				}
			}
			s.emitSpaceActivity(sess.SpaceID())
		}
	}
	jsonOK(w, map[string]string{"content": buf.String()})
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, 400, "agent name is required")
		return
	}
	// Enforce minimum: cannot delete the last agent.
	if existing, err := agents.LoadAgents(); err == nil && len(existing.Agents) <= 1 {
		jsonError(w, 409, "cannot delete the last agent")
		return
	}

	// Check if the agent is assigned as a lead agent in any space.
	if s.spaceStore != nil {
		if spaces, err := s.spaceStore.SpacesByLeadAgent(name); err == nil && len(spaces) > 0 {
			var spaceNames []string
			for _, sp := range spaces {
				spaceNames = append(spaceNames, sp.Name)
			}
			jsonError(w, 409, fmt.Sprintf("cannot delete agent %q: assigned as lead agent in spaces: %v", name, spaceNames))
			return
		}
	}

	if err := agents.DeleteAgentDefault(name); err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	_ = agents.DeleteHeartbeatYAMLDefault(name) // best effort; ignore error
	// Refresh the live agent registry so the deleted agent immediately stops
	// resolving for delegation and mention parsing (issue #124).
	s.notifyAgentsChanged()
	// Broadcast so all connected frontends remove the deleted agent.
	s.BroadcastWS(WSMessage{
		Type: "agent_changed",
		Payload: map[string]any{
			"name":   name,
			"action": "deleted",
		},
	})
	jsonOK(w, map[string]bool{"deleted": true})
}

// handleCloneAgent (Phase 7) clones an existing agent under a new name and
// optionally swaps its model. The use case is "Use Haiku for the classifier,
// Sonnet for the writer, Opus for the auditor": users clone an existing,
// well-tuned agent and just bump the model.
//
//	POST /api/v1/agents/{name}/clone
//	body: {"new_name": "Bob-Sonnet", "model": "claude-sonnet-4-6", "provider": "anthropic"}
//
// Response: 200 + the redacted clone, or 4xx on validation errors. The
// source agent is never modified. Skills, memory mode, vault description and
// every other field are copied verbatim — only Name (mandatory) and Model /
// Provider / Endpoint (optional) are overrideable.
func (s *Server) handleCloneAgent(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("name")
	if source == "" {
		jsonError(w, 400, "source agent name is required")
		return
	}
	var body struct {
		NewName  string `json:"new_name"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.NewName) == "" {
		jsonError(w, 400, "new_name is required")
		return
	}
	if strings.EqualFold(body.NewName, source) {
		jsonError(w, 400, "new_name must differ from source agent name")
		return
	}

	cfg, err := agents.LoadAgents()
	if err != nil || cfg == nil {
		cfg = agents.DefaultAgentsConfig()
	}
	var src *agents.AgentDef
	for i := range cfg.Agents {
		if strings.EqualFold(cfg.Agents[i].Name, source) {
			src = &cfg.Agents[i]
			break
		}
	}
	if src == nil {
		jsonError(w, 404, "source agent not found")
		return
	}
	// Reject target-name collisions early so we don't write a half-baked
	// agent and then have to undo it.
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.Name, body.NewName) {
			jsonError(w, 409, "agent name already exists")
			return
		}
	}

	clone := *src // shallow copy is fine: AgentDef is value-only, no pointers besides MemoryEnabled.
	clone.Name = body.NewName
	clone.ID = "" // force a fresh id assignment downstream
	clone.IsDefault = false
	clone.Version = 0                       // version starts at 0 for new agents
	clone.CreatedAt = ""                    // SaveAgentDefault stamps it
	if mb := src.MemoryEnabled; mb != nil { // deep-copy the *bool to break sharing
		v := *mb
		clone.MemoryEnabled = &v
	}
	if strings.TrimSpace(body.Model) != "" {
		clone.Model = body.Model
	}
	if strings.TrimSpace(body.Provider) != "" {
		clone.Provider = body.Provider
	} else if body.Model != "" && body.Provider == "" {
		// Re-infer provider from the new model when the caller swaps model
		// but not provider (the most common case).
		clone.Provider = agents.InferProvider(clone.Model)
	}
	if strings.TrimSpace(body.Endpoint) != "" {
		clone.Endpoint = body.Endpoint
	}
	if err := clone.Validate(); err != nil {
		jsonError(w, 422, "invalid clone: "+err.Error())
		return
	}
	if err := agents.SaveAgentDefault(clone); err != nil {
		jsonError(w, 500, "save clone: "+err.Error())
		return
	}
	// Refresh the live agent registry so the clone is immediately usable
	// for delegation and space rosters (issue #124).
	s.notifyAgentsChanged()
	s.BroadcastWS(WSMessage{
		Type: "agent_changed",
		Payload: map[string]any{
			"name":   clone.Name,
			"action": "created",
			"source": source,
		},
	})
	jsonOK(w, redactAgentDef(clone))
}

// stateString converts an agent.State to a human-readable string.
func stateString(st int) string {
	switch st {
	case 0:
		return "idle"
	case 1:
		return "iterating"
	case 2:
		return "agent_loop"
	default:
		return "unknown"
	}
}

func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	state := int(s.orch.CurrentState())
	jsonOK(w, map[string]any{
		"state":      stateString(state),
		"session_id": s.orch.SessionID(),
		"machine_id": s.orch.MachineID(),
	})
}

func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	if s.ca == nil {
		jsonOK(w, map[string]any{"session_total_usd": 0.0})
		return
	}
	jsonOK(w, map[string]any{"session_total_usd": s.ca.Total()})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var prompt, completion any
	if s.orch != nil {
		p, c := s.orch.LastUsage()
		if p > 0 || c > 0 {
			prompt, completion = p, c
		}
	}
	jsonOK(w, map[string]any{
		"last_prompt_tokens":     prompt,
		"last_completion_tokens": completion,
	})
}

func persistModelName(sess *session.Session, ag *agents.Agent) string {
	if ag != nil {
		if id := ag.GetModelID(); id != "" {
			return id
		}
	}
	if sess != nil {
		return sess.Manifest.Model
	}
	return ""
}

// applyKnownUsage stamps LastUsage onto the assistant message and records it
// for cost_history when the backend reported tokens. A 0/0 usage is treated
// as unknown — a lying zero is worse than a gap.
func (s *Server) applyKnownUsage(msg *session.SessionMessage, sessionID, model string) {
	if s.orch == nil || msg == nil {
		return
	}
	prompt, completion := s.orch.LastUsage()
	if prompt == 0 && completion == 0 {
		return
	}
	msg.PromptTok = prompt
	msg.CompTok = completion
	if model != "" {
		msg.ModelName = model
	}
	if s.ca != nil {
		s.ca.Record(sessionID, prompt, completion, model)
		return
	}
	if s.statsPersister != nil {
		s.statsPersister.EnqueueCost(stats.CostEvent{
			SessionID:        sessionID,
			PromptTokens:     prompt,
			CompletionTokens: completion,
		})
	}
}

// handleGetLogLevel returns the current log level.
func (s *Server) handleGetLogLevel(w http.ResponseWriter, r *http.Request) {
	level := logger.Level()
	jsonOK(w, map[string]string{"level": level.String()})
}

// handleSetLogLevel sets the log level at runtime (no restart needed).
func (s *Server) handleSetLogLevel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(body.Level)); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid level: use debug, info, warn, or error")
		return
	}
	logger.SetLevel(level)
	slog.Info("log level changed", "level", level.String())
	jsonOK(w, map[string]string{"level": level.String()})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if qs := r.URL.Query().Get("n"); qs != "" {
		if parsed, err := strconv.Atoi(qs); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > 1000 {
		n = 1000
	}
	lines, err := logger.TailLog(s.huginnDir, n)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if lines == nil {
		lines = []string{}
	}
	jsonOK(w, map[string]any{"lines": lines})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	// Return a redacted copy: API keys and OAuth secrets are masked so they
	// cannot be exfiltrated via the REST API even if the token is compromised.
	safe := s.cfg
	if safe.Backend.APIKey != "" {
		safe.Backend.APIKey = "[REDACTED]"
	}
	if safe.Integrations.Google.ClientSecret != "" {
		safe.Integrations.Google.ClientSecret = "[REDACTED]"
	}
	if safe.Integrations.GitHub.ClientSecret != "" {
		safe.Integrations.GitHub.ClientSecret = "[REDACTED]"
	}
	if safe.Integrations.Slack.ClientSecret != "" {
		safe.Integrations.Slack.ClientSecret = "[REDACTED]"
	}
	if safe.Integrations.Jira.ClientSecret != "" {
		safe.Integrations.Jira.ClientSecret = "[REDACTED]"
	}
	if safe.Integrations.Bitbucket.ClientSecret != "" {
		safe.Integrations.Bitbucket.ClientSecret = "[REDACTED]"
	}
	// Redact MCP server env vars whose key suffix indicates a secret.
	for i, srv := range safe.MCPServers {
		for j, env := range srv.Env {
			eqIdx := strings.Index(env, "=")
			if eqIdx < 0 {
				continue
			}
			key := env[:eqIdx]
			if config.IsSecretEnvKey(key) {
				safe.MCPServers[i].Env[j] = key + "=[REDACTED]"
			}
		}
	}
	jsonOK(w, safe)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if err := config.Validate(newCfg); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	// Preserve real secrets when the UI sends the [REDACTED] sentinel back.
	// This prevents a GET → PUT round-trip from overwriting the live key.
	if newCfg.Backend.APIKey == "[REDACTED]" {
		newCfg.Backend.APIKey = s.cfg.Backend.APIKey
	}
	// Restore redacted MCP env var values from the live config.
	// When a client GETs config, secret env vars are returned as KEY=[REDACTED].
	// If the client sends those values back unchanged, restore the real secrets.
	for i, newSrv := range newCfg.MCPServers {
		for _, liveSrv := range s.cfg.MCPServers {
			if liveSrv.Name != newSrv.Name {
				continue
			}
			for j, env := range newSrv.Env {
				eqIdx := strings.Index(env, "=")
				if eqIdx < 0 {
					continue
				}
				key, val := env[:eqIdx], env[eqIdx+1:]
				if val == "[REDACTED]" && config.IsSecretEnvKey(key) {
					for _, liveEnv := range liveSrv.Env {
						if strings.HasPrefix(liveEnv, key+"=") {
							newCfg.MCPServers[i].Env[j] = liveEnv
							break
						}
					}
				}
			}
		}
	}
	// Migrate literal API keys to the OS keychain so they are never stored in
	// plaintext in config.json. Keys already expressed as "keyring:…" or "$ENV"
	// references are left unchanged (IsLiteralAPIKey returns false for them).
	if backend.IsLiteralAPIKey(newCfg.Backend.APIKey) {
		slot := newCfg.Backend.Provider
		if slot == "" {
			slot = "backend"
		}
		ref, err := s.storeAPIKey(slot, newCfg.Backend.APIKey)
		if err != nil {
			// storeAPIKey returns the literal when keychain is unavailable (e.g. Linux/CI).
			// Log the warning but continue — the returned ref is still safe to persist.
			slog.Warn("keychain unavailable, storing API key as literal", "err", err)
		}
		newCfg.Backend.APIKey = ref
	}
	// Check if restart is needed
	needsRestart := s.cfg.WebUI.Port != newCfg.WebUI.Port ||
		s.cfg.WebUI.Bind != newCfg.WebUI.Bind
	s.cfg = newCfg
	s.mu.Unlock()
	// Push the updated provider key into the live BackendCache so agents
	// immediately inherit the new key without requiring a server restart.
	// This must happen outside the server mutex; BackendCache has its own lock.
	if s.backendCache != nil && newCfg.Backend.Provider != "" && newCfg.Backend.APIKey != "" {
		s.backendCache.SetProviderKey(newCfg.Backend.Provider, newCfg.Backend.APIKey)
	}
	// Save config to disk
	if err := s.cfg.Save(); err != nil {
		jsonError(w, 500, "save config: "+err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"saved":            true,
		"requires_restart": needsRestart,
	})
}

func (s *Server) handlePullModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if body.Name == "" {
		jsonError(w, 400, "name is required")
		return
	}
	baseURL := s.cfg.OllamaBaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	payload, err := json.Marshal(map[string]any{"name": body.Name, "stream": false})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "marshal error: "+err.Error())
		return
	}
	pullReq, pullReqErr := http.NewRequestWithContext(r.Context(), http.MethodPost, baseURL+"/api/pull", bytes.NewReader(payload))
	if pullReqErr != nil {
		jsonError(w, http.StatusInternalServerError, "build request: "+pullReqErr.Error())
		return
	}
	pullReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(pullReq)
	if err != nil {
		jsonError(w, 502, "Ollama not reachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		jsonError(w, 502, "decode Ollama response: "+err.Error())
		return
	}
	jsonOK(w, result)
}

// validModelName accepts Ollama-style names like "llama3:8b", "library/mistral", etc.
// The leading character must be alphanumeric; subsequent chars may include . _ : / -.
// Length is capped at 128 characters. Path traversal sequences ("../") are implicitly
// rejected because leading "/" and ".." are not matched by the regex.
var validModelName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,127}$`)

// handleDeleteOllamaModel proxies DELETE /api/v1/models/{name} to the local Ollama instance.
//
//	DELETE /api/v1/models/{name}
func (s *Server) handleDeleteOllamaModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || !validModelName.MatchString(name) {
		jsonError(w, http.StatusBadRequest, "invalid model name")
		return
	}
	baseURL := s.cfg.OllamaBaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	payload, _ := json.Marshal(map[string]string{"name": name})
	ollamaCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ollamaCtx, http.MethodDelete, baseURL+"/api/delete", bytes.NewReader(payload))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "build request: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "ollama unavailable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		jsonError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	jsonOK(w, map[string]bool{"deleted": true})
}

// handleChatStream streams an LLM response as Server-Sent Events.
//
//	POST /api/v1/sessions/{id}/chat/stream
//	Body: {"content": "user message"}
//	Response: text/event-stream with data: {"type":"token","content":"..."} events
//
// The stream emits:
//   - {"type":"token","content":"<chunk>"} — one per streamed token
//   - {"type":"<event-type>","content":"<content>"} — for richer backend events (e.g. "thought")
//   - {"type":"error","content":"<message>"} — if an error occurs
//   - {"type":"done"} — when the response is complete
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Content == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "content required"}) //nolint:errcheck
		return
	}

	// Set SSE headers before any write so the client sees them immediately.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	if s.orch == nil {
		data, _ := json.Marshal(map[string]string{"type": "error", "content": "orchestrator not ready"})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	sessionID := r.PathValue("id")
	ctx := r.Context()

	err := s.orch.ChatForSession(ctx, sessionID, body.Content,
		func(token string) {
			data, _ := json.Marshal(map[string]string{"type": "token", "content": token})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
		func(ev backend.StreamEvent) {
			data, _ := json.Marshal(map[string]string{"type": string(ev.Type), "content": ev.Content})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		},
	)
	if err != nil {
		logger.Error("chat completion", "session_id", sessionID, "err", err)
		data, _ := json.Marshal(map[string]string{"type": "error", "content": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", `{"type":"done"}`)
	flusher.Flush()
}

// handleCloudConnect starts an interactive registration flow with HuginnCloud.
// POST /api/v1/cloud/connect
func (s *Server) handleCloudConnect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	already := s.registering
	if !already {
		s.registering = true
	}
	storer := s.relayTokenStorer
	s.mu.Unlock()

	if already {
		jsonOK(w, map[string]any{"status": "registering"})
		return
	}

	// Check if already registered (token exists).
	if storer != nil {
		if _, err := storer.Load(); err == nil {
			// Token exists — try to connect satellite if not already connected.
			s.mu.Lock()
			sat := s.satellite
			s.registering = false
			s.mu.Unlock()
			if sat != nil {
				go func() {
					connectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					if err := sat.Connect(connectCtx); err != nil {
						slog.Warn("cloud: satellite reconnect (already registered) failed", "err", err)
					}
				}()
			}
			jsonOK(w, map[string]any{"status": "already_registered"})
			return
		}
	}

	go func() {
		defer func() {
			s.mu.Lock()
			s.registering = false
			s.mu.Unlock()
		}()

		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "my-machine"
		}

		cloudURL := os.Getenv("HUGINN_CLOUD_URL")
		var reg *relay.Registrar
		if storer != nil {
			reg = relay.NewRegistrarWithStore(cloudURL, storer)
		} else {
			reg = relay.NewRegistrar(cloudURL)
		}

		// Allow tests to override the browser opener to prevent real browser windows.
		s.mu.Lock()
		if s.openBrowserFn != nil {
			reg.OpenBrowserFn = s.openBrowserFn
		}
		if s.cloudRegisterPollInterval > 0 {
			reg.PollInterval = s.cloudRegisterPollInterval
		}
		regTimeout := s.cloudRegisterTimeout
		s.mu.Unlock()
		ctx := context.WithoutCancel(r.Context())
		if regTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, regTimeout)
			defer cancel()
		}
		if _, err := reg.Register(ctx, hostname); err != nil {
			slog.Warn("cloud: registration failed", "err", err)
			return
		}
		// Token is now saved — connect the satellite WebSocket immediately.
		s.mu.Lock()
		sat := s.satellite
		s.mu.Unlock()
		if sat != nil {
			connectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := sat.Connect(connectCtx); err != nil {
				slog.Warn("cloud: satellite connect after registration failed", "err", err)
			}
		}
	}()

	jsonOK(w, map[string]any{"status": "registering"})
}

// handleCloudDisconnect removes the HuginnCloud registration token.
// DELETE /api/v1/cloud/connect
func (s *Server) handleCloudDisconnect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	storer := s.relayTokenStorer
	s.mu.Unlock()

	if storer == nil {
		jsonOK(w, map[string]any{"status": "disconnected"})
		return
	}

	if err := storer.Clear(); err != nil {
		slog.Warn("cloud: failed to clear token", "err", err)
	}

	jsonOK(w, map[string]any{"status": "disconnected"})
}
