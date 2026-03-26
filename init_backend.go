package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/compact"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/modelconfig"
	modelslib "github.com/scrypster/huginn/internal/models"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/pricing"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/runtime"
	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/search/hnsw"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/stats"
)


// backendResult holds the initialised backend, orchestrator, and related objects.
type backendResult struct {
	Backend        backend.Backend
	Registry       *modelconfig.ModelRegistry
	Orch           *agent.Orchestrator
	AgentReg       *agentslib.AgentRegistry
	AgentsCfg      *agentslib.AgentsConfig
	StatsReg       *stats.Registry
	StatsCollector stats.Collector
	SkillReg       *skills.SkillRegistry
	LoadedSkills   []skills.Skill
	PriceTracker   *pricing.SessionTracker
}

// initBackend selects the backend (managed/external/anthropic/openrouter),
// builds model config and the orchestrator, loads agents and skills.
func initBackend(
	ctx context.Context,
	cfg config.Config,
	huginnHome string,
	cwd string,
	endpointOverride string,
	modelOverride string,
	detection repo.DetectionResult,
	idx *repo.Index,
	storage storageResult,
) (backendResult, error) {
	var res backendResult

	// --- Stats ---
	res.StatsReg = stats.NewRegistry()
	res.StatsCollector = res.StatsReg.Collector()

	// --- Backend selection ---
	// Cloud providers (anthropic, openai, openrouter) take priority over the
	// type field — when a provider + api_key is configured, use it directly.
	cloudProvider := cfg.Backend.Provider
	switch cloudProvider {
	case "anthropic", "openai", "openrouter":
		b, err := backend.NewFromConfig(cloudProvider, cfg.Backend.Endpoint, cfg.Backend.ResolvedAPIKey(), cfg.DefaultModel)
		if err != nil {
			return res, fmt.Errorf("backend (%s): %w", cloudProvider, err)
		}
		res.Backend = b
		slog.Info("backend: using cloud provider", "provider", cloudProvider)
	default:
		switch cfg.Backend.Type {
		case "managed":
			b, err := initManagedBackend(ctx, huginnHome, cfg)
			if err != nil {
				return res, fmt.Errorf("managed backend: %w", err)
			}
			res.Backend = b
		default:
			endpoint := cfg.Backend.Endpoint
			if endpointOverride != "" {
				endpoint = endpointOverride
			}
			if endpoint == "" {
				endpoint = "http://localhost:11434"
			}
			b := backend.NewExternalBackend(endpoint)
			go func(ep string, be backend.Backend) {
				probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				if err := be.Health(probeCtx); err != nil {
					slog.Warn("backend: not reachable at startup", "endpoint", ep, "err", err)
				}
			}(endpoint, b)
			res.Backend = b
		}
	}
	slog.Info("backend: initialized", "type", cfg.Backend.Type)

	// --- Model config ---
	defaultModel := cfg.DefaultModel
	if modelOverride != "" {
		defaultModel = modelOverride
	}
	if defaultModel == "" {
		defaultModel = "qwen2.5-coder:14b"
	}
	models := &modelconfig.Models{
		Reasoner: cfg.ReasonerModel,
	}
	if models.Reasoner == "" {
		models.Reasoner = defaultModel
	}
	res.Registry = modelconfig.NewRegistry(models)

	// --- Compactor ---
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

	// --- Orchestrator ---
	orch, err := agent.NewOrchestrator(res.Backend, models, idx, res.Registry, res.StatsCollector, compactor)
	if err != nil {
		return res, fmt.Errorf("orchestrator: %w", err)
	}
	backendCache := backend.NewBackendCache(res.Backend)
	backendCache.WithFallbackAPIKey(cfg.Backend.APIKey)
	orch.SetBackendCache(backendCache)
	orch.WithMachineID(relay.GetMachineID())
	orch.SetGitRoot(detection.Root)
	orch.SetHuginnHome(huginnHome)
	if storage.MemStore != nil {
		orch.SetMemoryStore(storage.MemStore)
	}
	res.Orch = orch
	slog.Info("backend: orchestrator created")

	// --- Agent registry ---
	agentsCfg, agentsErr := agentslib.LoadAgents()
	if agentsErr != nil {
		agentsCfg = agentslib.DefaultAgentsConfig()
	}
	tuiUsername := memory.ResolveUsername(cwd)
	agentReg := agentslib.BuildRegistryWithUsername(agentsCfg, models, tuiUsername)
	// Warn on literal API keys.
	for _, def := range agentsCfg.Agents {
		if backend.IsLiteralAPIKey(def.APIKey) {
			fmt.Fprintf(os.Stderr, "warning: agent %q has a literal API key; consider using $ENV_VAR or keyring:<service>:<user> instead\n", def.Name)
		}
	}
	res.AgentReg = agentReg
	res.AgentsCfg = agentsCfg
	orch.SetAgentRegistry(agentReg)

	// --- Skills ---
	skillLoader := skills.DefaultLoader()
	loadedSkills, loadErrs := skillLoader.LoadAll()
	for _, e := range loadErrs {
		fmt.Fprintf(os.Stderr, "huginn: warning: skills load: %v\n", e)
	}
	skillReg := skills.NewSkillRegistry()
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
		orch.SetSkillsFragment(joinStrings(skillsFragmentParts, "\n\n"))
	}
	res.SkillReg = skillReg
	res.LoadedSkills = loadedSkills

	// --- Notepads ---
	if cfg.NotepadsEnabled {
		if npMgr, err := notepad.DefaultManager(detection.Root); err == nil {
			if loaded, err := npMgr.Load(); err == nil && len(loaded) > 0 {
				orch.SetNotepads(loaded)
			}
		}
	}

	// --- Semantic search ---
	if cfg.SemanticSearch {
		initSemanticSearch(ctx, cfg, idx, orch)
	}

	// --- Price tracker ---
	res.PriceTracker = pricing.NewSessionTracker(pricing.DefaultTable)

	return res, nil
}

