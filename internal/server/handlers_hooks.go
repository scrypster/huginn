package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/scrypster/huginn/internal/agent"
)

// Hooks API — user-configurable PreToolUse/PostToolUse shell hooks
// (.huginn/hooks.json workspace, ~/.huginn/hooks.json global). See
// internal/agent/hooks_config.go for the runtime (loading, matching,
// execution, audit log) this handler layer is a thin CRUD/reload/test-run
// front-end for.
//
//	GET    /api/v1/hooks           list every hook, global+workspace, with scope
//	POST   /api/v1/hooks           create a hook (body: hookAPIEntry, scope defaults to "workspace")
//	PUT    /api/v1/hooks/{id}      update a hook in whichever scope it lives in
//	DELETE /api/v1/hooks/{id}      delete a hook from whichever scope it lives in
//	POST   /api/v1/hooks/reload    re-read both files on demand (also happens automatically on edit)
//	POST   /api/v1/hooks/test      dry-run a hook (by id, or an inline def) against a sample tool call
//	GET    /api/v1/hooks/audit     recent hook executions (allowed/vetoed, output, exit code)

// hookAPIEntry is the wire shape for one hook: agent.UserHookDef plus the
// scope it lives in (global ~/.huginn/hooks.json or per-workspace
// .huginn/hooks.json). Scope is round-tripped through the API so the UI can
// show/choose it; it is never stored inside hooks.json itself.
type hookAPIEntry struct {
	agent.UserHookDef
	Scope string `json:"scope"` // "global" | "workspace"
}

const (
	hookScopeGlobal    = "global"
	hookScopeWorkspace = "workspace"
)

// hooksPaths resolves the two hooks.json locations for the running server.
// workspacePath is "" when no workspace root is set (e.g. TUI-less server
// boot before a session picks a directory) — callers must treat that as
// "workspace scope unavailable."
func (s *Server) hooksPaths() (globalPath, workspacePath string) {
	if s.orch == nil {
		return "", ""
	}
	globalPath = agent.GlobalHooksPath(s.orch.HuginnHome())
	if root := s.orch.WorkspaceRoot(); root != "" {
		workspacePath = agent.WorkspaceHooksPath(root)
	}
	return
}

func (s *Server) hooksPathForScope(scope string) (string, bool) {
	globalPath, workspacePath := s.hooksPaths()
	switch scope {
	case hookScopeGlobal:
		return globalPath, globalPath != ""
	case hookScopeWorkspace, "":
		return workspacePath, workspacePath != ""
	default:
		return "", false
	}
}

// readHooksFileEntries reads one hooks.json (missing file = empty, not an
// error) and tags every entry with scope.
func readHooksFileEntries(path, scope string) ([]hookAPIEntry, error) {
	if path == "" {
		return nil, nil
	}
	f, err := agent.LoadUserHooksFile(path)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, nil
	}
	out := make([]hookAPIEntry, 0, len(f.Hooks))
	for _, h := range f.Hooks {
		out = append(out, hookAPIEntry{UserHookDef: h, Scope: scope})
	}
	return out, nil
}

