package patch_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/patch"
)

const simpleDiff = `--- a/hello.txt
+++ b/hello.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2-modified
 line3
`

func TestParse_Simple(t *testing.T) {
	diffs, err := patch.Parse(simpleDiff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	d := diffs[0]
	if d.FilePath != "hello.txt" {
		t.Errorf("expected path hello.txt, got %q", d.FilePath)
	}
	if len(d.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(d.Hunks))
	}
	hunk := d.Hunks[0]
	if hunk.OldStart != 1 {
		t.Errorf("OldStart: got %d, want 1", hunk.OldStart)
	}
	if len(hunk.Lines) != 4 {
		t.Errorf("expected 4 hunk lines, got %d", len(hunk.Lines))
	}
}

func TestDryRun(t *testing.T) {
	diffs, _ := patch.Parse(simpleDiff)
	original := []byte("line1\nline2\nline3\n")
	result, err := patch.DryRun(diffs[0], original)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	got := string(result)
	if !containsLine(got, "line2-modified") {
		t.Errorf("expected line2-modified in result, got:\n%s", got)
	}
	if containsLine(got, "line2\n") {
		t.Errorf("expected line2 to be removed, got:\n%s", got)
	}
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "hello.txt")
	original := "line1\nline2\nline3\n"
	if err := os.WriteFile(fpath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	diff := `--- a/` + fpath + `
+++ b/` + fpath + `
@@ -1,3 +1,3 @@
 line1
-line2
+line2-modified
 line3
`
	diffs, err := patch.Parse(diff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("no diffs parsed")
	}
	d := diffs[0]

	if err := patch.Apply(d, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	data, _ := os.ReadFile(fpath)
	if !containsLine(string(data), "line2-modified") {
		t.Errorf("expected line2-modified after Apply, got:\n%s", data)
	}
}

func TestValidate_StaleFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(fpath, []byte("original content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	d := patch.Diff{
		FilePath:     fpath,
		ExpectedHash: "deadbeefdeadbeefdeadbeefdeadbeef", // wrong hash
	}
	err := patch.Validate(d, nil)
	if err == nil {
		t.Fatal("expected ErrStaleFile, got nil")
	}
	if _, ok := err.(patch.ErrStaleFile); !ok {
		t.Errorf("expected ErrStaleFile, got %T: %v", err, err)
	}
}

func TestValidate_NoHash(t *testing.T) {
	d := patch.Diff{FilePath: "/nonexistent", ExpectedHash: ""}
	if err := patch.Validate(d, nil); err != nil {
		t.Errorf("expected nil with empty hash, got %v", err)
	}
}

func containsLine(s, sub string) bool {
	for _, line := range splitLines(s) {
		if line == sub {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range splitByNewline(s) {
		lines = append(lines, l)
	}
	return lines
}

func splitByNewline(s string) []string {
	var result []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

// sha256hexOf computes hex SHA-256 of data for test setup.
func sha256hexOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// mockStore is a test Store that records invalidated paths.
type mockStore struct {
	invalidated []string
	returnErr   error
}

func (s *mockStore) Invalidate(paths []string) error {
	s.invalidated = append(s.invalidated, paths...)
	return s.returnErr
}

// --- New patch tests ---

// TestParse_Empty verifies that an empty diff string returns an empty slice.
func TestParse_Empty(t *testing.T) {
	diffs, err := patch.Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs for empty input, got %d", len(diffs))
	}
}

// TestParse_MultiFile verifies that a diff with two files produces two Diff entries.
func TestParse_MultiFile(t *testing.T) {
	diff := `--- a/file1.txt
+++ b/file1.txt
@@ -1,2 +1,2 @@
 context
-old1
+new1
--- a/file2.txt
+++ b/file2.txt
@@ -1,2 +1,2 @@
 context
-old2
+new2
`
	diffs, err := patch.Parse(diff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}
	if diffs[0].FilePath != "file1.txt" {
		t.Errorf("expected file1.txt, got %q", diffs[0].FilePath)
	}
	if diffs[1].FilePath != "file2.txt" {
		t.Errorf("expected file2.txt, got %q", diffs[1].FilePath)
	}
}

// TestParse_HunkHeaderVariants verifies that all four hunk header formats parse.
func TestParse_HunkHeaderVariants(t *testing.T) {
	cases := []struct {
		name   string
		header string
		old, n int
	}{
		{"full", "@@ -1,3 +1,3 @@", 3, 3},
		{"new_single", "@@ -1,3 +1 @@", 3, 1},
		{"old_single", "@@ -1 +1,3 @@", 1, 3},
		{"both_single", "@@ -1 +1 @@", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff := "--- a/f.txt\n+++ b/f.txt\n" + tc.header + "\n"
			diffs, err := patch.Parse(diff)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(diffs) == 0 {
				t.Fatal("expected at least one diff")
			}
			if len(diffs[0].Hunks) == 0 {
				t.Fatal("expected at least one hunk")
			}
			h := diffs[0].Hunks[0]
			if h.OldLines != tc.old {
				t.Errorf("OldLines: want %d, got %d", tc.old, h.OldLines)
			}
			if h.NewLines != tc.n {
				t.Errorf("NewLines: want %d, got %d", tc.n, h.NewLines)
			}
		})
	}
}

// TestParse_BadHunkHeader verifies that a malformed hunk header returns an error.
func TestParse_BadHunkHeader(t *testing.T) {
	diff := "--- a/f.txt\n+++ b/f.txt\n@@ bogus @@\n"
	_, err := patch.Parse(diff)
	if err == nil {
		t.Fatal("expected error for bad hunk header, got nil")
	}
}

// TestParse_UnicodeContent verifies that unicode lines are handled correctly.
func TestParse_UnicodeContent(t *testing.T) {
	diff := `--- a/unicode.txt
+++ b/unicode.txt
@@ -1,2 +1,2 @@
 héllo wörld
-café
+Ünïcödé
`
	diffs, err := patch.Parse(diff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("expected diffs")
	}
	found := false
	for _, hl := range diffs[0].Hunks[0].Lines {
		if hl.Content == "Ünïcödé" {
			found = true
		}
	}
	if !found {
		t.Error("expected unicode add line in hunk")
	}
}

// TestParse_PathWithoutGitPrefix verifies that paths without a/ or b/ are kept verbatim.
func TestParse_PathWithoutGitPrefix(t *testing.T) {
	diff := "--- myfile.txt\n+++ myfile.txt\n@@ -1 +1 @@\n-old\n+new\n"
	diffs, err := patch.Parse(diff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff")
	}
	if diffs[0].FilePath != "myfile.txt" {
		t.Errorf("expected myfile.txt, got %q", diffs[0].FilePath)
	}
}

// TestDryRun_PureAddition verifies applying a pure addition hunk to an empty file.
func TestDryRun_PureAddition(t *testing.T) {
	// Diff that adds lines to an empty file (OldLines=0)
	diff := `--- a/new.txt
+++ b/new.txt
@@ -0,0 +1,2 @@
+line1
+line2
`
	diffs, err := patch.Parse(diff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff")
	}
	result, err := patch.DryRun(diffs[0], []byte(""))
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	got := string(result)
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Errorf("expected line1 and line2 in result, got:\n%s", got)
	}
}

// TestDryRun_ContextPastEOF verifies that a context line that references a line
// beyond the file end returns an error.
func TestDryRun_ContextPastEOF(t *testing.T) {
	// The file has 1 line but the hunk claims 3 context lines.
	d := patch.Diff{
		FilePath: "",
		Hunks: []patch.Hunk{
			{
				OldStart: 1,
				OldLines: 3,
				NewStart: 1,
				NewLines: 3,
				Lines: []patch.HunkLine{
					{Op: patch.OpContext, Content: "line1"},
					{Op: patch.OpContext, Content: "line2"}, // this line doesn't exist
					{Op: patch.OpContext, Content: "line3"},
				},
			},
		},
	}
	_, err := patch.DryRun(d, []byte("line1\n"))
	if err == nil {
		t.Fatal("expected error when context line past EOF, got nil")
	}
}

// TestApply_HashValidation verifies that Apply checks the hash and returns
// ErrStaleFile when the file content doesn't match.
func TestApply_HashValidation(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "file.txt")
	content := []byte("hello world\n")
	if err := os.WriteFile(fpath, content, 0644); err != nil {
		t.Fatal(err)
	}

	d := patch.Diff{
		FilePath:     fpath,
		ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000000",
		Hunks: []patch.Hunk{
			{
				OldStart: 1,
				OldLines: 1,
				NewStart: 1,
				NewLines: 1,
				Lines: []patch.HunkLine{
					{Op: patch.OpDelete, Content: "hello world"},
					{Op: patch.OpAdd, Content: "goodbye world"},
				},
			},
		},
	}
	err := patch.Apply(d, nil)
	if err == nil {
		t.Fatal("expected ErrStaleFile, got nil")
	}
	var stale patch.ErrStaleFile
	if !errors.As(err, &stale) {
		t.Errorf("expected ErrStaleFile, got %T: %v", err, err)
	}
}

