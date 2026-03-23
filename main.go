package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agent"
	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/compact"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/headless"
	"github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/pricing"
	modelslib "github.com/scrypster/huginn/internal/models"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/runtime"
	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/search/hnsw"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/workspace"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/symbol"
	"github.com/scrypster/huginn/internal/symbol/goext"
	"github.com/scrypster/huginn/internal/symbol/heuristic"
	"github.com/scrypster/huginn/internal/symbol/lsp"
	"github.com/scrypster/huginn/internal/symbol/tsext"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/connections/broker"
	connproviders "github.com/scrypster/huginn/internal/connections/providers"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/scheduler"
	"github.com/scrypster/huginn/internal/server"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/session"
	agentsession "github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/tools"
	traypkg "github.com/scrypster/huginn/internal/tray"
	"github.com/scrypster/huginn/internal/tui"
	"github.com/cockroachdb/pebble/v2"
)

// version is set at build time via -ldflags="-X main.version=<tag>".
// Falls back to "dev" for local builds without a tag.
var version = "dev"

func main() {
	// --- Flags ---
	versionFlag                  := flag.Bool("version", false, "print version and exit")
	headlessFlag                 := flag.Bool("headless", false, "run in headless mode (no TUI)")
	cwdFlag                      := flag.String("cwd", "", "working directory (headless mode)")
	commandFlag                  := flag.String("command", "", "slash command to run (headless mode)")
	jsonFlag                     := flag.Bool("json", false, "output JSON (headless mode)")
	workspaceFlag                := flag.String("workspace", "", "path to huginn.workspace.json")
	dangerouslySkipPermissions   := flag.Bool("dangerously-skip-permissions", false, "skip all permission prompts (allows all tool use without approval)")
	noToolsFlag                  := flag.Bool("no-tools", false, "disable tool use (plain chat mode)")
	maxTurnsFlag                 := flag.Int("max-turns", 0, "max agentic loop turns (0 = use config default)")
	modelFlag                    := flag.String("model", "", "set coder model (overrides config)")
	printFlag                    := flag.String("print", "", "non-interactive: run message and print response, then exit")
	endpointFlag                 := flag.String("endpoint", "", "OpenAI-compatible backend endpoint (overrides config)")
	agentFlag                    := flag.String("agent", "", "run with a specific named agent (e.g. 'Chris'); omit message to launch TUI")
	noTrayFlag                   := flag.Bool("no-tray", false, "disable system tray (headless/CI mode)")
	flag.StringVar(printFlag, "p", "", "shorthand for --print")
	flag.Parse()

	// Handle legacy -v / version sub-command
	if *versionFlag || (len(flag.Args()) > 0 && flag.Args()[0] == "version") {
		fmt.Printf("huginn %s\n", version)
		os.Exit(0)
	}

	// 1. Load config
	cfg, err := config.Load()
	if err != nil {
		fatalf("config: %v", err)
	}
	if *workspaceFlag != "" {
		cfg.WorkspacePath = *workspaceFlag
	}

	// 1b. Subcommand routing (before TUI launch)
	if len(flag.Args()) > 0 {
		switch flag.Args()[0] {
		case "init":
			if err := cmdInit(); err != nil {
				fmt.Fprintf(os.Stderr, "init: %v\n", err)
				os.Exit(1)
			}
			return
		case "pull":
			if len(flag.Args()) < 2 {
				fmt.Fprintln(os.Stderr, "usage: huginn pull <model-name-or-url>")
				os.Exit(1)
			}
			if err := cmdPull(flag.Args()[1]); err != nil {
				fmt.Fprintf(os.Stderr, "pull: %v\n", err)
				os.Exit(1)
			}
			return
		case "models":
			if err := cmdModels(flag.Args()[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "models: %v\n", err)
				os.Exit(1)
			}
			return
		case "runtime":
			if err := cmdRuntime(flag.Args()[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "runtime: %v\n", err)
				os.Exit(1)
			}
			return
		case "relay":
			if err := cmdRelay(flag.Args()[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "relay: %v\n", err)
				os.Exit(1)
			}
			return
		case "serve":
			serveFlags := flag.NewFlagSet("serve", flag.ContinueOnError)
			foregroundFlag := serveFlags.Bool("foreground", false, "run server in foreground (don't daemonize)")
			daemonFlag := serveFlags.Bool("daemon", false, "internal: already running as daemon subprocess")
			_ = serveFlags.Parse(flag.Args()[1:])
			cmdServe(cfg, *noTrayFlag, *foregroundFlag, *daemonFlag)
			return
		case "tray":
			trayFlags := flag.NewFlagSet("tray", flag.ContinueOnError)
			attachAddr := trayFlags.String("attach", "", "attach to existing server at this address")
			_ = trayFlags.Parse(flag.Args()[1:])
			cmdTray(cfg, *attachAddr)
			return
		case "connect":
			if err := cmdConnect(); err != nil {
				fmt.Fprintf(os.Stderr, "connect: %v\n", err)
				os.Exit(1)
			}
			return
		case "cloud":
			if err := cmdCloud(cfg, flag.Args()[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "cloud: %v\n", err)
				os.Exit(1)
			}
			return
		case "stats":
			if err := cmdStats(flag.Args()[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "stats: %v\n", err)
				os.Exit(1)
			}
			return
		case "export":
			handleExportCommand(flag.Args()[1:])
			return
		case "report-bug":
			cmdReportBug(flag.Args()[1:])
			return
		case "logs":
			if err := cmdLogs(flag.Args()[1:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "agents":
			if err := cmdAgents(flag.Args()[1:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
	case "skill":
			if err := cmdSkill(flag.Args()[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "skill: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// 1c. Initialize logger — writes to ~/.huginn/logs/huginn.log with 10MB rotation.
	// logger.Init sets the package-level global so that logger.Error() calls in ws.go
	// and other packages write to the file rather than being silently discarded.
	huginnHome, _ := huginnDir()
	_ = logger.Init(huginnHome)
	appLog := logger.L()
	defer appLog.Close()
	appLog.Info("huginn starting", "version", version)

	// 1d. Install crash handler — writes to ~/.huginn/crash/<timestamp>.txt on panic.
	crashDir := filepath.Join(huginnHome, "crash")
	defer logger.InstallPanicHandler(crashDir)()

	// 1e. Migrate agents from legacy agents.json to per-file storage (best-effort).
	if migrateErr := agentslib.MigrateAgents(huginnHome); migrateErr != nil {
		appLog.Info("agents migration skipped", "err", migrateErr)
	}
	if err := agentslib.MigrateEmptyToolbeltToWildcard(huginnHome); err != nil {
		appLog.Info("migrate toolbelt: non-fatal", "err", err)
	}

	// 2. Determine working directory
	cwd, err := os.Getwd()
	if err != nil {
		fatalf("cwd: %v", err)
	}
	if *cwdFlag != "" {
		cwd = *cwdFlag
	}

	// 2b. Headless short-circuit — runs its own init pipeline, no TUI/Ollama needed.
	if *headlessFlag {
		hcfg := headless.HeadlessConfig{
			CWD:     cwd,
			Command: *commandFlag,
			JSON:    *jsonFlag,
		}
		result, err := headless.Run(hcfg)
		if err != nil {
			fatalf("headless: %v", err)
		}
		if *jsonFlag {
			data, _ := json.Marshal(result)
			fmt.Println(string(data))
		} else {
			fmt.Printf("huginn headless | mode=%s | root=%s | scanned=%d | skipped=%d | findings=%d\n",
				result.Mode, result.Root, result.FilesScanned, result.FilesSkipped, len(result.TopFindings))
			for _, f := range result.TopFindings {
				fmt.Printf("  [%s] %s (score=%.1f)\n", f.Severity, f.Title, f.Score)
			}
		}
		return
	}

	// 2c. --print / -p: non-interactive single-turn mode
	if *printFlag != "" {
		// Minimal init: no TUI, no Pebble, just backend
		endpoint := cfg.Backend.Endpoint
		if *endpointFlag != "" {
			endpoint = *endpointFlag
		}
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		printModels := modelconfig.DefaultModels()
		_ = printModels // used by NewOrchestrator
		printBackend := backend.NewExternalBackend(endpoint)
		printOrch, err := agent.NewOrchestrator(printBackend, printModels, nil, nil, nil, nil)
		if err != nil {
			fatalf("failed to create orchestrator: %v", err)
		}
		err = printOrch.Chat(context.Background(), *printFlag, func(token string) {
			fmt.Print(token)
		}, nil)
		fmt.Println()
		if err != nil {
			fatalf("print: %v", err)
		}
		return
	}

	// 2d. --agent non-interactive: huginn --agent Chris "do this task"
	if *agentFlag != "" && len(flag.Args()) > 0 {
		agentsCfg, agentsErr := agentslib.LoadAgents()
		if agentsErr != nil {
			agentsCfg = agentslib.DefaultAgentsConfig()
		}
		agentModels := modelconfig.DefaultModels()
		agentUsername := memory.ResolveUsername("")
		agentReg := agentslib.BuildRegistryWithUsername(agentsCfg, agentModels, agentUsername)

		ag, ok := agentReg.ByName(*agentFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown agent %q; available: %s\n",
				*agentFlag, strings.Join(agentReg.Names(), ", "))
			os.Exit(1)
		}
		if *modelFlag != "" {
			ag.SwapModel(*modelFlag)
		}
		msg := strings.Join(flag.Args(), " ")
		endpoint := cfg.Backend.Endpoint
		if *endpointFlag != "" {
			endpoint = *endpointFlag
		}
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		b := backend.NewExternalBackend(endpoint)
		systemPrompt := ag.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = fmt.Sprintf("You are %s, an expert assistant.", ag.Name)
		}
		_, err = b.ChatCompletion(context.Background(), backend.ChatRequest{
			Model: ag.GetModelID(),
			Messages: []backend.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: msg},
			},
			OnToken: func(token string) { fmt.Print(token) },
		})
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// 3. Workspace detection
	detection := repo.Detect(cwd, cfg.WorkspacePath)

	// 3b. Workspace manager — provides stable Root() for skills rule-file discovery.
	wsMgr, wsErr := workspace.NewManager(cwd)
	if wsErr != nil {
		fmt.Fprintf(os.Stderr, "huginn: warning: workspace discovery: %v\n", wsErr)
	}
	_ = wsMgr // consumed by skills loader

	// 4. Open Pebble store at ~/.huginn/store/<sanitized-root>
	storeDir := storeDir(detection.Root)
	store, err := storage.Open(storeDir)
	if err != nil {
		// Graceful degradation: log warning and continue without persistence
		fmt.Fprintf(os.Stderr, "huginn: warning: store unavailable: %v\n", err)
		store = nil
	}
	if store != nil {
		defer store.Close()
	}

	// 4b. Open SQLite database — primary structured data store.
	dbPath := filepath.Join(huginnHome, "huginn.db")
	sqlDB, err := sqlitedb.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "huginn: warning: sqlite unavailable: %v\n", err)
		sqlDB = nil
	}
	if sqlDB != nil {
		defer sqlDB.Close()
		if err := sqlDB.ApplySchema(); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: sqlite schema failed: %v\n", err)
			sqlDB = nil
		}
	}
	if sqlDB != nil {
		if err := sqlDB.Migrate(notification.Migrations()); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: notification column migrations failed: %v\n", err)
		}
	}
	if sqlDB != nil {
		if err := sqlDB.Migrate(session.Migrations()); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: session schema migrations failed: %v\n", err)
		}
	}
	// sqlDB is used below for connection and memory stores (Phase 1+).

	var memStore agentslib.MemoryStoreIface
	if sqlDB != nil && store != nil {
		// Migrate from Pebble to SQLite (idempotent).
		// Use relay.GetMachineID() (stable 8-char hex) — NOT cfg.MachineID (hostname-hex which
		// changes on machine rename and would orphan all stored summaries).
		if err := agentslib.MigrateAgentMemoryFromPebble(context.Background(), store, sqlDB, relay.GetMachineID()); err != nil {
			appLog.Info("agents: memory migration warning", "err", err)
		}
		memStore = agentslib.NewSQLiteMemoryStore(sqlDB.Write(), relay.GetMachineID())
	} else if store != nil {
		memStore = agentslib.NewMemoryStore(store, relay.GetMachineID())
	}

	// Wire cloud vault memory replicator — pushes agent memory writes to HuginnCloud
	// so they persist across sessions and are available from all connected browsers.
	if sqlDB != nil {
		tuiMemReplicator := wireMemoryReplicator(sqlDB)
		tuiMemReplicator.Start()
		defer tuiMemReplicator.Stop()
	}

	// 5. Stats registry
	statsReg := stats.NewRegistry()
	statsCollector := statsReg.Collector()

	// 6. Index the repo (interactive: show progress bar)
	idx := tui.RunLoader(detection.Root)

	// 7. Backend + model config
	var b backend.Backend
	switch cfg.Backend.Type {
	case "managed":
		// Phase 3: managed llama-server
		huginnHome, err := huginnDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "huginnDir: %v\n", err)
			os.Exit(1)
		}
		mgr, err := runtime.NewManager(huginnHome)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runtime: %v\n", err)
			os.Exit(1)
		}
		managedStore, err := modelslib.NewStore(huginnHome)
		if err != nil {
			fmt.Fprintf(os.Stderr, "model store: %v\n", err)
			os.Exit(1)
		}

		// If runtime or models aren't ready, skip managed backend and start anyway.
		// Users can configure a local model via the web UI (huginn serve).
		if needsOnboarding(mgr, managedStore) {
			fmt.Fprintln(os.Stderr, "huginn: local model not set up — configure via the web UI. Starting without local model.")
			b = backend.NewExternalBackend(cfg.Backend.Endpoint)
			break
		}

		port, portErr := runtime.FindFreePort()
		if portErr != nil {
			fmt.Fprintf(os.Stderr, "find free port: %v\n", portErr)
			os.Exit(1)
		}
		// Get first installed model
		installed, _ := managedStore.Installed()
		var modelPath string
		for _, entry := range installed {
			modelPath = entry.Path
			break
		}
		if modelPath == "" {
			fmt.Fprintln(os.Stderr, "huginn: no models installed — install via the web UI. Starting without local model.")
			b = backend.NewExternalBackend(cfg.Backend.Endpoint)
			break
		}
		// Kill orphaned llama-server from a previous crash.
		pidPath := filepath.Join(huginnHome, "llama.pid")
		runtime.CleanupZombie(pidPath)
		if err := mgr.Start(modelPath, port); err != nil {
			fmt.Fprintf(os.Stderr, "start runtime: %v\n", err)
			os.Exit(1)
		}
		// Write PID file (pid + port) for zombie cleanup on next launch.
		_ = runtime.WritePIDFile(pidPath, mgr.Cmd().Process.Pid, port)
		defer os.Remove(pidPath)
		if err := mgr.WaitForReady(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "runtime ready: %v\n", err)
			os.Exit(1)
		}
		b = backend.NewManagedBackend(mgr.Endpoint(), func(ctx context.Context) error {
			return mgr.Shutdown()
		})
		defer b.Shutdown(context.Background())
	default: // "external" or anything else
		endpoint := cfg.Backend.Endpoint
		if *endpointFlag != "" {
			endpoint = *endpointFlag
		}
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		b = backend.NewExternalBackend(endpoint)
		go func(ep string, be backend.Backend) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := be.Health(ctx); err != nil {
				appLog.Warn("backend: not reachable at startup", "endpoint", ep, "err", err)
			}
		}(endpoint, b)
	}

	models := modelconfig.DefaultModels()
	if cfg.ReasonerModel != "" {
		models.Reasoner = cfg.ReasonerModel
	}

	registry := modelconfig.NewRegistry(models)

	// 7b. Load agent registry (non-fatal: falls back to defaults)
	agentsCfg, agentsErr := agentslib.LoadAgents()
	if agentsErr != nil {
		agentsCfg = agentslib.DefaultAgentsConfig()
	}
	// Resolve username up-front so vault names include the user segment
	// (e.g. "huginn:agent:mj:steve" rather than "huginn:agent::steve").
	tuiUsername := memory.ResolveUsername(cwd)
	agentReg := agentslib.BuildRegistryWithUsername(agentsCfg, models, tuiUsername)

	// 7b-warn. Warn if any agent uses a literal API key instead of $ENV or keyring:
	for _, def := range agentsCfg.Agents {
		if backend.IsLiteralAPIKey(def.APIKey) {
			fmt.Fprintf(os.Stderr, "warning: agent %q has a literal API key; consider using $ENV_VAR or keyring:<service>:<user> instead\n", def.Name)
		}
	}

	// 7c. Build compactor for context management
	compactBudget := cfg.ContextLimitKB * 1024 / 4
	if compactBudget <= 0 {
		compactBudget = 32_000
	}
	var compactStrategy compact.CompactionStrategy
	if compactBudget > 8_000 {
		compactStrategy = compact.NewLLMStrategy(cfg.CompactTrigger)
	} else {
		compactStrategy = compact.NewExtractiveStrategyWithTrigger(cfg.CompactTrigger)
	}
	compactor := compact.New(compact.Config{
		Mode:         compact.Mode(cfg.CompactMode),
		Trigger:      cfg.CompactTrigger,
		BudgetTokens: compactBudget,
		Strategy:     compactStrategy,
	})

	// 8. Orchestrator (with registry + stats)
	orch, err := agent.NewOrchestrator(b, models, idx, registry, statsCollector, compactor)
	if err != nil {
		fatalf("failed to create orchestrator: %v", err)
	}
	backendCache := backend.NewBackendCache(b)
	orch.SetBackendCache(backendCache)
	orch.WithMachineID(relay.GetMachineID()) // stable 8-char hex, not cfg.MachineID (hostname-dependent)
	orch.SetGitRoot(detection.Root)
	orch.SetAgentRegistry(agentReg)
	orch.SetHuginnHome(huginnHome)
	if memStore != nil {
		orch.SetMemoryStore(memStore)
	}

	// 8f. Wire HuginnCloud relay hub.
	// Dispatcher is wired below after gate creation (see "Wire relay dispatcher").
	var tuiRelayHub relay.Hub
	{
		satellite := relay.NewSatellite(os.Getenv("HUGINN_CLOUD_URL"))
		tuiRelayHub = satellite.Hub(context.Background())
		orch.SetRelayHub(tuiRelayHub)
	}

	// 8d. Load skills + workspace rules and inject into context builder.
	skillLoader := skills.DefaultLoader()
	loadedSkills, loadErrs := skillLoader.LoadAll()
	for _, e := range loadErrs {
		fmt.Fprintf(os.Stderr, "huginn: warning: skills load: %v\n", e)
	}
	skillReg := skills.NewSkillRegistry()
	// Load built-ins first so user skills can override them.
	if builtinErrs := skillReg.LoadBuiltins(); len(builtinErrs) > 0 {
		for _, e := range builtinErrs {
			fmt.Fprintf(os.Stderr, "huginn: warning: skills built-ins: %v\n", e)
		}
	}
	for _, s := range loadedSkills {
		skillReg.Register(s)
	}
	var skillsFragmentParts []string
	if combined := skillReg.CombinedPromptFragment(); combined != "" {
		skillsFragmentParts = append(skillsFragmentParts, combined)
	}
	if rules := skillLoader.LoadRuleFiles(cwd); rules != "" {
		skillsFragmentParts = append(skillsFragmentParts, rules)
	}
	if len(skillsFragmentParts) > 0 {
		orch.SetSkillsFragment(strings.Join(skillsFragmentParts, "\n\n"))
	}

	// 8d. Load notepads if enabled
	if cfg.NotepadsEnabled {
		if npMgr, err := notepad.DefaultManager(detection.Root); err == nil {
			if loaded, err := npMgr.Load(); err == nil && len(loaded) > 0 {
				orch.SetNotepads(loaded)
			}
		}
	}

	// 8a. Setup semantic search if enabled
	if cfg.SemanticSearch {
		embedder := search.NewOllamaEmbedder(cfg.OllamaBaseURL, cfg.EmbeddingModel)
		probeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := embedder.Probe(probeCtx); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: semantic search disabled (Ollama not reachable: %v)\n", err)
		} else {
			// Index repo chunks
			bm25 := search.NewBM25Searcher()
			hnswIdx := hnsw.New(16, 200)

			// Convert repo.FileChunk to search.Chunk and index
			if idx != nil {
				var searchChunks []search.Chunk
				for i, chunk := range idx.Chunks {
					searchChunks = append(searchChunks, search.Chunk{
						ID:        uint64(i + 1),
						Path:      chunk.Path,
						Content:   chunk.Content,
						StartLine: chunk.StartLine,
					})
				}

				indexCtx, indexCancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := bm25.Index(indexCtx, searchChunks); err != nil {
					fmt.Fprintf(os.Stderr, "huginn: BM25 indexing failed: %v\n", err)
					indexCancel()
				} else {
					// Create hybrid searcher and wire into orchestrator
					hybrid := search.NewHybridSearcher(bm25, hnswIdx, embedder)
					if err := hybrid.Index(indexCtx, searchChunks); err != nil {
						fmt.Fprintf(os.Stderr, "huginn: hybrid search indexing failed: %v\n", err)
					} else {
						orch.SetSearcher(hybrid)
					}
					indexCancel()
				}
			}
		}
	}

	// 8b. Apply flag overrides to config
	toolsEnabled := cfg.ToolsEnabled
	if *noToolsFlag {
		toolsEnabled = false
	}
	if *maxTurnsFlag > 0 {
		cfg.MaxTurns = *maxTurnsFlag
	}

	// autoRunAtom and permReqCh are declared here so they are accessible both
	// inside the toolsEnabled block (gate creation) and outside (TUI wiring).
	autoRunAtom := &atomic.Bool{}
	autoRunAtom.Store(true) // default: auto-approve, matches TUI default
	permReqCh := make(chan tui.PermissionPromptMsg, 1)

	// 8c. Wire tool registry + permission gate
	if toolsEnabled {
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

		// Register integration (OAuth) tools for all configured connections.
		{
			// Run one-time migration from JSON to SQLite (idempotent).
			connStorePath := filepath.Join(huginnHome, "connections.json")
			if sqlDB != nil {
				if err := connections.MigrateFromJSON(connStorePath, sqlDB); err != nil {
					appLog.Info("connections: migration warning", "err", err)
				}
			}

			// Use SQLite store if available, fall back to JSON store.
			var connStore connections.StoreInterface
			var connStoreErr error
			if sqlDB != nil {
				connStore = connections.NewSQLiteConnectionStore(sqlDB)
			} else {
				connStore, connStoreErr = connections.NewStore(connStorePath)
				if connStoreErr != nil {
					appLog.Info("connections: failed to open store", "err", connStoreErr)
				}
			}
			if connStore != nil {
				connSecrets := connections.NewSecretStore()
				webPort := cfg.WebUI.Port
				if webPort == 0 {
					webPort = 8477
				}
				redirectURL := fmt.Sprintf("http://localhost:%d/oauth/callback", webPort)
				connMgr := connections.NewManager(connStore, connSecrets, redirectURL)
				if regErr := conntools.RegisterAll(toolReg, connMgr, connStore); regErr != nil {
					appLog.Info("connections: tool registration failed", "err", regErr)
				}
				_ = connMgr
			}
		}

		// Register PromptTools from loaded skills (Phase 2).
		for _, s := range loadedSkills {
			for _, t := range s.Tools() {
				toolReg.Register(t)
			}
		}

		// Register LSP tools (graceful if no LSP configured)
		{
			lspMgrs := make(map[string]tools.LSPManager)
			for _, lang := range lsp.SupportedLanguages() {
				if detected := lsp.Detect(lang); detected.Command != "" {
					mgr := lsp.NewManager(lang, detected)
					go func(m *lsp.Manager, language string) {
						if err := m.Start(cwd); err != nil {
							fmt.Fprintf(os.Stderr, "huginn: LSP %s start failed: %v\n", language, err)
						}
					}(mgr, lang)
					lspMgrs[lang] = mgr
				}
			}
			tools.RegisterLSPTools(toolReg, cwd, lspMgrs)
		}

		// Start MCP servers if configured
		var mcpMgr *mcp.ServerManager
		if len(cfg.MCPServers) > 0 {
			mcpMgr = mcp.NewServerManager(cfg.MCPServers)
			mcpMgr.StartAll(context.Background(), toolReg)
		}

		// Apply allowed/disallowed filters from config
		if len(cfg.AllowedTools) > 0 {
			toolReg.SetAllowed(cfg.AllowedTools)
		}
		if len(cfg.DisallowedTools) > 0 {
			toolReg.SetBlocked(cfg.DisallowedTools)
		}

		// gate uses autoRunAtom (fast path) and permReqCh (slow path, user prompt).
		gate := permissions.NewGate(*dangerouslySkipPermissions, func(req permissions.PermissionRequest) permissions.Decision {
			// Fast path: autoRun on → allow immediately without blocking.
			if autoRunAtom.Load() {
				return permissions.Allow
			}
			respCh := make(chan permissions.Decision, 1)
			permReqCh <- tui.PermissionPromptMsg{Req: req, RespCh: respCh}
			return <-respCh
		})
		orch.SetTools(toolReg, gate)

		// Wire orchestrator as AgentExecutor into skill tools (Phase 2).
		// This enables agent-mode PromptTools to make LLM calls.
		skills.InjectAgentExecutor(toolReg, orch)

		// Wire relay dispatcher: deliver HuginnCloud permission responses to the gate,
		// and handle Phase 3 chat/session/model messages.
		//
		// Load the server auth token so the TUI-mode dispatcher can proxy
		// http_request messages (from the HuginnCloud web UI) to the local
		// HTTP server — matching the behaviour of server mode.
		tuiServerToken, _ := server.LoadOrCreateToken(huginnHome)
		tuiServerAddr := func() string {
			bind := cfg.WebUI.Bind
			if bind == "" {
				bind = "127.0.0.1"
			}
			port := cfg.WebUI.Port
			if port == 0 {
				port = 8477
			}
			return fmt.Sprintf("%s:%d", bind, port)
		}()
		if wsHub, ok := tuiRelayHub.(*relay.WebSocketHub); ok {
			activeSessions := relay.NewActiveSessions()
			var sessionStore *relay.SessionStore
			if store != nil {
				sessionStore = relay.NewSessionStore(store)
			}

			// Wire outbox for durable message delivery across reconnects.
			if store != nil {
				tuiOutbox := relay.NewOutbox(store, tuiRelayHub)
				outboxCtx, outboxCancel := context.WithCancel(context.Background())
				defer outboxCancel()
				go func() {
					ticker := time.NewTicker(5 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-outboxCtx.Done():
							return
						case <-ticker.C:
							if err := tuiOutbox.Flush(outboxCtx); err != nil && !errors.Is(err, context.Canceled) {
								appLog.Warn("relay: outbox flush", "err", err)
							}
						}
					}
				}()
			}

			// Mutex protecting concurrent reads/writes to cfg.Backend fields from relay callbacks.
			var backendMu sync.RWMutex

			chatFn := func(ctx context.Context, sessionID, userMsg string,
				onToken func(string),
				onToolEvent func(eventType string, payload map[string]any),
				onEvent func(backend.StreamEvent)) error {
				return orch.ChatForSessionWithAgent(ctx, sessionID, userMsg, onToken, onToolEvent, onEvent)
			}
			newSessionFn := func(id string) string {
				sess, err := orch.NewSession(id)
				if err != nil {
					logger.Error("failed to create session", "err", err)
					return ""
				}
				return sess.ID
			}
			// Use relay.GetMachineID() (8-char hex) — NOT cfg.MachineID (full hostname-hex).
			// The cloud hub registers satellites under relay.GetMachineID(); using cfg.MachineID
			// causes the machine ID filter to silently drop all relayed messages.
			appLog.Info("dispatcher: routing machine_id", "machine_id", relay.GetMachineID())
			runAgentFn := func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error {
				reg := orch.GetAgentRegistry()
				if reg == nil {
					return fmt.Errorf("agent registry not available")
				}
				ag, ok := reg.ByName(agentName)
				if !ok {
					return fmt.Errorf("agent %q not found", agentName)
				}
				return orch.ChatWithAgent(ctx, ag, prompt, sessionID, onToken, nil, nil)
			}
			wsHub.SetOnMessage(relay.NewDispatcher(relay.DispatcherConfig{
				MachineID:   relay.GetMachineID(),
				DeliverPerm: gate.DeliverRelayResponse,
				Hub:         tuiRelayHub,
				Store:       sessionStore,
				Shell:       relay.NewShellManager(),
				ChatSession: chatFn,
				NewSession:  newSessionFn,
				RunAgent:    runAgentFn,
				ListModels:  orch.ModelNames,
				GetModelProviders: func() []relay.ModelProviderInfo {
					backendMu.RLock()
					provider := cfg.Backend.Provider
					endpoint := cfg.Backend.Endpoint
					apiKey := cfg.Backend.APIKey
					backendMu.RUnlock()
					if provider == "" {
						provider = "ollama"
					}
					return []relay.ModelProviderInfo{{
						ID:        provider,
						Name:      providerDisplayName(provider),
						Endpoint:  endpoint,
						APIKey:    apiKey,
						Connected: cfg.Backend.ResolvedAPIKey() != "" || provider == "ollama",
						Models:    fetchOllamaModels(cfg.OllamaBaseURL),
					}}
				},
				GetModelConfig: func(provider string) (*relay.ModelProviderInfo, error) {
					backendMu.RLock()
					configured := cfg.Backend.Provider
					endpoint := cfg.Backend.Endpoint
					apiKey := cfg.Backend.APIKey
					backendMu.RUnlock()
					if configured == "" {
						configured = "ollama"
					}
					if provider != configured {
						return nil, fmt.Errorf("provider %q not configured", provider)
					}
					return &relay.ModelProviderInfo{
						ID:        configured,
						Name:      providerDisplayName(configured),
						Endpoint:  endpoint,
						APIKey:    apiKey,
						Connected: cfg.Backend.ResolvedAPIKey() != "" || configured == "ollama",
						Models:    fetchOllamaModels(cfg.OllamaBaseURL),
					}, nil
				},
				UpdateModelConfig: func(provider, endpoint, apiKey string) error {
					backendMu.Lock()
					cfg.Backend.Provider = provider
					cfg.Backend.Endpoint = endpoint
					if apiKey != "" {
						cfg.Backend.APIKey = apiKey
					}
					backendMu.Unlock()
					return cfg.Save()
				},
				PullModel: func(name string) error {
					baseURL := cfg.OllamaBaseURL
					if baseURL == "" {
						baseURL = "http://localhost:11434"
					}
					payload, _ := json.Marshal(map[string]any{"name": name, "stream": false})
					client := &http.Client{Timeout: 10 * time.Minute} // model pulls can take minutes
					resp, err := client.Post(baseURL+"/api/pull", "application/json", bytes.NewReader(payload))
					if err != nil {
						return fmt.Errorf("ollama not reachable: %w", err)
					}
					defer resp.Body.Close()
					if resp.StatusCode >= 400 {
						return fmt.Errorf("ollama pull returned %d", resp.StatusCode)
					}
					return nil
				},
				HTTPProxy:   makeLocalHTTPProxy(tuiServerAddr, tuiServerToken),
				Active:      activeSessions,
			}))
			// Cancel all in-flight remote sessions on graceful shutdown.
			defer activeSessions.CancelAll()
		}

		// Defer MCP manager shutdown
		if mcpMgr != nil {
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				mcpMgr.StopAll(shutdownCtx)
			}()
		}
	}

	// 9. Launch interactive TUI (wire enterprise extensions)
	tuiApp := tui.New(cfg, orch, models, version)
	tuiApp.SetUseAgentLoop(toolsEnabled)
	tuiApp.SetStatsRegistry(statsReg)
	tuiApp.SetWorkspace(detection.Root, idx)
	tuiApp.SetStore(store)
	tuiApp.SetAgentRegistry(agentReg)
	priceTracker := pricing.NewSessionTracker(pricing.DefaultTable)
	tuiApp.SetPriceTracker(priceTracker)
	tuiApp.SetSkillRegistry(skillReg)

	// Wire MuninnDB connection status into the TUI so the agent wizard can
	// offer the memory configuration step.
	{
		muninnHome, _ := os.UserHomeDir()
		muninnCfgPath := filepath.Join(muninnHome, ".config", "huginn", "muninn.json")
		if muninnCfg, muninnErr := memory.LoadGlobalConfig(muninnCfgPath); muninnErr == nil {
			tuiApp.SetMuninnConnection(muninnCfg.Endpoint, muninnCfg.Endpoint != "")
		}
	}

	// 9a. Wire session persistence.
	sessionsDir := filepath.Join(huginnHome, "sessions")

	// Run one-time filesystem → SQLite migration when SQLite is available.
	if sqlDB != nil {
		if migrErr := session.MigrateFromFilesystem(sessionsDir, sqlDB); migrErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: session migration: %v\n", migrErr)
		}
	}

	// Use SQLite store if available; otherwise fall back to filesystem store.
	var sessStore session.StoreInterface
	if sqlDB != nil {
		sessStore = session.NewSQLiteSessionStore(sqlDB)
	} else {
		sessStore = session.NewStore(sessionsDir)
	}

	tuiApp.SetSessionStore(sessStore)
	activeModel := cfg.DefaultModel
	newSess := sessStore.New(
		fmt.Sprintf("Session %s", time.Now().Format("2006-01-02 15:04")),
		cwd,
		activeModel,
	)
	tuiApp.SetActiveSession(newSess)
	// Wire permission prompting: share autoRunAtom so the gate can read it without
	// going through channels, and permReqCh so tool-level approvals are forwarded.
	tuiApp.SetAutoRunAtom(autoRunAtom)
	if cfg.NotepadsEnabled {
		if npMgr, err := notepad.DefaultManager(detection.Root); err == nil {
			tuiApp.SetNotepadManager(npMgr)
		}
	}

	// 9b. Background symbol extraction — runs after loader so it doesn't block TUI startup.
	if idx == nil {
		idx = &repo.Index{Root: detection.Root}
	}
	if store != nil {
		symReg := buildSymbolRegistry()
		go func() {
			seen := make(map[string]bool)
			for _, chunk := range idx.Chunks {
				// Only process the first chunk of each file (StartLine == 1) to avoid duplicates.
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
					continue
				}
				if len(syms) > 0 {
					storeSyms := make([]storage.Symbol, len(syms))
					for i, s := range syms {
						storeSyms[i] = storage.Symbol{
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
					_ = store.SetEdge(e.From, e.To, storage.Edge{
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

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := orch.SessionClose(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: session close: %v\n", err)
		}
	}()

	p := tea.NewProgram(tuiApp, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Forward permission requests from the gate goroutine to the TUI event loop.
	// When autoRun=true the gate takes the fast path and never sends here, so this
	// goroutine is idle most of the time. We close permReqCh after p.Run() returns
	// so the goroutine exits cleanly.
	go func() {
		for msg := range permReqCh {
			p.Send(msg)
		}
	}()

	if _, err := p.Run(); err != nil {
		fatalf("tui: %v", err)
	}
	close(permReqCh)
}

// buildProviders constructs OAuth provider implementations from the loaded config.
// Providers with empty ClientID are skipped (not configured).
func buildProviders(cfg *config.Config) []connections.IntegrationProvider {
	var providers []connections.IntegrationProvider
	if cfg.Integrations.Google.ClientID != "" {
		providers = append(providers, connproviders.NewGoogle(
			cfg.Integrations.Google.ClientID,
			cfg.Integrations.Google.ClientSecret,
			[]string{"gmail", "calendar", "drive", "docs", "sheets", "contacts"},
		))
	}
	if cfg.Integrations.GitHub.ClientID != "" {
		providers = append(providers, connproviders.NewGitHub(
			cfg.Integrations.GitHub.ClientID,
			cfg.Integrations.GitHub.ClientSecret,
		))
	}
	if cfg.Integrations.Slack.ClientID != "" {
		providers = append(providers, connproviders.NewSlack(
			cfg.Integrations.Slack.ClientID,
			cfg.Integrations.Slack.ClientSecret,
		))
	}
	if cfg.Integrations.Jira.ClientID != "" {
		providers = append(providers, connproviders.NewJira(
			cfg.Integrations.Jira.ClientID,
			cfg.Integrations.Jira.ClientSecret,
		))
	}
	if cfg.Integrations.Bitbucket.ClientID != "" {
		providers = append(providers, connproviders.NewBitbucket(
			cfg.Integrations.Bitbucket.ClientID,
			cfg.Integrations.Bitbucket.ClientSecret,
		))
	}
	return providers
}

// buildCredentialResolver returns a scheduler.CredentialResolver that looks up
// connection credentials by AccountLabel (or ID). Returns nil if either store
// is nil so callers can safely pass it to MakeWorkflowRunner.
func buildCredentialResolver(store connections.StoreInterface, secrets connections.SecretStore) scheduler.CredentialResolver {
	if store == nil || secrets == nil {
		return nil
	}
	return func(ctx context.Context, connectionName string) (map[string]string, error) {
		conns, err := store.List()
		if err != nil {
			return nil, fmt.Errorf("credential resolver: list connections: %w", err)
		}
		for _, c := range conns {
			if c.AccountLabel == connectionName || c.ID == connectionName {
				creds, err := secrets.GetCredentials(c.ID)
				if err != nil {
					return nil, fmt.Errorf("credential resolver: get credentials for %q: %w", connectionName, err)
				}
				return creds, nil
			}
		}
		return nil, fmt.Errorf("credential resolver: connection %q not found", connectionName)
	}
}

// buildSymbolRegistry creates a Registry with all supported extractors.
func buildSymbolRegistry() *symbol.Registry {
	reg := symbol.NewRegistry()
	reg.Register(goext.New(), ".go")
	reg.Register(tsext.New(), ".ts", ".tsx", ".js", ".jsx")
	reg.SetFallback(heuristic.New())
	return reg
}

// storeDir returns the path to the Pebble store for a given repo root.
// Uses ~/.huginn/store/<sanitized-root-path>.
func storeDir(root string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "huginn-store")
	}
	// Sanitize root path: replace path separators with underscores.
	// Keep only safe characters to avoid filesystem issues.
	name := sanitizePath(root)
	if len(name) > 64 {
		name = name[len(name)-64:]
	}
	return filepath.Join(home, ".huginn", "store", name)
}

// sanitizePath converts a filesystem path to a safe directory name.
func sanitizePath(p string) string {
	var result strings.Builder
	for _, c := range p {
		switch c {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			result.WriteByte('_')
		default:
			result.WriteRune(c)
		}
	}
	return result.String()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "huginn: error: "+format+"\n", args...)
	os.Exit(1)
}

// needsOnboarding returns true if the managed runtime or at least one model is not yet installed.
func needsOnboarding(mgr *runtime.Manager, store *modelslib.Store) bool {
	if !mgr.IsInstalled() {
		return true
	}
	installed, _ := store.Installed()
	return len(installed) == 0
}

// systemRAMGB returns a rough estimate of available system RAM in gigabytes.
// Falls back to a safe default of 16 GB when detection is not implemented.
func systemRAMGB() int {
	return 16
}

// cmdInit prints setup guidance and directs users to the web UI.
// The old Ollama-only wizard has been replaced: provider setup (Anthropic,
// OpenAI, Ollama, etc.) and agent creation are handled through the web UI.
func cmdInit() error {
	cfg, _ := config.Load()
	port := 8421
	if cfg != nil && cfg.WebUI.Port != 0 {
		port = cfg.WebUI.Port
	}
	fmt.Printf("\nWelcome to Huginn!\n\n")
	fmt.Printf("Setup is done through the web UI. To get started:\n\n")
	fmt.Printf("  1. Start the server:   huginn serve\n")
	fmt.Printf("  2. Open your browser:  http://127.0.0.1:%d\n\n", port)
	fmt.Printf("From the web UI you can add an API key (Anthropic, OpenAI, etc.),\n")
	fmt.Printf("configure a local model via Ollama, and create your first agent.\n\n")
	return nil
}

func cmdPull(nameOrURL string) error {
	catalog, err := modelslib.LoadMerged()
	if err != nil {
		return err
	}

	huginnHome, err := huginnDir()
	if err != nil {
		return err
	}
	store, err := modelslib.NewStore(huginnHome)
	if err != nil {
		return err
	}

	var entry modelslib.ModelEntry
	var name string

	if strings.HasPrefix(nameOrURL, "http://") || strings.HasPrefix(nameOrURL, "https://") {
		name = filepath.Base(nameOrURL)
		entry = modelslib.ModelEntry{URL: nameOrURL}
	} else {
		e, ok := catalog[nameOrURL]
		if !ok {
			return fmt.Errorf("model %q not found in catalog (use full URL or 'huginn models --available')", nameOrURL)
		}
		entry = e
		name = nameOrURL
	}

	destPath := store.ModelPath(entry.Filename)
	fmt.Printf("Pulling %s\n", name)
	if entry.SizeBytes > 0 {
		fmt.Printf("Size: %s\n", formatBytes(entry.SizeBytes))
	}
	if entry.SHA256 == "" {
		fmt.Println("Warning: no checksum provided — skipping integrity verification")
	}

	err = modelslib.Pull(context.Background(), entry.URL, destPath, entry.SHA256, func(p modelslib.PullProgress) {
		if p.Done {
			fmt.Printf("\r%-60s\n", "Done!")
			return
		}
		bar := progressBar(p.Downloaded, p.Total, 30)
		pct := ""
		if p.Total > 0 {
			pct = fmt.Sprintf(" %.0f%%", float64(p.Downloaded)/float64(p.Total)*100)
		}
		eta := ""
		if p.Speed > 0 && p.Total > 0 {
			remaining := float64(p.Total-p.Downloaded) / p.Speed
			eta = fmt.Sprintf(" ~%s remaining", formatDuration(remaining))
		}
		fmt.Printf("\r[%s]%s  %s / %s  %.1f MB/s%s",
			bar, pct,
			formatBytes(p.Downloaded), formatBytes(p.Total),
			p.Speed/1024/1024, eta)
	})
	if err != nil {
		return err
	}

	return store.Record(name, modelslib.LockEntry{
		Name:        name,
		Filename:    entry.Filename,
		Path:        destPath,
		SHA256:      entry.SHA256,
		SizeBytes:   entry.SizeBytes,
		InstalledAt: time.Now(),
	})
}

func cmdModels(args []string) error {
	if len(args) > 0 && args[0] == "add" {
		addFlags := flag.NewFlagSet("models add", flag.ExitOnError)
		nameFlag := addFlags.String("name", "", "friendly name for the model")
		urlFlag := addFlags.String("url", "", "GGUF download URL")
		if err := addFlags.Parse(args[1:]); err != nil {
			return err
		}
		if *nameFlag == "" || *urlFlag == "" {
			return fmt.Errorf("usage: huginn models add --name <name> --url <url>")
		}
		return appendUserManifest(*nameFlag, *urlFlag)
	}

	if len(args) > 0 && args[0] == "validate" {
		return validateUserManifest()
	}

	// Determine Ollama base URL from config (fall back to default).
	baseURL := "http://localhost:11434"
	if cfg, err := config.Load(); err == nil && cfg.OllamaBaseURL != "" {
		baseURL = cfg.OllamaBaseURL
	}

	// Load agents and build a map of model name → agent names.
	modelAgents := map[string][]string{}
	if agentCfg, err := agentslib.LoadAgents(); err == nil && agentCfg != nil {
		for _, a := range agentCfg.Agents {
			if a.Model != "" {
				modelAgents[a.Model] = append(modelAgents[a.Model], a.Name)
			}
		}
	}

	// Query Ollama for installed models and sizes.
	type ollamaModelDetails struct {
		ParameterSize     string `json:"parameter_size"`
		QuantizationLevel string `json:"quantization_level"`
	}
	type ollamaModel struct {
		Name    string             `json:"name"`
		Size    int64              `json:"size"`
		Details ollamaModelDetails `json:"details"`
	}
	type ollamaTags struct {
		Models []ollamaModel `json:"models"`
	}

	var ollamaModels []ollamaModel
	ollamaOnline := false
	if resp, err := http.Get(baseURL + "/api/tags"); err == nil {
		defer resp.Body.Close()
		var tags ollamaTags
		if decodeErr := json.NewDecoder(resp.Body).Decode(&tags); decodeErr == nil {
			ollamaModels = tags.Models
			ollamaOnline = true
		}
	}

	if !ollamaOnline {
		fmt.Fprintf(os.Stderr, "Warning: Ollama not reachable at %s — showing agent assignments only\n\n", baseURL)

		// Collect all known models from agent assignments.
		seen := map[string]bool{}
		var modelNames []string
		if agentCfg, err := agentslib.LoadAgents(); err == nil && agentCfg != nil {
			for _, a := range agentCfg.Agents {
				if a.Model != "" && !seen[a.Model] {
					seen[a.Model] = true
					modelNames = append(modelNames, a.Model)
				}
			}
		}

		fmt.Printf("%-36s %s\n", "MODEL", "AGENTS")
		fmt.Println(strings.Repeat("-", 60))
		for _, name := range modelNames {
			fmt.Printf("%-36s %s\n", name, strings.Join(modelAgents[name], ", "))
		}
		return nil
	}

	fmt.Printf("%-36s %-12s %-12s %s\n", "MODEL", "PARAMS", "SIZE", "AGENTS")
	fmt.Println(strings.Repeat("-", 80))
	for _, m := range ollamaModels {
		size := ""
		if m.Size > 0 {
			size = formatBytes(m.Size)
		}
		fmt.Printf("%-36s %-12s %-12s %s\n",
			m.Name, m.Details.ParameterSize, size,
			strings.Join(modelAgents[m.Name], ", "))
	}
	return nil
}

func cmdRuntime(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	dir, err := huginnDir()
	if err != nil {
		return fmt.Errorf("huginn dir: %w", err)
	}

	mgr, err := runtime.NewManager(dir)
	if err != nil {
		return fmt.Errorf("runtime manager: %w", err)
	}

	switch sub {
	case "status":
		if !mgr.IsInstalled() {
			fmt.Println("llama-server: not installed")
			fmt.Printf("  Run: huginn runtime download\n")
			return nil
		}
		fmt.Printf("llama-server: installed\n")
		fmt.Printf("  Binary: %s\n", mgr.BinaryPath())
		return nil

	case "download":
		fmt.Println("Downloading llama-server runtime...")
		err := mgr.Download(context.Background(), func(dl, total int64) {
			if total > 0 {
				pct := int(float64(dl) / float64(total) * 100)
				fmt.Printf("\r  %s / %s  %d%%", formatBytes(dl), formatBytes(total), pct)
			} else {
				fmt.Printf("\r  %s", formatBytes(dl))
			}
		})
		fmt.Println()
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
		fmt.Println("Runtime downloaded successfully.")
		return nil

	default:
		fmt.Println("huginn runtime commands:")
		fmt.Println("  huginn runtime status     — show installation status")
		fmt.Println("  huginn runtime download   — download llama-server binary")
		return nil
	}
}

// wireMemoryReplicator creates and configures a MemoryReplicator backed by sqlDB.
// If HUGINN_CLOUD_URL is set and the machine is registered with HuginnCloud,
// the replicator is wired with an HTTPVaultClient so agent memory writes are
// replicated to the cloud vault. The caller must call Start() and arrange for
// Stop() to be called on shutdown.
func wireMemoryReplicator(sqlDB *sqlitedb.DB) *agentslib.MemoryReplicator {
	mr := agentslib.NewMemoryReplicator(sqlDB)
	tokenStore := relay.NewTokenStore()
	if cloudURL := os.Getenv("HUGINN_CLOUD_URL"); cloudURL != "" && tokenStore.IsRegistered() {
		mr.WithVaultClient(agentslib.NewHTTPVaultClient(cloudURL, func() string {
			tok, _ := tokenStore.Load()
			return tok
		}), relay.GetMachineID())
	}
	return mr
}

func huginnDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".huginn")
	return dir, os.MkdirAll(dir, 0755)
}

func appendUserManifest(name, url string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("model URL cannot be empty")
	}
	path, err := userManifestWritePath()
	if err != nil {
		return err
	}
	type entry struct {
		URL string `json:"url"`
	}
	type manifest struct {
		Version int              `json:"huginn_manifest_version"`
		Models  map[string]entry `json:"models"`
	}
	var mf manifest
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &mf); err != nil {
			return fmt.Errorf("parse models.user.json: %w", err)
		}
	}
	if mf.Models == nil {
		mf.Models = make(map[string]entry)
	}
	mf.Version = 1
	mf.Models[name] = entry{URL: url}
	out, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	fmt.Printf("Added %q to %s\n", name, path)
	return os.WriteFile(path, out, 0644)
}

