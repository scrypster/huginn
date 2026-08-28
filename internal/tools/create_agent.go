package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// Hire vault default is {name}-huginn; Vue/TUI skins may still offer huginn-{name}.
const CreateAgentName = "create_agent"

// CreateAgentRequest is the persist payload. The server builds AgentDef and
// calls PersistAgent — this tool does not keep a second store.
type CreateAgentRequest struct {
	Name         string
	Description  string
	SystemPrompt string
	Model        string
	LocalTools   []string
	Toolbelt     []CreateAgentBeltEntry
	Memory       bool
	VaultName    string
}

type CreateAgentBeltEntry struct {
	ConnectionID string
	Provider     string
}

// CreateAgentDeps is wired by the live server. Callbacks keep this package
// free of server/agents import cycles.
type CreateAgentDeps struct {
	Persist         func(CreateAgentRequest) error
	AgentExists     func(name string) bool
	TryVault        func(vaultName, label string) bool
	SpaceCompanyID  func(spaceID string) (string, error)
	AgentInCompany  func(agent, companyID string) (bool, error)
	CompanyName     func(id string) (string, error)
	SeatMember      func(companyID, agentName string) error
	SeatSpaceMember func(spaceID, agentName string) error
	ResolveConn     func(id string) (provider string, ok bool)
	// ResolveVaultName returns the canonical vault name for a new hire.
	// Standard (MJ, 2026-08-28): "huginn:agent:<user>:<name>" — the live
	// server wires agents.ResolveAgentVaultName. When nil (tests, TUI
	// harnesses), the legacy "<name>-huginn" slug is used as a fallback.
	ResolveVaultName func(agentName string) string
	ValidateName     func(name string) error
	CallerFromCtx    func(ctx context.Context) string
	SpaceFromCtx     func(ctx context.Context) string
	CallerModel      func(ctx context.Context) string
	MachineModel     string
	// Registry is used to infer LocalTools for coding/engineering hires when
	// the caller didn't pass local_tools explicitly. Optional — nil skips
	// inference (LocalTools stays nil, matching prior behavior).
	Registry *Registry
}

// CreateAgentTool is grant-gated: register it, do not tag "builtin", do not
// add to BuiltinToolNames / delegationNames. Only named local_tools: ["create_agent"].
type CreateAgentTool struct {
	Deps CreateAgentDeps
}

func (t *CreateAgentTool) Name() string { return CreateAgentName }

func (t *CreateAgentTool) Description() string {
	return "This is how you hire, create an agent, add a teammate, or make a bot. One tool. Persist their profile onto this machine, optionally open a memory vault, and seat them in the current company. If the human already gave a name and a role, call this immediately — do not interview, do not delegate. Only ask if name or role is missing. Only offer connections that exist here. Memory is optional."
}

func (t *CreateAgentTool) Permission() PermissionLevel { return PermWrite }

func (t *CreateAgentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        CreateAgentName,
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"name", "description"},
				Properties: map[string]backend.ToolProperty{
					"name": {
						Type:        "string",
						Description: "Teammate name (letters, numbers, hyphens). Propose one during the interview.",
					},
					"description": {
						Type:        "string",
						Description: "Short role (e.g. researcher, bookkeeper).",
					},
					"role": {
						Type:        "string",
						Description: "Alias for description.",
					},
					"system_prompt": {
						Type:        "string",
						Description: "Optional persona. If empty, a short one is written from the role.",
					},
					"model": {
						Type:        "string",
						Description: "Model id. Defaults to your model, then the machine default.",
					},
					"local_tools": {
						Type:        "array",
						Description: "Builtin tools to grant this teammate (not hire).",
					},
					"toolbelt": {
						Type:        "array",
						Description: "Connection ids that already exist on this machine.",
					},
					"memory": {
						Type:        "boolean",
						Description: "Whether they keep a memory vault. Default true.",
					},
					"vault_name": {
						Type:        "string",
						Description: "Optional vault name. Default is {name}-huginn.",
					},
				},
			},
		},
	}
}

