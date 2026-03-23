package diffview_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/diffview"
)

func TestComputeDiff_NewFile(t *testing.T) {
	result := diffview.ComputeDiff("auth.go", nil, []byte("package auth\n\nfunc New() {}\n"))
	if result.Path != "auth.go" {
		t.Errorf("path = %q", result.Path)
	}
	if result.Added < 1 {
		t.Errorf("expected Added > 0, got %d", result.Added)
	}
	if result.Deleted != 0 {
		t.Errorf("expected Deleted = 0, got %d", result.Deleted)
	}
}

func TestComputeDiff_ModifiedFile(t *testing.T) {
	old := []byte("package auth\n\nfunc Old() {}\n")
	new := []byte("package auth\n\nfunc New() {}\nfunc Extra() {}\n")
	result := diffview.ComputeDiff("auth.go", old, new)
	if result.Added < 1 {
		t.Errorf("expected Added > 0")
	}
	if result.Deleted < 1 {
		t.Errorf("expected Deleted > 0")
	}
	if !strings.Contains(result.UnifiedDiff, "+") {
		t.Error("expected + lines in diff")
	}
}

func TestComputeDiff_NoChange(t *testing.T) {
	content := []byte("package auth\n\nfunc Same() {}\n")
	result := diffview.ComputeDiff("auth.go", content, content)
	if result.Added != 0 || result.Deleted != 0 {
		t.Errorf("expected no changes")
	}
}

func TestRenderBatch_ZeroWidth_DefaultsTo80(t *testing.T) {
	diffs := []diffview.FileDiff{
		diffview.ComputeDiff("a.go", nil, []byte("package a\n")),
	}
	// Should not panic with width=0
	rendered := diffview.RenderBatch(diffs, 0)
	if rendered == "" {
		t.Error("expected non-empty output")
	}
}

func TestComputeDiff_BinaryContent(t *testing.T) {
	// Binary content should not crash
	binary := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}
	result := diffview.ComputeDiff("binary.bin", nil, binary)
	if result.Path != "binary.bin" {
		t.Errorf("path = %q", result.Path)
	}
}

func TestRenderBatch_ShowsAllFiles(t *testing.T) {
	diffs := []diffview.FileDiff{
		diffview.ComputeDiff("a.go", nil, []byte("package a\n")),
		diffview.ComputeDiff("b.go", nil, []byte("package b\n")),
	}
	rendered := diffview.RenderBatch(diffs, 80)
	if !strings.Contains(rendered, "a.go") {
		t.Error("expected a.go")
	}
	if !strings.Contains(rendered, "b.go") {
		t.Error("expected b.go")
	}
	if !strings.Contains(rendered, "[A]ccept all") {
		t.Error("expected [A]ccept all")
	}
	if !strings.Contains(rendered, "[R]eject all") {
		t.Error("expected [R]eject all")
	}
}