func validateUserManifest() error {
	_, errs := loadUserManifestFromPath()
	if len(errs) == 0 {
		fmt.Println("models.user.json: OK")
		return nil
	}
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "warn: %v\n", e)
	}
	return fmt.Errorf("%d validation error(s)", len(errs))
}

func loadUserManifestFromPath() (map[string]any, []error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".huginn", "models.user.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Println("models.user.json: not found (optional)")
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	type mf struct {
		Models map[string]struct {
			URL string `json:"url"`
		} `json:"models"`
	}
	var m mf
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, []error{fmt.Errorf("parse error: %w", err)}
	}
	var errs []error
	for name, e := range m.Models {
		if e.URL == "" {
			errs = append(errs, fmt.Errorf("entry %q: missing url", name))
		}
	}
	return nil, errs
}

func userManifestWritePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".huginn", "models.user.json"), nil
}

func formatBytes(b int64) string {
	if b < 0 {
		return "?"
	}
	const mb = 1024 * 1024
	const gb = 1024 * mb
	if b >= gb {
		return fmt.Sprintf("%.1fG", float64(b)/float64(gb))
	}
	if b >= mb {
		return fmt.Sprintf("%.1fM", float64(b)/float64(mb))
	}
	return fmt.Sprintf("%dB", b)
}

