package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/scheduler"
)

func TestHandleCreateWorkflow_FromStepCycle_ClientErrorNoPanic(t *testing.T) {
	_, ts := newTestServer(t)
	payload := `{
		"name":"cycle",
		"enabled":false,
		"schedule":"",
		"steps":[
			{"name":"a","agent":"Steve","prompt":"{{inputs.b}}","position":0,"inputs":[{"from_step":"b","as":"b"}]},
			{"name":"b","agent":"Winston","prompt":"{{inputs.a}}","position":1,"inputs":[{"from_step":"a","as":"a"}]}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("cycle must be 4xx (not 500/panic), got %d", resp.StatusCode)
	}
}

func TestHandleCreateWorkflow_OneShotEmptySchedule(t *testing.T) {
	srv, ts := newTestServer(t)
	sched := scheduler.New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *scheduler.Workflow) error { return nil })
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()
	payload := `{"name":"once","enabled":true,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("one-shot enabled+empty schedule should save (200), got %d", resp.StatusCode)
	}
}

func TestHandleCreateWorkflow_MissingAgent_No500(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{Name: "Steve"}, {Name: "Winston"}}}, nil
	}
	payload := `{"name":"ghost","enabled":false,"schedule":"","steps":[{"name":"s","agent":"Phantom","prompt":"go","position":0}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		t.Fatalf("missing agent must not 500, got %d", resp.StatusCode)
	}
	if resp.StatusCode < 400 {
		t.Fatalf("missing agent must be 4xx, got %d", resp.StatusCode)
	}
}

func TestHandleListWorkflows_CorruptFile_No500(t *testing.T) {
	srv, ts := newTestServer(t)
	dir := filepath.Join(srv.huginnDir, "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{invalid:"), 0o600); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("corrupt drop file must not 500 list, got %d", resp.StatusCode)
	}
}

func TestHandleCreateWorkflow_LabCannotWakeHuginnOnlyReggie(t *testing.T) {
	srv, store := companyTestServer(t)
	lab, err := store.CreateCompany("Lab", "lab", []string{"Steve", "Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Reggie exists as an agent but is not seated in Lab.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Steve"}, {Name: "Winston"}, {Name: "Reggie"},
		}}, nil
	}
	body := map[string]any{
		"name":       "Lab standup",
		"enabled":    false,
		"schedule":   "",
		"company_id": lab.ID,
		"steps": []map[string]any{
			{"name": "wake", "agent": "Reggie", "prompt": "hello", "position": 0},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows", bytes.NewReader(b))
	w := httptest.NewRecorder()
	srv.handleCreateWorkflow(w, req)
	if w.Code >= 500 {
		t.Fatalf("company wall must not 500, got %d %s", w.Code, w.Body.String())
	}
	if w.Code < 400 {
		t.Fatalf("Lab workflow waking Reggie must be rejected, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Reggie") {
		t.Errorf("error should name Reggie, got %s", w.Body.String())
	}
}

func TestHandleCreateWorkflow_LabAllowsSeatedSteve(t *testing.T) {
	srv, store := companyTestServer(t)
	lab, err := store.CreateCompany("Lab", "lab", []string{"Steve", "Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Steve"}, {Name: "Winston"}, {Name: "Reggie"},
		}}, nil
	}
	body := map[string]any{
		"name":       "Lab pipeline",
		"enabled":    false,
		"schedule":   "",
		"company_id": lab.ID,
		"steps": []map[string]any{
			{"name": "draft", "agent": "Steve", "prompt": "draft", "position": 0},
			{"name": "review", "agent": "Winston", "prompt": "review {{inputs.draft}}", "position": 1,
				"inputs": []map[string]any{{"from_step": "draft", "as": "draft"}}},
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows", bytes.NewReader(b))
	w := httptest.NewRecorder()
	srv.handleCreateWorkflow(w, req)
	if w.Code != 200 {
		t.Fatalf("seated teammates should save, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateWorkflow_UnknownCompany_ClientError(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := map[string]any{
		"name":       "x",
		"enabled":    false,
		"schedule":   "",
		"company_id": "does-not-exist",
		"steps":      []map[string]any{{"name": "s", "agent": "Steve", "prompt": "go", "position": 0}},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows", bytes.NewReader(b))
	w := httptest.NewRecorder()
	srv.handleCreateWorkflow(w, req)
	if w.Code >= 500 || w.Code < 400 {
		t.Fatalf("unknown company must be 4xx, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateWorkflow_DuplicateID_Conflict(t *testing.T) {
	_, ts := newTestServer(t)
	payload := `{"id":"dup-id","name":"once","enabled":false,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("first create want 200, got %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second create same id must 409, got %d", resp.StatusCode)
	}
}

