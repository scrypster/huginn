package tools

import (
	"fmt"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
)

// GitStager stages a file into git after writing.
type GitStager interface {
	StageFile(absPath string) error
}

// DefaultGitStager stages files using go-git.
type DefaultGitStager struct {
	SandboxRoot string
}

func (s *DefaultGitStager) StageFile(absPath string) error {
	r, err := git.PlainOpenWithOptions(absPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil // Not in git repo — silently skip
	}
	wt, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("stager: worktree: %w", err)
	}
	root := wt.Filesystem.Root()
	relPath, err := filepath.Rel(root, absPath)
	if err != nil {
		return fmt.Errorf("stager: rel path: %w", err)
	}
	_, err = wt.Add(relPath)
	return err
}
