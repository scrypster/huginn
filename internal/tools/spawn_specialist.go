package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// SpawnSpecialistName is the grant-gated tool name. Like create_agent, it is
// registered but never tagged "builtin" and never implied by a local_tools
// wildcard or God Mode — see agent_dispatcher.go's applyToolbelt step 4b
// equivalent for this tool, and stripHireGrant (which also strips this name
// so a spawned specialist can never itself spawn another specialist — S11).
const SpawnSpecialistName = "spawn_specialist"

// SpawnSpecialistRequest is what Deps.Spawn receives once all tool-level
// validation (name, model, rationale) has passed.
type SpawnSpecialistRequest struct {
	Name      string
	Task      string
	Model     string
	Rationale string
}

// ModelChoice is what Deps.ResolveModel returns for a validated model ID —
// the display name and verified per-MTok cost, both sourced from
// internal/models' provider catalog (see models.ProviderCatalog.Info).
type ModelChoice struct {
	DisplayName       string
	InputCostPerMTok  float64
	OutputCostPerMTok float64
}

// SpawnSpecialistDeps is wired by the live server. Callbacks keep this
// package free of server/agents/threadmgr import cycles, mirroring
// CreateAgentDeps.
type SpawnSpecialistDeps struct {
	// ValidateName rejects malformed names (letters/digits/spaces/hyphens
	// only — no colons; see agents.AgentDef.Validate).
	ValidateName func(name string) error
	// NameTaken reports whether name collides with a seated agent OR an
	// already-active ephemeral specialist (checks both overlay maps).
	NameTaken func(name string) bool
	// ResolveModel validates model against the provider catalog and returns
	// its display name + verified cost. ok=false for an unrecognized model.
	ResolveModel func(model string) (choice ModelChoice, ok bool)
	// Spawn creates the ephemeral overlay agent, fixes its company, runs it
	// through the delegation preview gate (S10 — specialist spawns always
	// require approval; a preview timeout DENIES rather than auto-approves),
	// creates and starts its thread, and registers it for auto-eviction.
	// Returns the new thread ID, or an error (e.g. preview denied) whose
	// message is surfaced to the CoS as the tool error.
	Spawn func(ctx context.Context, req SpawnSpecialistRequest) (threadID string, err error)
}

// SpawnSpecialistTool is grant-gated: register it, do not tag "builtin", do
// not add to BuiltinToolNames / delegationNames. Only named
// local_tools: ["spawn_specialist"], and CoS-only by convention (the same
// agent granted create_agent).
type SpawnSpecialistTool struct {
	Deps SpawnSpecialistDeps
}

func (t *SpawnSpecialistTool) Name() string { return SpawnSpecialistName }

func (t *SpawnSpecialistTool) Description() string {
	return "Bring in a one-off specialist for THIS thread only, when no one on the roster covers the task. " +
		"Not a hire — they are never added to the team, never seated in a company, and are auto-archived the moment their thread finishes. " +
		"Always state why the roster doesn't cover this in `rationale` (which existing teammate is the closest fit, and what they're missing) — empty rationale is refused. " +
		"Name the specialist descriptively, e.g. \"Rust Audit Specialist\" (no colons). " +
		"Pick `model` from the available models list in your system prompt, matched to the task's difficulty and cost."
}

func (t *SpawnSpecialistTool) Permission() PermissionLevel { return PermWrite }

func (t *SpawnSpecialistTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        SpawnSpecialistName,
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"name", "task", "model", "rationale"},
				Properties: map[string]backend.ToolProperty{
					"name": {
						Type:        "string",
						Description: "Specialist name, e.g. \"Rust Audit Specialist\" (letters, numbers, hyphens — no colons).",
					},
					"task": {
						Type:        "string",
						Description: "What the specialist should do, for this thread only.",
					},
					"model": {
						Type:        "string",
						Description: "Model id from the available models list, matched to task difficulty and cost.",
					},
					"rationale": {
						Type:        "string",
						Description: "Why the roster doesn't cover this: the closest-fit existing teammate and what they're missing. Required.",
					},
				},
			},
		},
	}
}

