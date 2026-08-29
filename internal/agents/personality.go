package agents

// Personality preset names, persisted verbatim on AgentDef.Personality /
// Agent.Personality. Empty string is also valid and behaves like "default".
//
// SAFETY RULE: a personality preset may only ADD checks to the harness
// (e.g. defaulting vet_work on for strict-reviewer) — it must never remove
// or weaken a permission gate, exec prompt, or hook. This file does not
// import internal/permissions and never will; ResolveVetWork below is the
// only harness binding a preset controls, and it only ever turns a review
// pass ON by default, never off for another agent, and never touches the
// Gate itself.
const (
	PersonalityDefault            = "default"
	PersonalityStrictReviewer     = "strict-reviewer"
	PersonalityFastBuilder        = "fast-builder"
	PersonalitySkepticalArchitect = "skeptical-architect"
	PersonalityTerseOperator      = "terse-operator"
)

// PersonalityPreset describes one preset for the web editor's select.
type PersonalityPreset struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// PersonalityPresets lists every valid preset in display order, for the
// agent editor UI and for validation. "" (empty string) is also accepted
// on AgentDef/Agent and is treated identically to "default".
var PersonalityPresets = []PersonalityPreset{
	{Value: PersonalityDefault, Label: "Default", Description: "No behavioral overlay — standard Huginn tone."},
	{Value: PersonalityStrictReviewer, Label: "Strict Reviewer", Description: "Verification-first; vets its own work before calling anything done."},
	{Value: PersonalityFastBuilder, Label: "Fast Builder", Description: "Biases to action, ships in small steps."},
	{Value: PersonalitySkepticalArchitect, Label: "Skeptical Architect", Description: "Challenges your framing with evidence before agreeing."},
	{Value: PersonalityTerseOperator, Label: "Terse Operator", Description: "Short, no-filler replies — facts and next steps only."},
}

var validPersonalityValues = map[string]bool{
	"":                            true,
	PersonalityDefault:            true,
	PersonalityStrictReviewer:     true,
	PersonalityFastBuilder:        true,
	PersonalitySkepticalArchitect: true,
	PersonalityTerseOperator:      true,
}

// ValidPersonality reports whether name is a recognized preset (including "").
func ValidPersonality(name string) bool {
	return validPersonalityValues[name]
}

// PersonalityAddendum returns the persona system-prompt addendum for the
// given preset, or "" for "default"/unknown/empty — no overlay.
//
// Written for a 14b local model: short, imperative, concrete behavioral
// instructions, not a flowery character sheet. Injected by BuildPersonaPrompt
// (see agent.go), so it reaches direct chat, CodeWithAgent, and delegated
// worker threads (threadmgr's buildPersonaContent calls BuildPersonaPrompt
// too) through the single shared code path — no second injection point to
// keep in sync.
func PersonalityAddendum(name string) string {
	switch name {
	case PersonalityStrictReviewer:
		return strictReviewerAddendum
	case PersonalityFastBuilder:
		return fastBuilderAddendum
	case PersonalitySkepticalArchitect:
		return skepticalArchitectAddendum
	case PersonalityTerseOperator:
		return terseOperatorAddendum
	default:
		return ""
	}
}

const strictReviewerAddendum = `## Personality: Strict Reviewer

You verify before you claim. Never say "done", "fixed", "working", or "complete" without naming the exact check that proves it — a command you ran, a test that passed, a file you re-read after editing it. If you have not run that check yet, say "unverified" instead of guessing. Prefer the slower, correct path over a fast unverified one. When you finish a task, end with one short line naming what you verified and how.`

const fastBuilderAddendum = `## Personality: Fast Builder

Bias to action. Take the smallest next step that moves the task forward and run it — do not over-plan before touching anything. Prefer one small edit plus a quick check over a long upfront analysis. Ship incrementally and report progress after each small step instead of going silent for a long stretch. This changes your pacing only — it never relaxes a permission prompt or a safety check.`

const skepticalArchitectAddendum = `## Personality: Skeptical Architect

Do not agree with the user's framing by default. Before accepting an approach, name at least one concrete risk, alternative, or piece of missing evidence — even when the user sounds confident. If their instinct is right but their stated reason for it is wrong, say both halves: agree with the conclusion, correct the reasoning. Ground every objection in something checkable — a file, a test, a log line — never a vague feeling.`

const terseOperatorAddendum = `## Personality: Terse Operator

Keep replies short: a few sentences or a short list, no preamble, no restating the question, no closing summary unless asked for one. Lead with the answer or the result, not the process. Skip filler like "Great question" or "I'll now...". If real detail is needed, say so in one line and let the user ask for it.`

// ResolveVetWork computes the effective vet_work setting for an agent.
// An explicit override always wins — a user's own choice is never
// overridden by their personality preset. With no override, vet_work
// defaults to true only for strict-reviewer and false for every other
// preset (including "" / "default").
//
// This is a config-level default only: it decides whether the vet pass
// described in internal/agent's vet wiring runs for this agent's threads.
// It does not read or modify the permissions Gate.
func ResolveVetWork(personality string, override *bool) bool {
	if override != nil {
		return *override
	}
	return personality == PersonalityStrictReviewer
}
