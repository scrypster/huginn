package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the PROCESS-LEVEL entry point, not runClaudeApprove.
//
// The invariant: `huginn claude-approve` can never exit with a code that lets
// the tool run. Claude Code blocks on exit 0 + a JSON deny, and on exit 2, and
// on NOTHING ELSE — 1, 127 and "killed" are all non-blocking errors that let
// the action proceed. Every test below asserts BOTH the decision and an exit
// code that actually blocks.

// blockingExit reports whether code is one Claude Code actually treats as a
// refusal. Deliberately narrow: 0 only counts when a deny was printed, which
// each caller checks separately.
func blockingExit(code int) bool { return code == 0 || code == 2 }

// writeHuginnConfig points HOME at a temp dir and writes body to
// ~/.huginn/config.json. Redirecting HOME is mandatory — config.Load()
// otherwise reads (and LoadFrom may rewrite) the developer's real config.
func writeHuginnConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".huginn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestClaudeApproveDeniesOnCorruptConfig is Finding 1's headline case: a
// hand-edited ~/.huginn/config.json with a trailing comma used to reach
// config.Load() in main()'s preamble, fatalf, and exit 1 — before
// runClaudeApprove ever ran. Exit 1 is a NON-blocking error, so the gated tool
// ran unapproved.
func TestClaudeApproveDeniesOnCorruptConfig(t *testing.T) {
	writeHuginnConfig(t, `{"web_ui": {"port": 8421,}}`) // trailing comma

	var out bytes.Buffer
	code := claudeApproveMain(nil, strings.NewReader(hookStdin), &out)

	if !blockingExit(code) {
		t.Fatalf("exit code = %d, which Claude Code treats as a non-blocking error and RUNS THE TOOL", code)
	}
	d, reason := decision(t, out.String())
	if d != "deny" {
		t.Fatalf("decision = %q, want deny — an unreadable config must never approve", d)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0: a printed decision is only parsed on exit 0", code)
	}
	if !strings.Contains(reason, "endpoint") {
		t.Errorf("reason = %q, want it to name the endpoint as the problem", reason)
	}
}

