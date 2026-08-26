package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// THESE TESTS PIN WIRING, NOT BEHAVIOUR.
//
// Every other test on this feature calls claudeApproveMain (or
// claudeCodeUnavailable) directly, so all of them stay green while the wiring
// that reaches those functions is wrong. That is not hypothetical: this plan
// shipped seven tests that passed against broken code, all of them behaviour
// tests over correct-by-reading wiring. An ordering bug and a missing
// installation are both invisible to a behaviour test by construction.

// mainSource parses main.go once for the structural assertions below.
func mainSource(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	return fset, f
}

// funcNamed returns the top-level function declaration called name.
func funcNamed(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("func %s not found in main.go", name)
	return nil
}

// callName renders a call's function as "pkg.Fn" or "recv.Method" or "Fn".
func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return "." + fn.Sel.Name
	}
	return ""
}

// firstCallOffset returns the byte offset of the first call to name inside fn,
// or -1 when it never happens.
func firstCallOffset(fset *token.FileSet, fn *ast.FuncDecl, name string) int {
	best := -1
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || callName(call) != name {
			return true
		}
		off := fset.Position(call.Pos()).Offset
		if best == -1 || off < best {
			best = off
		}
		return true
	})
	return best
}

// TestClaudeApproveIsDispatchedBeforeAnythingThatCanExit is GAP 1, structural
// half — and the one that names the regression precisely.
//
// `huginn claude-approve` is the PreToolUse hook for an unattended agent.
// Claude Code treats every exit code other than 0 and 2 as a NON-blocking
// error and runs the tool anyway, so nothing on this route may os.Exit
// non-zero. config.Load() -> fatalf() on a hand-edited config was exactly that
// bug: exit 1, no decision printed, gated Bash executed.
//
// Moving the dispatch below config.Load reintroduces the Critical while every
// behaviour test stays green, because they all call claudeApproveMain directly.
// This asserts the ORDER instead.
func TestClaudeApproveIsDispatchedBeforeAnythingThatCanExit(t *testing.T) {
	fset, f := mainSource(t)
	mainFn := funcNamed(t, f, "main")

	dispatch := firstCallOffset(fset, mainFn, "claudeApproveMain")
	if dispatch == -1 {
		t.Fatal("main() never calls claudeApproveMain: the hook subcommand is unreachable")
	}

	// Everything below can terminate the process with a code that does NOT
	// block, so all of it must happen after the dispatch.
	//
	//   flag.Parse   — flag.ExitOnError exits 2 on a bad flag, and worse,
	//                  consumes argv before we can look at it.
	//   config.Load  — its error reaches fatalf() -> os.Exit(1).
	//   fatalf       — os.Exit(1), full stop.
	//   logger.InitWithLevel / huginnDir / MigrateAgents — later preamble that
	//                  must stay later; none of it is needed to print a deny.
	for _, mustFollow := range []string{
		"flag.Parse",
		"config.Load",
		"fatalf",
		"logger.InitWithLevel",
		"huginnDir",
		"agentslib.MigrateAgents",
	} {
		off := firstCallOffset(fset, mainFn, mustFollow)
		if off == -1 {
			continue // not called in main() at all: nothing to order against
		}
		if off < dispatch {
			t.Errorf("main() calls %s at offset %d, BEFORE the claude-approve dispatch at %d.\n"+
				"That path can exit with a code Claude Code treats as a non-blocking error, "+
				"which means the gated tool RUNS UNAPPROVED. Move the dispatch back to the top of main().",
				mustFollow, off, dispatch)
		}
	}

	// And the dispatch must be the very first statement, not merely early:
	// anything ahead of it is one edit away from becoming an abort path.
	if len(mainFn.Body.List) == 0 {
		t.Fatal("main() has no body")
	}
	firstStmtEnd := fset.Position(mainFn.Body.List[0].End()).Offset
	if dispatch > firstStmtEnd {
		t.Errorf("the claude-approve dispatch is not main()'s first statement; "+
			"it sits after a statement ending at offset %d", firstStmtEnd)
	}
}

