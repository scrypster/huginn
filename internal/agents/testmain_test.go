package agents_test

import (
	"os"
	"testing"
)

// TestMain sets HUGINN_HOME to a temporary directory for the entire agents test
// package so that tests which call SaveAgentDefault / SaveAgents / DeleteAgentDefault
// never write to the developer's real ~/.huginn/agents/.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "huginn-agents-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("HUGINN_HOME", tmp) //nolint:errcheck
	os.Exit(m.Run())
}
