package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// TestSessionStartedSurvivesTheBackendItWasRecordedOn is the unit-level proof
// for Finding 4. markSessionExists() used to write only to the instance, and
// the instance is thrown away at the end of every turn — so the flag was a
// no-op in production and the cross-turn answer fell to the disk probe alone.
//
// The whole point is that a SECOND, INDEPENDENT backend for the same session
// id sees the record, even when its caller still insists FirstTurn is true.
func TestSessionStartedSurvivesTheBackendItWasRecordedOn(t *testing.T) {
	cfg := agentBackendCfg(t)
	bin, _ := writeFakeCLI(t, fakeCLI{stream: execToolStream})
	cfg.Binary = bin
	cfg.FirstTurn = true

	first := NewAgentBackend(cfg)
	if _, err := first.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "turn one"}},
	}); err != nil {
		t.Fatalf("turn one: %v", err)
	}

	if !sessionStarted(cfg.SessionID) {
		t.Fatalf("the session's spent --session-id chance was not recorded anywhere outside the instance")
	}

	// A brand-new backend, same session id, caller STILL saying FirstTurn —
	// this is what a wrong disk probe looks like on turn 2.
	second := NewAgentBackend(cfg)
	args, err := second.buildArgs("turn two")
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--session-id") {
		t.Errorf("turn 2 re-claimed an id the CLI already owns; that fails PERMANENTLY: %v", args)
	}
	if !strings.Contains(joined, "--resume "+cfg.SessionID) {
		t.Errorf("turn 2 must resume: %v", args)
	}
}

// An empty session id must never be recorded: such a backend starts a fresh
// CLI session every turn, so "resuming" it would resume nothing.
func TestSessionStartedIgnoresTheEmptySessionID(t *testing.T) {
	markSessionStarted("")
	if sessionStarted("") {
		t.Error("the empty session id was recorded; every idless backend would then resume a conversation that does not exist")
	}
}

// TestDefaultRootHonoursClaudeConfigDir. Claude Code stores transcripts under
// $CLAUDE_CONFIG_DIR/projects when that variable is set. Ignoring it broke two
// things silently at once: the transcript bridge watched a directory that does
// not exist and ingested nothing, and the session-exists probe always answered
// false so every turn re-spent the one --session-id chance.
func TestDefaultRootHonoursClaudeConfigDir(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", custom)

	got := DefaultRoot()
	want := filepath.Join(custom, "projects")
	if got != want {
		t.Fatalf("DefaultRoot() = %q, want %q — a user with CLAUDE_CONFIG_DIR set gets no transcripts and a permanently broken agent", got, want)
	}

	// And the transcript really is findable there, which is what the probe and
	// the watcher both depend on.
	proj := filepath.Join(want, "-tmp-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(proj, "abc.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(DefaultRoot(), "-tmp-project", "abc.jsonl")); err != nil {
		t.Errorf("transcript not reachable under DefaultRoot(): %v", err)
	}
}

// Unset, the root falls back to ~/.claude/projects.
func TestDefaultRootFallsBackToHomeDotClaude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got, want := DefaultRoot(), filepath.Join(home, ".claude", "projects"); got != want {
		t.Errorf("DefaultRoot() = %q, want %q", got, want)
	}
}
