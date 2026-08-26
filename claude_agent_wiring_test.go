package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode"
)

// TestGatedToolsForUsesTheAgentsExplicitList: a configured list wins outright,
// with no silent union against the defaults.
func TestGatedToolsForUsesTheAgentsExplicitList(t *testing.T) {
	ag := &agentslib.Agent{ClaudeGatedTools: []string{"Bash"}}
	got := gatedToolsFor(ag)
	if len(got) != 1 || got[0] != "Bash" {
		t.Fatalf("gatedToolsFor = %v, want exactly [Bash]", got)
	}
}

// TestGatedToolsForNeverReturnsEmpty is the security-critical case.
//
// BuildHookSettings emits NO PreToolUse hooks for an empty list, so an agent
// with no configured gating would run every tool — Bash included — with no
// approval round-trip at all. This test fails if the empty case returns nil or
// an empty slice, and it proves the fallback actually produces hooks.
func TestGatedToolsForNeverReturnsEmpty(t *testing.T) {
	cases := map[string]*agentslib.Agent{
		"nil agent":         nil,
		"unset gated tools": {Name: "Unconfigured"},
		"empty gated tools": {Name: "Empty", ClaudeGatedTools: []string{}},
		// LocalTools must not be consulted: it is Huginn's namespace, and its
		// "*" wildcard means "all Huginn builtins", never "no Claude gating".
		"wildcard local tools": {Name: "Wild", LocalTools: []string{"*"}},
	}
	for name, ag := range cases {
		t.Run(name, func(t *testing.T) {
			got := gatedToolsFor(ag)
			if len(got) == 0 {
				t.Fatal("gatedToolsFor returned an empty set: BuildHookSettings would emit no hooks and the agent would run ungated")
			}
			settings, err := claudecode.BuildHookSettings(got, "huginn claude-approve")
			if err != nil {
				t.Fatalf("BuildHookSettings: %v", err)
			}
			if settings == "" {
				t.Fatal("the fallback gated set produced no --settings payload, i.e. no approval hook")
			}
			var sawBash bool
			for _, tool := range got {
				if tool == "Bash" {
					sawBash = true
				}
			}
			if !sawBash {
				t.Errorf("default gated set does not gate Bash: %v", got)
			}
		})
	}
}

// TestGatedToolsForDoesNotAliasTheAgentSlice: the returned slice must be a
// copy, or a caller mutating it would rewrite the agent's stored policy.
func TestGatedToolsForDoesNotAliasTheAgentSlice(t *testing.T) {
	ag := &agentslib.Agent{ClaudeGatedTools: []string{"Bash", "Write"}}
	got := gatedToolsFor(ag)
	got[0] = "Read"
	if ag.ClaudeGatedTools[0] != "Bash" {
		t.Fatalf("agent's gated tools mutated through the returned slice: %v", ag.ClaudeGatedTools)
	}

	def := gatedToolsFor(&agentslib.Agent{})
	def[0] = "Read"
	if claudecode.DefaultGatedTools[0] == "Read" {
		t.Fatal("package-level DefaultGatedTools mutated through the returned slice")
	}
}