// TestClaudeApproveBinaryDeniesOnACorruptConfig is GAP 1's end-to-end half: it
// runs the REAL binary as a PreToolUse hook would, with a deliberately corrupt
// config, and checks the process contract Claude Code actually observes —
// stdout carries a deny, and the exit code is one that blocks.
//
// Hermetic: HOME and USERPROFILE point at t.TempDir(), so config.Load() can
// neither read nor rewrite the developer's real config. No --endpoint is
// passed and the config is unparseable, so the endpoint resolves to "" and the
// process denies without opening a socket. The real `claude` CLI is never run.
func TestClaudeApproveBinaryDeniesOnACorruptConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the huginn binary")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "huginn-test-bin")
	if _, err := os.Stat("go.mod"); err != nil {
		t.Skipf("not running from the module root: %v", err)
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".huginn"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A trailing comma — the exact hand-edit from the review's failure scenario.
	if err := os.WriteFile(filepath.Join(home, ".huginn", "config.json"),
		[]byte(`{"web_ui": {"port": 8421,}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctxDeadline := time.Now().Add(30 * time.Second)
	cmd := exec.Command(bin, "claude-approve")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"HUGINN_LOG_LEVEL=error",
	)
	cmd.Dir = tmp
	cmd.Stdin = strings.NewReader(hookStdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if time.Now().After(ctxDeadline) {
		t.Fatal("the hook took longer than the Claude Code hook timeout allows")
	}

	code := cmd.ProcessState.ExitCode()
	if runErr != nil && code == -1 {
		t.Fatalf("the hook process died by signal (%v); Claude Code treats that as a non-blocking error and runs the tool.\nstderr: %s", runErr, stderr.String())
	}
	// 0 (with a printed decision) and 2 are the ONLY codes that block.
	if code != 0 && code != 2 {
		t.Fatalf("exit code = %d on a corrupt config. Claude Code treats that as a NON-BLOCKING error: the gated tool RUNS UNAPPROVED.\nstdout: %s\nstderr: %s",
			code, stdout.String(), stderr.String())
	}
	if code == 0 {
		d, reason := decision(t, stdout.String())
		if d != "deny" {
			t.Fatalf("decision = %q on a corrupt config, want deny (reason %q)", d, reason)
		}
	}
	if strings.Contains(stderr.String(), "huginn: error: config:") {
		t.Errorf("the hook reached the fatalf config path; it must never load config before dispatching.\nstderr: %s", stderr.String())
	}
}

// resolverInstall names one (enclosing function, receiver, method) install site.
type resolverInstall struct{ fn, recv, method string }

// installedResolvers walks main.go and returns every place a claude-code
// backend resolver is installed on an orchestrator or thread manager.
func installedResolvers(t *testing.T) map[resolverInstall]bool {
	t.Helper()
	_, f := mainSource(t)
	found := map[resolverInstall]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "SetAgentBackendOverride" && sel.Sel.Name != "SetAgentBackendResolver" {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			found[resolverInstall{fn.Name.Name, recv.Name, sel.Sel.Name}] = true
			return true
		})
	}
	return found
}

// TestEveryOrchestratorInstallsAClaudeCodeResolver is GAP 2.
//
// TestClaudeCodeUnavailableNamesTheLimitation proves the CLOSURE is right; it
// says nothing about whether anyone installs it. Delete one of these five
// lines and the whole suite stays green, while @Codey fails with
// `unknown provider "claude-code"` on exactly one surface — the bug that gets
// reported months later as "it works in the web UI but not the TUI".
func TestEveryOrchestratorInstallsAClaudeCodeResolver(t *testing.T) {
	found := installedResolvers(t)

	required := []struct {
		site resolverInstall
		why  string
	}{
		{resolverInstall{"startServer", "orch", "SetAgentBackendOverride"},
			"the primary web-chat path: without it a claude-code agent cannot be chatted with at all"},
		{resolverInstall{"startServer", "tm", "SetAgentBackendResolver"},
			"delegated sub-threads (@Codey, delegate_to_agent): the four-tuple resolver cannot express a session binding, so this fails with `unknown provider \"claude-code\"`"},
		{resolverInstall{"main", "orch", "SetAgentBackendOverride"},
			"the interactive TUI: without it the failure is an opaque `unknown provider` instead of a message naming the server-mode limitation"},
		{resolverInstall{"main", "printOrch", "SetAgentBackendOverride"},
			"--print"},
		{resolverInstall{"main", "hlOrch", "SetAgentBackendOverride"},
			"headless mode"},
	}
	for _, req := range required {
		if !found[req.site] {
			t.Errorf("%s.%s never calls %s.\nThis is %s.\nInstalled sites: %s",
				req.site.fn, req.site.recv, req.site.method, req.why, formatInstalls(found))
		}
	}

	// The `huginn --agent <name>` path builds an ExternalBackend directly and
	// never consults ag.Provider, so it cannot install a resolver — it calls
	// claudeCodeUnavailable inline instead. Without that call a claude-code
	// agent is silently answered by an unrelated endpoint wearing its name,
	// with none of its session, tools or approval gate.
	//
	// Its signature there is distinctive — the resolver is BUILT and then
	// IMMEDIATELY APPLIED, `claudeCodeUnavailable(mode)(ag)` — which is what
	// this looks for, so the three installs above cannot stand in for it.
	_, f := mainSource(t)
	mainFn := funcNamed(t, f, "main")
	applied := false
	ast.Inspect(mainFn, func(n ast.Node) bool {
		outer, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		inner, ok := outer.Fun.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callName(inner) == "claudeCodeUnavailable" {
			applied = true
		}
		return true
	})
	if !applied {
		t.Error("main() never applies claudeCodeUnavailable to an agent: " +
			"`huginn --agent Codey` builds an ExternalBackend directly and never reads ag.Provider, " +
			"so it would answer from an unrelated endpoint wearing that agent's name")
	}
}

func formatInstalls(found map[resolverInstall]bool) string {
	var out []string
	for k := range found {
		out = append(out, fmt.Sprintf("%s:%s.%s", k.fn, k.recv, k.method))
	}
	if len(out) == 0 {
		return "(none)"
	}
	return strings.Join(out, ", ")
}