func (t *SpawnSpecialistTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	name := strings.TrimSpace(asToolString(args["name"]))
	task := strings.TrimSpace(asToolString(args["task"]))
	model := strings.TrimSpace(asToolString(args["model"]))
	rationale := strings.TrimSpace(asToolString(args["rationale"]))

	if name == "" {
		return hireErr("I need a name for the specialist.")
	}
	if task == "" {
		return hireErr("I need to know what the specialist should do.")
	}
	if model == "" {
		return hireErr("I need a model for the specialist — pick one from the available models list.")
	}
	// Empty rationale forces the CoS to justify bringing in a specialist
	// rather than delegating to an existing teammate.
	if rationale == "" {
		return hireErr("I need to say why nobody on the roster covers this before bringing in a specialist.")
	}

	// Auto-sanitize before validating: small local models routinely propose
	// "Specialist: COBOL" or "COBOL/Security". Rather than bounce the model
	// into a retry loop against a rule it keeps breaking, clean the name the
	// way a careful colleague would and proceed (live falsification on
	// v0.4.0-fable21 — the CoS looped apologizing for invalid names).
	name = sanitizeSpecialistName(name)
	if t.Deps.ValidateName != nil {
		if err := t.Deps.ValidateName(name); err != nil {
			return hireErr("That name won't work for a specialist (letters, numbers, hyphens only — no colons). Try something like \"" + suggestSpecialistName(name) + "\".")
		}
	}
	if t.Deps.NameTaken != nil && t.Deps.NameTaken(name) {
		return hireErr(name + " is already on the roster or already working as a specialist.")
	}

	var choice ModelChoice
	if t.Deps.ResolveModel != nil {
		var ok bool
		choice, ok = t.Deps.ResolveModel(model)
		if !ok {
			return hireErr("I don't recognize that model — pick one from the available models list.")
		}
	}

	if t.Deps.Spawn == nil {
		return hireErr("I couldn't bring in a specialist just now.")
	}
	if _, err := t.Deps.Spawn(ctx, SpawnSpecialistRequest{
		Name:      name,
		Task:      task,
		Model:     model,
		Rationale: rationale,
	}); err != nil {
		return hireErr(spawnSpecialistFailSpeech(name, err))
	}

	return ToolResult{Output: spawnSpecialistSpeech(name, model, choice, rationale)}
}

// specialistSuffixRE strips a trailing "specialist" (any case) from a name
// to recover its domain for the S13 speech contract, e.g.
// "Rust Audit Specialist" → "Rust Audit".
var specialistSuffixRE = regexp.MustCompile(`(?i)\s*specialist\s*$`)

// specialistDomain derives the domain word for speech from a specialist
// name. Falls back to the full name when it doesn't follow the suggested
// "<Domain> Specialist" convention.
func specialistDomain(name string) string {
	domain := strings.TrimSpace(specialistSuffixRE.ReplaceAllString(name, ""))
	if domain == "" {
		return name
	}
	return domain
}

// SpecialistDomain exports specialistDomain for the S14 promotion counter
// (server.SpecialistPromotionTracker), which needs the same domain both
// when recording a spawn and when checking the count at the specialist's
// finish line — reusing this rather than re-deriving it keeps the two
// call sites from ever disagreeing on what a specialist's "label" is.
func SpecialistDomain(name string) string {
	return specialistDomain(name)
}

// suggestSpecialistName proposes a "<Domain> Specialist" formatted name from
// a rejected name, for the validation error message.
// sanitizeSpecialistName strips characters agent names disallow (colons,
// slashes, etc.), collapses whitespace, and ensures an alphanumeric start —
// turning "Specialist: COBOL" into "Specialist COBOL" and "COBOL/Security"
// into "COBOLSecurity" so a small model's near-miss still spawns.
func sanitizeSpecialistName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if isValidSpecialistNameChar(r) {
			b.WriteRune(r)
		} else if r == ':' || r == '/' || r == ',' || r == '\\' {
			b.WriteRune(' ')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	for len(out) > 0 && !isAlnum(rune(out[0])) {
		out = strings.TrimSpace(out[1:])
	}
	if out == "" {
		out = "Domain Specialist"
	}
	return out
}

func isValidSpecialistNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == ' ' || r == '-' || r == '_' || r == '.'
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func suggestSpecialistName(name string) string {
	domain := specialistSuffixRE.ReplaceAllString(name, "")
	domain = strings.Join(strings.Fields(domain), " ")
	if domain == "" {
		domain = "Domain"
	}
	return domain + " Specialist"
}

// spawnSpecialistSpeech implements the S13 speech contract: state the why,
// name the model, and make clear this is not a hire.
func spawnSpecialistSpeech(name, model string, choice ModelChoice, rationale string) string {
	domain := specialistDomain(name)
	modelLabel := model
	if choice.DisplayName != "" {
		modelLabel = choice.DisplayName
	}
	return fmt.Sprintf(
		"Nobody on the roster covers %s — %s. Bringing in a one-off %s specialist on %s for this thread only. They won't be added to the team.",
		domain, rationale, strings.ToLower(domain), modelLabel,
	)
}

// SpecialistFinishSpeech is the deterministic finish line broadcast when a
// specialist's thread lands terminal — wired by the server's specialist
// evictor (see threadmgr.ThreadManager.SetSpecialistEvictor).
func SpecialistFinishSpeech(name string) string {
	return name + " is done and gone."
}

func spawnSpecialistFailSpeech(name string, err error) string {
	if err == nil {
		return name + " couldn't be brought in."
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "denied") || strings.Contains(msg, "declined") || strings.Contains(msg, "timeout") {
		return "That specialist spawn wasn't approved."
	}
	return name + " couldn't be brought in right now."
}

// RegisterSpawnSpecialistTool puts the specialist tool on the live registry
// without tagging it builtin.
func RegisterSpawnSpecialistTool(reg *Registry, tool *SpawnSpecialistTool) {
	if reg == nil || tool == nil {
		return
	}
	reg.Register(tool)
}
