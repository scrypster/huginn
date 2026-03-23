package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/server"
	"github.com/scrypster/huginn/internal/storage"
	"sync"
)

// initRelayHub creates the HuginnCloud satellite relay hub and wires it into the orchestrator.
// Returns the hub (a relay.Hub) for further use by initRelayDispatcher.
// ctx is the app context — cancels on SIGINT/SIGTERM.
func initRelayHub(ctx context.Context, orch *agent.Orchestrator) relay.Hub {
	satellite := relay.NewSatellite(os.Getenv("HUGINN_CLOUD_URL"))
	hub := satellite.Hub(ctx) // app context — cancels on SIGINT/SIGTERM
	orch.SetRelayHub(hub)
	slog.Info("relay: hub initialized")
	return hub
}

// relayDispatcherConfig holds everything needed to wire the relay dispatcher.
type relayDispatcherConfig struct {
	Cfg         config.Config
	HuginnHome  string
	Orch        *agent.Orchestrator
	AgentReg    *agentslib.AgentRegistry
	Gate        *permissions.Gate
	Hub         relay.Hub
	Store       *storage.Store
	ServerAddr  string
	ServerToken string
	CancelAll   func()
	// Srv is optional: when set, initRelayDispatcher will call Srv.SetOutbox so
	// /api/v1/health can expose outbox_depth and outbox_dropped counters.
	Srv interface{ SetOutbox(*relay.Outbox) }
}

// initRelayDispatcher wires the relay WebSocket dispatcher, outbox, and active sessions
// onto the hub. Should only be called when toolsEnabled is true.
// ctx is the app context — cancels on SIGINT/SIGTERM.
// Returns a cleanup function to cancel all active sessions on shutdown.
func initRelayDispatcher(ctx context.Context, dcfg relayDispatcherConfig) func() {
	wsHub, ok := dcfg.Hub.(*relay.WebSocketHub)
	if !ok {
		return func() {}
	}

	activeSessions := relay.NewActiveSessions()

	var sessionStore *relay.SessionStore
	if dcfg.Store != nil {
		sessionStore = relay.NewSessionStore(dcfg.Store)
	}

	// --- Outbox for durable delivery ---
	if dcfg.Store != nil {
		tuiOutbox := relay.NewOutbox(dcfg.Store, dcfg.Hub)
		// Expose the outbox to the HTTP server so /health can report its depth.
		if dcfg.Srv != nil {
			dcfg.Srv.SetOutbox(tuiOutbox)
		}
		outboxCtx, outboxCancel := context.WithCancel(ctx) // app context — cancels on SIGINT/SIGTERM
		_ = outboxCancel // ctx cancellation drives shutdown; outboxCancel kept for vet
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-outboxCtx.Done():
					return
				case <-ticker.C:
					if err := tuiOutbox.Flush(outboxCtx); err != nil && !errors.Is(err, context.Canceled) {
						slog.Warn("relay: outbox flush", "err", err)
					}
				}
			}
		}()
	}

	// Mutex protecting concurrent reads/writes to cfg.Backend fields from relay callbacks.
	var backendMu sync.RWMutex
	cfg := dcfg.Cfg

	chatFn := func(ctx context.Context, sessionID, userMsg string,
		onToken func(string),
		onToolEvent func(eventType string, payload map[string]any),
		onEvent func(backend.StreamEvent)) error {
		return dcfg.Orch.ChatForSessionWithAgent(ctx, sessionID, userMsg, onToken, onToolEvent, onEvent)
	}
	newSessionFn := func(id string) string {
		sess, err := dcfg.Orch.NewSession(id)
		if err != nil {
			logger.Error("failed to create session", "err", err)
			return ""
		}
		return sess.ID
	}
	slog.Info("dispatcher: routing machine_id", "machine_id", relay.GetMachineID())

	runAgentFn := func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error {
		reg := dcfg.Orch.GetAgentRegistry()
		if reg == nil {
			return fmt.Errorf("agent registry not available")
		}
		ag, ok := reg.ByName(agentName)
		if !ok {
			return fmt.Errorf("agent %q not found", agentName)
		}
		return dcfg.Orch.ChatWithAgent(ctx, ag, prompt, sessionID, onToken, nil, nil)
	}

	wsHub.SetOnMessage(relay.NewDispatcher(relay.DispatcherConfig{
		MachineID:   relay.GetMachineID(),
		DeliverPerm: dcfg.Gate.DeliverRelayResponse,
		Hub:         dcfg.Hub,
		Store:       sessionStore,
		Shell:       relay.NewShellManager(),
		ChatSession: chatFn,
		NewSession:  newSessionFn,
		RunAgent:    runAgentFn,
		ListModels:  dcfg.Orch.ModelNames,
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
			client := &http.Client{Timeout: 10 * time.Minute}
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
		HTTPProxy: makeLocalHTTPProxy(dcfg.ServerAddr, dcfg.ServerToken),
		Active:    activeSessions,
	}))

	slog.Info("relay: dispatcher wired")
	return activeSessions.CancelAll
}

// tuiServerAddr returns the local server bind address from config.
func tuiServerAddr(cfg config.Config) string {
	bind := cfg.WebUI.Bind
	if bind == "" {
		bind = "127.0.0.1"
	}
	port := cfg.WebUI.Port
	if port == 0 {
		port = 8477
	}
	return fmt.Sprintf("%s:%d", bind, port)
}

// loadTUIServerToken loads or creates the server auth token for the TUI mode proxy.
func loadTUIServerToken(huginnHome string) string {
	token, _ := server.LoadOrCreateToken(huginnHome)
	return token
}
