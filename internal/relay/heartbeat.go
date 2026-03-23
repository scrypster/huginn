package relay

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"
)

// HeartbeatConfig controls the heartbeat sender.
type HeartbeatConfig struct {
	// Interval between heartbeat messages. Default: 60s.
	Interval     time.Duration
	SessionStore *SessionStore // optional; used for active_sessions count
	Outbox       *Outbox       // optional; used for pending_outbox count
}

// Heartbeater sends periodic satellite_heartbeat messages via hub.
type Heartbeater struct {
	hub       Hub
	machineID string
	cfg       HeartbeatConfig
	startedAt time.Time
}

// NewHeartbeater creates a Heartbeater.
// If cfg.Interval is zero, defaults to 60s.
func NewHeartbeater(hub Hub, machineID string, cfg HeartbeatConfig) *Heartbeater {
	if cfg.Interval == 0 {
		cfg.Interval = 60 * time.Second
	}
	return &Heartbeater{
		hub:       hub,
		machineID: machineID,
		cfg:       cfg,
		startedAt: time.Now(),
	}
}

// Start runs the heartbeat loop until ctx is cancelled.
// Call in a goroutine: go h.Start(ctx).
func (h *Heartbeater) Start(ctx context.Context) {
	ticker := time.NewTicker(h.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.send(); err != nil {
				slog.Warn("relay: heartbeat send failed", "err", err)
			}
		}
	}
}

func (h *Heartbeater) send() error {
	payload := map[string]any{
		"machine_id":     h.machineID,
		"uptime_seconds": int64(time.Since(h.startedAt).Seconds()),
		"os":             runtime.GOOS,
		"arch":           runtime.GOARCH,
	}

	// Best-effort disk check — ignore errors.
	if gb, err := availableDiskGB(); err == nil {
		payload["available_disk_gb"] = gb
	}

	// Best-effort CPU load check — ignore errors.
	if load, err := cpuLoad1m(); err == nil {
		payload["cpu_load_1m"] = load
	}

	// Best-effort active sessions count — ignore errors.
	if h.cfg.SessionStore != nil {
		if active, err := h.cfg.SessionStore.ListActive(); err == nil {
			payload["active_sessions"] = len(active)
		}
	}

	// Best-effort pending outbox count — ignore errors.
	if h.cfg.Outbox != nil {
		if n, err := h.cfg.Outbox.Len(); err == nil {
			payload["pending_outbox"] = n
		}
	}

	return h.hub.Send("", Message{
		Type:      MsgSatelliteHeartbeat,
		MachineID: h.machineID,
		Payload:   payload,
	})
}

// availableDiskGB returns available disk space in GB for the huginn data dir.
func availableDiskGB() (float64, error) {
	return diskFreeGB(huginnDataDir())
}

// huginnDataDir returns ~/.huginn (or best-effort path).
func huginnDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".huginn"
	}
	return home + "/.huginn"
}
