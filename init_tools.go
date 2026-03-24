package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/symbol/lsp"
	"github.com/scrypster/huginn/internal/tools"
	"github.com/scrypster/huginn/internal/tui"
)

// toolsResult holds the initialized tool registry, permission gate, and related state.
type toolsResult struct {
	ToolReg     *tools.Registry
	Gate        *permissions.Gate
	AutoRunAtom *atomic.Bool
	PermReqCh   chan tui.PermissionPromptMsg
	MCPMgr      *mcp.ServerManager
	StopLSP     func() // stops all tracked LSP server processes
}

// initTools builds the tool registry, registers all built-in and external tools,
// starts MCP servers, and wires the permission gate.
func initTools(
	ctx context.Context,
	cfg config.Config,
	huginnHome string,
	cwd string,
	sqlDB *sqlitedb.DB,
	loadedSkills []skills.Skill,
	agentReg *agentslib.AgentRegistry,
	orch *agent.Orchestrator,
	dangerouslySkipPermissions bool,
	artifactStore artifact.Store,
) toolsResult {
	res := toolsResult{}

	// autoRunAtom and permReqCh are wired between gate and TUI.
	autoRunAtom := &atomic.Bool{}
	autoRunAtom.Store(true) // default: auto-approve
	permReqCh := make(chan tui.PermissionPromptMsg, 1)
	res.AutoRunAtom = autoRunAtom
	res.PermReqCh = permReqCh

	bashTimeout := time.Duration(cfg.BashTimeoutSecs) * time.Second
	if bashTimeout == 0 {
		bashTimeout = 120 * time.Second
	}

	toolReg := tools.NewRegistry()
	tools.RegisterBuiltins(toolReg, cwd, bashTimeout)
	tools.RegisterGitTools(toolReg, cwd)
	tools.RegisterTestsTool(toolReg, cwd, bashTimeout)
	tools.RegisterGitHubTools(toolReg)
	toolReg.TagTools(tools.GitHubCLIToolNames(), "github_cli")
	toolReg.TagTools(tools.BuiltinToolNames(), "builtin")
	tools.RegisterWorktreeTools(toolReg, cwd)
	tools.RegisterNotesTool(toolReg, huginnHome, agentReg)

	// --- Connection (OAuth) tools ---
	initConnectionTools(cfg, huginnHome, sqlDB, toolReg)

	// --- Skill PromptTools ---
	for _, s := range loadedSkills {
		for _, t := range s.Tools() {
			toolReg.Register(t)
		}
	}

	// --- Artifact tool ---
	if artifactStore != nil {
		toolReg.Register(tools.NewArtifactTool(artifactStore))
	}

	// --- LSP tools ---
	res.StopLSP = initLSPTools(cwd, toolReg)

	// --- MCP servers ---
	var mcpMgr *mcp.ServerManager
	if len(cfg.MCPServers) > 0 {
		mcpMgr = mcp.NewServerManager(cfg.MCPServers)
		mcpMgr.StartAll(ctx, toolReg)
		slog.Info("tools: MCP servers started", "count", len(cfg.MCPServers))
	}
	res.MCPMgr = mcpMgr

	// --- Allowed/disallowed filters ---
	if len(cfg.AllowedTools) > 0 {
		toolReg.SetAllowed(cfg.AllowedTools)
	}
	if len(cfg.DisallowedTools) > 0 {
		toolReg.SetBlocked(cfg.DisallowedTools)
	}

	// --- Permission gate ---
	gate := permissions.NewGate(dangerouslySkipPermissions, func(req permissions.PermissionRequest) permissions.Decision {
		if autoRunAtom.Load() {
			return permissions.Allow
		}
		respCh := make(chan permissions.Decision, 1)
		permReqCh <- tui.PermissionPromptMsg{Req: req, RespCh: respCh}
		return <-respCh
	})
	res.Gate = gate
	res.ToolReg = toolReg

	orch.SetTools(toolReg, gate)

	// Wire orchestrator as AgentExecutor into skill tools (Phase 2).
	skills.InjectAgentExecutor(toolReg, orch)

	slog.Info("tools: registry initialized")
	return res
}

