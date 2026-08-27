package tools

import (
	"context"
	"strings"
	"testing"
)

type hireRec struct {
	persisted []CreateAgentRequest
	vaults    []string
	seated      []string
	spaceSeated []string
	exists    map[string]bool
	conns     map[string]string
	spaceCo   string
	inCompany map[string]bool
	coName    string
	persistErr error
	nameErr    error
}

func (r *hireRec) deps() CreateAgentDeps {
	if r.exists == nil {
		r.exists = map[string]bool{}
	}
	if r.conns == nil {
		r.conns = map[string]string{}
	}
	if r.inCompany == nil {
		r.inCompany = map[string]bool{}
	}
	return CreateAgentDeps{
		Persist: func(req CreateAgentRequest) error {
			if r.persistErr != nil {
				return r.persistErr
			}
			r.persisted = append(r.persisted, req)
			r.exists[strings.ToLower(req.Name)] = true
			return nil
		},
		AgentExists: func(name string) bool {
			return r.exists[strings.ToLower(name)]
		},
		TryVault: func(vaultName, _ string) bool {
			r.vaults = append(r.vaults, vaultName)
			return true
		},
		SpaceCompanyID: func(string) (string, error) { return r.spaceCo, nil },
		AgentInCompany: func(agent, _ string) (bool, error) {
			if v, ok := r.inCompany[strings.ToLower(agent)]; ok {
				return v, nil
			}
			return false, nil
		},
		CompanyName: func(string) (string, error) { return r.coName, nil },
		SeatMember: func(companyID, agentName string) error {
			r.seated = append(r.seated, companyID+"/"+agentName)
			return nil
		},
		SeatSpaceMember: func(spaceID, agentName string) error {
			r.spaceSeated = append(r.spaceSeated, spaceID+"/"+agentName)
			return nil
		},
		ResolveConn: func(id string) (string, bool) {
			p, ok := r.conns[id]
			return p, ok
		},
		ValidateName: func(name string) error { return r.nameErr },
		CallerFromCtx: func(ctx context.Context) string {
			if v, _ := ctx.Value(hireCallerKey{}).(string); v != "" {
				return v
			}
			return "Winston"
		},
		SpaceFromCtx: func(ctx context.Context) string {
			if v, _ := ctx.Value(hireSpaceKey{}).(string); v != "" {
				return v
			}
			return "space-1"
		},
		CallerModel:  func(context.Context) string { return "qwen2.5-coder:14b" },
		MachineModel: "qwen2.5-coder:14b",
	}
}

type hireCallerKey struct{}
type hireSpaceKey struct{}

func hireCtx(caller, space string) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, hireCallerKey{}, caller)
	ctx = context.WithValue(ctx, hireSpaceKey{}, space)
	return ctx
}

func TestCreateAgent_DuplicateRefusesNoPersist(t *testing.T) {
	r := &hireRec{exists: map[string]bool{"morgan": true}, spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{"name": "Morgan", "description": "researcher"})
	if !res.IsError {
		t.Fatal("expected duplicate error")
	}
	if len(r.persisted) != 0 {
		t.Fatalf("must not persist overwrite, got %+v", r.persisted)
	}
	if strings.Contains(res.Error, "create_agent") || strings.Contains(res.Error, "409") {
		t.Errorf("jargon in error: %q", res.Error)
	}
	if !strings.Contains(res.Error, "already on the roster") {
		t.Errorf("want roster speech, got %q", res.Error)
	}
}

func TestCreateAgent_UnknownConnectionRefuses(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Morgan", "description": "researcher",
		"toolbelt": []any{"no-such-conn"},
	})
	if !res.IsError {
		t.Fatal("expected unknown connection refuse")
	}
	if len(r.persisted) != 0 {
		t.Fatal("must not persist")
	}
	if !strings.Contains(res.Error, "connection") {
		t.Errorf("got %q", res.Error)
	}
}

func TestCreateAgent_MuninnDownStillCreates(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	d := r.deps()
	d.TryVault = func(string, string) bool { return false }
	tool := &CreateAgentTool{Deps: d}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{"name": "Morgan", "description": "researcher"})
	if res.IsError {
		t.Fatalf("hire should succeed: %s", res.Error)
	}
	if len(r.persisted) != 1 {
		t.Fatalf("expected persist, got %d", len(r.persisted))
	}
	if !strings.Contains(res.Output, "No vault yet") {
		t.Errorf("speech should say vault skipped: %q", res.Output)
	}
	if jargon(res.Output) {
		t.Errorf("jargon in speech: %q", res.Output)
	}
}

