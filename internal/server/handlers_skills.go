package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/skills"
)

// validSkillName returns true if name is safe to use as a filename.
func validSkillName(name string) bool {
	return name != "" &&
		!strings.Contains(name, "/") &&
		!strings.Contains(name, "\\") &&
		!strings.Contains(name, "..")
}

// skillsDirPath returns the path to huginnDir/skills/ for this server instance.
func (s *Server) skillsDirPath() string {
	return filepath.Join(s.huginnDir, "skills")
}

func (s *Server) cacheDirPath() string {
	return filepath.Join(s.huginnDir, "cache")
}

// GET /api/v1/skills — list installed skills with metadata from manifest
func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	sdir := s.skillsDirPath()
	manifest, _ := skills.LoadManifest(filepath.Join(sdir, "installed.json"))

	type skillItem struct {
		Name      string `json:"name"`
		Author    string `json:"author"`
		Source    string `json:"source"`
		Enabled   bool   `json:"enabled"`
		ToolCount int    `json:"tool_count"`
	}

	entries, err := os.ReadDir(sdir)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, "failed to read skills dir", http.StatusInternalServerError)
		return
	}

	var result []skillItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		sk, err := skills.LoadMarkdownSkill(filepath.Join(sdir, entry.Name()))
		if err != nil {
			continue
		}
		// Deny-by-default: skill is disabled unless manifest explicitly enables it.
		// This is consistent with the loader's deny-by-default security posture.
		item := skillItem{
			Name:      sk.Name(),
			Author:    sk.Author(),
			Enabled:   false,
			Source:    "local",
			ToolCount: len(sk.Tools()),
		}
		if manifest != nil {
			if m := manifest.Get(sk.Name()); m != nil {
				item.Enabled = m.Enabled
				item.Source = m.Source
			}
		}
		result = append(result, item)
	}
	if result == nil {
		result = []skillItem{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		slog.Warn("handleSkillsList: encode response", "err", err)
	}
}

// GET /api/v1/skills/{name} — get full skill content
func (s *Server) handleSkillsGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validSkillName(name) {
		http.Error(w, "invalid skill name", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.skillsDirPath(), name+".md")
	sk, err := skills.LoadMarkdownSkill(path)
	if err != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	manifest, _ := skills.LoadManifest(filepath.Join(s.skillsDirPath(), "installed.json"))
	type skillDetail struct {
		Name    string `json:"name"`
		Author  string `json:"author"`
		Source  string `json:"source"`
		Enabled bool   `json:"enabled"`
		Prompt  string `json:"prompt"`
		Rules   string `json:"rules"`
	}
	detail := skillDetail{
		Name:    sk.Name(),
		Author:  sk.Author(),
		Source:  sk.Source(),
		Enabled: true,
		Prompt:  sk.SystemPromptFragment(),
		Rules:   sk.RuleContent(),
	}
	if manifest != nil {
		if m := manifest.Get(sk.Name()); m != nil {
			detail.Enabled = m.Enabled
			detail.Source = m.Source
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(detail); err != nil {
		slog.Warn("handleSkillsGet: encode response", "err", err)
	}
}

// POST /api/v1/skills — create/save skill from UI (Create section)
func (s *Server) handleSkillsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	sk, err := skills.ParseMarkdownSkillBytes([]byte(body.Content))
	if err != nil {
		http.Error(w, "invalid SKILL.md: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !validSkillName(sk.Name()) {
		http.Error(w, "invalid skill name in SKILL.md", http.StatusBadRequest)
		return
	}
	sdir := s.skillsDirPath()
	if err := os.MkdirAll(sdir, 0755); err != nil {
		http.Error(w, "create skills dir", http.StatusInternalServerError)
		return
	}
	destPath := filepath.Join(sdir, sk.Name()+".md")
	if err := os.WriteFile(destPath, []byte(body.Content), 0644); err != nil {
		http.Error(w, "write skill", http.StatusInternalServerError)
		return
	}
	manifest, _ := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	if manifest != nil {
		manifest.Upsert(skills.InstalledEntry{Name: sk.Name(), Source: "local", Enabled: true})
		if err := manifest.Save(); err != nil {
			jsonError(w, http.StatusInternalServerError, "failed to save skill manifest: "+err.Error())
			return
		}
	}
	s.reloadSkills()
	s.BroadcastWS(WSMessage{Type: "skill_changed", Payload: map[string]any{"name": sk.Name(), "action": "created"}})
	s.SendRelay(relay.Message{Type: relay.MsgSkillChanged, Payload: map[string]any{"name": sk.Name(), "action": "created"}})
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]string{"name": sk.Name()}); err != nil {
		slog.Warn("handleSkillsCreate: encode response", "err", err)
	}
}

