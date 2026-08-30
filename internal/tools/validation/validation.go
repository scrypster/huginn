// Package validation runs cheap, model-agnostic per-language syntax checks
// against a file's would-be content — the mechanism behind edit-time syntax
// validation (G1). Callers own the write/deny decision; this package only
// answers "is this syntactically valid" for the languages it knows, and says
// so honestly when it does not check at all (unsupported extension, or a
// required external tool missing from PATH).
package validation

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// validateTimeout bounds every external validator subprocess. Syntax checks
// must be fast enough to run on every edit; a hung interpreter must never
// stall a tool call.
const validateTimeout = 5 * time.Second

// Mode selects how a caller should react to a failed Result.
type Mode string

const (
	// ModeBlock rejects the write: the tool call is denied and the file is
	// never touched. Default.
	ModeBlock Mode = "block"
	// ModeWarn allows the write to proceed but annotates the tool result
	// with the validation failure.
	ModeWarn Mode = "warn"
	// ModeOff disables syntax validation entirely.
	ModeOff Mode = "off"
)

// NormalizeMode maps a raw config string (case/whitespace-insensitive) to a
// valid Mode. Empty or unrecognized input defaults to ModeBlock — the
// documented default for .huginn/workspace.json's syntax_validation field.
func NormalizeMode(raw string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case ModeWarn:
		return ModeWarn
	case ModeOff:
		return ModeOff
	default:
		return ModeBlock
	}
}

// Result is the outcome of validating one file's would-be content.
type Result struct {
	// Checked is true only when a validator actually ran for this file's
	// language. False means "no opinion" — an unsupported extension, or a
	// required external tool (e.g. python3) was not found on PATH. Callers
	// must never treat Checked=false as a failure.
	Checked bool
	// OK is true when the content is syntactically valid. Meaningless when
	// Checked is false (content was never examined).
	OK bool
	// Message is the compiler/parser error text, set only when Checked &&
	// !OK.
	Message string
	// Lang identifies which validator ran ("go", "python", "javascript",
	// "typescript", "ruby", "php", "rust"), or the recognized-but-unchecked
	// language name when Checked is false because the required external
	// tool wasn't found. Empty when the extension was not recognized at
	// all.
	Lang string
	// SkipReason explains why Checked is false, for logging — e.g. "python3
	// not found on PATH" or "TS/JS syntax validation not implemented in v1".
	SkipReason string
}

// Validate runs a per-language syntax check against content, chosen by
// path's file extension. Unrecognized extensions, and languages whose
// required external tool is not installed, return Checked=false — never a
// hard failure. Validate itself never writes to path; content is checked
// in memory (Go), via stdin (Ruby, PHP), or via a temp file (Python,
// JS/TS/JSX/TSX, Rust) whose name is never path.
func Validate(path string, content []byte) Result {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return validateGo(path, content)
	case ".py":
		return validatePython(content)
	case ".js", ".mjs", ".cjs":
		return validateJS(content)
	case ".ts", ".tsx", ".jsx":
		return validateTSX(content, filepath.Ext(path))
	case ".rb":
		return validateRuby(content)
	case ".php":
		return validatePHP(content)
	case ".rs":
		return validateRust(content)
	default:
		return Result{Checked: false}
	}
}

// validateGo parses content as Go source using go/parser — compiled into
// the huginn binary, so unlike shelling out to `gofmt -e` it needs no
// external tool on PATH and is always available. It catches syntax errors
// (the failure class G1 targets); it does not additionally require
// gofmt-canonical formatting.
func validateGo(path string, content []byte) Result {
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, filepath.Base(path), content, parser.AllErrors); err != nil {
		return Result{Checked: true, OK: false, Lang: "go", Message: err.Error()}
	}
	return Result{Checked: true, OK: true, Lang: "go"}
}

// validatePython shells out to `python3 -m py_compile` against a temp copy
// of content. Skips gracefully (Checked=false) when python3 is not on
// PATH — never fails a write just because the validator isn't installed.
func validatePython(content []byte) Result {
	py, err := exec.LookPath("python3")
	if err != nil {
		return Result{Checked: false, Lang: "python", SkipReason: "python3 not found on PATH — syntax validation skipped"}
	}

	tmp, err := os.CreateTemp("", "huginn-pyvalidate-*.py")
	if err != nil {
		return Result{Checked: false, Lang: "python", SkipReason: "could not create temp file for validation: " + err.Error()}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return Result{Checked: false, Lang: "python", SkipReason: "could not write temp file for validation: " + err.Error()}
	}
	if err := tmp.Close(); err != nil {
		return Result{Checked: false, Lang: "python", SkipReason: "could not close temp file for validation: " + err.Error()}
	}

	cmd := exec.Command(py, "-m", "py_compile", tmpName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ReplaceAll(string(out), tmpName, "<file>")
		return Result{Checked: true, OK: false, Lang: "python", Message: strings.TrimSpace(msg)}
	}
	return Result{Checked: true, OK: true, Lang: "python"}
}

