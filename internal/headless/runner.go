package headless

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/radar"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/storage"
)

// AgentRunFunc is the signature for the optional agent execution callback.
// main.go injects this when --print is also set, keeping internal/headless
// free of agent/backend imports.
// Returns (output, toolNames, tokenCount, err).
type AgentRunFunc func(ctx context.Context, agentName, prompt, sessionID string) (output string, toolsCalled []string, tokensUsed int, err error)

// HeadlessConfig holds configuration for a headless run.
type HeadlessConfig struct {
	CWD     string
	Command string
	JSON    bool
	// Agent execution (optional). When Prompt is non-empty and AgentRun is set,
	// the runner executes the prompt via the provided agent and populates
	// RunResult.AgentOutput, ToolsCalled, and TokensUsed.
	Prompt    string
	Agent     string        // agent name; uses default if empty
	SessionID string        // optional session ID for multi-turn
	AgentRun  AgentRunFunc  // injected by main.go; nil means no agent execution
}

// FindingSummary is a trimmed representation of a radar Finding for JSON output.
type FindingSummary struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Title    string  `json:"title"`
	Severity string  `json:"severity"`
	Score    float64 `json:"score"`
	Files    []string `json:"files"`
}

// RunResult is the structured output of a headless run.
type RunResult struct {
	Mode            string           `json:"mode"`
	Root            string           `json:"root"`
	ReposFound      []string         `json:"reposFound"`
	IndexDuration   string           `json:"indexDuration"`
	FilesScanned    int              `json:"filesScanned"`
	FilesSkipped    int              `json:"filesSkipped"`
	RadarDuration   string           `json:"radarDuration"`
	BFSNodesVisited int              `json:"bfsNodesVisited"`
	TopFindings     []FindingSummary `json:"topFindings"`
	BannersEmitted  int              `json:"bannersEmitted"`
	Errors          []string         `json:"errors,omitempty"`
	// Agent output fields (populated when HeadlessConfig.Prompt != "").
	AgentOutput string   `json:"agentOutput,omitempty"`
	ToolsCalled []string `json:"toolsCalled,omitempty"`
	TokensUsed  int      `json:"tokensUsed,omitempty"`
}

// Run executes the headless pipeline: detect → index → radar → (optional) agent prompt.
func Run(cfg HeadlessConfig) (*RunResult, error) {
	result := &RunResult{}

	cwd := cfg.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
	}

	// 1. Workspace detection (inline: single-repo or plain)
	detection := repo.Detect(cwd, "")
	switch detection.Mode {
	case repo.ModeWorkspace:
		result.Mode = "workspace"
		if detection.WorkspaceConfig != nil {
			for _, r := range detection.WorkspaceConfig.Repos {
				result.ReposFound = append(result.ReposFound, r.Path)
			}
		}
	case repo.ModeRepo:
		result.Mode = "repo"
		result.ReposFound = []string{detection.Root}
	default:
		result.Mode = "plain"
		result.ReposFound = []string{detection.Root}
	}
	result.Root = detection.Root

	// 2. Open Pebble store
	storeDir := headlessStoreDir(detection.Root)
	store, err := storage.Open(storeDir)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("store: %v", err))
		return result, nil
	}
	defer store.Close()

	// 3. Incremental index
	indexStart := time.Now()
	idxResult, err := repo.BuildIncrementalWithStats(detection.Root, store, nil)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("index: %v", err))
		return result, nil
	}
	result.IndexDuration = time.Since(indexStart).String()
	result.FilesScanned = idxResult.FilesScanned
	result.FilesSkipped = idxResult.FilesSkipped
	_ = idxResult.Index // available for future context building

	// 4. Run radar (only for /radar run command)
	totalFiles := idxResult.FilesScanned + idxResult.FilesSkipped
	if cfg.Command == "/radar run" || cfg.Command == "radar" {
		radarResult, err := runRadar(store, detection, totalFiles)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("radar: %v", err))
		} else {
			result.RadarDuration = radarResult.duration
			result.BFSNodesVisited = radarResult.bfsNodes
			result.TopFindings = radarResult.findings
			result.BannersEmitted = radarResult.banners
		}
	}

	// 5. Agent prompt execution (optional; injected by main.go via AgentRun).
	if cfg.Prompt != "" && cfg.AgentRun != nil {
		output, toolsCalled, tokens, agentErr := cfg.AgentRun(context.Background(), cfg.Agent, cfg.Prompt, cfg.SessionID)
		if agentErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("agent: %v", agentErr))
		} else {
			result.AgentOutput = output
			result.ToolsCalled = toolsCalled
			result.TokensUsed = tokens
		}
	}

	return result, nil
}