// POST /api/v1/skills/install
func (s *Server) handleSkillsInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	cachePath := filepath.Join(s.cacheDirPath(), "skills-index.json")
	entries, _, err := skills.LoadIndex(cachePath)
	if err != nil {
		http.Error(w, "registry unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var found *skills.IndexEntry
	for i := range entries {
		if entries[i].Name == body.Target {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "skill not found in registry", http.StatusNotFound)
		return
	}
	if found.SourceURL == "" {
		http.Error(w, "skill has no source_url in registry", http.StatusBadGateway)
		return
	}
	url := found.SourceURL
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "failed to fetch skill", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	const maxBytes = 10 << 20
	lr := io.LimitReader(resp.Body, int64(maxBytes+1))
	rawBytes, err := io.ReadAll(lr)
	if err != nil || len(rawBytes) > maxBytes {
		http.Error(w, "failed to read skill content", http.StatusBadGateway)
		return
	}
	sk, err := skills.ParseMarkdownSkillBytes(rawBytes)
	if err != nil {
		http.Error(w, "invalid SKILL.md: "+err.Error(), http.StatusBadGateway)
		return
	}
	sdir := s.skillsDirPath()
	os.MkdirAll(sdir, 0755)
	if err := os.WriteFile(filepath.Join(sdir, sk.Name()+".md"), rawBytes, 0644); err != nil {
		http.Error(w, "write skill", http.StatusInternalServerError)
		return
	}
	manifest, _ := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	if manifest != nil {
		manifest.Upsert(skills.InstalledEntry{Name: sk.Name(), Source: "registry", Enabled: true})
		if err := manifest.Save(); err != nil {
			jsonError(w, http.StatusInternalServerError, "failed to save skill manifest: "+err.Error())
			return
		}
	}
	s.reloadSkills()
	s.BroadcastWS(WSMessage{Type: "skill_changed", Payload: map[string]any{"name": sk.Name(), "action": "installed"}})
	s.SendRelay(relay.Message{Type: relay.MsgSkillChanged, Payload: map[string]any{"name": sk.Name(), "action": "installed"}})
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]string{"name": sk.Name()}); err != nil {
		slog.Warn("handleSkillsInstall: encode response", "err", err)
	}
}