// runStdinCheck LookPath-gates bin, then runs bin(args...) with content
// piped on stdin and combined stdout+stderr captured. Used by validators
// whose tool reads a syntax check target from stdin, avoiding a temp file
// entirely (ruby -c -, php -l).
func runStdinCheck(bin string, args []string, content []byte, lang string) Result {
	path, err := exec.LookPath(bin)
	if err != nil {
		return Result{Checked: false, Lang: lang, SkipReason: bin + " not found on PATH — syntax validation skipped"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = bytes.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Checked: true, OK: false, Lang: lang, Message: strings.TrimSpace(string(out))}
	}
	return Result{Checked: true, OK: true, Lang: lang}
}

// validateRuby shells out to `ruby -c -` (syntax check, reading source from
// stdin) — ships with any ruby install, no gems required. Skips gracefully
// when ruby is not on PATH.
//
// Empirically verified: `ruby -c` exits 0 with "Syntax OK" on valid source,
// and exits 1 with a "<file>:<line>: syntax error, ..." message on broken
// source (e.g. an unclosed paren).
func validateRuby(content []byte) Result {
	return runStdinCheck("ruby", []string{"-c", "-"}, content, "ruby")
}

// validatePHP shells out to `php -l` (lint, syntax-only — no autoloading or
// execution), reading source from stdin. Ships with any php install. Skips
// gracefully when php is not on PATH.
//
// Empirically verified: `php -l` exits 0 with "No syntax errors detected"
// on valid source, and exits 255 with a "PHP Parse error: ..." message on
// broken source.
func validatePHP(content []byte) Result {
	return runStdinCheck("php", []string{"-l"}, content, "php")
}

// validateJS shells out to `node --check`, Node's built-in syntax-only
// check (no execution) — ships with any node install, no packages needed.
// Written to a temp .js file because node's syntax checker keys its parse
// mode off the file extension; content is discarded after the check
// (`Validate` never writes to the real path). Falls back to esbuild (see
// validateTSX) when node is not on PATH but esbuild is, since esbuild
// parses plain JS too. Skips gracefully when neither is available.
//
// Empirically verified: `node --check` exits 0 on valid source and exits 1
// with a "SyntaxError: ..." message (plus a stack trace, stripped below)
// on broken source. Note: `node --check` cannot parse JSX (rejects the `<`
// token) — .jsx is routed to validateTSX/esbuild instead, never here.
func validateJS(content []byte) Result {
	if _, err := exec.LookPath("node"); err == nil {
		return validateWithTempFile("node", []string{"--check"}, "huginn-jsvalidate-*.js", content, "javascript", firstLineOfNodeError)
	}
	if _, err := exec.LookPath("esbuild"); err == nil {
		return validateWithTempFile("esbuild", []string{"--log-level=error", "--outfile=/dev/null"}, "huginn-jsvalidate-*.js", content, "javascript", nil)
	}
	return Result{Checked: false, Lang: "javascript", SkipReason: "neither node nor esbuild found on PATH — JS syntax validation skipped"}
}

// validateTSX shells out to `esbuild <file> --log-level=error`, which
// parses (but does not type-check) TS/TSX/JSX — exactly the syntax-only
// check this package wants: esbuild is a transpiler, not a type checker,
// so a file with real type errors but valid grammar still exits 0.
// esbuild is not bundled with node the way `node --check` is, so this
// LookPath-gates separately and skips (rather than falling back to
// node --check, which cannot parse TS syntax or JSX at all) when esbuild
// isn't installed — a documented gap for TS/TSX/JSX when only node is
// present.
//
// Empirically verified: valid .ts/.tsx source exits 0 (including a file
// with a real type error — esbuild only transpiles); broken syntax (e.g. a
// missing closing paren) exits 1 with an "✘ [ERROR] ..." message.
func validateTSX(content []byte, ext string) Result {
	lang := "typescript"
	if ext == ".jsx" {
		lang = "javascript"
	}
	if _, err := exec.LookPath("esbuild"); err != nil {
		return Result{
			Checked:    false,
			Lang:       lang,
			SkipReason: "esbuild not found on PATH — " + strings.TrimPrefix(ext, ".") + " syntax validation skipped (node --check cannot parse TS/JSX)",
		}
	}
	pattern := "huginn-tsvalidate-*" + ext
	return validateWithTempFile("esbuild", []string{"--log-level=error", "--outfile=/dev/null"}, pattern, content, lang, nil)
}