// TestApply_CorrectHash verifies that Apply succeeds when the hash matches.
func TestApply_CorrectHash(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "file.txt")
	content := []byte("hello world\n")
	if err := os.WriteFile(fpath, content, 0644); err != nil {
		t.Fatal(err)
	}

	hash := sha256hexOf(content)

	d := patch.Diff{
		FilePath:     fpath,
		ExpectedHash: hash,
		Hunks: []patch.Hunk{
			{
				OldStart: 1,
				OldLines: 1,
				NewStart: 1,
				NewLines: 1,
				Lines: []patch.HunkLine{
					{Op: patch.OpDelete, Content: "hello world"},
					{Op: patch.OpAdd, Content: "goodbye world"},
				},
			},
		},
	}
	if err := patch.Apply(d, nil); err != nil {
		t.Fatalf("Apply with correct hash: %v", err)
	}
}

// TestApply_StoreInvalidated verifies that Apply calls store.Invalidate.
func TestApply_StoreInvalidated(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "file.txt")
	content := []byte("line1\nline2\n")
	if err := os.WriteFile(fpath, content, 0644); err != nil {
		t.Fatal(err)
	}

	store := &mockStore{}
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
					{Op: patch.OpAdd, Content: "REPLACED"},
				},
			},
		},
	}
	if err := patch.Apply(d, store); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(store.invalidated) == 0 {
		t.Error("expected store.Invalidate to be called")
	}
	if store.invalidated[0] != fpath {
		t.Errorf("expected invalidated path %q, got %q", fpath, store.invalidated[0])
	}
}