func progressBar(done, total int64, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	filled := int(float64(done) / float64(total) * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatDuration(sec float64) string {
	if sec < 60 {
		return fmt.Sprintf("%.0fs", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%.0fm %.0fs", sec/60, math.Mod(sec, 60))
	}
	return fmt.Sprintf("%.0fh", sec/3600)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// sessionAggregation holds aggregated statistics across all persisted sessions.
type sessionAggregation struct {
	TotalSessions int
	TotalMessages int
	Models        map[string]int // model name -> number of sessions that used it
	Oldest        time.Time      // CreatedAt of the oldest session (zero if none)
	Newest        time.Time      // CreatedAt of the newest session (zero if none)
}

// aggregateSessionStats reads all manifest.json files inside baseDir (one
// subdirectory per session) and returns aggregated statistics.  If baseDir
// does not exist the function returns an empty aggregation without error.
func aggregateSessionStats(baseDir string) (sessionAggregation, error) {
	result := sessionAggregation{
		Models: make(map[string]int),
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("read sessions dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mpath := filepath.Join(baseDir, e.Name(), "manifest.json")
		data, err := os.ReadFile(mpath)
		if err != nil {
			continue // skip sessions without readable manifests
		}
		// Use an anonymous struct to avoid importing the session package.
		var m struct {
			Model        string    `json:"model"`
			CreatedAt    time.Time `json:"created_at"`
			MessageCount int       `json:"message_count"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}

		result.TotalSessions++
		result.TotalMessages += m.MessageCount
		if m.Model != "" {
			result.Models[m.Model]++
		}
		if !m.CreatedAt.IsZero() {
			if result.Oldest.IsZero() || m.CreatedAt.Before(result.Oldest) {
				result.Oldest = m.CreatedAt
			}
			if result.Newest.IsZero() || m.CreatedAt.After(result.Newest) {
				result.Newest = m.CreatedAt
			}
		}
	}
	return result, nil
}

// formatStatsOutput renders an aggregateSessionStats result as a plain-text table.
func formatStatsOutput(sa sessionAggregation) string {
	var sb strings.Builder

	sb.WriteString("huginn stats\n")
	sb.WriteString(strings.Repeat("─", 44) + "\n")
	sb.WriteString(fmt.Sprintf("  %-28s %d\n", "Sessions", sa.TotalSessions))
	sb.WriteString(fmt.Sprintf("  %-28s %d\n", "Total messages", sa.TotalMessages))

	if !sa.Oldest.IsZero() {
		sb.WriteString(fmt.Sprintf("  %-28s %s\n", "Oldest session", sa.Oldest.Local().Format("2006-01-02")))
	}
	if !sa.Newest.IsZero() {
		sb.WriteString(fmt.Sprintf("  %-28s %s\n", "Newest session", sa.Newest.Local().Format("2006-01-02")))
	}

	if len(sa.Models) > 0 {
		sb.WriteString("\n  Models used:\n")
		// Collect and sort model names for deterministic output.
		names := make([]string, 0, len(sa.Models))
		for name := range sa.Models {
			names = append(names, name)
		}
		// Sort inline to avoid importing "sort" only for this function.
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				if names[j] < names[i] {
					names[i], names[j] = names[j], names[i]
				}
			}
		}
		for _, name := range names {
			sb.WriteString(fmt.Sprintf("    %-26s %d session(s)\n", name, sa.Models[name]))
		}
	}

	sb.WriteString("\n  Note: session cost tracking is available during active sessions only.\n")
	return sb.String()
}

// cmdStats implements `huginn stats` — displays aggregated session history
// read from ~/.huginn/sessions/.
func cmdStats(args []string) error {
	_ = args // no flags in v1

	huginnHome, err := huginnDir()
	if err != nil {
		return fmt.Errorf("huginn dir: %w", err)
	}

	sessionsDir := filepath.Join(huginnHome, "sessions")
	sa, err := aggregateSessionStats(sessionsDir)
	if err != nil {
		return err
	}

	fmt.Print(formatStatsOutput(sa))
	return nil
}

// cmdLogs implements `huginn logs [--tail N]` — prints the last N lines of huginn.log.
func cmdLogs(args []string) error {
	n := 50
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.IntVar(&n, "tail", 50, "number of lines to show")
	fs.IntVar(&n, "n", 50, "number of lines to show (shorthand)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	huginnHome, err := huginnDir()
	if err != nil {
		return fmt.Errorf("huginn dir: %w", err)
	}
	_ = cfg // config loaded; baseDir derived from huginnDir
	lines, err := logger.TailLog(huginnHome, n)
	if err != nil {
		return fmt.Errorf("read logs: %w", err)
	}
	if len(lines) == 0 {
		fmt.Println("No log entries found.")
		return nil
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

// cmdAgents handles the agents subcommand.
func cmdAgents(args []string) error {
	if len(args) == 0 {
		return cmdAgentsList()
	}
	switch args[0] {
	case "list":
		return cmdAgentsList()
	case "new":
		home, _ := os.UserHomeDir()
		muninnCfgPath := filepath.Join(home, ".config", "huginn", "muninn.json")
		muninnCfg, _ := memory.LoadGlobalConfig(muninnCfgPath)
		muninnEndpoint := ""
		if muninnCfg != nil {
			muninnEndpoint = muninnCfg.Endpoint
		}
		m := tui.NewStandaloneAgentWizardWithMemory(muninnEndpoint, muninnEndpoint != "")
		p := tea.NewProgram(m)
		result, err := p.Run()
		if err != nil {
			return err
		}
		if wizard, ok := result.(tui.StandaloneAgentWizard); ok && wizard.WasSaved() {
			agent := wizard.SavedAgent()
			if saveErr := agentslib.SaveAgentDefault(agent); saveErr != nil {
				return fmt.Errorf("save agent: %w", saveErr)
			}
			fmt.Printf("Agent %q created successfully.\n", agent.Name)
			// If memory is enabled and a vault name was specified, create the vault now.
			if agent.MemoryEnabled != nil && *agent.MemoryEnabled && agent.VaultName != "" {
				home, _ := os.UserHomeDir()
				cfgPath := filepath.Join(home, ".config", "huginn", "muninn.json")
				cfg, cfgErr := memory.LoadGlobalConfig(cfgPath)
				if cfgErr == nil && cfg.Endpoint != "" {
					ps := memory.NewKeychainPasswordStore()
					if pwd, pwdErr := ps.GetPassword(); pwdErr == nil {
						client := memory.NewMuninnSetupClient(cfg.Endpoint)
						if cookie, loginErr := client.Login(cfg.Username, pwd); loginErr == nil {
							if token, vaultErr := client.CreateVaultAndKey(cookie, agent.VaultName, "huginn-"+agent.Name); vaultErr == nil {
								cfg.VaultTokens[agent.VaultName] = token
								if err := memory.SaveGlobalConfig(cfgPath, cfg); err != nil {
									fmt.Fprintf(os.Stderr, "warning: vault token created but could not save to config: %v\n", err)
								}
								fmt.Printf("MuninnDB vault %q created.\n", agent.VaultName)
							}
						}
					}
				}
			}
		} else {
			fmt.Println("Agent creation cancelled.")
		}
		return nil
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: huginn agents delete <name>")
		}
		if err := agentslib.DeleteAgentDefault(args[1]); err != nil {
			return err
		}
		fmt.Printf("Agent %q deleted.\n", args[1])
		return nil
	case "edit":
		return cmdAgentsEdit(args[1:])
	case "use":
		return cmdAgentsUse(args[1:])
	case "show":
		return cmdAgentsShow(args[1:])
	default:
		return fmt.Errorf("unknown agents subcommand: %s\nUsage: huginn agents [list|new|delete <name>|edit <name>|use <name>|show <name>]", args[0])
	}
}

func cmdAgentsList() error {
	agentCfg, err := agentslib.LoadAgents()
	if err != nil {
		return err
	}
	if len(agentCfg.Agents) == 0 {
		fmt.Println("No agents configured.")
		return nil
	}
	cfg, _ := config.Load()
	activeAgent := ""
	if cfg != nil {
		activeAgent = cfg.ActiveAgent
	}
	fmt.Printf("%-20s %-30s %-10s\n", "NAME", "MODEL", "STATUS")
	for _, a := range agentCfg.Agents {
		marker := ""
		if strings.EqualFold(a.Name, activeAgent) {
			marker = "* active"
		}
		fmt.Printf("%-20s %-30s %-10s\n", a.Name, a.Model, marker)
	}
	return nil
}

func cmdAgentsEdit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: huginn agents edit <name> [--model M] [--system-prompt P] [--color C] [--icon I]")
	}
	name := args[0]
	fs := flag.NewFlagSet("agents edit", flag.ExitOnError)
	modelFlag := fs.String("model", "", "model ID to assign")
	promptFlag := fs.String("system-prompt", "", "new system prompt")
	colorFlag := fs.String("color", "", "hex color e.g. #58A6FF")
	iconFlag := fs.String("icon", "", "single character icon")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	agentCfg, err := agentslib.LoadAgents()
	if err != nil {
		return err
	}

	found := false
	for i, a := range agentCfg.Agents {
		if strings.EqualFold(a.Name, name) {
			if *modelFlag != "" {
				agentCfg.Agents[i].Model = *modelFlag
			}
			if *promptFlag != "" {
				agentCfg.Agents[i].SystemPrompt = *promptFlag
			}
			if *colorFlag != "" {
				agentCfg.Agents[i].Color = *colorFlag
			}
			if *iconFlag != "" {
				agentCfg.Agents[i].Icon = *iconFlag
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("agent %q not found", name)
	}

	if err := agentslib.SaveAgents(agentCfg); err != nil {
		return fmt.Errorf("save agent: %w", err)
	}
	fmt.Printf("Agent %q updated.\n", name)
	return nil
}

func cmdAgentsUse(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: huginn agents use <name>")
	}
	name := args[0]

	agentCfg, err := agentslib.LoadAgents()
	if err != nil {
		return err
	}

	canonicalName := ""
	for _, a := range agentCfg.Agents {
		if strings.EqualFold(a.Name, name) {
			canonicalName = a.Name
			break
		}
	}
	if canonicalName == "" {
		return fmt.Errorf("agent %q not found", name)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.ActiveAgent = canonicalName
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("Active agent set to %q.\n", canonicalName)
	return nil
}

func cmdAgentsShow(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: huginn agents show <name>")
	}
	name := args[0]

	agentCfg, err := agentslib.LoadAgents()
	if err != nil {
		return err
	}

	var found *agentslib.AgentDef
	for i, a := range agentCfg.Agents {
		if strings.EqualFold(a.Name, name) {
			found = &agentCfg.Agents[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("agent %q not found", name)
	}

	cfg, _ := config.Load()
	isActive := false
	if cfg != nil {
		isActive = strings.EqualFold(found.Name, cfg.ActiveAgent)
	}

	displayName := found.Name
	if isActive {
		displayName += " (active)"
	}

	fmt.Printf("Name:          %s\n", displayName)
	fmt.Printf("Model:         %s\n", found.Model)
	fmt.Printf("Color:         %s\n", found.Color)
	fmt.Printf("Icon:          %s\n", found.Icon)
	fmt.Printf("System Prompt: %s\n", found.SystemPrompt)
	return nil
}

// startServer initialises and starts the Huginn HTTP+WebSocket server.
// It returns the running server, the auth token, and any error.
// The caller is responsible for waiting for a shutdown signal and calling srv.Stop.
// startServer initialises and starts the Huginn HTTP server.
// The returned cleanup function must be called when the server is stopped
// (after srv.Stop) to release pebble and scheduler resources. Callers that
// block until a signal (cmdServe) may defer cleanup(); callers that return
// immediately (cmdTray OnStart) must store cleanup and invoke it from OnStop.
func startServer(cfg *config.Config) (srv *server.Server, token string, cleanup func(), err error) {
	huginnHome, err := huginnDir()
	if err != nil {
		return nil, "", nil, fmt.Errorf("huginn dir: %w", err)
	}

	// Initialize logger
	if logErr := logger.Init(huginnHome); logErr != nil {
		fmt.Fprintf(os.Stderr, "huginn: warning: logger init: %v\n", logErr)
	}

	// Clean up orphaned session directories from previous crashes.
	agentsession.SweepStale()

	// Load auth token
	token, err = server.LoadOrCreateToken(huginnHome)
	if err != nil {
		return nil, "", nil, fmt.Errorf("server token: %w", err)
	}

	// Backend
	endpoint := cfg.Backend.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	b := backend.NewExternalBackend(endpoint)
	go func(ep string, be backend.Backend) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := be.Health(ctx); err != nil {
			logger.Warn("backend: not reachable at startup", "endpoint", ep, "err", err)
		}
	}(endpoint, b)

	// Models
	models := modelconfig.DefaultModels()
	if cfg.ReasonerModel != "" {
		models.Reasoner = cfg.ReasonerModel
	}

	// Permissions gate for server mode: auto-approve all tool calls (headless).
	// Also used by the relay dispatcher to deliver remote permission responses.
	serverGate := permissions.NewGate(true, nil)

	// Orchestrator (minimal setup for serve mode)
	orch, err := agent.NewOrchestrator(b, models, nil, nil, nil, nil)
	if err != nil {
		return nil, "", nil, err
	}
	serveCache := backend.NewBackendCache(b)
	// Register the global provider's API key as the per-provider fallback so that
	// agents with provider="anthropic" (or any provider) but no per-agent api_key
	// inherit the globally configured key instead of creating a broken backend.
	if cfg.Backend.Provider != "" && cfg.Backend.APIKey != "" {
		serveCache.SetProviderKey(cfg.Backend.Provider, cfg.Backend.APIKey)
	}
	orch.SetBackendCache(serveCache)
	orch.WithMachineID(relay.GetMachineID()) // stable 8-char hex, not cfg.MachineID (hostname-dependent)

	// Open SQLite database for structured stores.
	var sqlDB *sqlitedb.DB
	{
		dbPath := filepath.Join(huginnHome, "huginn.db")
		db, dbErr := sqlitedb.Open(dbPath)
		if dbErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: sqlite unavailable: %v\n", dbErr)
		} else {
			if schemaErr := db.ApplySchema(); schemaErr != nil {
				fmt.Fprintf(os.Stderr, "huginn: warning: sqlite schema failed: %v\n", schemaErr)
			} else if migrErr := db.Migrate(session.Migrations()); migrErr != nil {
				fmt.Fprintf(os.Stderr, "huginn: warning: sqlite migrations failed: %v\n", migrErr)
			} else {
				sqlDB = db
			}
		}
	}

	// Session store — use SQLite if available, fall back to filesystem.
	serverSessionsDir := filepath.Join(huginnHome, "sessions")

	// Run one-time filesystem → SQLite migration when SQLite is available.
	if sqlDB != nil {
		if migrErr := session.MigrateFromFilesystem(serverSessionsDir, sqlDB); migrErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: session migration: %v\n", migrErr)
		}
	}

	var sessStore session.StoreInterface
	if sqlDB != nil {
		sessStore = session.NewSQLiteSessionStore(sqlDB)
	} else {
		sessStore = session.NewStore(serverSessionsDir)
	}

	// Connections manager
	connStorePath := filepath.Join(huginnHome, "connections.json")
	// Run one-time migration from JSON to SQLite (idempotent).
	if sqlDB != nil {
		if err := connections.MigrateFromJSON(connStorePath, sqlDB); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: connections migration: %v\n", err)
		}
	}
	// Use SQLite store if available, fall back to JSON store.
	var connStore connections.StoreInterface
	var connMgr *connections.Manager
	var connProviders []connections.IntegrationProvider
	if sqlDB != nil {
		connStore = connections.NewSQLiteConnectionStore(sqlDB)
	} else {
		var connStoreErr error
		connStore, connStoreErr = connections.NewStore(connStorePath)
		if connStoreErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: connections store: %v\n", connStoreErr)
		}
	}
	var connSecrets connections.SecretStore
	if connStore != nil {
		connSecrets = connections.NewSecretStore()
		connProviders = buildProviders(cfg)
		connMgr = connections.NewManager(connStore, connSecrets, "")
	}

	// Wire built-in llama.cpp runtime + model store (always configured for the web UI).
	runtimeMgr, runtimeMgrErr := runtime.NewManager(huginnHome)
	if runtimeMgrErr == nil {
		modelStore, modelStoreErr := modelslib.NewStore(huginnHome)
		if modelStoreErr == nil {
			// Start server
			srv = server.New(*cfg, orch, sessStore, token, huginnHome, connMgr, connStore, connProviders)
			srv.SetRuntimeManager(runtimeMgr)
			srv.SetModelStore(modelStore)
		} else {
			srv = server.New(*cfg, orch, sessStore, token, huginnHome, connMgr, connStore, connProviders)
		}
	} else {
		srv = server.New(*cfg, orch, sessStore, token, huginnHome, connMgr, connStore, connProviders)
	}

	// Wire the BackendCache into the server so handleUpdateConfig can push key
	// changes into running backends without requiring a restart.
	srv.WithBackendCache(serveCache)

	// Wire multi-agent subsystems.
	tm := threadmgr.New()
	srv.SetThreadManager(tm)

	// Wire SQLite DB so the thread panel endpoint can query parent_message_id.
	if sqlDB != nil {
		srv.SetDB(sqlDB)
	}

	// Wire thread persistence store — run migration then attach to manager.
	if sqlDB != nil {
		if migrErr := sqlDB.Migrate(threadmgr.Migrations()); migrErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: thread persistence migration failed: %v\n", migrErr)
		} else {
			tm.SetStore(threadmgr.NewSQLiteThreadStore(sqlDB))
		}
	}

	ca := threadmgr.NewCostAccumulator(0) // 0 = unlimited; no budget field in config
	srv.SetCostAccumulator(ca)

	previewGate := threadmgr.NewDelegationPreviewGate(false) // disabled by default; no config field
	srv.SetPreviewGate(previewGate)

	// ── Spaces store ─────────────────────────────────────────────────────
	var spaceStore spaces.StoreInterface
	if sqlDB != nil {
		if migrErr := sqlDB.Migrate(spaces.Migrations()); migrErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: spaces migration failed: %v\n", migrErr)
		} else {
			spaceStore = spaces.NewSQLiteSpaceStore(sqlDB)
			srv.SetSpaceStore(spaceStore)
			autoCreateDMSpaces(spaceStore)
		}
	}

	// ── Notification store ──────────────────────────────────────────────
	notifDir := filepath.Join(huginnHome, "store", "notifications")
	if err := os.MkdirAll(notifDir, 0755); err != nil {
		return nil, "", nil, fmt.Errorf("huginn: create notification store dir: %w", err)
	}
	notifDB, err := pebble.Open(notifDir, &pebble.Options{})
	if err != nil {
		return nil, "", nil, fmt.Errorf("huginn: open notification store: %w", err)
	}
	// cleanup is built up incrementally below; callers must invoke it after srv.Stop.
	var cleanupFns []func()
	if sqlDB != nil {
		cleanupFns = append(cleanupFns, func() { sqlDB.Close() })
	}
	cleanupFns = append(cleanupFns, func() { notifDB.Close() })

	// Wire cloud vault memory replicator — drains cloud_vault_queue and pushes agent
	// memory writes to HuginnCloud so they persist across sessions and devices.
	if sqlDB != nil {
		srvMemReplicator := wireMemoryReplicator(sqlDB)
		srvMemReplicator.Start()
		cleanupFns = append(cleanupFns, srvMemReplicator.Stop)
	}

	// Run one-time Pebble → SQLite migration if SQLite is available.
	if sqlDB != nil {
		if migrErr := notification.MigrateFromPebble(notifDB, sqlDB); migrErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: notification migration: %v\n", migrErr)
		}
	}

	// Use SQLite store if available; fall back to Pebble store.
	var notifStore notification.StoreInterface
	if sqlDB != nil {
		notifStore = notification.NewSQLiteNotificationStore(sqlDB)
		// Purge expired notifications on startup (non-fatal).
		sqlDB.Write().Exec(
			`DELETE FROM notifications WHERE expires_at IS NOT NULL AND expires_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		)
	} else {
		notifStore = notification.NewStore(notifDB)
	}
	srv.SetNotificationStore(notifStore)

	// ── Scheduler ───────────────────────────────────────────────────────
	if cfg.SchedulerEnabled {
		agentFn := func(ctx context.Context, opts scheduler.RunOptions) (string, error) {
			reg := orch.GetAgentRegistry()
			if reg == nil {
				return "", fmt.Errorf("workflow: agent registry not initialised")
			}
			ag, ok := reg.ByName(opts.AgentName)
			if !ok {
				return "", fmt.Errorf("workflow: agent %q not found", opts.AgentName)
			}
			sessionID := "workflow-" + opts.RunID
			var buf strings.Builder
			onToken := func(tok string) { buf.WriteString(tok) }
			if err := orch.ChatWithAgent(ctx, ag, opts.Prompt, sessionID, onToken, nil, nil); err != nil {
				return "", fmt.Errorf("workflow: agent execution: %w", err)
			}
			output := strings.TrimSpace(buf.String())
			if output == "" {
				output = "(agent produced no output)"
			}
			return output, nil
		}

		sched := scheduler.New()
		sched.Start()
		cleanupFns = append(cleanupFns, func() { sched.Stop(context.Background()) })
		srv.SetScheduler(sched)

		// Workflows
		workflowsDir := filepath.Join(huginnHome, "workflows")
		if err := os.MkdirAll(workflowsDir, 0755); err != nil {
			logger.Warn("huginn: create workflows dir", "err", err)
		}
		workflowRunsDir := filepath.Join(huginnHome, "workflow-runs")

		// Run one-time routine → workflow migration.
		routinesDir := filepath.Join(huginnHome, "routines")
		if err := scheduler.MigrateRoutinesToWorkflows(routinesDir, workflowsDir); err != nil {
			logger.Warn("huginn: migrate routines to workflows", "err", err)
		}

		// Apply scheduler schema migrations (e.g. workflow_runs CHECK constraint update).
		if sqlDB != nil {
			if err := sqlDB.Migrate(scheduler.Migrations()); err != nil {
				fmt.Fprintf(os.Stderr, "huginn: warning: scheduler schema migrations failed: %v\n", err)
			}
		}

		// Run one-time JSONL → SQLite migration if SQLite is available.
		if sqlDB != nil {
			if err := scheduler.MigrateFromJSONL(workflowRunsDir, sqlDB); err != nil {
				fmt.Fprintf(os.Stderr, "huginn: warning: workflow runs migration: %v\n", err)
			}
		}

		// Use SQLite store if available; fall back to JSONL store.
		var workflowRunStore scheduler.WorkflowRunStoreInterface
		if sqlDB != nil {
			workflowRunStore = scheduler.NewSQLiteWorkflowRunStore(sqlDB)
		} else {
			if err := os.MkdirAll(workflowRunsDir, 0755); err != nil {
				logger.Warn("huginn: create workflow-runs dir", "err", err)
			}
			workflowRunStore = scheduler.NewWorkflowRunStore(workflowRunsDir)
		}
		srv.SetWorkflowRunStore(workflowRunStore)

		wfDeliverers := scheduler.NewDelivererRegistry(buildCredentialResolver(connStore, connSecrets))
		wfRunner := scheduler.MakeWorkflowRunner(
			workflowRunStore,
			agentFn,
			notifStore,
			func(eventType string, payload map[string]any) {
				srv.BroadcastWS(server.WSMessage{Type: eventType, Payload: payload})
				// Push to HuginnCloud (huginncloud-app inbox) when a workflow finishes.
				// On completion/failure/partial we fetch the newest notification for this
				// workflow and forward it as notification_sync so the remote inbox updates
				// without the user needing to manually refresh.
				if notifStore != nil && (eventType == "workflow_complete" || eventType == "workflow_failed" || eventType == "workflow_partial") {
					if wfID, _ := payload["workflow_id"].(string); wfID != "" {
						if ns, err := notifStore.ListByWorkflow(wfID); err == nil && len(ns) > 0 {
							n := ns[0] // newest first
							// pending_count is not available here; pass 0 so the cloud
							// inbox icon updates without an incorrect badge count.
							srv.SendRelay(server.BuildNotificationRelayMsg(n, 0))
						}
					}
				}
			},
			func(title, msg string) { _ = traypkg.Notify(title, msg) },
			func(spaceID, summary, detail string) error {
				if spaceStore == nil {
					logger.Warn("scheduler: space delivery skipped — space store not configured", "space_id", spaceID)
					return nil
				}
				space, err := spaceStore.GetSpace(spaceID)
				if err != nil || space == nil {
					logger.Warn("scheduler: space delivery skipped — space not found", "space_id", spaceID)
					return nil
				}
				srv.BroadcastWS(server.WSMessage{
					Type: "space_notification",
					Payload: map[string]any{
						"space_id":   spaceID,
						"space_name": space.Name,
						"summary":    summary,
						"detail":     detail,
					},
				})
				return nil
			},
			huginnHome,
			wfDeliverers,
			func(wfID, runID, dtype, target, errMsg string) {
				srv.BroadcastWS(server.WSMessage{
					Type: "notification_delivery_failed",
					Payload: map[string]any{
						"workflow_id":   wfID,
						"run_id":        runID,
						"delivery_type": dtype,
						"target":        target, // already redacted (scheme+host / *@domain)
						"error":         errMsg,
					},
				})
			},
		)
		sched.SetWorkflowRunner(wfRunner)
		if err := sched.LoadWorkflows(workflowsDir); err != nil {
			logger.Warn("huginn: load workflows", "err", err)
		}
	}

	// Wire delegate_to_agent tool so the primary agent can spawn sub-threads
	// during web chat. Agents are loaded fresh; failure is non-fatal.
	if agentsCfg, agentsErr := agentslib.LoadAgents(); agentsErr == nil && agentsCfg != nil && len(agentsCfg.Agents) > 0 {
		srvUsername := memory.ResolveUsername("")
		agentReg := agentslib.BuildRegistryWithUsername(agentsCfg, models, srvUsername)
		logger.Info("startServer: wiring agents", "count", len(agentsCfg.Agents), "names", agentReg.Names())
		orch.SetAgentRegistry(agentReg)

		toolReg := tools.NewRegistry()
		delegateTool := &threadmgr.DelegateToAgentTool{
			Fn: func(ctx context.Context, p threadmgr.DelegateParams) threadmgr.DelegateResult {
				sessionID := agent.GetSessionID(ctx)
				if sessionID == "" {
					return threadmgr.DelegateResult{Err: fmt.Errorf("delegate_to_agent: no session ID in context")}
				}

				// Validate agent exists.
				if _, found := agentReg.ByName(p.AgentName); !found {
					return threadmgr.DelegateResult{Err: fmt.Errorf("delegate_to_agent: unknown agent %q", p.AgentName)}
				}

				// Load the session for SpawnThread (may be a stub if not yet persisted).
				sess, loadErr := sessStore.Load(sessionID)
				if loadErr != nil {
					sess = &session.Session{ID: sessionID}
				}

				// Create the thread in the thread manager.
				t, createErr := tm.Create(threadmgr.CreateParams{
					SessionID:      sessionID,
					AgentID:        p.AgentName,
					Task:           p.Task,
					Rationale:      p.Rationale,
					DependsOnHints: p.DependsOn,
					SpaceID:        sess.SpaceID(),
				})
				if createErr != nil {
					return threadmgr.DelegateResult{Err: createErr}
				}

				tm.ResolveDependencies(t.ID)

				// Acquire file leases to detect conflicts.
				if len(p.FileIntents) > 0 {
					conflicts, leaseErr := tm.AcquireLeases(t.ID, p.FileIntents)
					if leaseErr != nil {
						return threadmgr.DelegateResult{Err: leaseErr}
					}
					if len(conflicts) > 0 {
						tm.Cancel(t.ID)
						return threadmgr.DelegateResult{ThreadID: t.ID, Conflicts: conflicts}
					}
				}

				// Broadcast function routes events to WS clients.
				broadcastFn := func(sid, msgType string, payload map[string]any) {
					srv.BroadcastToSession(sid, msgType, payload)
				}

				// Spawn immediately if dependencies are met.
				if tm.IsReady(t.ID) {
					tid := t.ID
					dagFn := func() {
						tm.EvaluateDAG(ctx, sessionID, sessStore, sess, agentReg, b, broadcastFn, ca)
					}
					tm.SpawnThread(ctx, tid, sessStore, sess, agentReg, b, broadcastFn, ca, dagFn)
					return threadmgr.DelegateResult{ThreadID: t.ID, Spawned: true}
				}
				return threadmgr.DelegateResult{ThreadID: t.ID, Spawned: false}
			},
		}
		toolReg.Register(delegateTool)

		// list_team_status — lets a lead agent see all thread statuses in its session.
		listTeamTool := &threadmgr.ListTeamStatusTool{
			Fn: func(ctx context.Context) ([]*threadmgr.Thread, error) {
				sessionID := agent.GetSessionID(ctx)
				if sessionID == "" {
					return nil, fmt.Errorf("no session ID in context")
				}
				return tm.ListBySession(sessionID), nil
			},
		}
		toolReg.Register(listTeamTool)

		// recall_thread_result — lets a lead agent read a sub-thread's FinishSummary.
		recallTool := &threadmgr.RecallThreadResultTool{
			Fn: func(ctx context.Context, threadID string) (*threadmgr.Thread, error) {
				sessionID := agent.GetSessionID(ctx)
				t, ok := tm.Get(threadID)
				if !ok {
					return nil, fmt.Errorf("thread %q not found", threadID)
				}
				if sessionID != "" && t.SessionID != sessionID {
					return nil, fmt.Errorf("thread %q not found", threadID)
				}
				return t, nil
			},
		}
		toolReg.Register(recallTool)

		// Auto-approve all tools in server mode — reuse the gate created above.
		orch.SetTools(toolReg, serverGate)

		// Also wire the mention-based delegation path so @AgentName in chat
		// spawns threads even when the primary model doesn't support tool calls.
		srv.SetMentionDelegate(func(ctx context.Context, sessionID, userMsg, parentMsgID string) {
			logger.Info("mentionDelegate: called", "session_id", sessionID, "msg", userMsg, "parent_msg_id", parentMsgID)
			sess, loadErr := sessStore.Load(sessionID)
			if loadErr != nil {
				logger.Warn("mentionDelegate: session load failed, using stub", "err", loadErr)
				sess = &session.Session{ID: sessionID}
			}
			broadcastFn := func(sid, msgType string, payload map[string]any) {
				srv.BroadcastToSession(sid, msgType, payload)
			}
			threadmgr.CreateFromMentions(ctx, sessionID, userMsg, parentMsgID, agentReg, sessStore, sess, b, broadcastFn, ca, tm)
		})

		// Wire automatic help resolution: when a sub-agent calls request_help,
		// the primary agent answers via a focused background LLM call.
		helpResolver := &threadmgr.AutoHelpResolver{
			Backend:  b,
			AgentReg: agentReg,
			Store:    sessStore,
			Broadcast: func(sessionID, msgType string, payload map[string]any) {
				srv.BroadcastToSession(sessionID, msgType, payload)
			},
			PrimaryAgent: func(sessionID string) *agentslib.Agent {
				return srv.ResolveAgent(sessionID)
			},
		}
		tm.SetHelpResolver(helpResolver)

		// Wire completion notifier: when a sub-agent finishes, the primary agent
		// posts a brief natural-language summary in the main chat.
		completionNotifier := &threadmgr.CompletionNotifier{
			Backend:  b, // fallback only; BackendFor below takes precedence
			AgentReg: agentReg,
			Store:    sessStore,
			// BackendFor resolves the correct provider backend for the primary agent
			// (e.g. Anthropic for Tom) so the synthesis LLM call doesn't go to Ollama.
			BackendFor: func(ag *agentslib.Agent) (backend.Backend, error) {
				if ag == nil {
					return serveCache.For("", "", "", "")
				}
				return serveCache.For(ag.Provider, ag.Endpoint, ag.APIKey, ag.GetModelID())
			},
			Broadcast: func(sessionID, msgType string, payload map[string]any) {
				srv.BroadcastToSession(sessionID, msgType, payload)
			},
			PrimaryAgent: func(sessionID string) *agentslib.Agent {
				return srv.ResolveAgent(sessionID)
			},
		}
		// Wire lead-agent follow-up: when Sam finishes, Tom synthesizes and replies
		// in the main chat so it feels like teammates communicating naturally.
		// Runs in a goroutine (non-blocking); uses session-scoped WS broadcast.
		// Guards against agents following up on themselves (e.g. Sam → Sam loop).
		completionNotifier.FollowUpFn = func(ctx context.Context, sessionID, completedAgentID string, summary *threadmgr.FinishSummary) {
			// Resolve the lead agent using space-aware lookup so channel sessions
			// always get the channel's lead agent (e.g. Tom) rather than the
			// global default/first agent.
			var spaceID string
			if loadedSess, loadErr := sessStore.Load(sessionID); loadErr == nil {
				spaceID = loadedSess.SpaceID()
			}
			ag := srv.ResolveAgentForSpace(sessionID, spaceID)
			if ag == nil || strings.EqualFold(ag.Name, completedAgentID) {
				return // No lead agent found, or the lead agent completed itself
			}

			// Wait for Sam's session to fully release its run slot before Tom runs.
			// Sam's goroutine may still be in defer sess.endRun() when FollowUpFn fires.
			waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
			defer waitCancel()
			if !orch.WaitForSessionIdle(sessionID, waitCtx) {
				logger.Warn("follow-up: session did not become idle in time", "session_id", sessionID, "agent", ag.Name)
				srv.BroadcastToSession(sessionID, "follow_up_cancelled", map[string]any{
					"agent":  ag.Name,
					"reason": "session busy",
				})
				return
			}

			// Immediately signal that the lead agent is preparing a response so the
			// frontend can show a "thinking" indicator before the first token arrives.
			// This closes the UX gap where Sam finishes but Tom is silent for 30-60s.
			srv.BroadcastToSession(sessionID, "follow_up_start", map[string]any{
				"agent": ag.Name,
			})

			// Build a concise synthesis prompt for the lead agent.
			// Truncate the summary to avoid Tom regurgitating Sam's entire report —
			// the full report lives in the thread panel. Tom writes a brief synthesis.
			summaryText := strings.TrimSpace(summary.Summary)
			const maxSummaryLen = 800
			truncNote := ""
			if len(summaryText) > maxSummaryLen {
				summaryText = summaryText[:maxSummaryLen]
				truncNote = "\n\n(Full report is available in the thread panel.)"
			}
			followUpMsg := completedAgentID + " has completed their task. Key findings:\n\n" +
				summaryText + truncNote +
				"\n\nPlease give the user a brief synthesis (3-5 sentences max) in your own words. Do NOT repeat the full report — just the key takeaways and recommended next steps."

			// Stream Tom's reply tokens to the session's WS clients.
			var replyBuf strings.Builder
			onToken := func(tok string) {
				replyBuf.WriteString(tok)
				srv.BroadcastToSession(sessionID, "follow_up_token", map[string]any{
					"agent": ag.Name,
					"token": tok,
				})
			}
			onEvent := func(ev backend.StreamEvent) {
				if ev.Type == backend.StreamText {
					replyBuf.WriteString(ev.Content)
				}
			}
			// noopToolEvent suppresses Tom's synthesis tool calls from the main
			// channel UI — tool calls during synthesis are implementation details.
			noopToolEvent := func(string, map[string]any) {}

			chatErr := orch.ChatWithAgent(ctx, ag, followUpMsg, sessionID, onToken, noopToolEvent, onEvent)
			if chatErr != nil && strings.Contains(chatErr.Error(), "already running") {
				// Defensive: if the idle wait raced with another request, retry once.
				time.Sleep(500 * time.Millisecond)
				chatErr = orch.ChatWithAgent(ctx, ag, followUpMsg, sessionID, onToken, noopToolEvent, onEvent)
			}
			if chatErr != nil {
				logger.Warn("follow-up: ChatWithAgent failed", "session_id", sessionID, "agent", ag.Name, "err", chatErr)
				srv.BroadcastToSession(sessionID, "follow_up_cancelled", map[string]any{
					"agent":  ag.Name,
					"reason": chatErr.Error(),
				})
				return
			}

			replyStr := strings.TrimSpace(replyBuf.String())
			if replyStr == "" {
				return
			}

			// Persist Tom's follow-up reply to the session store.
			if loadedSess, loadErr := sessStore.Load(sessionID); loadErr == nil {
				_ = sessStore.Append(loadedSess, session.SessionMessage{
					ID:      session.NewID(),
					Role:    "assistant",
					Content: replyStr,
					Agent:   ag.Name,
					Ts:      time.Now().UTC(),
				})
			}

			// Broadcast the finalized reply so the frontend replaces the streaming
			// bubble with the persisted message content.
			srv.BroadcastToSession(sessionID, "agent_follow_up", map[string]any{
				"agent":   ag.Name,
				"content": replyStr,
			})
		}
		tm.SetCompletionNotifier(completionNotifier)

		// Wire thread event emitter so the browser sees delegation in real-time.
		// Events are broadcast as "thread_event" WS envelopes scoped to the session.
		tm.SetEventEmitter(srv.MakeThreadEventEmitter())

		// Wire backend resolver so delegated threads (Sam, Dave, etc.) get the
		// correct agent-specific backend (Anthropic, not Ollama) via BackendCache.
		tm.SetBackendResolver(func(provider, endpoint, apiKey, model string) (backend.Backend, error) {
			return serveCache.For(provider, endpoint, apiKey, model)
		})

		// Wire the tool registry so sub-agents get their configured toolbelt
		// (bash, read_file, git_status, etc.) when running as threads.
		{
			cwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				cwd = huginnHome
			}
			bashTimeout := time.Duration(cfg.BashTimeoutSecs) * time.Second
			if bashTimeout == 0 {
				bashTimeout = 120 * time.Second
			}
			subToolReg := tools.NewRegistry()
			tools.RegisterBuiltins(subToolReg, cwd, bashTimeout)
			tools.RegisterGitTools(subToolReg, cwd)
			tools.RegisterTestsTool(subToolReg, cwd, bashTimeout)
			tm.SetToolRegistry(subToolReg)
		}
	}

	// Wire relay config if HuginnCloud is configured.
	relayStore := relay.NewTokenStore()
	srv.SetRelayConfig(relayStore, os.Getenv("HUGINN_JWT_SECRET"))

	// Wire HuginnCloud broker client when the machine is registered.
	if relayStore.IsRegistered() {
		brokerURL := os.Getenv("HUGINN_OAUTH_BROKER_URL")
		if brokerURL == "" {
			brokerURL = relay.OAuthBrokerURL
		}
		srv.SetBrokerClient(broker.NewClient(brokerURL, relayStore))
	}

	// Boot the provider catalog (embedded JSON) and soft-check for CDN updates
	// in the background. The catalog resolves friendly aliases (e.g. "haiku" →
	// "claude-haiku-4-5-20251001") and surfaces deprecation warnings so users
	// are never silently broken by Anthropic/OpenAI model renames.
	modelslib.GlobalProviderCatalog() // eagerly init from embedded JSON
	modelslib.TryRefreshProviderCatalog(
		"https://models.huginncloud.com/v1/catalog.json",
		7*24*time.Hour, // refresh if cache is older than 7 days
	)

	// Wire MuninnDB config path to BOTH the server (REST handlers) and the
	// orchestrator (per-session vault connections). Both must receive the path
	// or the orchestrator's connectAgentVault will return early with a warning
	// and vault tools won't be registered for any serve-path chat session.
	home, _ := os.UserHomeDir()
	muninnCfgFilePath := filepath.Join(home, ".config", "huginn", "muninn.json")
	srv.SetMuninnConfigPath(muninnCfgFilePath)
	orch.SetMuninnConfigPath(muninnCfgFilePath)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		return nil, "", nil, fmt.Errorf("server start: %w", err)
	}
	orch.StartSessionCleanup(ctx)

	// Now that we know the real address, update the OAuth callback URL.
	if connMgr != nil {
		connMgr.SetRedirectURL(fmt.Sprintf("http://%s/oauth/callback", srv.Addr()))
	}

	// Wire stats persister: flushes Registry snapshots + cost records to SQLite every 5 min.
	// Must be wired before srv.Start() so Stop() drains it before the DB closes.
	var servePersister *stats.Persister
	if sqlDB != nil {
		serveReg := stats.NewRegistry()
		srv.SetStatsRegistry(serveReg)
		servePersister = stats.NewPersister(sqlDB, serveReg)
		srv.SetStatsPersister(servePersister)
		// Forward CostAccumulator events to the persister for SQLite storage.
		ca.SetCostSink(func(threadID string, costUSD float64, promptTokens, completionTokens int) {
			servePersister.EnqueueCost(stats.CostEvent{
				SessionID:        threadID,
				CostUSD:          costUSD,
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
			})
		})
	}

	// Wire audit logger: non-blocking permission gate event recording to SQLite.
	srv.StartAuditLog(sqlDB)

	// Wire connection token refresh events → WS broadcast.
	// Lets the frontend react to proactive refresh failures (e.g. revoked tokens).
	if connMgr != nil {
		connMgr.SetOnRefreshEvent(func(event, connID string, provider connections.Provider, errMsg string) {
			srv.BroadcastWS(server.WSMessage{
				Type: event,
				Payload: map[string]any{
					"connection_id": connID,
					"provider":      string(provider),
					"error":         errMsg,
				},
			})
		})
	}

	// Wire HuginnCloud satellite: connect if registered, wire inbound dispatcher.
	{
		satellite := relay.NewSatellite(os.Getenv("HUGINN_CLOUD_URL"))
		hub := satellite.Hub(context.Background())
		orch.SetRelayHub(hub)
		if wsHub, ok := hub.(*relay.WebSocketHub); ok {
			activeSessions := relay.NewActiveSessions()
			chatFn := func(ctx context.Context, sessionID, userMsg string,
				onToken func(string),
				onToolEvent func(eventType string, payload map[string]any),
				onEvent func(backend.StreamEvent)) error {
				return orch.ChatForSessionWithAgent(ctx, sessionID, userMsg, onToken, onToolEvent, onEvent)
			}
			newSessionFn := func(id string) string {
				sess, err := orch.NewSession(id)
				if err != nil {
					logger.Error("failed to create session", "err", err)
					return ""
				}
				return sess.ID
			}
			// Wire outbox for durable message delivery across reconnects.
			var serveOutbox *relay.Outbox
			{
				relayStorePath := filepath.Join(huginnHome, "relay-store")
				if relayStore, relayStoreErr := storage.Open(relayStorePath); relayStoreErr == nil {
					cleanupFns = append(cleanupFns, func() { relayStore.Close() })
					serveOutbox = relay.NewOutbox(relayStore, hub)
				}
			}
			if serveOutbox != nil {
				outboxCtx, outboxCancel := context.WithCancel(context.Background())
				cleanupFns = append(cleanupFns, outboxCancel)
				go func() {
					ticker := time.NewTicker(5 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-outboxCtx.Done():
							return
						case <-ticker.C:
							if err := serveOutbox.Flush(outboxCtx); err != nil && !errors.Is(err, context.Canceled) {
								logger.Warn("relay: outbox flush", "err", err)
							}
						}
					}
				}()
			}
			
			// Use relay.GetMachineID() (8-char hex) — NOT cfg.MachineID (full hostname-hex).
			// The cloud hub registers satellites under relay.GetMachineID(); using cfg.MachineID
			// causes the machine ID filter to silently drop all relayed messages.
			// Mutex protecting concurrent reads/writes to cfg.Backend fields from relay callbacks.
			var backendMu sync.RWMutex

			logger.Info("dispatcher: routing machine_id", "machine_id", relay.GetMachineID())
			runAgentFn := func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error {
				reg := orch.GetAgentRegistry()
				if reg == nil {
					return fmt.Errorf("agent registry not available")
				}
				ag, ok := reg.ByName(agentName)
				if !ok {
					return fmt.Errorf("agent %q not found", agentName)
				}
				return orch.ChatWithAgent(ctx, ag, prompt, sessionID, onToken, nil, nil)
			}
			wsHub.SetOnMessage(relay.NewDispatcher(relay.DispatcherConfig{
				MachineID:   relay.GetMachineID(),
				DeliverPerm: serverGate.DeliverRelayResponse,
				Hub:         hub,
				Shell:       relay.NewShellManager(),
				ChatSession: chatFn,
				NewSession:  newSessionFn,
				RunAgent:    runAgentFn,
				ListModels:  orch.ModelNames,
				GetModelProviders: func() []relay.ModelProviderInfo {
					backendMu.RLock()
					provider := cfg.Backend.Provider
					endpoint := cfg.Backend.Endpoint
					apiKey := cfg.Backend.APIKey
					backendMu.RUnlock()
					if provider == "" {
						provider = "ollama"
					}
					return []relay.ModelProviderInfo{{
						ID:        provider,
						Name:      providerDisplayName(provider),
						Endpoint:  endpoint,
						APIKey:    apiKey,
						Connected: cfg.Backend.ResolvedAPIKey() != "" || provider == "ollama",
						Models:    fetchOllamaModels(cfg.OllamaBaseURL),
					}}
				},
				GetModelConfig: func(provider string) (*relay.ModelProviderInfo, error) {
					backendMu.RLock()
					configured := cfg.Backend.Provider
					endpoint := cfg.Backend.Endpoint
					apiKey := cfg.Backend.APIKey
					backendMu.RUnlock()
					if configured == "" {
						configured = "ollama"
					}
					if provider != configured {
						return nil, fmt.Errorf("provider %q not configured", provider)
					}
					return &relay.ModelProviderInfo{
						ID:        configured,
						Name:      providerDisplayName(configured),
						Endpoint:  endpoint,
						APIKey:    apiKey,
						Connected: cfg.Backend.ResolvedAPIKey() != "" || configured == "ollama",
						Models:    fetchOllamaModels(cfg.OllamaBaseURL),
					}, nil
				},
				UpdateModelConfig: func(provider, endpoint, apiKey string) error {
					backendMu.Lock()
					cfg.Backend.Provider = provider
					cfg.Backend.Endpoint = endpoint
					if apiKey != "" {
						cfg.Backend.APIKey = apiKey
					}
					backendMu.Unlock()
					return cfg.Save()
				},
				PullModel: func(name string) error {
					baseURL := cfg.OllamaBaseURL
					if baseURL == "" {
						baseURL = "http://localhost:11434"
					}
					payload, _ := json.Marshal(map[string]any{"name": name, "stream": false})
					client := &http.Client{Timeout: 10 * time.Minute} // model pulls can take minutes
					resp, err := client.Post(baseURL+"/api/pull", "application/json", bytes.NewReader(payload))
					if err != nil {
						return fmt.Errorf("ollama not reachable: %w", err)
					}
					defer resp.Body.Close()
					if resp.StatusCode >= 400 {
						return fmt.Errorf("ollama pull returned %d", resp.StatusCode)
					}
					return nil
				},
				HTTPProxy:   makeLocalHTTPProxy(srv.Addr(), token),
				Active:      activeSessions,
			}))
			cleanupFns = append(cleanupFns, activeSessions.CancelAll)
		}
		srv.SetSatellite(satellite)
	}

	// Build cleanup func: run in reverse order (scheduler before DB).
	cleanupFn := func() {
		for i := len(cleanupFns) - 1; i >= 0; i-- {
			cleanupFns[i]()
		}
	}

	return srv, token, cleanupFn, nil
}

// cmdServe launches the headless HTTP + WebSocket server (no TUI).
// Unless --no-tray is passed, it also spawns the system tray as a detached
// subprocess (skipped if the tray is already running).
// By default the server daemonizes itself (gives back the terminal). Pass
// --foreground to keep the server attached to the terminal.
func cmdServe(cfg *config.Config, noTray, foreground, daemon bool) {
	if !foreground && !daemon {
		// Daemonize: re-exec self with --daemon in background, write logs to file.
		huginnHome, err := huginnDir()
		if err != nil {
			fatalf("huginnDir: %v", err)
		}

		// Prevent duplicate servers: check serve.pid before spawning.
		pidPath := filepath.Join(huginnHome, "serve.pid")
		owned, err := traypkg.AcquireLock(pidPath)
		if err != nil {
			fatalf("serve: check lockfile: %v", err)
		}
		if !owned {
			fmt.Fprintln(os.Stderr, "huginn serve: server is already running. Use 'huginn serve --foreground' to force, or kill the existing process.")
			os.Exit(1)
		}
		// Release immediately — daemon subprocess will re-acquire it.
		traypkg.ReleaseLock(pidPath)

		logPath := filepath.Join(huginnHome, "serve.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fatalf("open serve.log: %v", err)
		}

		exe, err := os.Executable()
		if err != nil {
			fatalf("executable: %v", err)
		}
		// Rebuild args: top-level flags first, then "serve --daemon".
		var args []string
		if noTray {
			args = append(args, "--no-tray")
		}
		args = append(args, "serve", "--daemon")
		cmd := exec.Command(exe, args...)
		cmd.Stdin = nil
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			logFile.Close()
			fatalf("daemonize: %v", err)
		}
		_ = cmd.Process.Release()
		logFile.Close() // child has the fd; parent can close its copy
		webURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.WebUI.Port)
		fmt.Printf("Huginn server starting...\n")
		fmt.Printf("  Web UI:  %s\n", webURL)
		fmt.Printf("  Logs:    %s\n", logPath)
		fmt.Printf("  Token:   %s\n", filepath.Join(huginnHome, "server.token"))

		// On first run (no agents configured), open the browser automatically
		// so users land directly in the web UI to complete setup.
		agentCfg, agentErr := agentslib.LoadAgents()
		isFirstRun := agentErr != nil || agentCfg == nil || len(agentCfg.Agents) == 0
		if isFirstRun {
			fmt.Printf("\n  Opening browser for first-time setup...\n")
			// Give the daemon a moment to bind before the browser hits it.
			time.Sleep(1500 * time.Millisecond)
			_ = traypkg.OpenURL(webURL)
		}
		return
	}

	// Daemon: acquire the serve.pid lockfile for the lifetime of the server.
	if daemon {
		huginnHome, err := huginnDir()
		if err == nil {
			pidPath := filepath.Join(huginnHome, "serve.pid")
			if owned, _ := traypkg.AcquireLock(pidPath); owned {
				defer traypkg.ReleaseLock(pidPath)
			}
		}
	}

	if !noTray {
		spawnTrayIfNeeded(fmt.Sprintf("127.0.0.1:%d", cfg.WebUI.Port))
	}

	srv, token, cleanup, err := startServer(cfg)
	if err != nil {
		fatalf("%v", err)
	}
	defer cleanup()
	fmt.Printf("Huginn Web UI: http://%s\n", srv.Addr())
	fmt.Printf("Auth token:    %s\n", token)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\nShutting down...")
	srv.Stop(context.Background())
}

