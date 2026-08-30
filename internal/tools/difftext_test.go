package tools

import (
	"strings"
	"testing"
)

func TestBuildFileDiff_NoChange(t *testing.T) {
	fd := BuildFileDiff("a.go", "same\n", "same\n")
	if fd.Unified != "" || fd.Added != 0 || fd.Removed != 0 {
		t.Fatalf("expected no diff, got %+v", fd)
	}
}

func TestBuildFileDiff_NewFile(t *testing.T) {
	fd := BuildFileDiff("new.go", "", "line1\nline2\n")
	if !fd.IsNew {
		t.Fatalf("expected IsNew=true")
	}
	if fd.Added != 2 || fd.Removed != 0 {
		t.Fatalf("expected +2/-0, got +%d/-%d", fd.Added, fd.Removed)
	}
	if !strings.Contains(fd.Unified, "+line1") || !strings.Contains(fd.Unified, "+line2") {
		t.Fatalf("unified diff missing added lines: %s", fd.Unified)
	}
	if !strings.HasPrefix(fd.Unified, "--- new.go\n+++ new.go\n") {
		t.Fatalf("unified diff missing headers: %s", fd.Unified)
	}
}

func TestBuildFileDiff_SimpleEdit(t *testing.T) {
	before := "package main\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	after := "package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"
	fd := BuildFileDiff("mathutil.go", before, after)
	if fd.Added != 1 || fd.Removed != 1 {
		t.Fatalf("expected +1/-1, got +%d/-%d\ndiff:\n%s", fd.Added, fd.Removed, fd.Unified)
	}
	if !strings.Contains(fd.Unified, "-\treturn a - b") {
		t.Fatalf("missing removed line: %s", fd.Unified)
	}
	if !strings.Contains(fd.Unified, "+\treturn a + b") {
		t.Fatalf("missing added line: %s", fd.Unified)
	}
	// Context lines should be present (surrounding func signature).
	if !strings.Contains(fd.Unified, " func Add(a, b int) int {") {
		t.Fatalf("missing context line: %s", fd.Unified)
	}
}

func TestBuildFileDiff_Truncation(t *testing.T) {
	var beforeLines, afterLines []string
	for i := 0; i < 500; i++ {
		beforeLines = append(beforeLines, "old line")
		afterLines = append(afterLines, "new line")
	}
	before := strings.Join(beforeLines, "\n") + "\n"
	after := strings.Join(afterLines, "\n") + "\n"
	fd := BuildFileDiff("big.txt", before, after)
	if !fd.Truncated {
		t.Fatalf("expected truncation for a 500-line rewrite")
	}
	lineCount := strings.Count(fd.Unified, "\n") + 1
	if lineCount > maxDiffLines+5 { // small slack for the truncation note itself
		t.Fatalf("expected output capped near %d lines, got %d", maxDiffLines, lineCount)
	}
	if !strings.Contains(fd.Unified, "truncated") {
		t.Fatalf("expected truncation note in output: %s", fd.Unified)
	}
}

func TestBuildFileDiff_DeleteContent(t *testing.T) {
	fd := BuildFileDiff("gone.go", "content\n", "")
	if !fd.IsDelete {
		t.Fatalf("expected IsDelete=true")
	}
	if fd.Removed != 1 || fd.Added != 0 {
		t.Fatalf("expected +0/-1, got +%d/-%d", fd.Added, fd.Removed)
	}
}