// validateRust shells out to `rustfmt --check`. rustfmt is not a syntax
// checker by design — it also fails (exit 1) on syntactically valid but
// non-canonically-formatted source — so a raw exit code cannot distinguish
// "syntax error" from "just needs formatting". Empirically, though, the two
// cases are distinguishable by output channel: rustc-level parse errors
// (unclosed delimiter, mismatched brace, etc.) print an "error: ..." line
// to stderr, while a pure formatting diff prints only a "Diff in <file>:"
// block to stdout with nothing on stderr. Only the former is treated as a
// syntax failure here; a formatting-only diff is reported OK — this
// package checks syntax, not style.
//
// This was verified empirically against four cases: valid+formatted (exit
// 0), unclosed-delimiter syntax error (exit 1, stderr has "error:", stdout
// empty), mismatched-delimiter syntax error (exit 1, stderr has "error:"),
// and valid-but-unformatted source (exit 1, stdout has "Diff in", stderr
// empty). rustc itself was ruled out per the mission brief: a single-file
// `rustc --emit=metadata` syntax check needs a resolvable crate/dependency
// graph and is unreliable for real project files.
func validateRust(content []byte) Result {
	bin, err := exec.LookPath("rustfmt")
	if err != nil {
		return Result{Checked: false, Lang: "rust", SkipReason: "rustfmt not found on PATH — syntax validation skipped"}
	}

	tmp, err := os.CreateTemp("", "huginn-rsvalidate-*.rs")
	if err != nil {
		return Result{Checked: false, Lang: "rust", SkipReason: "could not create temp file for validation: " + err.Error()}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return Result{Checked: false, Lang: "rust", SkipReason: "could not write temp file for validation: " + err.Error()}
	}
	if err := tmp.Close(); err != nil {
		return Result{Checked: false, Lang: "rust", SkipReason: "could not close temp file for validation: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--check", "--edition", "2021", tmpName)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "error:") {
			msg := strings.ReplaceAll(stderr.String(), tmpName, "<file>")
			return Result{Checked: true, OK: false, Lang: "rust", Message: strings.TrimSpace(msg)}
		}
		// Exit non-zero with no rustc-level error on stderr: a pure
		// formatting diff on stdout. Syntax is valid.
		return Result{Checked: true, OK: true, Lang: "rust"}
	}
	return Result{Checked: true, OK: true, Lang: "rust"}
}

// validateWithTempFile writes content to a temp file matching pattern (so
// the checked tool sees the right extension), runs bin(args..., tmpFile),
// and reports the outcome. cleanMsg, if non-nil, post-processes the raw
// combined output before it becomes Result.Message (e.g. to drop a noisy
// stack trace). Used by the JS/TS/JSX family, whose tools (node, esbuild)
// both take a file argument rather than reading stdin.
func validateWithTempFile(bin string, args []string, pattern string, content []byte, lang string, cleanMsg func(string) string) Result {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return Result{Checked: false, Lang: lang, SkipReason: "could not create temp file for validation: " + err.Error()}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return Result{Checked: false, Lang: lang, SkipReason: "could not write temp file for validation: " + err.Error()}
	}
	if err := tmp.Close(); err != nil {
		return Result{Checked: false, Lang: lang, SkipReason: "could not close temp file for validation: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()

	cmdArgs := append(append([]string{}, args...), tmpName)
	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ReplaceAll(string(out), tmpName, "<file>")
		if cleanMsg != nil {
			msg = cleanMsg(msg)
		}
		return Result{Checked: true, OK: false, Lang: lang, Message: strings.TrimSpace(msg)}
	}
	return Result{Checked: true, OK: true, Lang: lang}
}

// firstLineOfNodeError trims node --check's SyntaxError output down to the
// first "SyntaxError: ..." line, dropping the file-location preview and
// module-loader stack trace that follow it — those reference the discarded
// temp file, not anything the model can act on.
func firstLineOfNodeError(msg string) string {
	for _, line := range strings.Split(msg, "\n") {
		if idx := strings.Index(line, "SyntaxError:"); idx != -1 {
			return line[idx:]
		}
	}
	return msg
}
