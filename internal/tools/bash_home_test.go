package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent/session"
)

func TestBashTool_LsTilde_ListsFakeHome(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "alpha.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".huginn"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".huginn", "steve.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	tool := &BashTool{SandboxRoot: t.TempDir(), Timeout: 10 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{"command": "ls -1 ~"})
	if result.IsError {
		t.Fatalf("ls ~ error: %s", result.Error)
	}
	if strings.TrimSpace(result.Output) == "" {
		t.Fatal("ls ~ returned empty-success; ~ must expand to the fake home")
	}
	if !strings.Contains(result.Output, "alpha.txt") {
		t.Errorf("ls ~ = %q, want alpha.txt", result.Output)
	}

	result = tool.Execute(context.Background(), map[string]any{"command": "ls -1 ~/.huginn"})
	if result.IsError {
		t.Fatalf("ls ~/.huginn error: %s", result.Error)
	}
	if strings.TrimSpace(result.Output) == "" {
		t.Fatal("ls ~/.huginn returned empty-success")
	}
	if !strings.Contains(result.Output, "steve.json") {
		t.Errorf("ls ~/.huginn = %q, want steve.json", result.Output)
	}
}

func TestBashTool_DollarHome_ListsFakeHome(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "marker"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	tool := &BashTool{SandboxRoot: t.TempDir(), Timeout: 10 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{"command": "ls -1 $HOME"})
	if result.IsError {
		t.Fatalf("ls $HOME error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "marker") {
		t.Errorf("ls $HOME = %q, want marker", result.Output)
	}

	result = tool.Execute(context.Background(), map[string]any{"command": "ls -1 ${HOME}"})
	if result.IsError {
		t.Fatalf("ls ${HOME} error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "marker") {
		t.Errorf("ls ${HOME} = %q, want marker", result.Output)
	}
}

func TestBashTool_TildeUsesProcessHomeNotSessionHome(t *testing.T) {
	realHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(realHome, "real-marker"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", realHome)

	sessionHome := t.TempDir() // empty: the live bug listed this and looked successful
	ctx := session.WithEnv(context.Background(), []string{"HOME=" + sessionHome})

	tool := &BashTool{SandboxRoot: t.TempDir(), Timeout: 10 * time.Second}
	result := tool.Execute(ctx, map[string]any{"command": "ls -1 ~"})
	if result.IsError {
		t.Fatalf("ls ~ with session HOME remapped: %s", result.Error)
	}
	if strings.TrimSpace(result.Output) == "" {
		t.Fatal("session HOME remapping produced empty-success for ls ~")
	}
	if !strings.Contains(result.Output, "real-marker") {
		t.Errorf("ls ~ listed session home instead of process home: %q", result.Output)
	}
}

func TestBashTool_UnexpandedTildeIsLoudError(t *testing.T) {
	_, err := expandHomeInCommandFrom("", "ls -1 ~")
	if err == nil {
		t.Fatal("expected loud expansion error when home is unset")
	}
	if !strings.Contains(err.Error(), "not expanded") {
		t.Errorf("error = %q, want 'not expanded'", err)
	}

	_, err = expandHomeInCommandFrom("", "ls -1 $HOME/.huginn")
	if err == nil {
		t.Fatal("expected loud expansion error for $HOME when home is unset")
	}

	// Execute path: empty HOME + empty USERPROFILE. If the OS still reports a
	// passwd home, expansion succeeds (correct). The From("", ...) cases above
	// cover the empty-success hole.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if home, err := resolveHomeDir(); err == nil && home != "" {
		t.Logf("OS still resolved home %q; skip Execute-level unset test", home)
		return
	}
	tool := &BashTool{SandboxRoot: t.TempDir(), Timeout: 10 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{"command": "ls -1 ~"})
	if !result.IsError {
		t.Fatalf("expected loud expansion error, got success output %q", result.Output)
	}
	if !strings.Contains(result.Error, "not expanded") {
		t.Errorf("error = %q, want 'not expanded'", result.Error)
	}
}

func TestExpandHomeInCommand_QuotedTildeUnchanged(t *testing.T) {
	got, err := expandHomeInCommand("ls '~'")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ls '~'" {
		t.Errorf("quoted tilde rewritten: %q", got)
	}
}

func TestExpandHomeInCommand_NoHomeRefsUnchanged(t *testing.T) {
	got, err := expandHomeInCommand("echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if got != "echo hello" {
		t.Errorf("plain command rewritten: %q", got)
	}
}

func TestExpandHomeRefs_WordStartOnly(t *testing.T) {
	got := expandHomeRefs("echo foo~bar", "/home/mj")
	if got != "echo foo~bar" {
		t.Errorf("mid-word tilde expanded: %q", got)
	}
	got = expandHomeRefs("echo $HOSTNAME", "/home/mj")
	if got != "echo $HOSTNAME" {
		t.Errorf("$HOSTNAME must not become $ + home: %q", got)
	}
}