func TestClaudeSessionExistsUnder(t *testing.T) {
	root := t.TempDir()
	const cwd = "/Users/dev/project"
	proj := filepath.Join(root, "-Users-dev-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "11111111-2222-3333-4444-555555555555"
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if !claudeSessionExistsUnder(root, cwd, id) {
		t.Error("an existing transcript nested under a project dir was not found; the first turn would try to claim an id the CLI already owns")
	}
	if claudeSessionExistsUnder(root, cwd, "99999999-9999-4999-8999-999999999999") {
		t.Error("reported a session that does not exist; the turn would --resume a session Claude Code has never created")
	}
	if claudeSessionExistsUnder(root, cwd, "") {
		t.Error("an empty session id must never count as existing")
	}
	if claudeSessionExistsUnder("", cwd, id) {
		t.Error("an empty root must never count as existing")
	}
	if claudeSessionExistsUnder(filepath.Join(root, "missing"), cwd, id) {
		t.Error("a missing root must not report the session as existing")
	}
}

// TestClaudeSessionExistsFallsBackWhenTheProjectDirGuessIsWrong is the reason
// the fast path is only a fast path. The CLI's directory naming is verified
// only for the separator rule, so a cwd whose derived name does not match must
// still find the transcript — a false "does not exist" makes the next turn
// claim a session id Claude Code already owns.
func TestClaudeSessionExistsFallsBackWhenTheProjectDirGuessIsWrong(t *testing.T) {
	root := t.TempDir()
	// Deliberately NOT the name claudeProjectDirName would derive.
	proj := filepath.Join(root, "some-name-we-did-not-predict")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "22222222-3333-4444-5555-666666666666"
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if !claudeSessionExistsUnder(root, "/Users/dev/some.dotted.dir", id) {
		t.Error("the walk fallback did not run: a mis-derived project dir must not be reported as a missing session")
	}
	// And with no cwd at all there is nothing to derive, so the walk is the
	// only path available.
	if !claudeSessionExistsUnder(root, "", id) {
		t.Error("an empty cwd must still find the transcript via the walk")
	}
}

// TestClaudeProjectDirNameMatchesRealDirectories pins the rule against names
// taken from an actual ~/.claude/projects: separators become dashes, and an
// existing dash in the path survives (hence the doubled dash).
func TestClaudeProjectDirNameMatchesRealDirectories(t *testing.T) {
	cases := map[string]string{
		"/Users/me/Development/huginn": "-Users-me-Development-huginn",
		"/private/tmp":                 "-private-tmp",
		"/private/tmp/-Users-me/x":     "-private-tmp--Users-me-x",
		"/Users/me/Development/":       "-Users-me-Development",
	}
	for cwd, want := range cases {
		if got := claudeProjectDirName(cwd); got != want {
			t.Errorf("claudeProjectDirName(%q) = %q, want %q", cwd, got, want)
		}
	}
	if got := claudeProjectDirName(""); got != "" {
		t.Errorf("empty cwd = %q, want empty so the caller skips the fast path", got)
	}
}

// TestClaudeHookCommandQuotesThePath: Claude Code runs the hook through a
// shell, so an unquoted path with a space is split into two words and the hook
// never runs — every gated tool silently stops working (fails closed, but
// broken). "/Applications/My Apps/huginn" is an ordinary macOS path.
func TestClaudeHookCommandQuotesThePath(t *testing.T) {
	const spaced = "/Users/ada lovelace/bin/huginn"

	posix := claudeHookCommand(spaced, "darwin", "")
	if !strings.HasSuffix(posix, " claude-approve") {
		t.Fatalf("hook command lost its subcommand: %q", posix)
	}
	// The shell must see ONE word for the binary. Strip the subcommand, then
	// the surviving token has to be quoted as a unit.
	quoted := strings.TrimSuffix(posix, " claude-approve")
	if quoted != "'"+spaced+"'" {
		t.Errorf("posix hook command = %q, want the path single-quoted as one word", posix)
	}
	if strings.HasPrefix(posix, "/Users/ada ") {
		t.Error("path left unquoted: the shell would run /Users/ada with 'lovelace/bin/huginn' as an argument")
	}

	// An apostrophe in a path is legal on macOS and Linux and must not break
	// out of the quoting.
	tricky := claudeHookCommand("/Users/o'brien/huginn", "linux", "")
	if tricky != `'/Users/o'\''brien/huginn' claude-approve` {
		t.Errorf("single quote not escaped: %q", tricky)
	}

	win := claudeHookCommand(`C:\Program Files\Huginn\huginn.exe`, "windows", "")
	if win != `"C:\Program Files\Huginn\huginn.exe" claude-approve` {
		t.Errorf("windows hook command = %q, want the path double-quoted", win)
	}

	// A path with no space must still be quoted rather than special-cased:
	// one rule is easier to keep correct than two.
	plain := claudeHookCommand("/usr/local/bin/huginn", "darwin", "")
	if plain != "'/usr/local/bin/huginn' claude-approve" {
		t.Errorf("plain hook command = %q", plain)
	}
}

func TestClaudeSessionIDsOf(t *testing.T) {
	if got := claudeSessionIDsOf(nil); got != nil {
		t.Errorf("nil config = %v, want nil", got)
	}
	cfg := &agentslib.AgentsConfig{Agents: []agentslib.AgentDef{
		{Name: "Bound", ClaudeSessionID: "sess-1"},
		{Name: "Native"},
		{Name: "AlsoBound", ClaudeSessionID: "sess-2"},
	}}
	got := claudeSessionIDsOf(cfg)
	if len(got) != 2 || got[0] != "sess-1" || got[1] != "sess-2" {
		t.Fatalf("claudeSessionIDsOf = %v, want [sess-1 sess-2]", got)
	}
}

func TestIsClaudeUUID(t *testing.T) {
	good := []string{
		"11111111-2222-3333-4444-555555555555",
		"AABBCCDD-1122-4333-8444-abcdefABCDEF",
	}
	for _, s := range good {
		if !isClaudeUUID(s) {
			t.Errorf("isClaudeUUID(%q) = false, want true", s)
		}
	}
	bad := map[string]string{
		"":                                      "empty",
		"not-a-uuid":                            "too short",
		"11111111-2222-3333-4444-55555555555":   "35 chars",
		"11111111-2222-3333-4444-5555555555555": "37 chars",
		"1111111122223333-4444-555555555555":    "dash in the wrong place",
		"1111111g-2222-3333-4444-555555555555":  "non-hex digit",
		"11111111-2222-3333-4444-55555555555 ":  "trailing space",
	}
	for s, why := range bad {
		if isClaudeUUID(s) {
			t.Errorf("isClaudeUUID(%q) = true, want false (%s)", s, why)
		}
	}
}

// TestValidateClaudeBindingRejectsUnusableSessions: a bad binding produces
// three unrelated-looking symptoms (no continuity, no serialisation, every
// approval denied), so it has to be caught at the binding, with a message that
// names the cause.
func TestValidateClaudeBindingRejectsUnusableSessions(t *testing.T) {
	if err := validateClaudeBinding("Codey", "11111111-2222-3333-4444-555555555555"); err != nil {
		t.Fatalf("a valid binding was rejected: %v", err)
	}

	for _, id := range []string{"", "   "} {
		err := validateClaudeBinding("Codey", id)
		if err == nil {
			t.Fatalf("empty session id %q accepted", id)
		}
		if !strings.Contains(err.Error(), "Codey") || !strings.Contains(err.Error(), "claude_session_id") {
			t.Errorf("error must name the agent and the field: %v", err)
		}
	}

	// A malformed id is treated exactly like a missing one: the CLI requires a
	// UUID and the approval endpoint matches literally, so it fails the same
	// three ways.
	err := validateClaudeBinding("Codey", "session-one")
	if err == nil {
		t.Fatal("a non-UUID session id was accepted; Claude Code's --session-id would reject it with an opaque error instead")
	}
	if !strings.Contains(err.Error(), "session-one") {
		t.Errorf("error must quote the offending value: %v", err)
	}

	// An unnamed agent must still produce a usable message.
	if err := validateClaudeBinding("", ""); err == nil || !strings.Contains(err.Error(), "unnamed") {
		t.Errorf("unnamed agent error = %v, want it to say so rather than quoting an empty name", err)
	}
}

func TestClaudeBindingProblemsOnlyFlagsClaudeCodeAgents(t *testing.T) {
	cfg := &agentslib.AgentsConfig{Agents: []agentslib.AgentDef{
		{Name: "Tom", Provider: "anthropic"},                                  // not a claude-code agent: never flagged
		{Name: "Bad", Provider: "claude-code"},                                // no session id
		{Name: "Malformed", Provider: "claude-code", ClaudeSessionID: "nope"}, // not a UUID
		{Name: "Good", Provider: "claude-code", ClaudeSessionID: "11111111-2222-3333-4444-555555555555"},
	}}
	got := claudeBindingProblems(cfg)
	if len(got) != 2 {
		t.Fatalf("claudeBindingProblems returned %d problems, want 2: %v", len(got), got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"Bad", "Malformed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q not reported: %v", want, got)
		}
	}
	for _, unwanted := range []string{"Tom", "Good"} {
		if strings.Contains(joined, `"`+unwanted+`"`) {
			t.Errorf("%q reported but its binding is fine (or it is not a claude-code agent): %v", unwanted, got)
		}
	}
	if claudeBindingProblems(nil) != nil {
		t.Error("nil config must report no problems")
	}
}

