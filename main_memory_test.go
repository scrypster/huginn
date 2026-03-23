package main_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestHuginnBuild_Succeeds is a build-time smoke test that verifies "go build ."
// exits cleanly. It catches import cycles, missing exported symbols, and wiring
// errors in main.go that unit tests cannot detect.
func TestHuginnBuild_Succeeds(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "huginn-smoketest")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build . failed:\n%s\nerror: %v", string(out), err)
	}
}

// TestHuginnMemoryStatus_ExitsCleanly was removed because the `huginn memory`
// CLI subcommand was deleted as part of the MCP migration (Task 5).
// MuninnDB is now accessed exclusively via the per-session MCP connection.