type radarRunResult struct {
	duration string
	bfsNodes int
	findings []FindingSummary
	banners  int
}

func runRadar(store *storage.Store, detection repo.DetectionResult, totalFiles int) (*radarRunResult, error) {
	start := time.Now()

	if store.DB() == nil {
		return nil, fmt.Errorf("store DB is nil — cannot run radar")
	}

	repoID := workspaceHash(detection.Root)
	sha := getGitHead(detection.Root)
	branch := getGitBranch(detection.Root)

	// Collect changed files: in workspace mode, try each sub-repo; otherwise use root.
	var changedFiles []string
	if detection.Mode == repo.ModeWorkspace && detection.WorkspaceConfig != nil {
		for _, r := range detection.WorkspaceConfig.Repos {
			repoPath := filepath.Join(detection.Root, r.Path)
			changed := getChangedFiles(repoPath)
			changedFiles = append(changedFiles, changed...)
		}
	} else {
		changedFiles = getChangedFiles(detection.Root)
	}

	if totalFiles == 0 {
		totalFiles = 100 // fallback estimate
	}

	ackStore := &radar.PebbleAckStore{
		DB:     store.DB(),
		RepoID: repoID,
	}

	input := radar.EvaluateInput{
		DB:           store.DB(),
		RepoID:       repoID,
		SHA:          sha,
		Branch:       branch,
		ChangedFiles: changedFiles,
		ChurnRecords: map[string]radar.ChurnRecord{},
		TotalFiles:   totalFiles,
		AckStore:     ackStore,
	}

	findings, err := radar.Evaluate(input)
	if err != nil {
		return nil, err
	}

	duration := time.Since(start).String()

	// Extract BFS nodes from first high-impact finding title
	bfsNodes := 0
	for _, f := range findings {
		if f.Type == "high-impact" {
			// Title format: "Change impacts %d files (depth %d)"
			var n, depth int
			if _, err := fmt.Sscanf(f.Title, "Change impacts %d files (depth %d)", &n, &depth); err == nil {
				bfsNodes = n
			}
			break
		}
	}

	// Build FindingSummary list (top 10)
	summaries := make([]FindingSummary, 0, len(findings))
	banners := 0
	for _, f := range findings {
		if f.Notify == radar.NotifyBanner || f.Notify == radar.NotifyUrgent {
			banners++
		}
		summaries = append(summaries, FindingSummary{
			ID:       f.ID,
			Type:     f.Type,
			Title:    f.Title,
			Severity: f.Severity.String(),
			Score:    f.Score.Total,
			Files:    f.Files,
		})
		if len(summaries) >= 10 {
			break
		}
	}

	return &radarRunResult{
		duration: duration,
		bfsNodes: bfsNodes,
		findings: summaries,
		banners:  banners,
	}, nil
}

// workspaceHash returns a short stable ID for a repo root path.
func workspaceHash(root string) string {
	h := sha256.Sum256([]byte(root))
	return hex.EncodeToString(h[:])[:12]
}

// headlessStoreDir returns the Pebble store path for headless mode.
func headlessStoreDir(root string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "huginn-headless-store")
	}
	name := sanitizePath(root)
	if len(name) > 64 {
		name = name[len(name)-64:]
	}
	return filepath.Join(home, ".huginn", "store", name)
}

func sanitizePath(p string) string {
	var result strings.Builder
	for _, c := range p {
		switch c {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			result.WriteByte('_')
		default:
			result.WriteRune(c)
		}
	}
	return result.String()
}

// getGitHead returns the current HEAD SHA, or "HEAD" if unavailable.
func getGitHead(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(string(out))
}

// getGitBranch returns the current branch name.
func getGitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
}

// getChangedFiles returns files changed since the previous commit.
// Falls back to empty slice if git is unavailable.
func getChangedFiles(dir string) []string {
	out, err := exec.Command("git", "-C", dir, "diff", "--name-only", "HEAD~1", "HEAD").Output()
	if err != nil {
		// HEAD~1 doesn't exist on the initial commit — list all files in HEAD.
		out, err = exec.Command("git", "-C", dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
		if err != nil {
			// Try staged changes as a last resort
			out, err = exec.Command("git", "-C", dir, "diff", "--name-only", "--cached").Output()
			if err != nil {
				return []string{}
			}
		}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			files = append(files, l)
		}
	}
	return files
}