// initConnectionTools registers OAuth integration tools for all configured connections.
func initConnectionTools(cfg config.Config, huginnHome string, sqlDB *sqlitedb.DB, toolReg *tools.Registry) {
	connStorePath := filepath.Join(huginnHome, "connections.json")
	if sqlDB != nil {
		if err := connections.MigrateFromJSON(connStorePath, sqlDB); err != nil {
			slog.Info("connections: migration warning", "err", err)
		}
	}

	var connStore connections.StoreInterface
	var connStoreErr error
	if sqlDB != nil {
		connStore = connections.NewSQLiteConnectionStore(sqlDB)
	} else {
		connStore, connStoreErr = connections.NewStore(connStorePath)
		if connStoreErr != nil {
			slog.Info("connections: failed to open store", "err", connStoreErr)
		}
	}
	if connStore == nil {
		return
	}

	connSecrets := connections.NewSecretStore()
	webPort := cfg.WebUI.Port
	if webPort == 0 {
		webPort = 8477
	}
	redirectURL := fmt.Sprintf("http://localhost:%d/oauth/callback", webPort)
	connMgr := connections.NewManager(connStore, connSecrets, redirectURL)
	if regErr := conntools.RegisterAll(toolReg, connMgr, connStore); regErr != nil {
		slog.Info("connections: tool registration failed", "err", regErr)
	}
	_ = connMgr
}

// langExtensions maps language names to the file extensions that indicate
// a project uses that language. Only start LSP servers when relevant files exist.
var langExtensions = map[string][]string{
	"go":         {".go"},
	"typescript": {".ts", ".tsx"},
	"javascript": {".js", ".jsx"},
	"rust":       {".rs"},
	"python":     {".py"},
}

// projectHasLanguage reports whether cwd (or its immediate subdirectories)
// contains at least one file with the given extensions.
func projectHasLanguage(cwd string, exts []string) bool {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return false
	}
	extSet := make(map[string]bool, len(exts))
	for _, e := range exts {
		extSet[e] = true
	}
	for _, entry := range entries {
		name := entry.Name()
		for _, ext := range exts {
			if strings.HasSuffix(name, ext) {
				return true
			}
		}
		if entry.IsDir() && !strings.HasPrefix(name, ".") {
			sub, err := os.ReadDir(filepath.Join(cwd, name))
			if err != nil {
				continue
			}
			for _, subEntry := range sub {
				for _, ext := range exts {
					if strings.HasSuffix(subEntry.Name(), ext) {
						return true
					}
				}
			}
		}
	}
	return false
}

// initLSPTools detects and starts LSP servers for languages present in cwd,
// then registers the LSP tools into the registry.
// Returns a stop function that shuts down all started LSP server processes.
func initLSPTools(cwd string, toolReg *tools.Registry) func() {
	lspMgrs := make(map[string]tools.LSPManager)
	var started []*lsp.Manager
	for _, lang := range lsp.SupportedLanguages() {
		// Skip languages whose files aren't present in this project.
		if exts, ok := langExtensions[lang]; ok && !projectHasLanguage(cwd, exts) {
			continue
		}
		if detected := lsp.Detect(lang); detected.Command != "" {
			mgr := lsp.NewManager(lang, detected)
			go func(m *lsp.Manager, language string) {
				if err := m.Start(cwd); err != nil {
					slog.Warn("huginn: LSP start failed", "lang", language, "err", err)
				}
			}(mgr, lang)
			lspMgrs[lang] = mgr
			started = append(started, mgr)
		}
	}
	tools.RegisterLSPTools(toolReg, cwd, lspMgrs)
	return func() {
		for _, m := range started {
			if err := m.Stop(); err != nil {
				slog.Warn("huginn: LSP stop failed", "err", err)
			}
		}
	}
}