// spawnTrayIfNeeded launches `huginn tray --attach=<addr>` as a detached background process.
// Does nothing if the tray is already running (checked via lockfile).
func spawnTrayIfNeeded(serverAddr string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	lockPath := filepath.Join(home, ".huginn", "tray.pid")

	// Check lockfile — if live process holds it, tray is already running.
	owned, err := traypkg.AcquireLock(lockPath)
	if err != nil || !owned {
		// Either tray is running or we couldn't check — either way, don't spawn.
		return
	}
	// We acquired the lock, but we're not the tray — release it so the tray subprocess can take it.
	traypkg.ReleaseLock(lockPath)

	binPath, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(binPath, "tray", "--attach="+serverAddr)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
	if cmd.Process != nil {
		_ = cmd.Process.Release() // detach — let it run independently
	}
}

// cmdTray starts the Huginn system tray application.
// Acquires a lockfile at ~/.huginn/tray.pid to prevent duplicate instances.
// Starts the server in-process and runs the systray event loop.
// Blocks until the user quits from the tray menu.
func cmdTray(cfg *config.Config, attachAddr string) {
	huginnHome, err := huginnDir()
	if err != nil {
		fatalf("huginn dir: %v", err)
	}

	lockPath := filepath.Join(huginnHome, "tray.pid")
	owned, err := traypkg.AcquireLock(lockPath)
	if err != nil {
		fatalf("tray: lockfile: %v", err)
	}
	if !owned {
		fmt.Fprintln(os.Stderr, "huginn: tray is already running.")
		os.Exit(1)
	}
	defer traypkg.ReleaseLock(lockPath)

	binPath, err := os.Executable()
	if err != nil {
		binPath = "huginn"
	}

	var activeSrv *server.Server
	var activeCleanup func()

	// stopActive tears down any running server instance and releases its
	// resources (pebble, scheduler). Safe to call when nothing is running.
	stopActive := func() {
		if activeSrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			activeSrv.Stop(ctx)
			cancel()
			activeSrv = nil
		}
		if activeCleanup != nil {
			activeCleanup()
			activeCleanup = nil
		}
	}

	onStart := func() (string, error) {
		// If the health check detected the previous server as dead without an
		// explicit Stop (serverOwned flipped to false by the poll), we must
		// release its resources before opening a new instance — otherwise pebble
		// will refuse to open: "lock held by current process".
		stopActive()

		srv, _, cleanup, err := startServer(cfg)
		if err != nil {
			return "", err
		}
		activeSrv = srv
		activeCleanup = cleanup
		return srv.Addr(), nil
	}

	// In attach mode the tray is a subprocess that doesn't own the server or
	// satellite. All satellite state comes from the health-poll endpoint, so
	// the in-process callbacks must not be wired (they would always return
	// false / "server not running" and override the correct health-poll state).
	var onSatConnect func() error
	var onSatDisconnect func()
	var onSatStatus func() bool
	if attachAddr == "" {
		onSatConnect = func() error {
			if activeSrv == nil {
				return fmt.Errorf("server not running")
			}
			sat := activeSrv.Satellite()
			if sat == nil {
				return fmt.Errorf("satellite not initialised")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return sat.Connect(ctx)
		}
		onSatDisconnect = func() {
			if activeSrv == nil {
				return
			}
			if sat := activeSrv.Satellite(); sat != nil {
				sat.Disconnect()
			}
		}
		onSatStatus = func() bool {
			if activeSrv == nil {
				return false
			}
			sat := activeSrv.Satellite()
			if sat == nil {
				return false
			}
			return sat.Status().Connected
		}
	}

	traypkg.Run(traypkg.Config{
		Port:                  cfg.WebUI.Port,
		HuginnBinPath:         binPath,
		AttachAddr:            attachAddr,
		OnStart:               onStart,
		OnStop:                stopActive,
		OnSatelliteConnect:    onSatConnect,
		OnSatelliteDisconnect: onSatDisconnect,
		OnSatelliteStatus:     onSatStatus,
	})
}

