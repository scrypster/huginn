package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExtractGHToken runs `gh auth token` with the given gh binary and returns the
// OAuth token. If account is non-empty, runs `gh auth token -u <account>`.
// ghBinaryPath must be the real binary path (not PATH-resolved) to avoid shims.
func ExtractGHToken(ghBinaryPath, account string) (string, error) {
	args := []string{"auth", "token"}
	if account != "" {
		args = append(args, "-u", account)
	}
	cmd := exec.Command(ghBinaryPath, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned empty output")
	}
	return token, nil
}

// SetupGH extracts the GitHub token for the given account and adds GH_TOKEN
// to the session env. Also creates a minimal .config/gh/ in the session dir.
// account may be "" to use the active/default account.
func SetupGH(sess *Session, ghBinaryPath, account string) error {
	token, err := ExtractGHToken(ghBinaryPath, account)
	if err != nil {
		return err
	}

	// Create minimal gh config dir so gh doesn't complain about missing config.
	ghConfigDir := filepath.Join(sess.Dir, ".config", "gh")
	if err := os.MkdirAll(ghConfigDir, 0700); err != nil {
		return fmt.Errorf("create gh config dir: %w", err)
	}

	// GH_TOKEN overrides all other auth. GH_CONFIG_DIR points to our empty dir.
	sess.Env = append(sess.Env,
		"GH_TOKEN="+token,
		"GH_CONFIG_DIR="+ghConfigDir,
	)
	return nil
}
