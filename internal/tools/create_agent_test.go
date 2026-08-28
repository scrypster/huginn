package tools

import (
	"context"
	"strings"
	"testing"
)

type hireRec struct {
	persisted   []CreateAgentRequest
	vaults      []string
	seated      []string
	spaceSeated []string
	exists      map[string]bool
	conns       map[string]string
	spaceCo     string
	inCompany   map[string]bool
	coName      string
	persistErr  error
	nameErr     error
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
		Registry:     hireTestRegistry(),
	}
}

// hireTestRegistry mirrors the real server's builtin tool wiring so
// role-based local_tools inference has real tools to look up.
func hireTestRegistry() *Registry {
	reg := NewRegistry()
	RegisterBuiltins(reg, "/tmp", 0)
	return reg
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

// Canonical standard (MJ, 2026-08-28): when the server wires ResolveVaultName,
// hires get "huginn:agent:<user>:<name>". The "<name>-huginn" slug is only the
// unwired fallback (covered by TestCreateAgent_VaultDefaultNameHuginn).
func TestCreateAgent_VaultUsesCanonicalResolver(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	d := r.deps()
	d.ResolveVaultName = func(agentName string) string {
		return "huginn:agent:tester:" + strings.ToLower(agentName)
	}
	tool := &CreateAgentTool{Deps: d}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{"name": "Morgan", "description": "researcher"})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if len(r.vaults) != 1 || r.vaults[0] != "huginn:agent:tester:morgan" {
		t.Fatalf("vaults=%v, want huginn:agent:tester:morgan", r.vaults)
	}
	if r.persisted[0].VaultName != "huginn:agent:tester:morgan" {
		t.Errorf("persisted vault %q", r.persisted[0].VaultName)
	}
	if !strings.Contains(res.Output, "huginn:agent:tester:morgan") {
		t.Errorf("speech missing canonical vault: %q", res.Output)
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

// TestCreateAgent_HireAckGrammarVerbPhraseRole covers defect #2: a verb-phrase
// description ("researches the web") was spliced verbatim after "is on the
// roster as", producing "fableprobe is on the roster as researches the web."
// It must now read as a grammatical sentence.
func TestCreateAgent_HireAckGrammarVerbPhraseRole(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "fableprobe", "description": "researches the web",
	})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if strings.Contains(res.Output, "on the roster as researches") {
		t.Fatalf("verbatim splice leaked ungrammatical phrase: %q", res.Output)
	}
	if !strings.Contains(res.Output, "fableprobe joined the roster to research the web") {
		t.Errorf("want a grammatical role phrase, got %q", res.Output)
	}
}

// TestCreateAgent_HireAckGrammarNounPhraseRole covers the other branch: a
// noun/role-phrase description keeps the original, already-grammatical
// "is on the roster as <role>" wording.
func TestCreateAgent_HireAckGrammarNounPhraseRole(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Morgan", "description": "researcher",
	})
	if res.IsError {
		t.Fatal(res.Error)
	}
	if !strings.Contains(res.Output, "Morgan is on the roster as researcher") {
		t.Errorf("want noun-phrase role wording, got %q", res.Output)
	}
}

func TestIsVerbPhraseRole(t *testing.T) {
	cases := []struct {
		role string
		want bool
	}{
		{"researches the web", true},
		{"manages the calendar", true},
		{"writes code", true},
		{"handles support tickets", true},
		{"researcher", false},
		{"bookkeeper", false},
		{"customer support", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isVerbPhraseRole(tc.role); got != tc.want {
			t.Errorf("isVerbPhraseRole(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
}

func TestBaseVerbForm(t *testing.T) {
	cases := []struct {
		word, want string
	}{
		{"researches", "research"},
		{"manages", "manage"},
		{"writes", "write"},
		{"helps", "help"},
		{"goes", "go"},
		{"studies", "study"},
	}
	for _, tc := range cases {
		if got := baseVerbForm(tc.word); got != tc.want {
			t.Errorf("baseVerbForm(%q) = %q, want %q", tc.word, got, tc.want)
		}
	}
}

// defaultHireCodingTools is the exact tool set role-based inference should
// grant a coding/engineering hire when no explicit local_tools was given.
var defaultHireCodingTools = []string{"read_file", "write_file", "edit_file", "bash", "list_dir"}

func TestCreateAgent_CodingRoleInfersLocalTools(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Dex", "description": "Fix bugs in our Go codebase",
	})
	if res.IsError {
		t.Fatalf("hire should succeed: %s", res.Error)
	}
	if len(r.persisted) != 1 {
		t.Fatalf("expected persist, got %d", len(r.persisted))
	}
	got := r.persisted[0].LocalTools
	if strings.Join(got, ",") != strings.Join(defaultHireCodingTools, ",") {
		t.Fatalf("LocalTools = %v, want %v", got, defaultHireCodingTools)
	}
}

func TestCreateAgent_CodingRoleTitleInfersLocalTools(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Priya", "description": "Software Engineer",
	})
	if res.IsError {
		t.Fatalf("hire should succeed: %s", res.Error)
	}
	got := r.persisted[0].LocalTools
	if strings.Join(got, ",") != strings.Join(defaultHireCodingTools, ",") {
		t.Fatalf("LocalTools = %v, want %v", got, defaultHireCodingTools)
	}
}

