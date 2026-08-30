package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkflows_AcceptsJSON(t *testing.T) {
	dir := t.TempDir()
	body := map[string]any{
		"id":       "wf-json",
		"name":     "JSON Pipeline",
		"enabled":  true,
		"schedule": "0 9 * * 1-5",
		"steps": []map[string]any{
			{"name": "draft", "agent": "Steve", "prompt": "draft", "position": 0},
			{"name": "review", "agent": "Winston", "prompt": "review {{inputs.draft}}", "position": 1,
				"inputs": []map[string]any{{"from_step": "draft", "as": "draft"}}},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wf-json.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 1 {
		t.Fatalf("want 1 json workflow, got %d", len(wfs))
	}
	if wfs[0].ID != "wf-json" {
		t.Errorf("id = %q", wfs[0].ID)
	}
	if wfs[0].Slug != "wf-json" {
		t.Errorf("slug = %q, want wf-json", wfs[0].Slug)
	}
	if len(wfs[0].Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(wfs[0].Steps))
	}
	if wfs[0].Steps[1].Agent != "Winston" {
		t.Errorf("step2 agent = %q", wfs[0].Steps[1].Agent)
	}
}

func TestLoadWorkflows_SkipsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkflowYAML(t, filepath.Join(dir, "good.yaml"), map[string]any{
		"id": "good", "name": "Good", "enabled": true, "schedule": "@daily",
	})
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 1 || wfs[0].ID != "good" {
		t.Fatalf("want only good.yaml, got %+v", wfs)
	}
}

func TestLoadWorkflows_SkipsHugeFile(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("a", int(MaxWorkflowFileBytes)+64)
	if err := os.WriteFile(filepath.Join(dir, "huge.yaml"), []byte("id: huge\nname: "+huge+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatalf("huge file must not 500/error the dir: %v", err)
	}
	if len(wfs) != 0 {
		t.Fatalf("huge file should be skipped, got %d", len(wfs))
	}
}

func TestLoadWorkflows_YAMLAndJSONTogether(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowYAML(t, filepath.Join(dir, "a.yaml"), map[string]any{
		"id": "a", "name": "A", "enabled": true, "schedule": "@daily",
	})
	if err := os.WriteFile(filepath.Join(dir, "b.json"), []byte(`{"id":"b","name":"B","enabled":true,"schedule":"@daily"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 2 {
		t.Fatalf("want 2, got %d", len(wfs))
	}
}

func TestIsWorkflowFilename(t *testing.T) {
	cases := map[string]bool{
		"x.yaml": true, "x.yml": true, "x.json": true,
		"x.txt": false, "x.yaml.bak": false, ".yaml": false,
	}
	for name, want := range cases {
		if got := IsWorkflowFilename(name); got != want {
			t.Errorf("%s: got %v want %v", name, got, want)
		}
	}
}

func TestLoadWorkflows_SkipsEmptyAndBinary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.yaml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ws.yaml"), []byte("   \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin.yaml"), []byte{0x00, 0x01, 0xff, 0xfe, 'n', 'o'}, 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkflowYAML(t, filepath.Join(dir, "good.yaml"), map[string]any{
		"id": "good", "name": "Good", "enabled": false, "schedule": "",
	})
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatalf("junk files must not error the dir: %v", err)
	}
	if len(wfs) != 1 || wfs[0].ID != "good" {
		t.Fatalf("want only good.yaml, got %+v", wfs)
	}
	for _, w := range wfs {
		if strings.TrimSpace(w.ID) == "" {
			t.Fatalf("empty id leaked into list: %+v", w)
		}
	}
}

func TestParseWorkflow_RejectsEmptyAndBinary(t *testing.T) {
	if _, err := ParseWorkflow([]byte(""), "empty.yaml"); err == nil {
		t.Fatal("empty must fail")
	}
	if _, err := ParseWorkflow([]byte{0x00, 0x01}, "bin.yaml"); err == nil {
		t.Fatal("binary must fail")
	}
	if _, err := ParseWorkflow([]byte("{}\n"), "nobody.json"); err == nil {
		t.Fatal("no id/name/steps must fail")
	}
}

func TestValidWorkflowID(t *testing.T) {
	if ValidWorkflowID("../escape") {
		t.Fatal("../escape must be invalid")
	}
	if ValidWorkflowID("a/b") || ValidWorkflowID("a\\b") {
		t.Fatal("slash id must be invalid")
	}
	if !ValidWorkflowID("dup-wr87035") || !ValidWorkflowID("once") {
		t.Fatal("normal ids must be valid")
	}
}

func TestSaveWorkflow_RejectsPathTraversalID(t *testing.T) {
	dir := t.TempDir()
	w := &Workflow{ID: "../escape-out", Name: "esc", Steps: []WorkflowStep{{Name: "s", Agent: "Steve", Prompt: "go"}}}
	if err := SaveWorkflow(dir, w); err == nil {
		t.Fatal("expected path rejection")
	}
	escaped := filepath.Join(filepath.Dir(dir), "escape-out.yaml")
	if _, err := os.Stat(escaped); err == nil {
		t.Fatalf("wrote outside drop dir: %s", escaped)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "escape") {
			t.Fatalf("drop dir contains escaped file: %s", e.Name())
		}
	}
}

func TestLoadWorkflows_SkipsJSONSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte(`{"id":"leaked","name":"Leaked","enabled":true,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "evil.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	writeWorkflowYAML(t, filepath.Join(dir, "good.yaml"), map[string]any{
		"id": "good", "name": "Good", "enabled": false, "schedule": "",
	})
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatalf("symlink must not error the dir: %v", err)
	}
	if len(wfs) != 1 || wfs[0].ID != "good" {
		t.Fatalf("symlink must be skipped, got %+v", wfs)
	}
	for _, w := range wfs {
		if w.ID == "leaked" || strings.Contains(w.FilePath, "secret") {
			t.Fatalf("followed symlink: %+v", w)
		}
	}
}

func TestLoadWorkflows_SkipsUnicodeID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cafe.json"), []byte(`{"id":"café","name":"Cafe","enabled":true,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ü.json"), []byte(`{"id":"u","name":"U","enabled":true,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 0 {
		t.Fatalf("unicode workflow ids/filenames must skip, got %+v", wfs)
	}
}

func TestValidWorkflowID_RejectsUnicode(t *testing.T) {
	for _, id := range []string{"café", "wringer-ü", "パイ", "../ü"} {
		if ValidWorkflowID(id) {
			t.Fatalf("%q must be invalid", id)
		}
	}
}