// writeHooksFile atomically writes hooks (already-validated defs, scope
// stripped) to path, creating the parent directory if needed.
func writeHooksFile(path string, defs []agent.UserHookDef) error {
	if path == "" {
		return errHooksNoWorkspace
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(agent.UserHooksFile{Hooks: defs}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var errHooksNoWorkspace = &hookScopeError{"no workspace is open — global hooks only"}

type hookScopeError struct{ msg string }

func (e *hookScopeError) Error() string { return e.msg }

// reloadUserHooks re-reads both hooks.json files into the live runner so an
// API-driven create/update/delete takes effect without a restart, mirroring
// the auto-reload-on-edit behavior for hand-edited files.
func (s *Server) reloadUserHooks() error {
	if s.orch == nil {
		return nil
	}
	runner := s.orch.UserHooks()
	if runner == nil {
		return nil
	}
	globalPath, workspacePath := s.hooksPaths()
	return runner.Load(globalPath, workspacePath)
}

func (s *Server) handleListHooks(w http.ResponseWriter, r *http.Request) {
	globalPath, workspacePath := s.hooksPaths()
	globalEntries, err := readHooksFileEntries(globalPath, hookScopeGlobal)
	if err != nil {
		jsonError(w, 422, "global hooks.json: "+err.Error())
		return
	}
	workspaceEntries, err := readHooksFileEntries(workspacePath, hookScopeWorkspace)
	if err != nil {
		jsonError(w, 422, "workspace hooks.json: "+err.Error())
		return
	}
	all := append(globalEntries, workspaceEntries...)
	if all == nil {
		all = []hookAPIEntry{}
	}
	jsonOK(w, map[string]any{"hooks": all})
}

func (s *Server) handleCreateHook(w http.ResponseWriter, r *http.Request) {
	var entry hookAPIEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if entry.Scope == "" {
		entry.Scope = hookScopeWorkspace
	}
	path, ok := s.hooksPathForScope(entry.Scope)
	if !ok {
		jsonError(w, 400, "unknown or unavailable scope: "+entry.Scope)
		return
	}
	existing, err := agent.LoadUserHooksFile(path)
	if err != nil {
		jsonError(w, 422, "existing hooks.json is invalid: "+err.Error())
		return
	}
	var defs []agent.UserHookDef
	if existing != nil {
		defs = existing.Hooks
	}
	for _, h := range defs {
		if h.ID == entry.ID {
			jsonError(w, 409, "a hook with id "+entry.ID+" already exists in this scope")
			return
		}
	}
	defs = append(defs, entry.UserHookDef)
	if err := validateHooksList(defs); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := writeHooksFile(path, defs); err != nil {
		jsonError(w, 500, "couldn't save hooks.json: "+err.Error())
		return
	}
	if err := s.reloadUserHooks(); err != nil {
		slogWarnHookReload(err)
	}
	jsonOK(w, entry)
}

func (s *Server) handleUpdateHook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		jsonError(w, 400, "hook id is required")
		return
	}
	var entry hookAPIEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if entry.ID == "" {
		entry.ID = id
	}
	scope, path, defs, idx, err := s.findHookScope(id)
	if err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	// A scope change moves the hook from one file to the other.
	targetScope := entry.Scope
	if targetScope == "" {
		targetScope = scope
	}
	if targetScope != scope {
		targetPath, ok := s.hooksPathForScope(targetScope)
		if !ok {
			jsonError(w, 400, "unknown or unavailable scope: "+targetScope)
			return
		}
		// Build and validate the target list BEFORE writing anything —
		// mirroring the non-scope-change branch below. Only once the
		// target write has actually succeeded do we remove the hook from
		// its source file: writing source-then-target let a validation or
		// write failure on the target leave the hook in neither file
		// (F2). Worst case now is a transient duplicate in both files if
		// the second write fails, which is recoverable; the hook can
		// never vanish.
		targetExisting, err := agent.LoadUserHooksFile(targetPath)
		if err != nil {
			jsonError(w, 422, "target hooks.json is invalid: "+err.Error())
			return
		}
		var targetDefs []agent.UserHookDef
		if targetExisting != nil {
			targetDefs = targetExisting.Hooks
		}
		targetDefs = append(targetDefs, entry.UserHookDef)
		if err := validateHooksList(targetDefs); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if err := writeHooksFile(targetPath, targetDefs); err != nil {
			jsonError(w, 500, "couldn't save hooks.json: "+err.Error())
			return
		}
		remaining := append(defs[:idx:idx], defs[idx+1:]...)
		if err := writeHooksFile(path, remaining); err != nil {
			jsonError(w, 500, "hook saved to new scope, but couldn't remove it from the previous scope (now present in both): "+err.Error())
			return
		}
	} else {
		defs[idx] = entry.UserHookDef
		if err := validateHooksList(defs); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if err := writeHooksFile(path, defs); err != nil {
			jsonError(w, 500, "couldn't save hooks.json: "+err.Error())
			return
		}
	}
	if err := s.reloadUserHooks(); err != nil {
		slogWarnHookReload(err)
	}
	entry.Scope = targetScope
	jsonOK(w, entry)
}

func (s *Server) handleDeleteHook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		jsonError(w, 400, "hook id is required")
		return
	}
	_, path, defs, idx, err := s.findHookScope(id)
	if err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	remaining := append(defs[:idx:idx], defs[idx+1:]...)
	if err := writeHooksFile(path, remaining); err != nil {
		jsonError(w, 500, "couldn't save hooks.json: "+err.Error())
		return
	}
	if err := s.reloadUserHooks(); err != nil {
		slogWarnHookReload(err)
	}
	jsonOK(w, map[string]bool{"deleted": true})
}