func (t *CreateAgentTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	name := strings.TrimSpace(asToolString(args["name"]))
	role := strings.TrimSpace(firstToolString(args, "description", "role"))
	// Bare "hire someone" (rubric 2.3): neither name nor role given. Ask
	// exactly ONE clarifying question — not a two-step interview where the
	// human supplies a name only to be asked for a role next, or vice
	// versa in a way that reads like a form.
	if name == "" && role == "" {
		return hireErr("Who should I hire — name and what they'll do?")
	}
	if name == "" {
		return hireErr("I need a name for them.")
	}
	if role == "" {
		return hireErr("I need a role for them.")
	}
	if t.Deps.ValidateName != nil {
		if err := t.Deps.ValidateName(name); err != nil {
			return hireErr("That name won't work as a teammate name (letters, numbers, hyphens only).")
		}
	}
	if t.Deps.AgentExists != nil && t.Deps.AgentExists(name) {
		return hireErr(name + " is already on the roster.")
	}

	// Company wall BEFORE persist. Desk (empty company) skips seat later.
	spaceID := ""
	if t.Deps.SpaceFromCtx != nil {
		spaceID = strings.TrimSpace(t.Deps.SpaceFromCtx(ctx))
	}
	caller := ""
	if t.Deps.CallerFromCtx != nil {
		caller = strings.TrimSpace(t.Deps.CallerFromCtx(ctx))
	}
	companyID := ""
	if spaceID != "" && t.Deps.SpaceCompanyID != nil {
		id, err := t.Deps.SpaceCompanyID(spaceID)
		if err != nil {
			return hireErr("I couldn't see this company's roster just now.")
		}
		companyID = strings.TrimSpace(id)
	}
	if companyID != "" {
		if caller == "" {
			return hireErr("I can't hire into this company without knowing who is asking.")
		}
		if t.Deps.AgentInCompany != nil {
			in, err := t.Deps.AgentInCompany(caller, companyID)
			if err != nil || !in {
				return hireErr("You can't hire into this company.")
			}
		}
		if t.Deps.CompanyName != nil {
			cname, _ := t.Deps.CompanyName(companyID)
			if strings.EqualFold(strings.TrimSpace(cname), "Huginn") && caller != "" && t.Deps.AgentInCompany != nil {
				in, err := t.Deps.AgentInCompany(caller, companyID)
				if err != nil || !in {
					return hireErr("You can't hire into this company.")
				}
			}
		}
	}

	connIDs := stringSliceArg(args["toolbelt"])
	var belt []CreateAgentBeltEntry
	for _, id := range connIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if t.Deps.ResolveConn == nil {
			return hireErr("That connection isn't on this machine.")
		}
		provider, ok := t.Deps.ResolveConn(id)
		if !ok {
			return hireErr("That connection isn't on this machine.")
		}
		belt = append(belt, CreateAgentBeltEntry{ConnectionID: id, Provider: provider})
	}

	localTools := stringSliceArg(args["local_tools"])
	localTools = stripHireGrant(localTools)
	inferredTools := false
	if len(localTools) == 0 && t.Deps.Registry != nil && roleLooksCoding(role) {
		if inferred := defaultCodingLocalTools(t.Deps.Registry); len(inferred) > 0 {
			localTools = inferred
			inferredTools = true
		}
	}

	model := strings.TrimSpace(asToolString(args["model"]))
	if model == "" && t.Deps.CallerModel != nil {
		model = strings.TrimSpace(t.Deps.CallerModel(ctx))
	}
	if model == "" {
		model = strings.TrimSpace(t.Deps.MachineModel)
	}
	if model == "" {
		return hireErr("I need a model for them.")
	}

	systemPrompt := strings.TrimSpace(asToolString(args["system_prompt"]))
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are %s, %s.", name, role)
		if roleLooksCoding(role) {
			// Small local models mistype long paths and quit after one failed
			// tool call. Bake the self-correction discipline into the default
			// coding-hire prompt so a miss becomes a retry, not a shrug.
			systemPrompt += " Use your tools directly. If a file path fails, run list_dir or bash to find the correct path and retry — never report failure after a single attempt without checking the directory."
		}
	}

	memory := true
	if raw, ok := args["memory"]; ok {
		memory = asToolBool(raw)
	}

	vaultName := strings.TrimSpace(asToolString(args["vault_name"]))
	if memory && vaultName == "" {
		if t.Deps.ResolveVaultName != nil {
			vaultName = t.Deps.ResolveVaultName(name)
		}
		if vaultName == "" {
			vaultName = hireVaultName(name)
		}
	}
	if !memory {
		vaultName = ""
	}

	req := CreateAgentRequest{
		Name:         name,
		Description:  role,
		SystemPrompt: systemPrompt,
		Model:        model,
		LocalTools:   localTools,
		Toolbelt:     belt,
		Memory:       memory,
		VaultName:    vaultName,
	}
	if t.Deps.Persist == nil {
		return hireErr("I couldn't add them just now.")
	}
	if err := t.Deps.Persist(req); err != nil {
		return hireErr(teammatePersistSpeech(name, err))
	}

	vaultOK := false
	if t.Deps.TryVault != nil {
		vaultOK = t.Deps.TryVault(vaultName, "huginn-"+strings.ToLower(name))
	}

	seatedWhere := ""
	desk := companyID == ""
	if !desk && t.Deps.SeatMember != nil {
		if err := t.Deps.SeatMember(companyID, name); err != nil {
			desk = true
		} else if t.Deps.CompanyName != nil {
			seatedWhere, _ = t.Deps.CompanyName(companyID)
			seatedWhere = strings.TrimSpace(seatedWhere)
			if seatedWhere == "" {
				seatedWhere = "this company"
			}
		} else {
			seatedWhere = "this company"
		}
	}
	if spaceID != "" && t.Deps.SeatSpaceMember != nil {
		_ = t.Deps.SeatSpaceMember(spaceID, name)
	}

	return ToolResult{Output: hireSpeech(name, role, localTools, belt, vaultOK, vaultName, seatedWhere, desk, inferredTools)}
}

