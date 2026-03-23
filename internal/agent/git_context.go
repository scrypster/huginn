package agent

import (
	"fmt"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

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