// TestClaudeHookCommandCarriesTheBoundEndpoint pins Finding 2 at the build
// site. The hook process must be TOLD where Huginn is, because it runs in a
// separate process that would otherwise re-derive the port from config — and
// config says 0 whenever the user asked for dynamic allocation.
func TestClaudeHookCommandCarriesTheBoundEndpoint(t *testing.T) {
	// A dynamically-allocated port: nothing in config could have produced this.
	ep := claudeApproveEndpointFor("127.0.0.1:53412")
	if ep != "http://127.0.0.1:53412/api/v1/claude/approve" {
		t.Fatalf("claudeApproveEndpointFor = %q", ep)
	}
	if got := claudeApproveEndpointFor(""); got != "" {
		t.Errorf("claudeApproveEndpointFor(\"\") = %q, want \"\" so the caller refuses instead of guessing a port", got)
	}

	posix := claudeHookCommand("/usr/local/bin/huginn", "darwin", ep)
	if posix != "'/usr/local/bin/huginn' claude-approve --endpoint 'http://127.0.0.1:53412/api/v1/claude/approve'" {
		t.Errorf("posix hook command = %q, want the real bound endpoint baked in", posix)
	}
	if strings.Contains(posix, ":0/") {
		t.Error("hook command points at port 0: every gated tool would be denied 'Huginn unreachable'")
	}

	win := claudeHookCommand(`C:\Huginn\huginn.exe`, "windows", ep)
	if !strings.Contains(win, `--endpoint "`+ep+`"`) {
		t.Errorf("windows hook command = %q, want the endpoint quoted", win)
	}

	// The endpoint the hook is handed must be the one it actually parses back.
	cmd := claudeHookCommand("/usr/local/bin/huginn", "darwin", ep)
	fields := strings.Split(cmd, "--endpoint ")
	if len(fields) != 2 {
		t.Fatalf("hook command has no --endpoint: %q", cmd)
	}
	if got := endpointFromArgs([]string{"--endpoint", strings.Trim(fields[1], "'")}); got != ep {
		t.Errorf("round-trip endpoint = %q, want %q", got, ep)
	}
}