// hireCodingRoleRE matches role/description text that signals a
// coding/engineering hire, triggering default LocalTools inference.
//
// Inference hands a hire bash + write_file + edit_file without the human
// asking, so this deliberately favours precision over recall: a missed
// coding hire is fixed by passing local_tools explicitly, but a false
// positive silently arms a shell for someone who was hired to write
// marketing copy. Matching is therefore on word boundaries and on phrases,
// never bare substrings — a naive `strings.Contains` list of
// {code, test, fix, ...} fired on "Zip code data entry", "Barcode
// inventory clerk", "manages the latest news digest", "prefix/suffix
// naming specialist", "Greatest hits curator" and "fixes the coffee
// machine and tests recipes".
//
// Ambiguous single words are excluded on purpose: "code" (dress code, zip
// code), "test" (tests recipes, user testing), "fix" (fixture, fixes the
// espresso machine) and "software" (a poet who writes about software) only
// count inside an unambiguous compound or phrase.
var hireCodingRoleRE = regexp.MustCompile(`(?i)\b(?:` +
	// Unambiguous single words.
	`coding|codebase|code ?base|developer|developers|development|` +
	`engineer|engineers|engineering|programmer|programmers|programming|` +
	`debug|debugs|debugging|debugger|refactor|refactors|refactoring|` +
	`devops|sysadmin|compiler|repository|repositories|repo|repos|` +
	// Unambiguous phrases built from otherwise-ambiguous words.
	`(?:write|writes|writing|read|reads|reading|review|reviews|reviewing|ship|ships|shipping|maintain|maintains|maintaining|our|the|source|legacy|production) code|` +
	`code review|code reviews|pull request|pull requests|` +
	`(?:fix|fixes|fixing|squash|squashes|squashing|triage|triages|triaging|hunt|hunts|hunting) (?:the |a |our |these |those )?bugs?|` +
	`bug ?fix|bug ?fixes|bug ?fixing|` +
	`unit test|unit tests|unit testing|integration test|integration tests|` +
	`test suite|test suites|test coverage|` +
	`(?:write|writes|writing) tests` +
	`)\b`)

// roleLooksCoding reports whether role/description text signals a
// coding/engineering hire. See hireCodingRoleRE for the precision rationale.
func roleLooksCoding(role string) bool {
	return hireCodingRoleRE.MatchString(role)
}

// defaultCodingLocalToolNames are the builtin tools a coding/engineering
// hire is granted by default: read a file, write a file, edit a file, run a
// shell command, list a directory.
var defaultCodingLocalToolNames = []string{"read_file", "write_file", "edit_file", "bash", "list_dir"}

// defaultCodingLocalTools returns defaultCodingLocalToolNames filtered down
// to tool names that actually exist in reg — defensive against a registry
// that doesn't carry one of these (e.g. a stripped-down deployment).
func defaultCodingLocalTools(reg *Registry) []string {
	if reg == nil {
		return nil
	}
	out := make([]string, 0, len(defaultCodingLocalToolNames))
	for _, name := range defaultCodingLocalToolNames {
		if _, ok := reg.Get(name); ok {
			out = append(out, name)
		}
	}
	return out
}