// findHookScope locates id in either scope's hooks.json, returning the
// scope name, its file path, the full decoded def list, and id's index.
// Workspace is checked first to match runtime precedence: mergeUserHooks
// (see internal/agent/hooks_config.go) loads global then workspace and the
// LAST occurrence of a given id wins, so workspace is the copy actually
// firing when the same id exists in both files. Searching global first
// here would let an edit/delete silently target the inert global copy
// while the active workspace one keeps running (F3).
func (s *Server) findHookScope(id string) (scope, path string, defs []agent.UserHookDef, idx int, err error) {
	globalPath, workspacePath := s.hooksPaths()
	for _, cand := range []struct {
		scope, path string
	}{{hookScopeWorkspace, workspacePath}, {hookScopeGlobal, globalPath}} {
		if cand.path == "" {
			continue
		}
		f, loadErr := agent.LoadUserHooksFile(cand.path)
		if loadErr != nil || f == nil {
			continue
		}
		for i, h := range f.Hooks {
			if h.ID == id {
				return cand.scope, cand.path, f.Hooks, i, nil
			}
		}
	}
	return "", "", nil, -1, &hookScopeError{"hook not found: " + id}
}

func (s *Server) handleReloadHooks(w http.ResponseWriter, r *http.Request) {
	if err := s.reloadUserHooks(); err != nil {
		jsonError(w, 422, "reload failed: "+err.Error())
		return
	}
	jsonOK(w, map[string]bool{"reloaded": true})
}

// handleTestHook dry-runs one hook (either by {"id": "..."} against a
// stored hook, or {"hook": {...}} for one that hasn't been saved yet)
// against a sample tool call, WITHOUT touching hooks.json or the live
// runner's state — pure "what would this do" preview.
func (s *Server) handleTestHook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string             `json:"id"`
		Hook *agent.UserHookDef `json:"hook"`
		Tool string             `json:"tool"`
		Args map[string]any     `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.Tool == "" {
		req.Tool = "bash"
	}
	def := req.Hook
	if def == nil {
		if req.ID == "" {
			jsonError(w, 400, "pass either \"id\" (a saved hook) or \"hook\" (an inline def)")
			return
		}
		_, _, defs, idx, err := s.findHookScope(req.ID)
		if err != nil {
			jsonError(w, 404, err.Error())
			return
		}
		def = &defs[idx]
	}
	if err := validateHooksList([]agent.UserHookDef{*def}); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	var audit *agent.HookAuditLog
	if s.orch != nil {
		if runner := s.orch.UserHooks(); runner != nil {
			audit = runner.AuditLog()
		}
	}
	result := agent.RunHookForTest(r.Context(), audit, *def, req.Tool, req.Args)
	jsonOK(w, result)
}

func (s *Server) handleHooksAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := parseAuditLimit(raw); err == nil && v > 0 {
			limit = v
		}
	}
	var entries []agent.HookAuditEntry
	if s.orch != nil {
		if runner := s.orch.UserHooks(); runner != nil {
			entries = runner.AuditLog().Recent(limit)
		}
	}
	if entries == nil {
		entries = []agent.HookAuditEntry{}
	}
	jsonOK(w, map[string]any{"entries": entries})
}

func parseAuditLimit(raw string) (int, error) {
	return strconv.Atoi(raw)
}

// slogWarnHookReload logs a best-effort reload failure after a hooks.json
// write. The write itself already succeeded (and was already validated
// before writing), so this should be unreachable in practice — it exists as
// a diagnostic if the live runner and disk ever disagree.
func slogWarnHookReload(err error) {
	slog.Warn("hooks: reload after write failed", "err", err)
}

// validateHooksList re-runs the same validation LoadUserHooksFile applies,
// against an in-memory list — used so create/update/test-run reject a bad
// hook with the same loud, specific errors a hand-edited file would get,
// before anything touches disk.
func validateHooksList(defs []agent.UserHookDef) error {
	raw, err := json.Marshal(agent.UserHooksFile{Hooks: defs})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "huginn-hooks-validate-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_, err = agent.LoadUserHooksFile(tmp.Name())
	return err
}