// cmdRelay handles the relay subcommand.
func cmdRelay(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "register":
		return cmdRelayRegister()
	case "start":
		return cmdRelayStart()
	case "status":
		return cmdRelayStatus()
	case "unregister":
		return cmdRelayUnregister()
	case "install":
		return cmdRelayInstall()
	case "uninstall":
		return cmdRelayUninstall()
	case "sentinel":
		return cmdRelaySentinel()
	case "install-sentinel":
		return cmdRelayInstallSentinel()
	case "uninstall-sentinel":
		return cmdRelayUninstallSentinel()
	default:
		fmt.Println("huginn relay commands:")
		fmt.Println("  huginn relay register           — register this machine as a relay agent")
		fmt.Println("  huginn relay start              — start the satellite relay (long-lived process)")
		fmt.Println("  huginn relay status             — check satellite registration and connection status")
		fmt.Println("  huginn relay unregister         — unregister from HuginnCloud")
		fmt.Println("  huginn relay install            — install as auto-start background service (macOS LaunchAgent / Linux systemd --user)")
		fmt.Println("  huginn relay uninstall          — remove background service")
		fmt.Println("  huginn relay sentinel           — run the boot-time sentinel (presence only, no code execution)")
		fmt.Println("  huginn relay install-sentinel   — install sentinel as auto-start LaunchDaemon/systemd")
		fmt.Println("  huginn relay uninstall-sentinel — remove sentinel daemon")
		return nil
	}
}

