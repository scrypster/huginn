package patch_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/patch"
)

var errFakeInvalidate = errors.New("store invalidate error")

// ---------------------------------------------------------------------------
// Validate — file read error (non-NotExist)
// ---------------------------------------------------------------------------

func TestValidate_FileReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	fpath := filepath.Join(dir, "unreadable.txt")
	if err := os.WriteFile(fpath, []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Remove read permission so ReadFile fails with a non-NotExist error.
	if err := os.Chmod(fpath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(fpath, 0o644) //nolint:errcheck

	d := patch.Diff{
		FilePath:     fpath,
		ExpectedHash: "abc123",
	}
	err := patch.Validate(d, nil)
	if err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
	// Should not be ErrStaleFile — should be a wrapped read error.
	if _, ok := err.(patch.ErrStaleFile); ok {
		t.Errorf("expected wrapped read error, got ErrStaleFile")
	}
}

// ---------------------------------------------------------------------------
// Apply — file not found (new file creation path)
// ---------------------------------------------------------------------------

func TestApply_FileNotFound_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "brand-new.txt")
	// File does not exist yet — Apply should treat it as an empty new file.
	d := patch.Diff{
		FilePath: fpath,
		Hunks: []patch.Hunk{
			{
				OldStart: 0,
				OldLines: 0,
				NewStart: 1,
				NewLines: 1,
				Lines: []patch.HunkLine{
					{Op: patch.OpAdd, Content: "hello new file"},
				},
			},
		},
	}
	if err := patch.Apply(d, nil); err != nil {
		t.Fatalf("Apply on non-existent file: %v", err)
	}
	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty file content")
	}
}

// ---------------------------------------------------------------------------
// Apply — store.Invalidate error is non-fatal
// ---------------------------------------------------------------------------

// errStoreInvalidate is a test store that returns an error from Invalidate.
type errStoreInvalidate struct{}

func (e *errStoreInvalidate) Invalidate(_ []string) error {
	return errFakeInvalidate
}

func TestApply_StoreInvalidateError_NonFatal(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(fpath, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := patch.Diff{
		FilePath: fpath,
		Hunks: []patch.Hunk{
			{
				OldStart: 1,
				OldLines: 1,
				NewStart: 1,
				NewLines: 1,
				Lines: []patch.HunkLine{
					{Op: patch.OpDelete, Content: "line1"},
					{Op: patch.OpAdd, Content: "replaced"},
				},
			},
		},
	}
	// Apply should succeed even if the store returns an error.
	if err := patch.Apply(d, &errStoreInvalidate{}); err != nil {
		t.Fatalf("Apply should not fail on store error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// applyHunks — delete line past EOF
// ---------------------------------------------------------------------------

func TestApplyHunks_DeleteLinePastEOF(t *testing.T) {
	// A hunk that tries to delete a line beyond the end of the file.
	d := patch.Diff{
		FilePath: "",
		Hunks: []patch.Hunk{
			{
				OldStart: 5, // beyond end of 2-line file
				OldLines: 1,
				NewStart: 5,
				NewLines: 0,
				Lines: []patch.HunkLine{
					{Op: patch.OpDelete, Content: "ghost-line"},
				},
			},
		},
	}
	_, err := patch.DryRun(d, []byte("line1\nline2\n"))
	if err == nil {
		t.Fatal("expected error when delete line past EOF, got nil")
	}
}

// ---------------------------------------------------------------------------
// applyHunks — context line past EOF (second context line missing)
// ---------------------------------------------------------------------------

func TestApplyHunks_ContextLinePastEOF_Second(t *testing.T) {
	d := patch.Diff{
		Hunks: []patch.Hunk{
			{
				OldStart: 1,
				OldLines: 3,
				NewStart: 1,
				NewLines: 3,
				Lines: []patch.HunkLine{
					{Op: patch.OpContext, Content: "only-line"},
					{Op: patch.OpContext, Content: "missing-line"}, // past EOF
					{Op: patch.OpContext, Content: "also-missing"},
				},
			},
		},
	}
	_, err := patch.DryRun(d, []byte("only-line\n"))
	if err == nil {
		t.Fatal("expected error for context line past EOF")
	}
}

// ---------------------------------------------------------------------------
// Apply — read error on non-NotExist file
// ---------------------------------------------------------------------------

func TestApply_FileReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	fpath := filepath.Join(dir, "noperm.txt")
	if err := os.WriteFile(fpath, []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fpath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(fpath, 0o644) //nolint:errcheck

	d := patch.Diff{
		FilePath: fpath,
		Hunks: []patch.Hunk{
			{
				OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
				Lines: []patch.HunkLine{
					{Op: patch.OpDelete, Content: "data"},
					{Op: patch.OpAdd, Content: "new"},
				},
			},
		},
	}
	err := patch.Apply(d, nil)
	if err == nil {
		t.Fatal("expected error for unreadable file in Apply")
	}
}
