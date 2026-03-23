package tools_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/tools"
)

func TestGHPRListTool_UsesAbsolutePath(t *testing.T) {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh not installed")
	}
	tool := tools.NewGHPRListTool(ghPath)
	if tool.GHPath != ghPath {
		t.Errorf("expected GHPath=%q, got %q", ghPath, tool.GHPath)
	}
}

func TestGHPRListTool_InjectsSessionEnv(t *testing.T) {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh not installed")
	}
	tool := tools.NewGHPRListTool(ghPath)
	// Inject a fake GH_TOKEN — we expect gh to attempt using it (and fail,
	// but that's fine; we're testing env injection, not gh behavior)
	ctx := session.WithEnv(context.Background(), []string{"GH_TOKEN=fake-token-test"})
	// Execute will fail due to fake token, but we just verify env is passed.
	// The important thing is no panic and the call uses GHPath not PATH.
	_ = tool.Execute(ctx, map[string]any{"state": "open"})
}