func TestCreateAgent_NonCodingRoleStaysToolless(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Marcy", "description": "Handles marketing copy",
	})
	if res.IsError {
		t.Fatalf("hire should succeed: %s", res.Error)
	}
	if r.persisted[0].LocalTools != nil {
		t.Fatalf("LocalTools = %v, want nil (unchanged behavior)", r.persisted[0].LocalTools)
	}
}

func TestCreateAgent_ExplicitLocalToolsNotOverriddenByInference(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Dex", "description": "Software Engineer",
		"local_tools": []any{"read_file"},
	})
	if res.IsError {
		t.Fatalf("hire should succeed: %s", res.Error)
	}
	got := r.persisted[0].LocalTools
	if strings.Join(got, ",") != "read_file" {
		t.Fatalf("LocalTools = %v, want [read_file] (explicit must win)", got)
	}
}

func TestCreateAgent_InferredToolsMentionedInAck(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	tool := &CreateAgentTool{Deps: r.deps()}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Dex", "description": "Fix bugs in our Go codebase",
	})
	if res.IsError {
		t.Fatalf("hire should succeed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "auto-granted for coding work") {
		t.Errorf("ack should mention inferred tools, got %q", res.Output)
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

// TestRoleLooksCoding_Precision pins the inference predicate against the
// false positives a bare substring match produced. Inference arms bash +
// write_file + edit_file without the human asking, so a non-coding role
// matching here is a real capability leak, not a cosmetic miss.
func TestRoleLooksCoding_Precision(t *testing.T) {
	coding := []string{
		"Software Engineer",
		"Fix bugs in our Go codebase",
		"Backend developer",
		"QA engineer who writes tests",
		"Debug production incidents",
		"Refactoring the payments module",
		"Reviews pull requests for the platform team",
		"Owns the integration test suite",
		"DevOps on call",
	}
	for _, role := range coding {
		if !roleLooksCoding(role) {
			t.Errorf("roleLooksCoding(%q) = false, want true", role)
		}
	}

	notCoding := []string{
		"writes poetry about software testing",
		"hiring manager",
		"fixes the coffee machine and tests recipes",
		"Handles marketing copy",
		"Dress code compliance officer",
		"Zip code data entry",
		"Barcode inventory clerk",
		"Manages the latest news digest",
		"Runs contests and protests coverage",
		"Prefix/suffix naming specialist",
		"Fixture photographer",
		"Greatest hits curator",
		"Tests our product with real users (UX researcher)",
		"Executive assistant",
		"Financial analyst",
	}
	for _, role := range notCoding {
		if roleLooksCoding(role) {
			t.Errorf("roleLooksCoding(%q) = true, want false (would auto-grant bash/write_file)", role)
		}
	}
}

// TestCreateAgent_NilRegistryDegradesSafely covers the TUI / stripped-down
// wiring where Deps.Registry is never set: inference is skipped entirely
// and LocalTools stays nil, matching pre-inference behavior.
func TestCreateAgent_NilRegistryDegradesSafely(t *testing.T) {
	r := &hireRec{spaceCo: "co1", inCompany: map[string]bool{"winston": true}, coName: "Huginn"}
	deps := r.deps()
	deps.Registry = nil
	tool := &CreateAgentTool{Deps: deps}
	res := tool.Execute(hireCtx("Winston", "space-1"), map[string]any{
		"name": "Dex", "description": "Software Engineer",
	})
	if res.IsError {
		t.Fatalf("hire should succeed: %s", res.Error)
	}
	if r.persisted[0].LocalTools != nil {
		t.Fatalf("LocalTools = %v, want nil with no registry", r.persisted[0].LocalTools)
	}
	if strings.Contains(res.Output, "auto-granted") {
		t.Errorf("ack should not claim auto-granted tools, got %q", res.Output)
	}
}