func hireSpeech(name, role string, local []string, belt []CreateAgentBeltEntry, vaultOK bool, vaultName, seated string, desk bool, inferredTools bool) string {
	var b strings.Builder
	b.WriteString(name)
	if isVerbPhraseRole(role) {
		b.WriteString(" joined the roster to ")
		b.WriteString(roleAsBaseVerbPhrase(role))
	} else {
		b.WriteString(" is on the roster as ")
		b.WriteString(role)
	}
	tools := make([]string, 0, len(local)+len(belt))
	tools = append(tools, local...)
	for _, e := range belt {
		if e.Provider != "" {
			tools = append(tools, e.Provider)
		} else {
			tools = append(tools, e.ConnectionID)
		}
	}
	if len(tools) > 0 {
		b.WriteString(" with ")
		b.WriteString(strings.Join(tools, ", "))
		if inferredTools {
			b.WriteString(" (auto-granted for coding work)")
		}
	}
	b.WriteString(". ")
	if vaultOK {
		b.WriteString("Memory vault ")
		b.WriteString(vaultName)
		b.WriteString(" is ready. ")
	} else {
		b.WriteString("No vault yet. ")
	}
	if desk || seated == "" {
		b.WriteString("This is the desk — they're not seated in a company.")
	} else {
		b.WriteString("They're seated in ")
		b.WriteString(seated)
		b.WriteString(".")
	}
	return b.String()
}

// isVerbPhraseRole reports whether role reads as a 3rd-person-singular verb
// phrase ("researches the web") rather than a noun/role phrase
// ("researcher"). It's a best-effort heuristic (not full NLP): a role whose
// first word ends in "s" (but not "ss") is treated as a verb phrase.
func isVerbPhraseRole(role string) bool {
	fields := strings.Fields(role)
	if len(fields) == 0 {
		return false
	}
	w := strings.ToLower(fields[0])
	if len(w) < 3 {
		return false
	}
	return strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss")
}

// roleAsBaseVerbPhrase de-conjugates the first word of role from 3rd-person
// singular ("researches") to base form ("research") so it reads naturally
// after "joined the roster to". Best-effort; unusual verbs fall through
// unchanged rather than mangled.
func roleAsBaseVerbPhrase(role string) string {
	fields := strings.Fields(role)
	if len(fields) == 0 {
		return role
	}
	fields[0] = baseVerbForm(fields[0])
	return strings.Join(fields, " ")
}

func baseVerbForm(word string) string {
	w := strings.ToLower(word)
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 3:
		return word[:len(word)-3] + "y"
	case strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"),
		strings.HasSuffix(w, "xes"), strings.HasSuffix(w, "zes"),
		strings.HasSuffix(w, "sses"), strings.HasSuffix(w, "oes"):
		return word[:len(word)-2]
	case strings.HasSuffix(w, "es") && len(w) > 3:
		return word[:len(word)-1]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 2:
		return word[:len(word)-1]
	default:
		return word
	}
}

func hireErr(msg string) ToolResult {
	return ToolResult{IsError: true, Error: msg}
}

func teammatePersistSpeech(name string, err error) string {
	if err == nil {
		return name + " couldn't be added."
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	if strings.Contains(low, "already exists") {
		return name + " is already on the roster."
	}
	if strings.Contains(low, "invalid toolbelt") || strings.Contains(low, "connection") {
		return "That connection isn't on this machine."
	}
	if strings.Contains(low, "vault") {
		return "That memory vault is already used."
	}
	if strings.Contains(low, "invalid agent") || strings.Contains(low, "invalid") {
		return "That teammate profile isn't valid."
	}
	return "I couldn't add them just now."
}

func hireVaultName(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' {
			sb.WriteRune('-')
		}
	}
	n := strings.Trim(sb.String(), "-")
	if n == "" {
		n = "teammate"
	}
	return n + "-huginn"
}

func stripHireGrant(tools []string) []string {
	var out []string
	for _, t := range tools {
		t = strings.TrimSpace(t)
		if t == "" || strings.EqualFold(t, CreateAgentName) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func asToolString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func firstToolString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(asToolString(args[k])); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func asToolBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || strings.EqualFold(t, "yes")
	default:
		return false
	}
}

func stringSliceArg(v any) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s := strings.TrimSpace(asToolString(item))
			if s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// RegisterCreateAgentTool puts the hire tool on the live registry without
// tagging it builtin.
func RegisterCreateAgentTool(reg *Registry, tool *CreateAgentTool) {
	if reg == nil || tool == nil {
		return
	}
	reg.Register(tool)
}