// cmdConnect handles the `huginn connect` subcommand.
// It opens the browser to HuginnCloud, waits for an API-key callback, and falls
// back to the Device Code flow if the browser cannot be opened.
func cmdConnect() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Honour SIGINT / SIGTERM so the user can Ctrl-C gracefully.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	baseURL := os.Getenv("HUGINN_CLOUD_URL")
	reg := relay.NewRegistrar(baseURL)

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "my-machine"
	}

	_, err := reg.Register(ctx, hostname)
	return err
}

// cmdCloud handles the `huginn cloud` subcommand for HuginnCloud registration.
func cmdCloud(cfg *config.Config, args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: huginn cloud <register|unregister|status>")
		os.Exit(1)
	}
	reg := relay.NewRegistrar(os.Getenv("HUGINN_CLOUD_URL"))
	switch args[0] {
	case "register":
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		hostname, _ := os.Hostname()
		_, err := reg.Register(ctx, hostname)
		return err
	case "unregister":
		if err := reg.Unregister(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Machine unregistered from HuginnCloud.")
	case "status":
		registered, id := reg.Status()
		if registered {
			fmt.Printf("Registered — Machine ID: %s\n", id)
		} else {
			fmt.Printf("Not registered — Run 'huginn cloud register'\nMachine ID: %s\n", id)
		}
	default:
		return fmt.Errorf("unknown cloud subcommand: %s\nUsage: huginn cloud <register|unregister|status>", args[0])
	}
	return nil
}

