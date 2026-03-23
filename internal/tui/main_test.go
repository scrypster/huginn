package tui

import (
	"os"
	"testing"
)

// TestMain sets HUGINN_HOME to a temporary directory for the entire TUI test
// package so that tests which call SaveAgentDefault (via handleAgentsCommand,
// update_overlays, etc.) never write to the developer's real ~/.huginn/agents/.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "huginn-tui-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("HUGINN_HOME", tmp) //nolint:errcheck
	os.Exit(m.Run())
}
