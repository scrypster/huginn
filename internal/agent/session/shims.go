package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// knownCLITools maps provider names (as used in ToolbeltEntry.Provider) to
// CLI binary names.
var knownCLITools = map[string]string{
	"github_cli": "gh",
	"aws":        "aws",
	"gcloud":     "gcloud",
	"kubectl":    "kubectl",
	"terraform":  "terraform",
}

// BinaryForProvider returns the CLI binary name for the given provider string.
// Returns "" if the provider is not a known system CLI provider.
func BinaryForProvider(provider string) string {
	return knownCLITools[provider]
}

// DetectAvailableCLIs checks which known CLI tools are installed on the machine
// and returns their binary names. Call before shims are installed.
func DetectAvailableCLIs() []string {
	var found []string
	for _, binary := range knownCLITools {
		if _, err := exec.LookPath(binary); err == nil {
			found = append(found, binary)
		}
	}
	sort.Strings(found)
	return found
}

// WriteDenyShim writes a bash deny-wrapper script for the named binary into
// the session's bin/ directory. The shim exits 1 with a clear error message.
func WriteDenyShim(sess *Session, binaryName string) error {
	if sess == nil {
		return fmt.Errorf("session: WriteDenyShim called with nil session")
	}
	script := fmt.Sprintf(`#!/bin/bash
echo '%s is available on this system but is not in your agent toolbelt. Add it in the agent configuration to enable access.' >&2
exit 1
`, binaryName)

	shimPath := filepath.Join(sess.Dir, "bin", binaryName)
	binDir := filepath.Dir(shimPath)
	if err := os.MkdirAll(binDir, 0700); err != nil {
		return fmt.Errorf("create shim dir for %s: %w", binaryName, err)
	}
	if err := os.WriteFile(shimPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("write shim for %s: %w", binaryName, err)
	}
	return nil
}

// InstallShims writes deny-shims for all detected CLI binaries that are NOT
// in the toolbelt binary set. toolbeltBinaries maps binary name → true for
// tools that are allowed (no shim written for these).
func InstallShims(sess *Session, detectedBinaries []string, toolbeltBinaries map[string]bool) {
	if sess == nil {
		return
	}
	for _, binary := range detectedBinaries {
		if !toolbeltBinaries[binary] {
			WriteDenyShim(sess, binary) // best-effort; ignore individual errors
		}
	}
}