// cmdRelayRegister handles relay registration with optional fleet token support.
// Usage:
//   huginn relay register                           # Interactive browser flow
//   huginn relay register --fleet-token <token>     # Pre-provisioned MDM token
//   huginn relay register --fleet-token-file <path> # Read token from file
func cmdRelayRegister() error {
	fs := flag.NewFlagSet("relay register", flag.ContinueOnError)
	fleetToken := fs.String("fleet-token", "", "pre-provisioned machine JWT for MDM/fleet deployment")
	fleetTokenFile := fs.String("fleet-token-file", "", "path to file containing pre-provisioned machine JWT")
	if err := fs.Parse(os.Args[3:]); err != nil {
		return err
	}

	// Resolve fleet token from flag or file.
	token := *fleetToken
	if *fleetTokenFile != "" {
		data, err := os.ReadFile(*fleetTokenFile)
		if err != nil {
			return fmt.Errorf("relay register: read fleet token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}

	// Fleet token path: register token directly without interactive flow.
	if token != "" {
		cloudURL := os.Getenv("HUGINN_CLOUD_URL")
		reg := relay.NewRegistrar(cloudURL)
		_, err := reg.RegisterWithToken(token, "")
		return err
	}

	// Default path: browser-based or device-code registration flow.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		<-sig
		cancel()
	}()

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "my-machine"
	}

	cloudURL := os.Getenv("HUGINN_CLOUD_URL")
	reg := relay.NewRegistrar(cloudURL)
	_, err := reg.Register(ctx, hostname)
	return err
}

// cmdRelayStart launches the satellite relay as a long-lived process.
func cmdRelayStart() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("relay start: get home dir: %w", err)
	}

	cfg := relay.RunnerConfig{
		MachineID:         relay.GetMachineID(),
		HeartbeatInterval: 60 * time.Second,
		CloudURL:          os.Getenv("HUGINN_CLOUD_URL"),
		StorePath:         filepath.Join(home, ".huginn", "relay"),
	}
	relay.RunWithSignals(cfg)
	return nil
}