func TestCreateAgent_VaultDefaultNameHuginn(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{"name": "Morgan", "description": "researcher"})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if len(r.vaults) != 1 || r.vaults[0] != "morgan-huginn" {
		t.Fatalf("vaults=%v, want morgan-huginn", r.vaults)
	}
	if r.persisted[0].VaultName != "morgan-huginn" {
		t.Errorf("persisted vault %q", r.persisted[0].VaultName)
	}
	if !strings.Contains(res.Output, "morgan-huginn") {
		t.Errorf("speech missing vault: %q", res.Output)
	}
}

func TestCreateAgent_BadSeatNameRejectsBeforePersist(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	d := r.deps()
	d.ValidateName = func(string) error { return errName }
	tool := &CreateAgentTool{Deps: d}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{"name": "Mary Jane", "description": "researcher"})
	if !res.IsError {
		t.Fatal("expected name reject")
	}
	if len(r.persisted) != 0 {
		t.Fatal("must reject before persist")
	}
}

var errName = errString("bad name")

type errString string

func (e errString) Error() string { return string(e) }

func TestCreateAgent_WinstonSeatsCurrentCompany(t *testing.T) {
	r := &hireRec{spaceCo: "huginn-id", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{"name": "Morgan", "description": "researcher", "local_tools": []any{"read_file"}})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if len(r.seated) != 1 || r.seated[0] != "huginn-id/Morgan" {
		t.Fatalf("seated=%v", r.seated)
	}
	if len(r.spaceSeated) != 1 || r.spaceSeated[0] != "space-1/Morgan" {
		t.Fatalf("space seated=%v", r.spaceSeated)
	}
	if !strings.Contains(res.Output, "Huginn") {
		t.Errorf("speech should name company: %q", res.Output)
	}
	if jargon(res.Output) {
		t.Errorf("jargon: %q", res.Output)
	}
}

func TestCreateAgent_LabCannotHireIntoHuginn(t *testing.T) {
	r := &hireRec{spaceCo: "huginn-id", inCompany: map[string]bool{"sam": false, "winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Sam", "huginn-space"), map[string]any{"name": "Morgan", "description": "researcher"})
	if !res.IsError {
		t.Fatal("Lab Sam must not hire into Huginn")
	}
	if len(r.persisted) != 0 {
		t.Fatal("must fail closed before persist")
	}
}

func TestCreateAgent_DeskSkipsSeat(t *testing.T) {
	r := &hireRec{spaceCo: "", inCompany: map[string]bool{"winston": true}}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "desk-dm"), map[string]any{"name": "Morgan", "description": "researcher"})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if len(r.seated) != 0 {
		t.Fatalf("desk must not seat, got %v", r.seated)
	}
	if !strings.Contains(res.Output, "desk") {
		t.Errorf("speech should say desk/unseated: %q", res.Output)
	}
}

func TestCreateAgent_SpeechHasNoAPIJargon(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn", conns: map[string]string{"c1": "github"}}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Morgan", "description": "researcher",
		"local_tools": []any{"read_file", "create_agent"},
		"toolbelt":    []any{"c1"},
	})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if jargon(res.Output) || jargon(res.Error) {
		t.Errorf("jargon in result: out=%q err=%q", res.Output, res.Error)
	}
	for _, tname := range r.persisted[0].LocalTools {
		if tname == "create_agent" {
			t.Fatal("hired specialist must not receive create_agent")
		}
	}
}

func TestCreateAgent_SchemaNameStable(t *testing.T) {
	tool := &CreateAgentTool{}
	if tool.Name() != "create_agent" {
		t.Fatalf("id must stay create_agent, got %q", tool.Name())
	}
	if tool.Schema().Function.Name != "create_agent" {
		t.Fatal("schema name must stay create_agent")
	}
}

func TestCreateAgent_SchemaDescriptionHireCreateAdd(t *testing.T) {
	tool := &CreateAgentTool{}
	desc := strings.ToLower(tool.Schema().Function.Description)
	for _, tok := range []string{"hire", "create", "add"} {
		if !strings.Contains(desc, tok) {
			t.Errorf("schema description must contain %q, got %q", tok, tool.Schema().Function.Description)
		}
	}
}

func TestCreateAgent_CreateAnAgentHitsSamePersist(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{"name": "hireprobe2", "description": "researches"})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if len(r.persisted) != 1 || r.persisted[0].Name != "hireprobe2" {
		t.Fatalf("create-an-agent must use the same persist, got %+v", r.persisted)
	}
	if jargon(res.Output) {
		t.Errorf("jargon: %q", res.Output)
	}
}

func jargon(s string) bool {
	low := strings.ToLower(s)
	for _, tok := range []string{"create_agent", "persistagent", "/api/", "409", "422", "put "} {
		if strings.Contains(low, tok) {
			return true
		}
	}
	return false
}
