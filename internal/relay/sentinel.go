package relay

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	sentinelTokenFile     = ".huginn/sentinel-token" // relative to home dir
	sentinelHelloInterval = 60 * time.Second
)

// SentinelConfig configures the boot-time sentinel.
type SentinelConfig struct {
	MachineID   string
	TokenStorer TokenStorer // loads the sentinel file token
	CloudURL    string
	SkipDial    bool // for tests: skip real WebSocket dial
}

// Sentinel is a minimal relay presence process that runs at system boot
// (before user login). It:
//   - Does NOT execute agent tasks
//   - Connects to HuginnCloud with a file-based token
//   - Sends satellite_hello with status: "locked" every 60s
//   - Enables HuginnCloud dashboard to show "online but locked" vs "offline"
type Sentinel struct {
	cfg SentinelConfig
}

// NewSentinel creates a Sentinel.
func NewSentinel(cfg SentinelConfig) *Sentinel {
	return &Sentinel{cfg: cfg}
}

// Run connects and sends periodic locked-status hellos until ctx is cancelled.
func (s *Sentinel) Run(ctx context.Context) {
	if s.cfg.SkipDial {
		slog.Info("sentinel: running in test mode (no dial)")
		<-ctx.Done()
		return
	}

	token, err := s.cfg.TokenStorer.Load()
	if err != nil {
		slog.Error("sentinel: no token stored — cannot connect", "err", err)
		<-ctx.Done()
		return
	}

	wsURL := s.cfg.CloudURL + "/ws/satellite"
	hub := NewWebSocketHub(WebSocketConfig{
		URL:       wsURL,
		Token:     token,
		MachineID: s.cfg.MachineID,
		Version:   "sentinel",
	})

	if err := hub.Connect(ctx); err != nil {
		slog.Error("sentinel: initial connect failed", "err", err)
		// Reconnect loop inside hub will retry.
	}
	defer hub.Close("")

	slog.Info("sentinel: connected, broadcasting locked status", "machine_id", s.cfg.MachineID)

	ticker := time.NewTicker(sentinelHelloInterval)
	defer ticker.Stop()

	s.sendLockedStatus(hub)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sendLockedStatus(hub)
		}
	}
}

func (s *Sentinel) sendLockedStatus(hub Hub) {
	if err := hub.Send("", Message{
		Type:      MsgSatelliteHello,
		MachineID: s.cfg.MachineID,
		Payload: map[string]any{
			"version": "sentinel",
			"status":  "locked",
		},
	}); err != nil {
		slog.Warn("sentinel: failed to send hello", "err", err)
	}
}

// SentinelTokenPath returns the path to the sentinel file-based token.
func SentinelTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return sentinelTokenFile
	}
	return filepath.Join(home, sentinelTokenFile)
}

// SentinelFileTokenStore is a TokenStorer backed by a plain file.
type SentinelFileTokenStore struct {
	path string
}

func NewSentinelFileTokenStore(path string) *SentinelFileTokenStore {
	return &SentinelFileTokenStore{path: path}
}

func (s *SentinelFileTokenStore) Save(token string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path, []byte(token), 0o600)
}

func (s *SentinelFileTokenStore) Load() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *SentinelFileTokenStore) Clear() error {
	return os.Remove(s.path)
}

func (s *SentinelFileTokenStore) IsRegistered() bool {
	_, err := s.Load()
	return err == nil
}
