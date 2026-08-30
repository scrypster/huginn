package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/scheduler"
)

func TestWriteWorkflowTool_WritesYAMLToDropDir(t *testing.T) {
	home := t.TempDir()
	tool := &WriteWorkflowTool{HuginnHome: home}
	content := "id: agent-made\nname: Agent Made\nenabled: false\nschedule: \"\"\nsteps:\n  - name: draft\n    agent: Steve\n    prompt: go\n    position: 0\n"
	res := tool.Execute(context.Background(), map[string]any{
		"filename": "agent-made.yaml",
		"content":  content,
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	dest := filepath.Join(home, "workflows", "agent-made.yaml")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	wfs, err := scheduler.LoadWorkflows(filepath.Join(home, "workflows"))
	if err != nil || len(wfs) != 1 {
		t.Fatalf("load: %v count=%d", err, len(wfs))
	}
	if wfs[0].ID != "agent-made" {
		t.Errorf("id=%q", wfs[0].ID)
	}
}

func TestWriteWorkflowTool_WritesJSON(t *testing.T) {
	home := t.TempDir()
	tool := &WriteWorkflowTool{HuginnHome: home}
	res := tool.Execute(context.Background(), map[string]any{
		"filename": "pipe.json",
		"content":  `{"id":"pipe","name":"Pipe","enabled":true,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`,
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	wfs, err := scheduler.LoadWorkflows(filepath.Join(home, "workflows"))
	if err != nil || len(wfs) != 1 || wfs[0].ID != "pipe" {
		t.Fatalf("json drop not loaded: %v %+v", err, wfs)
	}
}

func TestWriteWorkflowTool_RejectsPathTraversal(t *testing.T) {
	home := t.TempDir()
	tool := &WriteWorkflowTool{HuginnHome: home}
	res := tool.Execute(context.Background(), map[string]any{
		"filename": "../escape.yaml",
		"content":  "id: x\nname: x\n",
	})
	if !res.IsError {
		t.Fatal("expected path rejection")
	}
}

func TestWriteWorkflowTool_RejectsBadYAML(t *testing.T) {
	home := t.TempDir()
	tool := &WriteWorkflowTool{HuginnHome: home}
	res := tool.Execute(context.Background(), map[string]any{
		"filename": "bad.yaml",
		"content":  "{invalid:",
	})
	if !res.IsError {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(res.Error, "invalid") && !strings.Contains(res.Error, "parse") {
		t.Errorf("error should mention invalid/parse, got %q", res.Error)
	}
}

func TestWriteWorkflowTool_RejectsHuge(t *testing.T) {
	home := t.TempDir()
	tool := &WriteWorkflowTool{HuginnHome: home}
	res := tool.Execute(context.Background(), map[string]any{
		"filename": "huge.yaml",
		"content":  strings.Repeat("a", int(scheduler.MaxWorkflowFileBytes)+8),
	})
	if !res.IsError {
		t.Fatal("expected size rejection")
	}
}

func TestWriteWorkflowTool_RejectsAbsolutePath(t *testing.T) {
	home := t.TempDir()
	tool := &WriteWorkflowTool{HuginnHome: home}
	content := "id: x\nname: x\nenabled: false\nschedule: \"\"\nsteps:\n  - name: s\n    agent: Steve\n    prompt: go\n    position: 0\n"
	for _, name := range []string{"/tmp/evil.yaml", "/etc/passwd.yaml", `C:\Windows\evil.yaml`} {
		res := tool.Execute(context.Background(), map[string]any{
			"filename": name,
			"content":  content,
		})
		if !res.IsError {
			t.Fatalf("absolute path %q must be rejected", name)
		}
		escaped := filepath.Join(home, "workflows", filepath.Base(name))
		if _, err := os.Stat(escaped); err == nil {
			t.Fatalf("wrote drop file for %q", name)
		}
		if _, err := os.Stat(name); err == nil && strings.HasPrefix(name, "/tmp/") {
			t.Fatalf("wrote outside home: %s", name)
		}
	}
}
