package patch

import (
	"strings"
	"testing"
)

// TestParse_EmptyDiff verifies parsing an empty diff string.
func TestParse_EmptyDiff(t *testing.T) {
	diffs, err := Parse("")
	if err != nil {
		t.Errorf("Parse of empty string should not error: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected no diffs, got %d", len(diffs))
	}
}

// TestParse_SingleNewFile verifies parsing a new file addition.
func TestParse_SingleNewFile(t *testing.T) {
	unifiedDiff := `--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,2 @@
+package main
+func main() {}`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse new file failed: %v", err)
	}
	if len(diffs) != 1 {
		t.Errorf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].FilePath != "newfile.go" {
		t.Errorf("expected path 'newfile.go', got %q", diffs[0].FilePath)
	}
}

// TestParse_DeletedFile verifies parsing a file deletion.
func TestParse_DeletedFile(t *testing.T) {
	unifiedDiff := `--- a/oldfile.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package main
-func main() {}`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse deleted file failed: %v", err)
	}
	if len(diffs) != 1 {
		t.Errorf("expected 1 diff, got %d", len(diffs))
	}
	// The parser extracts path from the +++ line, which is /dev/null for deletions
	// This is expected behavior
	if diffs[0].FilePath != "/dev/null" && diffs[0].FilePath != "oldfile.go" {
		t.Errorf("expected path to be oldfile.go or /dev/null, got %q", diffs[0].FilePath)
	}
}

// TestParse_MultipleFiles verifies parsing multiple files in one diff.
func TestParse_MultipleFiles(t *testing.T) {
	unifiedDiff := `--- a/file1.go
+++ b/file1.go
@@ -1,1 +1,2 @@
 original
+added
--- a/file2.go
+++ b/file2.go
@@ -5,3 +5,2 @@
 line5
-deleted
 line6`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse multiple files failed: %v", err)
	}
	if len(diffs) != 2 {
		t.Errorf("expected 2 diffs, got %d", len(diffs))
	}
	if diffs[0].FilePath != "file1.go" {
		t.Errorf("expected first file 'file1.go', got %q", diffs[0].FilePath)
	}
	if diffs[1].FilePath != "file2.go" {
		t.Errorf("expected second file 'file2.go', got %q", diffs[1].FilePath)
	}
}

// TestParse_MultipleHunks verifies parsing multiple hunks in one file.
func TestParse_MultipleHunks(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -1,2 +1,3 @@
 line1
+inserted1
 line2
@@ -10,2 +11,2 @@
 line10
-deleted
 line11`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse multiple hunks failed: %v", err)
	}
	if len(diffs) != 1 {
		t.Errorf("expected 1 diff, got %d", len(diffs))
	}
	if len(diffs[0].Hunks) != 2 {
		t.Errorf("expected 2 hunks, got %d", len(diffs[0].Hunks))
	}
}

// TestParse_HunkStartLines verifies hunk start lines are parsed correctly.
func TestParse_HunkStartLines(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -10,5 +10,6 @@
 context
+new
 more`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse hunk failed: %v", err)
	}
	if len(diffs[0].Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}
	hunk := diffs[0].Hunks[0]
	if hunk.OldStart != 10 {
		t.Errorf("expected OldStart=10, got %d", hunk.OldStart)
	}
	if hunk.NewStart != 10 {
		t.Errorf("expected NewStart=10, got %d", hunk.NewStart)
	}
}

// TestParse_ExpectedHashPreserved verifies ExpectedHash extraction if present in comments.
func TestParse_ExpectedHashNotRequired(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -1,1 +1,2 @@
 line
+new`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse without hash should not error: %v", err)
	}
	if diffs[0].ExpectedHash != "" {
		t.Errorf("expected empty hash when not specified, got %q", diffs[0].ExpectedHash)
	}
}

// TestParse_ContextOnlyHunk verifies hunk with only context lines.
func TestParse_ContextOnlyHunk(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 context1
 context2
 context3`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse context-only hunk failed: %v", err)
	}
	if len(diffs[0].Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}
	hunk := diffs[0].Hunks[0]
	// All lines should be context (Op == ' ')
	for _, line := range hunk.Lines {
		if line.Op != ' ' {
			t.Errorf("expected context line, got op %c", line.Op)
		}
	}
}

// TestParse_AddOnlyHunk verifies hunk with only additions.
func TestParse_AddOnlyHunk(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -0,0 +1,3 @@
+line1
+line2
+line3`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse add-only hunk failed: %v", err)
	}
	hunk := diffs[0].Hunks[0]
	for _, line := range hunk.Lines {
		if line.Op != '+' {
			t.Errorf("expected addition line, got op %c", line.Op)
		}
	}
}

// TestParse_DeleteOnlyHunk verifies hunk with only deletions.
func TestParse_DeleteOnlyHunk(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -1,3 +0,0 @@
-line1
-line2
-line3`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse delete-only hunk failed: %v", err)
	}
	hunk := diffs[0].Hunks[0]
	for _, line := range hunk.Lines {
		if line.Op != '-' {
			t.Errorf("expected deletion line, got op %c", line.Op)
		}
	}
}

// TestParse_LineWithSpecialChars verifies lines with special characters.
func TestParse_SpecialCharacters(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -1,1 +1,2 @@
 fmt.Println("@@ special @@")
+x := "--- and +++"
`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse with special chars failed: %v", err)
	}
	if len(diffs[0].Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}
	// Last line should preserve the special content
	lastLine := diffs[0].Hunks[0].Lines[len(diffs[0].Hunks[0].Lines)-1]
	if lastLine.Op != '+' || !strings.Contains(lastLine.Content, "---") {
		t.Error("special characters should be preserved in line content")
	}
}

// TestParse_EmptyLines verifies handling of empty lines in diff.
func TestParse_EmptyLines(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 line1

+new line

 line3`

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse with empty lines failed: %v", err)
	}
	if len(diffs[0].Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}
	// Verify that the hunk has some lines
	if len(diffs[0].Hunks[0].Lines) < 1 {
		t.Errorf("expected at least one line in hunk, got %d", len(diffs[0].Hunks[0].Lines))
	}
	// Verify there's an addition in the lines
	hasAddition := false
	for _, line := range diffs[0].Hunks[0].Lines {
		if line.Op == OpAdd {
			hasAddition = true
			break
		}
	}
	if !hasAddition {
		t.Error("expected an addition line in hunk")
	}
}

// TestParse_TrailingEmptyLine verifies diff ending with an empty line.
func TestParse_TrailingEmptyLine(t *testing.T) {
	unifiedDiff := `--- a/file.go
+++ b/file.go
@@ -1,1 +1,2 @@
 content
+

` // note trailing newlines

	diffs, err := Parse(unifiedDiff)
	if err != nil {
		t.Errorf("Parse with trailing newlines failed: %v", err)
	}
	if len(diffs) != 1 {
		t.Errorf("expected 1 diff, got %d", len(diffs))
	}
}

// TestHunkOps verifies HunkOp constants.
func TestHunkOps(t *testing.T) {
	if OpContext != ' ' {
		t.Errorf("OpContext should be ' ', got %c", OpContext)
	}
	if OpAdd != '+' {
		t.Errorf("OpAdd should be '+', got %c", OpAdd)
	}
	if OpDelete != '-' {
		t.Errorf("OpDelete should be '-', got %c", OpDelete)
	}
}