func TestHandleCreateWorkflow_PathTraversalID(t *testing.T) {
	_, ts := newTestServer(t)
	payload := `{"id":"../escape-out","name":"esc","enabled":false,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("path id must be 4xx, got %d", resp.StatusCode)
	}
}

func TestHandleCreateWorkflow_UnicodeID(t *testing.T) {
	_, ts := newTestServer(t)
	payload := `{"id":"café","name":"cafe","enabled":false,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("unicode workflow id must be 400, got %d", resp.StatusCode)
	}
}

func TestHandleRunWorkflow_AfterCompanyDeleteFailsClosed(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("WfGone", "", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(srv.huginnDir, "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	wf := &scheduler.Workflow{
		ID:        "wfgone",
		Name:      "after-delete",
		Enabled:   false,
		CompanyID: co.ID,
		Steps: []scheduler.WorkflowStep{
			{Position: 0, Name: "wake", Agent: "Steve", Prompt: "hello"},
		},
	}
	if err := scheduler.SaveWorkflow(dir, wf); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteCompany(co.ID); err != nil {
		t.Fatal(err)
	}
	ran := false
	sched := scheduler.New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *scheduler.Workflow) error {
		ran = true
		return nil
	})
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wfgone/run", nil)
	req.SetPathValue("id", "wfgone")
	w := httptest.NewRecorder()
	srv.handleRunWorkflow(w, req)
	if w.Code == 200 {
		t.Fatalf("run after company delete must fail closed, got 200 %s", w.Body.String())
	}
	if w.Code >= 500 {
		t.Fatalf("must not 500, got %d %s", w.Code, w.Body.String())
	}
	if ran {
		t.Fatal("runner must not start after company delete")
	}
}

func TestHandleDropWorkflow_YAMLAndJSON(t *testing.T) {
	srv, ts := newTestServer(t)
	yamlBody := `{"filename":"standup.yaml","content":"id: standup\nname: Standup\nenabled: false\nschedule: \"\"\nsteps:\n  - name: draft\n    agent: Steve\n    prompt: go\n    position: 0\n"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/drop", strings.NewReader(yamlBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("yaml drop want 200, got %d", resp.StatusCode)
	}
	dir := filepath.Join(srv.huginnDir, "workflows")
	if _, err := os.Stat(filepath.Join(dir, "standup.yaml")); err != nil {
		t.Fatalf("raw yaml not written: %v", err)
	}

	jsonBody := `{"filename":"pipe.json","content":"{\"id\":\"pipe\",\"name\":\"Pipe\",\"enabled\":false,\"schedule\":\"\",\"steps\":[{\"name\":\"s\",\"agent\":\"Steve\",\"prompt\":\"go\",\"position\":0}]}"}`
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/workflows/drop", strings.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("json drop want 200, got %d", resp.StatusCode)
	}
}

func TestHandleDropWorkflow_RejectsPathAndCorrupt(t *testing.T) {
	_, ts := newTestServer(t)
	payload := `{"filename":"../escape.yaml","content":"id: x\nname: x\nsteps:\n  - name: s\n    agent: Steve\n    prompt: go\n    position: 0\n"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/drop", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("path drop must be 4xx, got %d", resp.StatusCode)
	}

	payload = `{"filename":"bad.yaml","content":"{invalid:"}`
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/workflows/drop", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("corrupt drop must be 4xx, got %d", resp.StatusCode)
	}
}