// TestApply_NewFileCreated verifies that Apply creates a new file when it
// doesn't exist yet (pure addition diff to empty/nonexistent file).
func TestApply_NewFileCreated(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "subdir", "newfile.txt")

	d := patch.Diff{
		FilePath: fpath,
		Hunks: []patch.Hunk{
			{
				OldStart: 0,
				OldLines: 0,
				NewStart: 1,
				NewLines: 1,
				Lines: []patch.HunkLine{
					{Op: patch.OpAdd, Content: "brand new content"},
				},
			},
		},
	}
	if err := patch.Apply(d, nil); err != nil {
		t.Fatalf("Apply (new file): %v", err)
	}
	data, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "brand new content") {
		t.Errorf("expected 'brand new content', got %q", string(data))
	}
}

// TestApply_OverlappingHunks verifies that out-of-order hunks return an error.
func TestApply_OverlappingHunks(t *testing.T) {
	d := patch.Diff{
		FilePath: "",
		Hunks: []patch.Hunk{
			{OldStart: 5, OldLines: 1, NewStart: 5, NewLines: 1,
				Lines: []patch.HunkLine{{Op: patch.OpContext, Content: "x"}}},
			{OldStart: 3, OldLines: 1, NewStart: 3, NewLines: 1,
				Lines: []patch.HunkLine{{Op: patch.OpContext, Content: "y"}}},
		},
	}
	_, err := patch.DryRun(d, []byte("a\nb\nc\nd\ne\nf\n"))
	if err == nil {
		t.Fatal("expected error for overlapping/out-of-order hunks")
	}
}

// TestValidate_MissingFile verifies that Validate with a hash and missing file returns ErrStaleFile.
func TestValidate_MissingFile(t *testing.T) {
	d := patch.Diff{
		FilePath:     "/nonexistent/path/file.txt",
		ExpectedHash: "abc123",
	}
	err := patch.Validate(d, nil)
	if err == nil {
		t.Fatal("expected ErrStaleFile for missing file, got nil")
	}
	var stale patch.ErrStaleFile
	if !errors.As(err, &stale) {
		t.Errorf("expected ErrStaleFile, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected 'missing' in error, got: %v", err)
	}
}

// TestErrStaleFile_ErrorMessage verifies the ErrStaleFile.Error() truncation.
func TestErrStaleFile_ErrorMessage(t *testing.T) {
	e := patch.ErrStaleFile{
		Path:     "foo.txt",
		Expected: "abcdefghijklmnop",
		Actual:   "1234567890abcdef",
	}
	msg := e.Error()
	if !strings.Contains(msg, "foo.txt") {
		t.Error("expected path in error message")
	}
	// Hash should be truncated to 8 chars
	if strings.Contains(msg, "abcdefghijklmnop") {
		t.Error("expected expected hash to be truncated to 8 chars")
	}
	if !strings.Contains(msg, "abcdefgh") {
		t.Error("expected first 8 chars of expected hash")
	}
}

// TestParse_EmptyHunk verifies that a diff with a hunk header but no lines parses.
func TestParse_EmptyHunk(t *testing.T) {
	// A hunk claiming 0,0 lines (new file marker in some tools)
	diff := "--- a/empty.txt\n+++ b/empty.txt\n@@ -0,0 +0,0 @@\n"
	diffs, err := patch.Parse(diff)
	if err != nil {
		t.Fatalf("Parse empty hunk: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff")
	}
}

// TestParse_WithTimestampSuffix verifies that tab-separated timestamps are stripped.
func TestParse_WithTimestampSuffix(t *testing.T) {
	diff := "--- a/file.txt\t2024-01-01 00:00:00.000000000 +0000\n+++ b/file.txt\t2024-01-02 00:00:00.000000000 +0000\n@@ -1 +1 @@\n-old\n+new\n"
	diffs, err := patch.Parse(diff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("expected at least one diff")
	}
	// Path should not contain the timestamp
	if strings.Contains(diffs[0].FilePath, "2024") {
		t.Errorf("expected timestamp stripped from path, got %q", diffs[0].FilePath)
	}
}
