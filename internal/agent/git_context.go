package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// gitContextShellTimeout bounds the native-git calls used to enrich the
// context block (upstream, ahead/behind, default branch) — these are cheap,
// local-only commands (no network), but a hard cap keeps a misbehaving repo
// from stalling context assembly on every turn.
const gitContextShellTimeout = 2 * time.Second

// runGitContextCmd runs a native git command in root and returns trimmed
// stdout, or "" on any error. Best-effort only — the git context block is an
// enrichment, never a hard dependency.
func runGitContextCmd(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitContextShellTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

// buildGitContext returns a "## Git Context" markdown section.
// Returns "" if root is not a git repo or on any error.
func buildGitContext(root string) string {
	if root == "" {
		return ""
	}

	r, err := git.PlainOpenWithOptions(root, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Git Context\n")

	head, err := r.Head()
	if err == nil {
		fmt.Fprintf(&sb, "Branch: %s\n", head.Name().Short())
	}

	// Upstream + ahead/behind + default branch — compact, structured facts
	// the harness can cheaply guarantee, so the model never has to guess or
	// invent them. Local-only native git calls (no network).
	if upstream := runGitContextCmd(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); upstream != "" {
		aheadBehind := runGitContextCmd(root, "rev-list", "--left-right", "--count", "HEAD..."+upstream)
		ahead, behind := "0", "0"
		if parts := strings.Fields(aheadBehind); len(parts) == 2 {
			ahead, behind = parts[0], parts[1]
		}
		fmt.Fprintf(&sb, "Upstream: %s (ahead %s, behind %s)\n", upstream, ahead, behind)
	}
	if defaultBranch := runGitContextCmd(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); defaultBranch != "" {
		fmt.Fprintf(&sb, "Default branch: %s\n", strings.TrimPrefix(defaultBranch, "origin/"))
	}

	wt, err := r.Worktree()
	if err == nil {
		status, err := wt.Status()
		if err == nil && !status.IsClean() {
			sb.WriteString("Status:\n")
			count := 0
			for path, s := range status {
				if count >= 20 {
					sb.WriteString("  ... (truncated)\n")
					break
				}
				fmt.Fprintf(&sb, " %c%c %s\n", s.Staging, s.Worktree, path)
				count++
			}
		}
	}

	log, err := r.Log(&git.LogOptions{})
	if err == nil {
		sb.WriteString("Recent commits:\n")
		count := 0
		log.ForEach(func(c *object.Commit) error {
			if count >= 5 {
				return fmt.Errorf("stop")
			}
			subject := strings.SplitN(c.Message, "\n", 2)[0]
			fmt.Fprintf(&sb, "  %s %s\n", c.Hash.String()[:7], subject)
			count++
			return nil
		})
	}

	return sb.String()
}
