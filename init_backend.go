package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	case "anthropic", "openai", "openrouter", "google", "deepseek", "zai", "custom":
		b, err := backend.NewFromConfig(cloudProvider, cfg.Backend.Endpoint, cfg.Backend.ResolvedAPIKey(), cfg.DefaultModel)
		if err != nil {
			return res, fmt.Errorf("backend (%s): %w", cloudProvider, err)
		}
		res.Backend = b
		slog.Info("backend: using cloud provider", "provider", cloudProvider)
	case "vertex":
		b, err := initVertexBackend(ctx, &cfg)
		if err != nil {
			return res, fmt.Errorf("backend (vertex): %w", err)
		}
		res.Backend = b
		slog.Info("backend: using cloud provider", "provider", "vertex", "project", cfg.Backend.Project, "location", cfg.Backend.Location)
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
			b.SetKeepAlive(cfg.Backend.KeepAlive)
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
	startModelCapabilityProbe(cfg.OllamaBaseURL, res.Registry)

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
	backendCache.SetOllamaKeepAlive(cfg.Backend.KeepAlive)
	// Pre-register a VertexBackend whenever credentials are configured so that
	// agents with provider="vertex" reuse it instead of going through the
	// env-only factory case — see main.go startServer for the same wiring.
	if cfg.Backend.CredentialsPath != "" || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		vCfg := cfg
		if vb, vbErr := initVertexBackend(ctx, &vCfg); vbErr == nil {
			backendCache.SetProviderBackend("vertex", vb)
		} else {
			slog.Warn("vertex pre-resolution failed", "err", vbErr)
		}
	}
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

// initVertexBackend resolves vertex configuration with config-file precedence
// and falls back to GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_LOCATION environment
// variables for blank project / location fields. The credentials reference
// is passed through unchanged — backend.ResolveVertexCredentials handles
// "$ENV_VAR" / "keyring:..." / literal-path / empty-with-env-fallback.
func initVertexBackend(ctx context.Context, cfg *config.Config) (backend.Backend, error) {
	project := cfg.Backend.Project
	if project == "" {
		project = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	location := cfg.Backend.Location
	if location == "" {
		location = os.Getenv("GOOGLE_CLOUD_LOCATION")
	}
	return backend.NewVertexBackend(ctx, backend.VertexConfig{
		Project:         project,
		Location:        location,
		CredentialsPath: cfg.Backend.CredentialsPath,
		Model:           cfg.DefaultModel,
	})
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
	case "anthropic", "openai", "openrouter", "google", "deepseek", "zai", "custom":
		var err error
		b, err = backend.NewFromConfig(cfg.Backend.Provider, cfg.Backend.Endpoint, cfg.Backend.ResolvedAPIKey(), cfg.DefaultModel)
		if err != nil {
			return nil, nil, fmt.Errorf("backend (%s): %w", cfg.Backend.Provider, err)
		}
	case "vertex":
		var err error
		b, err = initVertexBackend(ctx, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("backend (vertex): %w", err)
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
		if eb, ok := b.(*backend.ExternalBackend); ok {
			eb.SetKeepAlive(cfg.Backend.KeepAlive)
		}
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

// startModelCapabilityProbe fills ModelRegistry.Available from Ollama /api/tags
// + /api/show so ModelSupportsTools is not blindly true for installed models.
func startModelCapabilityProbe(baseURL string, reg *modelconfig.ModelRegistry) {
	if reg == nil {
		return
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	go func(ep string) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(ep, "/")+"/api/tags", nil)
		if err != nil {
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var result struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return
		}
		names := make([]string, 0, len(result.Models))
		for _, m := range result.Models {
			if m.Name != "" {
				names = append(names, m.Name)
			}
		}
		infos := backend.ProbeOllamaModels(ctx, ep, names)
		if len(infos) == 0 {
			return
		}
		reg.ReplaceAvailable(infos)
		slog.Info("backend: probed model capabilities", "count", len(infos))
	}(baseURL)
}

// warmOllamaModelSet resolves the small, explicit set of models to warm up
// at serve start (perf wave step 2b): the default agent's model and the
// Chief of Staff's model (agentslib.AgentIsCoS), deduplicated. Deliberately
// bounded to at most 2 models — this is a "don't pay a cold load on the
// first turn" optimization, not "keep every installed model resident",
// which would thrash a small machine's VRAM. Agents whose provider is not
// ollama-family, or whose endpoint points elsewhere, are skipped: this is
// ollama-only warm-up, never a probe of a remote/cloud provider.
//
// globalProvider is cfg.Backend.Provider (D8): when the globally-configured
// provider isn't ollama-family ("" or "ollama"), nothing is warmed even if
// individual per-agent Provider/Endpoint fields happen to look ollama-shaped
// — a global cloud/custom provider means huginn isn't managing a local
// ollama instance to warm in the first place.
func warmOllamaModelSet(agentReg *agentslib.AgentRegistry, localEndpoint, globalProvider string) []string {
	if agentReg == nil {
		return nil
	}
	switch globalProvider {
	case "", "ollama":
		// ollama-family global provider; proceed.
	default:
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(ag *agentslib.Agent) {
		if ag == nil {
			return
		}
		switch ag.Provider {
		case "", "ollama", "external":
			// ollama-family; fall through.
		default:
			return
		}
		if ag.Endpoint != "" && ag.Endpoint != localEndpoint {
			return
		}
		id := ag.GetModelID()
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(agentReg.DefaultAgent())
	for _, ag := range agentReg.All() {
		if agentslib.AgentIsCoS(ag) {
			add(ag)
			break
		}
	}
	return out
}

// warmOllamaModels fires one best-effort, non-blocking keep-alive request
// per model in warmOllamaModelSet so the first real user turn doesn't pay a
// cold model load. Every request runs in its own goroutine with a bounded
// timeout; failures are logged, never fatal — this is pure latency-hiding,
// not a readiness gate.
func warmOllamaModels(ctx context.Context, cfg config.Config, agentReg *agentslib.AgentRegistry) {
	endpoint := cfg.Backend.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	keepAlive := cfg.Backend.KeepAlive
	if keepAlive == "" {
		keepAlive = backend.DefaultOllamaKeepAlive
	}
	for _, model := range warmOllamaModelSet(agentReg, endpoint, cfg.Backend.Provider) {
		go func(model string) {
			warmCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			if err := warmOneOllamaModel(warmCtx, endpoint, model, keepAlive); err != nil {
				slog.Warn("backend: ollama warm-up failed", "model", model, "endpoint", endpoint, "err", err)
				return
			}
			slog.Info("backend: ollama warm-up requested", "model", model, "keep_alive", keepAlive)
		}(model)
	}
}

// warmOneOllamaModel POSTs to ollama's native /api/generate with an empty
// prompt — per ollama's documented behavior this loads (or refreshes the
// keep_alive TTL of) the model without running inference, the smallest
// request that accomplishes a warm-up.
func warmOneOllamaModel(ctx context.Context, endpoint, model, keepAlive string) error {
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"prompt":     "",
		"keep_alive": keepAlive,
	})
	if err != nil {
		return err
	}
	url := strings.TrimRight(endpoint, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck // best-effort drain
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
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