// PUT /api/v1/skills/{name} — update an existing skill's content.
// The request body is {"content": "<full SKILL.md content>"}.
//
// If the name parsed from the markdown matches the URL {name}, the file is
// updated in-place. If they differ (rename), the new file is written first and
// the old file is removed afterwards (atomic write-then-delete). The manifest is
// updated accordingly and the skills registry is reloaded.
func (s *Server) handleSkillsUpdate(w http.ResponseWriter, r *http.Request) {
	urlName := r.PathValue("name")
	if !validSkillName(urlName) {
		http.Error(w, "invalid skill name", http.StatusBadRequest)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	sk, err := skills.ParseMarkdownSkillBytes([]byte(body.Content))
	if err != nil {
		http.Error(w, "invalid SKILL.md: "+err.Error(), http.StatusBadRequest)
		return
	}
	newName := sk.Name()
	if !validSkillName(newName) {
		http.Error(w, "invalid skill name in SKILL.md", http.StatusBadRequest)
		return
	}
	sdir := s.skillsDirPath()
	oldPath := filepath.Join(sdir, urlName+".md")
	// Ensure the skill being updated actually exists.
	if _, statErr := os.Stat(oldPath); os.IsNotExist(statErr) {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	newPath := filepath.Join(sdir, newName+".md")
	if err := os.MkdirAll(sdir, 0755); err != nil {
		http.Error(w, "create skills dir", http.StatusInternalServerError)
		return
	}
	// Write new content (also handles same-name update — overwrites in place).
	if err := os.WriteFile(newPath, []byte(body.Content), 0644); err != nil {
		http.Error(w, "write skill: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Rename case: remove the old file after writing the new one.
	// If Remove fails, roll back the new file to avoid leaving an orphan.
	renamed := newName != urlName
	if renamed {
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			// Rollback: remove the new file we just wrote so the skills dir
			// stays consistent (no orphan new-name file alongside the still-present old-name file).
			_ = os.Remove(newPath)
			slog.Warn("handleSkillsUpdate: remove old skill file failed, rolled back", "path", oldPath, "err", err)
			http.Error(w, "rename failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// Update manifest: remove old entry (if renamed) and upsert new entry.
	manifest, _ := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	if manifest != nil {
		if renamed {
			manifest.Remove(urlName)
		}
		manifest.Upsert(skills.InstalledEntry{Name: newName, Source: "local", Enabled: true})
		if err := manifest.Save(); err != nil {
			jsonError(w, http.StatusInternalServerError, "failed to save skill manifest: "+err.Error())
			return
		}
	}
	s.reloadSkills()
	payload := map[string]any{"name": newName, "action": "updated"}
	if renamed {
		payload["old_name"] = urlName
	}
	s.BroadcastWS(WSMessage{Type: "skill_changed", Payload: payload})
	s.SendRelay(relay.Message{Type: relay.MsgSkillChanged, Payload: payload})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"name": newName}); err != nil {
		slog.Warn("handleSkillsUpdate: encode response", "err", err)
	}
}

// PUT /api/v1/skills/{name}/enable
func (s *Server) handleSkillsEnable(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, r, true)
}

// PUT /api/v1/skills/{name}/disable
func (s *Server) handleSkillsDisable(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, r, false)
}

func (s *Server) setSkillEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	name := r.PathValue("name")
	if !validSkillName(name) {
		http.Error(w, "invalid skill name", http.StatusBadRequest)
		return
	}
	sdir := s.skillsDirPath()
	manifest, err := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	if err != nil {
		http.Error(w, "manifest error", http.StatusInternalServerError)
		return
	}
	if !manifest.SetEnabled(name, enabled) {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	if err := manifest.Save(); err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to save skill manifest: "+err.Error())
		return
	}
	s.reloadSkills()
	s.BroadcastWS(WSMessage{Type: "skill_changed", Payload: map[string]any{"name": name, "enabled": enabled}})
	s.SendRelay(relay.Message{Type: relay.MsgSkillChanged, Payload: map[string]any{"name": name, "enabled": enabled}})
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/skills/{name}
func (s *Server) handleSkillsDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validSkillName(name) {
		http.Error(w, "invalid skill name", http.StatusBadRequest)
		return
	}
	sdir := s.skillsDirPath()
	path := filepath.Join(sdir, name+".md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		http.Error(w, "remove skill: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if manifest, _ := skills.LoadManifest(filepath.Join(sdir, "installed.json")); manifest != nil {
		manifest.Remove(name)
		if err := manifest.Save(); err != nil {
			jsonError(w, http.StatusInternalServerError, "failed to save skill manifest: "+err.Error())
			return
		}
	}
	s.reloadSkills()
	s.BroadcastWS(WSMessage{Type: "skill_changed", Payload: map[string]any{"name": name, "action": "deleted"}})
	s.SendRelay(relay.Message{Type: relay.MsgSkillChanged, Payload: map[string]any{"name": name, "action": "deleted"}})
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/skills/registry/search?q=
func (s *Server) handleSkillsRegistrySearch(w http.ResponseWriter, r *http.Request) {
	cachePath := filepath.Join(s.cacheDirPath(), "skills-index.json")
	entries, _, err := skills.LoadIndex(cachePath)
	if err != nil {
		http.Error(w, "registry unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	results := skills.SearchIndex(entries, q)
	if results == nil {
		results = []skills.IndexEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		slog.Warn("handleSkillsRegistrySearch: encode response", "err", err)
	}
}

// GET /api/v1/skills/registry/index — return and optionally refresh cached index
func (s *Server) handleSkillsRegistryIndex(w http.ResponseWriter, r *http.Request) {
	cachePath := filepath.Join(s.cacheDirPath(), "skills-index.json")
	var entries []skills.IndexEntry
	var collections []skills.IndexCollection
	var err error
	if r.URL.Query().Get("refresh") == "1" {
		entries, collections, err = skills.FetchAndCacheIndex(cachePath)
	} else {
		entries, collections, err = skills.LoadIndex(cachePath)
	}
	if err != nil {
		http.Error(w, "registry unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if entries == nil {
		entries = []skills.IndexEntry{}
	}
	if collections == nil {
		collections = []skills.IndexCollection{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(struct {
		Skills      []skills.IndexEntry      `json:"skills"`
		Collections []skills.IndexCollection `json:"collections"`
	}{Skills: entries, Collections: collections}); err != nil {
		slog.Warn("handleSkillsRegistryIndex: encode response", "err", err)
	}
}

// POST /api/v1/skills/{name}/execute — run a skill synchronously on a user input.
//
// The skill's system prompt fragment and rule content are injected as delimited
// context blocks ahead of the caller's input. The LLM receives a fresh session
// (no shared history) and the result is returned as {"output": "...", "skill": name}.
//
// The endpoint uses BatchChat which respects the orchestrator's configured model
// and backend. A missing or un-configured orchestrator returns 503.
func (s *Server) handleSkillsExecute(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validSkillName(name) {
		jsonError(w, 400, "invalid skill name")
		return
	}

	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Input) == "" {
		jsonError(w, 422, "input is required")
		return
	}

	path := filepath.Join(s.skillsDirPath(), name+".md")
	sk, err := skills.LoadMarkdownSkill(path)
	if err != nil {
		jsonError(w, 404, "skill not found")
		return
	}

	if s.orch == nil {
		jsonError(w, 503, "orchestrator not configured")
		return
	}

	// Compose the task by prepending the skill's content as delimited blocks.
	// BatchChat builds a fresh context (system prompt + task only, no history),
	// so the skill instructions arrive in the same turn as the user input.
	var taskParts []string
	if frag := sk.SystemPromptFragment(); frag != "" {
		taskParts = append(taskParts, "<skill-instructions>\n"+frag+"\n</skill-instructions>")
	}
	if rules := sk.RuleContent(); rules != "" {
		taskParts = append(taskParts, "<skill-rules>\n"+rules+"\n</skill-rules>")
	}
	taskParts = append(taskParts, body.Input)
	task := strings.Join(taskParts, "\n\n")

	results := s.orch.BatchChat(r.Context(), []string{task})
	if len(results) == 0 {
		jsonError(w, 500, "no result from skill execution")
		return
	}
	result := results[0]
	if result.Err != nil {
		jsonError(w, 500, "skill execution failed: "+result.Err.Error())
		return
	}
	jsonOK(w, map[string]string{
		"output": result.Output,
		"skill":  name,
	})
}

// reloadSkills re-reads the skills directory, rebuilds the registry, and
// pushes it to the orchestrator. Workspace rules flow through SetSkillsFragment
// (so they always appear in ctxText). Skills are accessed via SetSkillsRegistry
// for per-agent resolution; global fallback is handled in the orchestrator.
func (s *Server) reloadSkills() {
	if s.orch == nil {
		return
	}
	sdir := s.skillsDirPath()
	loader := skills.NewLoader(sdir)
	loadedSkills, loadErrs := loader.LoadAll()
	for _, e := range loadErrs {
		slog.Warn("skills: reload: failed to load custom skill", "path", sdir, "err", e)
	}
	reg := skills.NewSkillRegistry()
	if builtinErrs := reg.LoadBuiltins(); len(builtinErrs) > 0 {
		for _, e := range builtinErrs {
			slog.Warn("skills: reload: failed to load builtin skill", "err", e)
		}
	}
	for _, sk := range loadedSkills {
		reg.Register(sk)
	}

	// Push full registry to orchestrator for per-agent skill resolution.
	// The orchestrator handles global fallback via reg.CombinedPromptFragment().
	s.orch.SetSkillsRegistry(reg)

	// Re-inject the AgentExecutor into any new PromptTools that were loaded.
	// Without this, PromptTools added/changed after initial startup would have
	// a nil executor and silently fail when invoked.
	if toolReg := s.orch.ToolRegistry(); toolReg != nil {
		skills.InjectAgentExecutor(toolReg, s.orch)
	}

	// Workspace rules (CLAUDE.md-style files) still flow through the fragment path
	// so they always appear in ctxText regardless of per-agent skill assignment.
	// Skills are NOT included here — they arrive via agentSkillsFragment in buildAgentSystemPrompt.
	//
	// NOTE: TUI mode maintains its own skill registry snapshot built at startup.
	// A hot-reload via the server API updates only the server-side orchestrator's
	// registry. If the TUI also needs updates, a restart or explicit TUI-side
	// refresh is required.
	workspaceRules := loader.LoadRuleFiles(s.orch.WorkspaceRoot())
	s.orch.SetSkillsFragment(workspaceRules)
}