// cmdRelayStatus reports the current registration and connection status.
func cmdRelayStatus() error {
	sat := relay.NewSatellite(os.Getenv("HUGINN_CLOUD_URL"))
	status := sat.Status()

	fmt.Printf("Registration: %s\n", map[bool]string{true: "yes", false: "no"}[status.Registered])
	fmt.Printf("Machine ID:   %s\n", status.MachineID)
	fmt.Printf("Connected:    %s\n", map[bool]string{true: "yes", false: "no"}[status.Connected])
	fmt.Printf("Cloud URL:    %s\n", status.CloudURL)

	return nil
}

// cmdRelayUnregister removes the satellite registration and token.
func cmdRelayUnregister() error {
	if err := relay.ClearToken(); err != nil {
		return fmt.Errorf("failed to unregister: %w", err)
	}
	fmt.Println("Unregistered from HuginnCloud.")
	return nil
}

// cmdRelayInstall installs the relay as an auto-start background service.
func cmdRelayInstall() error {
	mgr, err := relay.NewServiceManager()
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("install: cannot determine binary path: %w", err)
	}
	if err := mgr.Install(binaryPath); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	fmt.Println("Huginn relay installed as background service.")
	fmt.Println("The satellite will start automatically at login and restart on failure.")
	return nil
}

// cmdRelayUninstall removes the relay background service.
func cmdRelayUninstall() error {
	mgr, err := relay.NewServiceManager()
	if err != nil {
		return fmt.Errorf("uninstall: %w", err)
	}
	if err := mgr.Uninstall(); err != nil {
		return fmt.Errorf("uninstall: %w", err)
	}
	fmt.Println("Huginn relay background service removed.")
	return nil
}

// cmdRelaySentinel runs the boot-time sentinel daemon.
func cmdRelaySentinel() error {
	tokenPath := relay.SentinelTokenPath()
	store := relay.NewSentinelFileTokenStore(tokenPath)
	if !store.IsRegistered() {
		return fmt.Errorf("sentinel token not found at %s — run `huginn relay install-sentinel` first", tokenPath)
	}
	machineID := relay.GetMachineID()
	cloudURL := os.Getenv("HUGINN_CLOUD_URL")
	if cloudURL == "" {
		cloudURL = "wss://relay.huginncloud.com"
	}
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sig; cancel() }()

	sentinel := relay.NewSentinel(relay.SentinelConfig{
		MachineID:   machineID,
		TokenStorer: store,
		CloudURL:    cloudURL,
	})
	sentinel.Run(ctx)
	return nil
}

// cmdRelayInstallSentinel installs the sentinel as a boot-time daemon.
func cmdRelayInstallSentinel() error {
	keyringStore := relay.NewTokenStore()
	token, err := keyringStore.Load()
	if err != nil {
		return fmt.Errorf("install-sentinel: not registered — run `huginn relay register` first")
	}
	fileStore := relay.NewSentinelFileTokenStore(relay.SentinelTokenPath())
	if err := fileStore.Save(token); err != nil {
		return fmt.Errorf("install-sentinel: save token: %w", err)
	}
	binaryPath, _ := os.Executable()
	mgr, err := relay.NewSentinelServiceManager()
	if err != nil {
		return fmt.Errorf("install-sentinel: %w", err)
	}
	if err := mgr.Install(binaryPath); err != nil {
		return fmt.Errorf("install-sentinel: %w", err)
	}
	fmt.Println("Huginn sentinel daemon installed.")
	fmt.Println("The sentinel will start at boot, even at the lock screen.")
	fmt.Println("It signals HuginnCloud that this machine is online (presence only, no code execution).")
	return nil
}

// cmdRelayUninstallSentinel removes the sentinel boot daemon.
func cmdRelayUninstallSentinel() error {
	mgr, err := relay.NewSentinelServiceManager()
	if err != nil {
		return fmt.Errorf("uninstall-sentinel: %w", err)
	}
	if err := mgr.Uninstall(); err != nil {
		return fmt.Errorf("uninstall-sentinel: %w", err)
	}
	relay.NewSentinelFileTokenStore(relay.SentinelTokenPath()).Clear() //nolint:errcheck
	fmt.Println("Huginn sentinel daemon removed.")
	return nil
}

// handleExportCommand implements `huginn export` with full flag support.
//
// Flags:
//
//	--session <id>       session ID to export (default: most recent)
//	--format  md|json    output format (default: md)
//	--output  <path>     write to file instead of stdout
//	--all                export every session to --output dir (default: ~/.huginn/export)
func handleExportCommand(args []string) {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	var (
		sessionID string
		format    string
		output    string
		all       bool
	)
	fs.StringVar(&sessionID, "session", "", "session ID to export")
	fs.StringVar(&format, "format", "md", "output format: md or json")
	fs.StringVar(&output, "output", "", "output file path (default: stdout)")
	fs.BoolVar(&all, "all", false, "export all sessions")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "export: %v\n", err)
		os.Exit(1)
	}

	// Support positional session ID for backward compat: huginn export <id>
	if sessionID == "" && fs.NArg() > 0 {
		sessionID = fs.Arg(0)
	}

	huginnHome, err := huginnDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "export: huginn dir: %v\n", err)
		os.Exit(1)
	}

	store := session.NewStore(filepath.Join(huginnHome, "sessions"))

	if all {
		if err := exportAllSessions(store, huginnHome, format, output); err != nil {
			fmt.Fprintf(os.Stderr, "export: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if sessionID == "" {
		manifests, err := store.List()
		if err != nil || len(manifests) == 0 {
			fmt.Fprintln(os.Stderr, "export: no sessions found; use --session <id>")
			os.Exit(1)
		}
		sessionID = manifests[0].SessionID
	}

	if err := exportOneSession(store, sessionID, format, output); err != nil {
		fmt.Fprintf(os.Stderr, "export: %v\n", err)
		os.Exit(1)
	}
}

// exportOneSession loads a single session and renders it to outputPath (or stdout).
func exportOneSession(store session.StoreInterface, sessionID, format, outputPath string) error {
	msgs, err := store.TailMessages(sessionID, 1<<30)
	if err != nil {
		return fmt.Errorf("read session %s: %w", sessionID, err)
	}

	title := sessionID
	sess, loadErr := store.Load(sessionID)
	if loadErr == nil && sess != nil {
		title = sess.Manifest.Title
	}

	var content string
	switch format {
	case "json":
		out, err := session.ExportJSON(msgs)
		if err != nil {
			return err
		}
		content = out
	default: // "md"
		content = session.ExportMarkdown(msgs, title)
	}

	return writeExportOutput(content, outputPath)
}

// exportAllSessions iterates the store and writes one file per session to outputDir.
func exportAllSessions(store session.StoreInterface, huginnHome, format, outputDir string) error {
	manifests, err := store.List()
	if err != nil {
		return err
	}
	if len(manifests) == 0 {
		fmt.Println("No sessions to export.")
		return nil
	}

	if outputDir == "" {
		outputDir = filepath.Join(huginnHome, "export")
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("create export dir: %w", err)
	}

	ext := ".md"
	if format == "json" {
		ext = ".json"
	}

	var exported int
	for _, m := range manifests {
		filename := filepath.Join(outputDir, m.SessionID+ext)
		if err := exportOneSession(store, m.SessionID, format, filename); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skip %s: %v\n", m.SessionID, err)
			continue
		}
		exported++
	}
	fmt.Printf("Exported %d sessions to %s\n", exported, outputDir)
	return nil
}

// writeExportOutput writes content to path, or stdout if path is empty.
func writeExportOutput(content, path string) error {
	if path == "" {
		fmt.Print(content)
		return nil
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// makeLocalHTTPProxy returns an HTTPProxy callback that forwards relay http_request
// messages to the satellite's own local HTTP server using the satellite auth token.
func makeLocalHTTPProxy(addr, token string) func(method, path string, body []byte) (int, []byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	return func(method, path string, body []byte) (int, []byte, error) {
		url := "http://" + addr + path
		var bodyReader *bytes.Reader
		if len(body) > 0 {
			bodyReader = bytes.NewReader(body)
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		return resp.StatusCode, respBody, err
	}
}

// cmdReportBug implements `huginn report-bug`: shows the most recent crash
// report and links to the GitHub issue tracker.
func cmdReportBug(_ []string) {
	huginnHome, err := huginnDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "report-bug: %v\n", err)
		os.Exit(1)
	}
	crashDir := filepath.Join(huginnHome, "crash")

	entries, readErr := os.ReadDir(crashDir)
	if readErr != nil || len(entries) == 0 {
		fmt.Println("No crash reports found.")
		fmt.Println("To file a bug report, visit: https://github.com/scrypster/huginn/issues/new")
		return
	}

	latest := entries[len(entries)-1]
	data, err := os.ReadFile(filepath.Join(crashDir, latest.Name()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "report-bug: read crash file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Most recent crash report (%s):\n\n", latest.Name())
	fmt.Printf("%s\n", data)
	fmt.Println("To file a bug report, visit: https://github.com/scrypster/huginn/issues/new")
	fmt.Println("Please paste the crash report above into the issue.")
}

// providerDisplayName returns a human-readable name for a provider ID.
func providerDisplayName(id string) string {
	switch id {
	case "ollama":
		return "Ollama (local)"
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "openrouter":
		return "OpenRouter"
	default:
		return id
	}
}

// fetchOllamaModels calls Ollama /api/tags and returns model info.
// Returns nil on any error (Ollama may not be running).
func fetchOllamaModels(baseURL string) []relay.ModelInfo {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	rawModels, _ := result["models"].([]any)
	var models []relay.ModelInfo
	for _, raw := range rawModels {
		entry, _ := raw.(map[string]any)
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		sizeBytes, _ := entry["size"].(float64)
		sizeStr := ""
		if sizeBytes > 0 {
			sizeStr = fmt.Sprintf("%.1f GB", sizeBytes/1e9)
		}
		quant := ""
		if details, ok := entry["details"].(map[string]any); ok {
			quant, _ = details["quantization_level"].(string)
		}
		models = append(models, relay.ModelInfo{
			Name:         name,
			Size:         sizeStr,
			Quantization: quant,
		})
	}
	return models
}

// autoCreateDMSpaces ensures a DM space exists for each configured agent.
// This is called once at startup and is idempotent — OpenDM is a no-op if
// the DM already exists.
//
// Orphaned DM spaces: if an agent is removed from config.json after a DM space
// has been created, the DM space persists in SQLite with no corresponding agent.
// The frontend will show the DM in the sidebar; clicking it will use the
// now-missing agent name (which the huginn API will reject with a 404/400).
// This is intentional — DM spaces are kept for conversation history rather than
// silently deleted. A future cleanup command or archive-on-agent-delete hook can
// address this if needed.
func autoCreateDMSpaces(store spaces.StoreInterface) {
	agentsCfg, err := agentslib.LoadAgents()
	if err != nil || agentsCfg == nil {
		return
	}
	for _, ag := range agentsCfg.Agents {
		if ag.Name == "" {
			continue
		}
		if _, err := store.OpenDM(ag.Name); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: auto-create DM space for %q: %v\n", ag.Name, err)
		}
	}
}
