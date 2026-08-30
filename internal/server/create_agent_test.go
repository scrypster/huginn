package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/tools"
)

func hireTestHome(t *testing.T) {
	t.Helper()
	setupAgentsDir(t, map[string]string{
		"winston.yaml": `name: Winston
model: qwen2.5-coder:14b
color: '#d2a8ff'
local_tools:
  - create_agent
`,
		"sam.yaml": `name: Sam
model: qwen2.5-coder:14b
color: '#3fb950'
`,
	})
}

func TestPersistAgent_DuplicatePUT409NoOverwrite(t *testing.T) {
	hireTestHome(t)
	srv, ts := newTestServer(t)

	body := `{"name":"Winston","model":"qwen2.5-coder:14b","color":"#111111"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/newhire", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT duplicate want 409, got %d", resp.StatusCode)
	}

	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.Name, "Winston") && a.Color == "#111111" {
			t.Fatal("PUT collision must not overwrite Winston")
		}
	}
	_ = srv
}

func TestPersistAgent_SharedByToolAndPUT(t *testing.T) {
	hireTestHome(t)
	srv, ts := newTestServer(t)

	created, err := srv.PersistAgent(agents.AgentDef{
		Name:  "Ava",
		Model: "qwen2.5-coder:14b",
		Color: "#58a6ff",
	})
	if err != nil {
		t.Fatalf("PersistAgent: %v", err)
	}
	if !created {
		t.Fatal("expected create")
	}

	// Same store as HTTP GET
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/Ava", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET after PersistAgent: %d", resp.StatusCode)
	}
	var def agents.AgentDef
	if err := json.NewDecoder(resp.Body).Decode(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(def.Name, "Ava") {
		t.Fatalf("name=%q", def.Name)
	}

	// Tool duplicate uses same persist → 409, no overwrite
	tool := srv.NewCreateAgentTool()
	res := tool.Execute(context.Background(), map[string]any{"name": "Ava", "description": "researcher"})
	if !res.IsError {
		t.Fatal("tool duplicate must error")
	}
	cfg, _ := agents.LoadAgents()
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.Name, "Ava") && a.Description == "researcher" {
			t.Fatal("tool must not overwrite Ava")
		}
	}
}

func TestCreateAgentTool_WinstonSeatsAndVaultDefault(t *testing.T) {
	hireTestHome(t)
	srv, store := companyTestServer(t)
	huginn, err := store.CreateCompany("Huginn", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannelForCompany("eng", "Winston", []string{"Winston"}, "", "", huginn.ID)
	if err != nil {
		t.Fatal(err)
	}

	tool := srv.NewCreateAgentTool()
	ctx := threadmgr.SetCallingAgent(agent.SetSpaceID(context.Background(), ch.ID), "Winston")
	res := tool.Execute(ctx, map[string]any{"name": "Morgan", "description": "researcher"})
	if res.IsError {
		t.Fatalf("hire: %s", res.Error)
	}
	in, err := store.AgentInCompany("Morgan", huginn.ID)
	if err != nil || !in {
		t.Fatalf("Morgan should be seated in Huginn: in=%v err=%v", in, err)
	}
	sp, err := store.GetSpace(ch.ID)
	if err != nil || sp == nil {
		t.Fatalf("space: %v", err)
	}
	var onChannel bool
	for _, m := range sp.Members {
		if strings.EqualFold(m, "Morgan") {
			onChannel = true
		}
	}
	if !onChannel {
		t.Fatalf("Morgan should be on the hire channel roster, members=%v", sp.Members)
	}
	if strings.Contains(res.Output, "Muninn is down") {
		t.Errorf("stale down speech: %q", res.Output)
	}
	if !strings.Contains(res.Output, "huginn:agent:") && !strings.Contains(res.Output, "No vault yet") {
		t.Errorf("vault default or skip missing: %q", res.Output)
	}
	cfg, _ := agents.LoadAgents()
	var found bool
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.Name, "Morgan") {
			found = true
			// Canonical standard (MJ, 2026-08-28): huginn:agent:<user>:<name>.
			if !strings.HasPrefix(a.VaultName, "huginn:agent:") || !strings.HasSuffix(a.VaultName, ":morgan") {
				t.Errorf("vault_name=%q want huginn:agent:<user>:morgan", a.VaultName)
			}
		}
	}
	if !found {
		t.Fatal("Morgan not persisted")
	}
	if strings.Contains(res.Output, "create_agent") {
		t.Errorf("jargon: %q", res.Output)
	}
}

func TestCreateAgentTool_LabRefusesHuginnSpace(t *testing.T) {
	hireTestHome(t)
	srv, store := companyTestServer(t)
	huginn, err := store.CreateCompany("Huginn", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCompany("Lab", "lab", []string{"Winston", "Sam"}, "", ""); err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannelForCompany("huginn-eng", "Winston", []string{"Winston"}, "", "", huginn.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool := srv.NewCreateAgentTool()
	ctx := threadmgr.SetCallingAgent(agent.SetSpaceID(context.Background(), ch.ID), "Sam")
	res := tool.Execute(ctx, map[string]any{"name": "Morgan", "description": "researcher"})
	if !res.IsError {
		t.Fatal("Lab Sam must not hire into Huginn")
	}
	cfg, _ := agents.LoadAgents()
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.Name, "Morgan") {
			t.Fatal("must not persist")
		}
	}
}

func TestCreateAgentTool_BadNameBeforePersist(t *testing.T) {
	hireTestHome(t)
	srv, _ := companyTestServer(t)
	tool := srv.NewCreateAgentTool()
	res := tool.Execute(context.Background(), map[string]any{"name": "Mary Jane", "description": "researcher"})
	if !res.IsError {
		t.Fatal("expected subdomain reject")
	}
	cfg, _ := agents.LoadAgents()
	for _, a := range cfg.Agents {
		if strings.Contains(a.Name, "Mary") {
			t.Fatal("must not persist invalid seat name")
		}
	}
}

func TestCreateAgentTool_MuninnDownCreates(t *testing.T) {
	hireTestHome(t)
	srv, _ := companyTestServer(t)
	srv.muninnCfgPath = filepath.Join(t.TempDir(), "no-muninn.json")
	_ = os.WriteFile(srv.muninnCfgPath, []byte(`{"endpoint":""}`), 0o600)
	tool := srv.NewCreateAgentTool()
	res := tool.Execute(context.Background(), map[string]any{"name": "Morgan", "description": "researcher"})
	if res.IsError {
		t.Fatalf("should create with vault skip: %s", res.Error)
	}
	if !strings.Contains(res.Output, "No vault yet") {
		t.Errorf("want vault-skip speech: %q", res.Output)
	}
	if !srv.agentExistsOnDisk("Morgan") {
		t.Fatal("agent should exist")
	}
}

func TestCreateAgentTool_UnknownConnection(t *testing.T) {
	hireTestHome(t)
	srv, _ := companyTestServer(t)
	tool := srv.NewCreateAgentTool()
	res := tool.Execute(context.Background(), map[string]any{
		"name": "Morgan", "description": "researcher",
		"toolbelt": []any{"missing-conn"},
	})
	if !res.IsError {
		t.Fatal("unknown connection must refuse")
	}
	if srv.agentExistsOnDisk("Morgan") {
		t.Fatal("must not persist")
	}
}

func TestBuiltinToolNamesOmitsCreateAgent(t *testing.T) {
	for _, n := range tools.BuiltinToolNames() {
		if n == "create_agent" {
			t.Fatal("create_agent must not be in BuiltinToolNames")
		}
	}
}

func TestUnseatedDelegateDocumented(t *testing.T) {
	// Unseated new agent + delegate_to_agent is existing company-wall behavior:
	// threadmgr.Create returns notInCompanyError → "<name> isn't in this company."
	// (errors.Is(err, threadmgr.ErrAgentNotInCompany)). Hire on the desk skips
	// SeatMember, so the first company-space delegate hits that sentence.
	if threadmgr.ErrAgentNotInCompany == nil {
		t.Fatal("expected ErrAgentNotInCompany")
	}
}

func TestCreateChannelForCompany_Exists(t *testing.T) {
	// compile-touch so spaces.CreateChannelForCompany stays imported if hire tests skip
	_ = spaces.ErrCompanyNotFound
}

func TestCreateAgentTool_VaultReadyWhenDaemonUp(t *testing.T) {
	hireTestHome(t)
	mcpSrv := startMockMCP(t, nil)
	t.Cleanup(mcpSrv.Close)
	srv, store := companyTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint: mcpSrv.URL + "/mcp",
		MCPToken: "mdb_hire_test",
	}); err != nil {
		t.Fatal(err)
	}
	srv.muninnCfgPath = cfgPath
	huginn, err := store.CreateCompany("Huginn", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannelForCompany("eng", "Winston", []string{"Winston"}, "", "", huginn.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool := srv.NewCreateAgentTool()
	ctx := threadmgr.SetCallingAgent(agent.SetSpaceID(context.Background(), ch.ID), "Winston")
	res := tool.Execute(ctx, map[string]any{"name": "Morgan", "description": "researcher"})
	if res.IsError {
		t.Fatalf("hire: %s", res.Error)
	}
	if !strings.Contains(res.Output, "huginn:agent:") || !strings.Contains(res.Output, ":morgan") || !strings.Contains(res.Output, "is ready") {
		t.Fatalf("want vault-ready speech, got %q", res.Output)
	}
	if strings.Contains(res.Output, "Muninn is down") || strings.Contains(res.Output, "No vault yet") {
		t.Fatalf("daemon up must not skip vault: %q", res.Output)
	}
}