// initManagedBackend starts the embedded llama-server runtime.
// Falls back to an external backend if the runtime or models aren't ready.
func initManagedBackend(ctx context.Context, huginnHome string, cfg config.Config) (backend.Backend, error) {
	mgr, err := runtime.NewManager(huginnHome)
	if err != nil {
		return nil, fmt.Errorf("runtime: %w", err)
	}
	managedStore, err := modelslib.NewStore(huginnHome)
	if err != nil {
		return nil, fmt.Errorf("model store: %w", err)
	}

	if needsOnboarding(mgr, managedStore) {
		fmt.Fprintln(os.Stderr, "huginn: local model not set up — run 'huginn init' to configure. Starting without local model.")
		return backend.NewExternalBackend(cfg.Backend.Endpoint), nil
	}

	port, portErr := runtime.FindFreePort()
	if portErr != nil {
		return nil, fmt.Errorf("find free port: %w", portErr)
	}

	installed, _ := managedStore.Installed()
	var modelPath string
	for _, entry := range installed {
		modelPath = entry.Path
		break
	}
	if modelPath == "" {
		fmt.Fprintln(os.Stderr, "huginn: no models installed — run 'huginn init'. Starting without local model.")
		return backend.NewExternalBackend(cfg.Backend.Endpoint), nil
	}

	pidPath := filepath.Join(huginnHome, "llama.pid")
	runtime.CleanupZombie(pidPath)
	if err := mgr.Start(modelPath, port); err != nil {
		return nil, fmt.Errorf("start runtime: %w", err)
	}
	_ = runtime.WritePIDFile(pidPath, mgr.Cmd().Process.Pid, port)
	// Note: caller must defer os.Remove(pidPath) and b.Shutdown(ctx)

	if err := mgr.WaitForReady(ctx); err != nil {
		return nil, fmt.Errorf("runtime ready: %w", err)
	}
	b := backend.NewManagedBackend(mgr.Endpoint(), func(ctx context.Context) error {
		return mgr.Shutdown()
	})
	slog.Info("backend: managed runtime started", "endpoint", mgr.Endpoint())
	return b, nil
}

// initSemanticSearch wires BM25 + HNSW hybrid search if Ollama is reachable.
func initSemanticSearch(ctx context.Context, cfg config.Config, idx *repo.Index, orch *agent.Orchestrator) {
	embedder := search.NewOllamaEmbedder(cfg.OllamaBaseURL, cfg.EmbeddingModel)
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := embedder.Probe(probeCtx); err != nil {
		fmt.Fprintf(os.Stderr, "huginn: semantic search disabled (Ollama not reachable: %v)\n", err)
		return
	}
	bm25 := search.NewBM25Searcher()
	hnswIdx := hnsw.New(16, 200)

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
		indexCtx, indexCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := bm25.Index(indexCtx, searchChunks); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: BM25 indexing failed: %v\n", err)
			indexCancel()
			return
		}
		hybrid := search.NewHybridSearcher(bm25, hnswIdx, embedder)
		if err := hybrid.Index(indexCtx, searchChunks); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: hybrid search indexing failed: %v\n", err)
		} else {
			orch.SetSearcher(hybrid)
		}
		indexCancel()
	}
}

// selectBackend picks the appropriate backend and resolves model configuration
// for non-interactive modes (--print, headless agent). It performs a blocking
// health probe with a 3-second timeout for local/external backends so failures
// are reported immediately rather than hanging on the first API call.
//
// Cloud providers (anthropic, openai, openrouter) bypass the health probe since
// their availability is validated implicitly on the first API call.
func selectBackend(ctx context.Context, cfg *config.Config, endpointOverride, modelOverride string) (backend.Backend, *modelconfig.Models, error) {
	var b backend.Backend

	switch cfg.Backend.Provider {
	case "anthropic", "openai", "openrouter":
		var err error
		b, err = backend.NewFromConfig(cfg.Backend.Provider, cfg.Backend.Endpoint, cfg.Backend.ResolvedAPIKey(), cfg.DefaultModel)
		if err != nil {
			return nil, nil, fmt.Errorf("backend (%s): %w", cfg.Backend.Provider, err)
		}
	default:
		// "managed" requires a running process — not suitable for non-interactive modes.
		// Fall through to ExternalBackend with the configured endpoint.
		endpoint := cfg.Backend.Endpoint
		if endpointOverride != "" {
			endpoint = endpointOverride
		}
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		b = backend.NewExternalBackend(endpoint)
		// Blocking probe: fail fast so the user gets a clear error instead of a hang.
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := b.Health(probeCtx); err != nil {
			return nil, nil, fmt.Errorf("backend not reachable at %s: %w", endpoint, err)
		}
	}

	// Mirror the model resolution logic from initBackend.
	defaultModel := cfg.DefaultModel
	if modelOverride != "" {
		defaultModel = modelOverride
	}
	if defaultModel == "" {
		defaultModel = "qwen2.5-coder:14b"
	}
	models := &modelconfig.Models{
		Reasoner: cfg.ReasonerModel,
	}
	if models.Reasoner == "" {
		models.Reasoner = defaultModel
	}

	return b, models, nil
}

// joinStrings joins a slice of strings with a separator.
// Avoids importing "strings" only for this use.
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
