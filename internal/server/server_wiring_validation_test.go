package server

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

func TestValidateWiring_MissingRequiredCapabilities(t *testing.T) {
	cfg := *config.Default()
	srv := New(cfg, nil, nil, testToken, t.TempDir(), nil, nil, nil)

	err := srv.ValidateWiring()
	if err == nil {
		t.Fatal("expected wiring validation error")
	}
	msg := err.Error()
	for _, part := range []string{"orchestrator", "session_store"} {
		if !strings.Contains(msg, part) {
			t.Fatalf("expected error to mention %q, got %q", part, msg)
		}
	}
}

func TestStart_FailsFastOnInvalidWiring(t *testing.T) {
	cfg := *config.Default()
	cfg.WebUI.Port = 0
	srv := New(cfg, nil, nil, testToken, t.TempDir(), nil, nil, nil)

	err := srv.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to fail on invalid wiring")
	}
	if !strings.Contains(err.Error(), "missing required capabilities") {
		t.Fatalf("unexpected start error: %v", err)
	}
}

func TestCapabilities_ReflectWiredDependencies(t *testing.T) {
	srv, _ := newTestServer(t)
	caps := srv.Capabilities()
	if !caps["orchestrator"] {
		t.Fatal("expected orchestrator capability to be true")
	}
	if !caps["session_store"] {
		t.Fatal("expected session_store capability to be true")
	}
	if caps["artifact_store"] {
		t.Fatal("expected artifact_store capability to be false by default")
	}
}
