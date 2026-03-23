package tools

import (
	"testing"
	"time"
)

// TestRegisterBuiltins_AllToolsRegistered verifies all expected tools are in the registry.
func TestRegisterBuiltins_AllToolsRegistered(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 10*time.Second)

	expectedTools := []string{
		"bash",
		"read_file",
		"write_file",
		"edit_file",
		"list_dir",
		"search_files",
		"grep",
	}

	for _, name := range expectedTools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered, but it was not found", name)
		}
	}
}

// TestRegisterBuiltins_CorrectCount verifies the total number of registered tools.
func TestRegisterBuiltins_CorrectCount(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 10*time.Second)

	all := reg.All()
	// We expect exactly 7 tools: bash, read_file, write_file, edit_file, list_dir, search_files, grep
	const expectedCount = 7
	if len(all) != expectedCount {
		t.Errorf("expected %d tools registered, got %d", expectedCount, len(all))
	}
}

// TestRegisterBuiltins_DefaultTimeout verifies that passing 0 timeout uses the 120s default.
func TestRegisterBuiltins_DefaultTimeout(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	// Passing 0 should not panic and should register tools normally.
	RegisterBuiltins(reg, root, 0)

	if _, ok := reg.Get("bash"); !ok {
		t.Error("expected bash to be registered even with zero timeout")
	}
}

// TestRegisterBuiltins_BashToolType verifies bash is registered as a BashTool.
func TestRegisterBuiltins_BashToolType(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 5*time.Second)

	tool, ok := reg.Get("bash")
	if !ok {
		t.Fatal("expected bash to be registered")
	}
	if _, ok := tool.(*BashTool); !ok {
		t.Errorf("expected bash to be *BashTool, got %T", tool)
	}
}

// TestRegisterBuiltins_WriteFileTool verifies write_file is registered as WriteFileTool.
func TestRegisterBuiltins_WriteFileTool(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 5*time.Second)

	tool, ok := reg.Get("write_file")
	if !ok {
		t.Fatal("expected write_file to be registered")
	}
	if _, ok := tool.(*WriteFileTool); !ok {
		t.Errorf("expected write_file to be *WriteFileTool, got %T", tool)
	}
}

// TestRegisterBuiltins_ListDirTool verifies list_dir is registered as ListDirTool.
func TestRegisterBuiltins_ListDirTool(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 5*time.Second)

	tool, ok := reg.Get("list_dir")
	if !ok {
		t.Fatal("expected list_dir to be registered")
	}
	if _, ok := tool.(*ListDirTool); !ok {
		t.Errorf("expected list_dir to be *ListDirTool, got %T", tool)
	}
}

// TestRegisterBuiltins_SearchFilesTool verifies search_files is registered as SearchFilesTool.
func TestRegisterBuiltins_SearchFilesTool(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 5*time.Second)

	tool, ok := reg.Get("search_files")
	if !ok {
		t.Fatal("expected search_files to be registered")
	}
	if _, ok := tool.(*SearchFilesTool); !ok {
		t.Errorf("expected search_files to be *SearchFilesTool, got %T", tool)
	}
}

// TestRegisterBuiltins_AllSchemasReturned verifies AllSchemas returns schemas for all registered tools.
func TestRegisterBuiltins_AllSchemasReturned(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 5*time.Second)

	schemas := reg.AllSchemas()
	if len(schemas) != 7 {
		t.Errorf("expected 7 schemas from AllSchemas, got %d", len(schemas))
	}
}

// TestRegisterBuiltins_SandboxRootPropagated verifies sandbox root is set on tools.
func TestRegisterBuiltins_SandboxRootPropagated(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	RegisterBuiltins(reg, root, 5*time.Second)

	tool, ok := reg.Get("bash")
	if !ok {
		t.Fatal("expected bash to be registered")
	}
	bashTool, ok := tool.(*BashTool)
	if !ok {
		t.Fatalf("expected *BashTool, got %T", tool)
	}
	if bashTool.SandboxRoot != root {
		t.Errorf("expected SandboxRoot=%q, got %q", root, bashTool.SandboxRoot)
	}
}

// TestRegisterBuiltins_BashTimeoutPropagated verifies that the bashTimeout argument is stored on
// the BashTool so that the config value (BashTimeoutSecs) is honoured at execution time.
func TestRegisterBuiltins_BashTimeoutPropagated(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	want := 42 * time.Second
	RegisterBuiltins(reg, root, want)

	tool, ok := reg.Get("bash")
	if !ok {
		t.Fatal("expected bash to be registered")
	}
	bashTool, ok := tool.(*BashTool)
	if !ok {
		t.Fatalf("expected *BashTool, got %T", tool)
	}
	if bashTool.Timeout != want {
		t.Errorf("expected Timeout=%v, got %v", want, bashTool.Timeout)
	}
}

// TestRegisterGitTools_AddsSevenTools verifies that RegisterGitTools registers all seven git tools.
func TestRegisterGitTools_AddsSevenTools(t *testing.T) {
	reg := NewRegistry()
	RegisterGitTools(reg, t.TempDir())
	expectedGitTools := []string{"git_status", "git_diff", "git_log", "git_blame", "git_branch", "git_commit", "git_stash"}
	for _, name := range expectedGitTools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected git tool %q to be registered", name)
		}
	}
}
