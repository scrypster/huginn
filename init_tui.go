package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/pricing"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/storage"
	storesym "github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/tui"
	"github.com/scrypster/huginn/internal/tui/services"
)

// tuiResult holds the fully initialized TUI application.
type tuiResult struct {
	App        *tui.App
	SessStore  session.StoreInterface
	ActiveSess *session.Session
}

// initTUI creates and wires the TUI application with session persistence,
// agent registry, stats, skills, and notepad support.
func initTUI(
	cfg *config.Config,
	huginnHome string,
	cwd string,
	version string,
	orch *agent.Orchestrator,
	detection repo.DetectionResult,
	idx *repo.Index,
	store *storage.Store,
	sqlDB *sqlitedb.DB,
	agentReg *agentslib.AgentRegistry,
	statsReg *stats.Registry,
	skillReg *skills.SkillRegistry,
	priceTracker *pricing.SessionTracker,
	autoRunAtom *atomic.Bool,
	toolsEnabled bool,
) tuiResult {
	var res tuiResult

	tuiModels := &modelconfig.Models{
		Reasoner: cfg.ReasonerModel,
	}
	if tuiModels.Reasoner == "" {
		tuiModels.Reasoner = cfg.DefaultModel
	}
	tuiApp := tui.New(cfg, orch, tuiModels, version)
	tuiApp.SetUseAgentLoop(toolsEnabled)
	tuiApp.SetStatsRegistry(statsReg)
	tuiApp.SetWorkspace(detection.Root, idx)
	tuiApp.SetStore(store)
	tuiApp.SetAgentRegistry(agentReg)
	tuiApp.SetPriceTracker(priceTracker)

	// Load channel names (and their lead agents) from the spaces store and wire
	// them into the sidebar and channel picker.
	if sqlDB != nil {
		spaceStore := spaces.NewSQLiteSpaceStore(sqlDB)
		if spaceRes, listErr := spaceStore.ListSpaces(spaces.ListOpts{Kind: spaces.KindChannel}); listErr == nil {
			var channelNames []string
			channelLeads := make(map[string]string)
			for _, sp := range spaceRes.Spaces {
				channelNames = append(channelNames, sp.Name)
				if sp.LeadAgent != "" {
					channelLeads[sp.Name] = sp.LeadAgent
				}
			}
			tuiApp.SetChannels(channelNames)
			tuiApp.SetChannelLeads(channelLeads)
		}
	}

	// Set the primary agent: use first alphabetical agent.
	{
		names := agentReg.Names()
		if len(names) > 0 {
			sort.Strings(names)
			tuiApp.SetPrimaryAgent(names[0])
		}
	}
	tuiApp.SetSkillRegistry(skillReg)

	// Wire MuninnDB connection status
	{
		muninnHome, _ := os.UserHomeDir()
		muninnCfgPath := filepath.Join(muninnHome, ".config", "huginn", "muninn.json")
		if muninnCfg, muninnErr := memory.LoadGlobalConfig(muninnCfgPath); muninnErr == nil {
			tuiApp.SetMuninnConnection(muninnCfg.Endpoint, muninnCfg.Endpoint != "")
		}
	}

	// --- Session persistence ---
	sessStore := initSessionStore(huginnHome, sqlDB)
	tuiApp.SetSessionStore(sessStore)
	orch.SetSessionStore(sessStore) // enable HydrateSession on resume
	res.SessStore = sessStore

	activeModel := cfg.DefaultModel
	newSess := sessStore.New(
		fmt.Sprintf("Session %s", time.Now().Format("2006-01-02 15:04")),
		cwd,
		activeModel,
	)
	tuiApp.SetActiveSession(newSess)
	res.ActiveSess = newSess

	tuiApp.SetAutoRunAtom(autoRunAtom)

	if cfg.NotepadsEnabled {
		if npMgr, err := notepad.DefaultManager(detection.Root); err == nil {
			tuiApp.SetNotepadManager(npMgr)
		}
	}

	// --- Background symbol extraction ---
	startSymbolExtraction(idx, store)

	// Build AppContext for future screen consumption.
	appCtx := &services.AppContext{
		Cfg:          cfg,
		Orch:         orch,
		Version:      version,
		AgentReg:     agentReg,
		SessionStore: sessStore,
		StatsReg:     statsReg,
		Store:        store,
		Idx:          idx,
		WorkspaceRoot: detection.Root,
		PriceTracker: priceTracker,
		SkillReg:     skillReg,
	}
	appCtx.Agents = services.NewDirectAgentService(agentReg)
	appCtx.Config = services.NewDirectConfigService(cfg)
	appCtx.Stats = services.NewDirectStatsService(statsReg)
	tuiApp.SetAppContext(appCtx)

	res.App = tuiApp
	slog.Info("tui: app initialized")
	return res
}

// initSessionStore sets up the session store, running filesystem → SQLite migration
// when SQLite is available.
func initSessionStore(huginnHome string, sqlDB *sqlitedb.DB) session.StoreInterface {
	sessionsDir := filepath.Join(huginnHome, "sessions")

	if sqlDB != nil {
		if migrErr := session.MigrateFromFilesystem(sessionsDir, sqlDB); migrErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: session migration: %v\n", migrErr)
		}
		slog.Info("storage: session store opened (sqlite)")
		return session.NewSQLiteSessionStore(sqlDB)
	}

	slog.Info("storage: session store opened (filesystem)", "dir", sessionsDir)
	return session.NewStore(sessionsDir)
}

// startSymbolExtraction starts background symbol extraction from repo chunks.
func startSymbolExtraction(idx *repo.Index, store *storage.Store) {
	if store == nil {
		return
	}
	symReg := buildSymbolRegistry()
	go func() {
		seen := make(map[string]bool)
		for _, chunk := range idx.Chunks {
			if chunk.StartLine != 1 {
				continue
			}
			if seen[chunk.Path] {
				continue
			}
			seen[chunk.Path] = true

			absPath := filepath.Join(idx.Root, chunk.Path)
			data, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			syms, edges, err := symReg.Extract(chunk.Path, data)
			if err != nil {
				slog.Warn("tui: symbol extraction error, skipping file", "path", chunk.Path, "err", err)
				continue
			}
			if len(syms) > 0 {
				storeSyms := make([]storesym.Symbol, len(syms))
				for i, s := range syms {
					storeSyms[i] = storesym.Symbol{
						Name:     s.Name,
						Kind:     string(s.Kind),
						Path:     s.Path,
						Line:     s.Line,
						Exported: s.Exported,
					}
				}
				_ = store.SetSymbols(chunk.Path, storeSyms)
			}
			for _, e := range edges {
				_ = store.SetEdge(e.From, e.To, storesym.Edge{
					From:       e.From,
					To:         e.To,
					Symbol:     e.Symbol,
					Confidence: string(e.Confidence),
					Kind:       string(e.Kind),
				})
			}
		}
	}()
}

