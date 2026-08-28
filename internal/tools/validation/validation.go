// Package validation runs cheap, model-agnostic per-language syntax checks
// against a file's would-be content — the mechanism behind edit-time syntax
// validation (G1). Callers own the write/deny decision; this package only
// answers "is this syntactically valid" for the languages it knows, and says
// so honestly when it does not check at all (unsupported extension, or a
// required external tool missing from PATH).
package validation

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	// Lang identifies which validator ran ("go", "python"), or the
	// recognized-but-unsupported language name for a documented gap
	// ("ts/js"). Empty when the extension was not recognized at all.
	Lang string
	// SkipReason explains why Checked is false, for logging — e.g. "python3
	// not found on PATH" or "TS/JS syntax validation not implemented in v1".
	SkipReason string
}

// Validate runs a per-language syntax check against content, chosen by
// path's file extension. Unrecognized extensions, and languages whose
// required external tool is not installed, return Checked=false — never a
// hard failure. Validate itself never writes to path; content is checked
// in memory (Go) or via a temp file (Python).
func Validate(path string, content []byte) Result {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return validateGo(path, content)
	case ".py":
		return validatePython(content)
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		// Documented gap: tsc is too heavy to shell out to on every edit,
		// and there is no cheap syntax-only JS/TS parser wired in v1. Skip
		// rather than pretend to check.
		return Result{
			Checked:    false,
			Lang:       "ts/js",
			SkipReason: "TS/JS syntax validation not implemented in v1 (tsc is heavy; no cheap parser wired) — writes to these files are not checked",
		}
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