// TestClaudeApproveDeniesWhenTheConfigPortIsDynamic pins Finding 2 on the hook
// side: web_ui.port 0 means "the server picked a port", so config cannot say
// where it landed. Guessing a port would deny every gated tool while the
// server sat healthy elsewhere; refusing to guess denies explicitly instead.
func TestClaudeApproveDeniesWhenTheConfigPortIsDynamic(t *testing.T) {
	writeHuginnConfig(t, `{"version":14,"web_ui":{"enabled":true,"bind":"127.0.0.1","port":0}}`)

	if ep := claudeApproveEndpoint(nil); ep != "" {
		t.Fatalf("claudeApproveEndpoint = %q, want \"\" — port 0 is not an address, and http://127.0.0.1:0/ is unreachable", ep)
	}

	var out bytes.Buffer
	code := claudeApproveMain(nil, strings.NewReader(hookStdin), &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if d, _ := decision(t, out.String()); d != "deny" {
		t.Fatalf("decision = %q, want deny", d)
	}
}

// TestClaudeApproveUsesTheEndpointFlag is the other half of Finding 2: the
// generated hook command carries the server's real bound address, and it must
// be preferred over anything config says. The test server here stands in for a
// dynamically-allocated port.
func TestClaudeApproveUsesTheEndpointFlag(t *testing.T) {
	writeHuginnConfig(t, `{"version":14,"web_ui":{"enabled":true,"bind":"127.0.0.1","port":0}}`)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"decision":"allow","reason":"ok"}`))
	}))
	defer srv.Close()

	for _, args := range [][]string{
		{"--endpoint", srv.URL + claudeApprovePath},
		{"--endpoint=" + srv.URL + claudeApprovePath},
		{"-endpoint", srv.URL + claudeApprovePath},
	} {
		gotPath = ""
		var out bytes.Buffer
		if code := claudeApproveMain(args, strings.NewReader(hookStdin), &out); code != 0 {
			t.Fatalf("%v: exit code = %d, want 0", args, code)
		}
		if d, _ := decision(t, out.String()); d != "allow" {
			t.Fatalf("%v: decision = %q, want allow — the flag endpoint was not used", args, d)
		}
		if gotPath != claudeApprovePath {
			t.Fatalf("%v: server saw path %q, want %q", args, gotPath, claudeApprovePath)
		}
	}
}

// errWriter fails every write, standing in for a closed or broken stdout.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("stdout is gone") }

// TestClaudeApproveExitsTwoWhenItCannotPrint covers the one case where there
// is no decision to read: 2 is the ONLY exit code Claude Code treats as
// blocking, so it is the correct failure mode when we have no other voice.
func TestClaudeApproveExitsTwoWhenItCannotPrint(t *testing.T) {
	writeHuginnConfig(t, `{ not json at all `)

	code := claudeApproveMain(nil, strings.NewReader(hookStdin), errWriter{})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 — with no printable decision, 2 is the only code that blocks; %d lets the tool run", code, code)
	}
}

// panicReader panics on Read, standing in for any unexpected panic on the
// hook's route.
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("stdin exploded") }

// TestClaudeApproveRecoversFromAPanicAndDenies is the last-resort guard: an
// unrecovered panic exits 2 today by luck of Go's runtime, but a recovered one
// can name the tool and deny explicitly. Either way it must never be one of
// the exit codes that let the action proceed.
func TestClaudeApproveRecoversFromAPanicAndDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"decision":"allow"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	code := claudeApproveMain([]string{"--endpoint", srv.URL}, panicReader{}, &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 with a printed deny", code)
	}
	d, reason := decision(t, out.String())
	if d != "deny" {
		t.Fatalf("decision = %q, want deny — a panicking hook must not approve", d)
	}
	if !strings.Contains(reason, "crashed") {
		t.Errorf("reason = %q, want it to say the hook crashed", reason)
	}
}

// TestClaudeApproveNeverSendsToolInput is Finding 3. The handler decoded
// tool_input and discarded it, while the route caps the body at 1 MiB — so a
// Write of a large file blew the cap, the decode failed, and an explicitly
// allowlisted tool was DENIED. Permission must not depend on payload size.
//
// The test server applies the same 1 MiB cap the real route does.
func TestClaudeApproveNeverSendsToolInput(t *testing.T) {
	const cap1MiB = 1 << 20
	huge := strings.Repeat("x", 2<<20) // 2 MiB of file content
	stdin := fmt.Sprintf(
		`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_use_id":"tu1","session_id":"s1","cwd":"/tmp","tool_input":{"file_path":"/tmp/big","content":%q}}`,
		huge)

	var sawToolInput bool
	var bodyLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, cap1MiB)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			// This is exactly the old failure: over the cap, so deny.
			w.Write([]byte(`{"decision":"deny","reason":"Huginn could not parse the approval request"}`))
			return
		}
		bodyLen = len(raw)
		var probe map[string]json.RawMessage
		_ = json.Unmarshal(raw, &probe)
		_, sawToolInput = probe["tool_input"]
		w.Write([]byte(`{"decision":"allow","reason":"Write is allowed"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	code := claudeApproveMain([]string{"--endpoint", srv.URL}, strings.NewReader(stdin), &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if sawToolInput {
		t.Errorf("the hook forwarded tool_input; nothing reads it and it is what blows the 1 MiB cap")
	}
	if bodyLen >= cap1MiB {
		t.Errorf("request body = %d bytes, must stay under the %d-byte cap regardless of tool input size", bodyLen, cap1MiB)
	}
	if d, reason := decision(t, out.String()); d != "allow" {
		t.Fatalf("decision = %q (%s), want allow — an allowlisted tool must not be denied for writing a big file", d, reason)
	}
}
