package relay

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/scrypster/huginn/internal/storage"
)

// RunnerConfig controls the satellite runner.
type RunnerConfig struct {
	MachineID          string
	HeartbeatInterval  time.Duration
	CloudURL           string
	SessionStore       *SessionStore // optional; overrides StorePath-derived store
	Outbox             *Outbox       // optional; overrides StorePath-derived outbox
	StorePath          string        // when set, Runner opens a Pebble store here
	TokenStore         TokenStorer   // optional; overrides OS-keyring token store (for tests)
	// SkipConnectOnStart is used in tests to avoid real network dials.
	SkipConnectOnStart bool
}

// Runner manages a long-lived satellite relay connection.
type Runner struct {
	cfg RunnerConfig
}

// NewRunner creates a Runner.
func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 60 * time.Second
	}
	if cfg.MachineID == "" {
		cfg.MachineID = GetMachineID()
	}
	if cfg.CloudURL == "" {
		cfg.CloudURL = "wss://relay.huginncloud.com"
	}
	return &Runner{cfg: cfg}
}

// Run connects the satellite and blocks until ctx is cancelled.
// It starts a heartbeater that periodically sends satellite_heartbeat messages,
// and a dispatcher that routes inbound messages to handlers.
func (r *Runner) Run(ctx context.Context) {
	var sat *Satellite
	if r.cfg.TokenStore != nil {
		sat = NewSatelliteWithStore(r.cfg.CloudURL, r.cfg.TokenStore)
	} else {
		sat = NewSatellite(r.cfg.CloudURL)
	}
	sat.SetMachineID(r.cfg.MachineID)

	// Open relay store if StorePath is set and stores were not injected.
	var relayStore *storage.Store
	sessionStore := r.cfg.SessionStore
	outbox := r.cfg.Outbox
	if r.cfg.StorePath != "" && sessionStore == nil {
		var storeErr error
		relayStore, storeErr = storage.Open(r.cfg.StorePath)
		if storeErr != nil {
			slog.Error("relay: failed to open relay store", "path", r.cfg.StorePath, "err", storeErr)
		} else {
			sessionStore = NewSessionStore(relayStore)
		}
	}
	if relayStore != nil {
		defer relayStore.Close()
	}

	// Pre-create the WebSocketHub (unconnected) so we can wire the dispatcher
	// callback BEFORE readPump starts. This prevents inbound messages sent by
	// HuginnCloud immediately after satellite_hello from being silently dropped.
	var preHub *WebSocketHub
	if !r.cfg.SkipConnectOnStart {
		preHub = sat.NewHubForConnect()
	}

	// Determine which Hub the dispatcher will send responses through.
	// If preHub is nil (not registered or SkipConnectOnStart), fall back to
	// InProcessHub so the dispatcher still compiles and runs safely.
	var dispHub Hub
	if preHub != nil {
		dispHub = preHub
	} else {
		dispHub = &InProcessHub{}
	}

	// Wire dispatcher using the pre-allocated hub reference.
	dispCfg := DispatcherConfig{
		MachineID: r.cfg.MachineID,
		Hub:       dispHub,
		Store:     sessionStore,
		Active:    NewActiveSessions(),
		Shell:     NewShellManager(),
	}
	dispatcher := NewDispatcher(dispCfg)

	// Register callback on the hub BEFORE Connect() starts readPump,
	// so no early messages are dropped.
	if preHub != nil {
		preHub.SetOnMessage(dispatcher)
		if err := sat.ConnectHub(ctx, preHub); err != nil {
			slog.Error("relay: initial connect failed — will retry automatically", "err", err)
		}
	}

	hub := sat.ActiveHub()

	// Create outbox if we have a store and weren't given one.
	if outbox == nil && relayStore != nil {
		outbox = NewOutbox(relayStore, hub)
	}

	// Heartbeater sends satellite_heartbeat on the configured interval.
	hb := NewHeartbeater(hub, r.cfg.MachineID, HeartbeatConfig{
		Interval:     r.cfg.HeartbeatInterval,
		SessionStore: sessionStore,
		Outbox:       outbox,
	})
	// WaitGroup covers the heartbeater and the outbox flusher so that
	// relayStore.Close() (deferred above) does not run until both goroutines
	// have exited. Without this, a heartbeat tick that fires after ctx is
	// cancelled can call sessionStore.List() on a closed pebble DB and panic.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		hb.Start(ctx)
	}()

	// Periodic outbox flush: drain queued messages every 5 seconds.
	if outbox != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := outbox.Flush(ctx); err != nil && !errors.Is(err, context.Canceled) {
						slog.Warn("relay: outbox flush", "err", err)
					}
				}
			}
		}()
	}

	// Wake notifier: reset reconnect backoff immediately on OS wake.
	wakeNotifier := NewWakeNotifier()
	wakeCh := wakeNotifier.Watch(ctx)
	go func() {
		for range wakeCh {
			slog.Info("relay: OS wake detected — triggering immediate reconnect")
			sat.Reconnect(ctx)
		}
	}()

	slog.Info("relay: satellite running", "machine_id", r.cfg.MachineID)
	<-ctx.Done()
	slog.Info("relay: satellite stopping")
	// Cancel any in-flight chat sessions so their goroutines can exit.
	if dispCfg.Active != nil {
		dispCfg.Active.CancelAll()
	}
	wg.Wait()
	sat.Disconnect()
}

// RunWithSignals is the entry point for `huginn relay start`.
// It sets up signal handling (SIGTERM, SIGINT) and runs the relay until interrupted.
func RunWithSignals(cfg RunnerConfig) {
	ctx, cancel := context.WithCancel(context.Background())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		slog.Info("relay: received shutdown signal")
		cancel()
	}()

	NewRunner(cfg).Run(ctx)
	cancel()
}
