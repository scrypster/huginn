package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSandboxed_HappyPath(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "subdir", "file.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveSandboxed(root, "subdir/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Resolve root through EvalSymlinks so the comparison works on macOS
	// where /var/folders is a symlink to /private/var/folders.
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if !strings.HasPrefix(got, resolvedRoot) {
		t.Errorf("resolved path %q is not under root %q", got, resolvedRoot)
	}
}

func TestResolveSandboxed_TraversalBlocked(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveSandboxed(root, "../escape.txt")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestResolveSandboxed_AbsolutePathOutside(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveSandboxed(root, "../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path outside root, got nil")
	}
}

func TestResolveSandboxed_EmptyPath(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveSandboxed(root, "")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestResolveSandboxed_RootItself(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveSandboxed(root, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	absRoot, _ := filepath.Abs(root)
	absRoot, _ = filepath.EvalSymlinks(absRoot)
	if got != absRoot {
		t.Errorf("expected %q, got %q", absRoot, got)
	}
}

func TestResolveSandboxed_NewFileInRoot(t *testing.T) {
	root := t.TempDir()
	// newfile.txt does not exist yet — tests the fallback path
	got, err := ResolveSandboxed(root, "newfile.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Resolve root through EvalSymlinks so the comparison works on macOS.
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if !strings.HasPrefix(got, resolvedRoot) {
		t.Errorf("resolved path %q is not under root %q", got, resolvedRoot)
	}
	if filepath.Base(got) != "newfile.txt" {
		t.Errorf("expected base name newfile.txt, got %q", filepath.Base(got))
	}
}

func TestResolveSandboxed_SymlinkedParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside root that points to outside
	linkPath := filepath.Join(root, "link")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Fatalf("symlink creation failed: %v", err)
	}

	// Try to create a new file through the symlinked parent
	_, err := ResolveSandboxed(root, "link/newfile.txt")
	if err == nil {
		t.Fatal("expected error: symlinked parent escapes sandbox, but got nil")
	}
}

// TestResolveSandboxed_DeepNestedNewPath verifies that a new file in an existing
// parent directory resolves correctly within the sandbox.
// On macOS, /tmp is a symlink to /private/tmp, so EvalSymlinks must be used
// on both the resolved path and root for the prefix check to pass correctly.
func TestResolveSandboxed_DeepNestedNewPath(t *testing.T) {
	root := t.TempDir()
	// Pre-create the full parent directory chain so EvalSymlinks can resolve it.
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0755); err != nil {
		t.Fatal(err)
	}
	// "a/b" exists, so "a/b/newfile.txt" resolves via the parent path.
	got, err := ResolveSandboxed(root, "a/b/newfile.txt")
	if err != nil {
		t.Fatalf("unexpected error for nested new path: %v", err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if !strings.HasPrefix(got, resolvedRoot) {
		t.Errorf("resolved path %q not under root %q", got, resolvedRoot)
	}
}

// TestResolveSandboxed_DeeplyNestedNewPathNoParent verifies that a deeply nested
// new file where neither the file, its parent, nor its grandparent exist still
// resolves correctly. On macOS, t.TempDir() returns /var/folders/... which is
// a symlink to /private/var/folders/..., so the ancestor-walking logic must
// resolve via EvalSymlinks rather than falling back to Abs.
func TestResolveSandboxed_DeeplyNestedNewPathNoParent(t *testing.T) {
	root := t.TempDir()
	// Do NOT create any subdirectories — "x/y/z/file.txt" is fully new.
	got, err := ResolveSandboxed(root, "x/y/z/file.txt")
	if err != nil {
		t.Fatalf("unexpected error for deeply nested new path: %v", err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if !strings.HasPrefix(got, resolvedRoot) {
		t.Errorf("resolved path %q not under resolved root %q", got, resolvedRoot)
	}
	// Verify the full path structure is preserved.
	wantSuffix := filepath.Join("x", "y", "z", "file.txt")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("resolved path %q does not end with %q", got, wantSuffix)
	}
}

// TestResolveSandboxed_DotDotInsideSandbox verifies that a path with internal
// ".." that still resolves inside the sandbox is accepted.
func TestResolveSandboxed_DotDotInsideSandbox(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// "sub/../file.txt" is still inside root.
	got, err := ResolveSandboxed(root, "sub/../file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if !strings.HasPrefix(got, resolvedRoot) {
		t.Errorf("resolved %q not inside root %q", got, resolvedRoot)
	}
}

// TestErrSandboxEscape verifies the helper returns a populated ToolResult.
func TestErrSandboxEscape(t *testing.T) {
	r := ErrSandboxEscape("/root", "../../etc/passwd")
	if !r.IsError {
		t.Error("expected IsError=true")
	}
	if !strings.Contains(r.Error, "escapes sandbox") {
		t.Errorf("expected 'escapes sandbox' in error, got: %s", r.Error)
	}
}

// TestResolveSandboxed_SlashOnlyPath verifies that an absolute path which
// escapes the sandbox is rejected. Note: filepath.Join(root, "/etc/passwd")
// actually produces root+"/etc/passwd" (inside sandbox), so we use "../../"
// traversal to truly escape.
func TestResolveSandboxed_SlashOnlyPath(t *testing.T) {
	root := t.TempDir()
	// filepath.Join(root, "/etc/passwd") = root/etc/passwd (inside sandbox),
	// so this tests "." which resolves to root itself and should be accepted.
	got, err := ResolveSandboxed(root, ".")
	if err != nil {
		t.Fatalf("unexpected error for '.': %v", err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if got != resolvedRoot {
		t.Errorf("expected %q, got %q", resolvedRoot, got)
	}

	// A true escape attempt via traversal should be blocked.
	_, err = ResolveSandboxed(root, "../../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for traversal escape to /etc/passwd")
	}
}
