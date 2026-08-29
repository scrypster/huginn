package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// VetTimeout bounds a single vet pass. Exported so the wiring that spawns
// vet passes (and tests) reference the same number instead of guessing one.
const VetTimeout = 120 * time.Second

// VetResult is the outcome of a one-shot adversarial reviewer pass run over
// a completed thread's diff + task description.
type VetResult struct {
	// Label is the short verdict surfaced on the thread result: "no
	// findings", "N findings", or "did not complete" on error/timeout —
	// fail-open WITH HONESTY, the caller must never present a run as
	// "Vetted" when the reviewer didn't actually finish.
	Label string
	// Findings is the reviewer's structured findings text. Empty when Label
	// is "no findings", "did not complete", or "not vetted — no diff
	// captured".
	Findings string
}

// LabelNotVetted is the HONESTY BACKSTOP label: it means capture (diff +
// untracked-file content) came back completely empty, so no reviewer pass
// was even attempted. This is a DISTINCT outcome from "did not complete"
// (which means a pass WAS attempted and failed/timed out) — callers must
// never render either one as "Vetted: ...", and must never let a model
// reply of "PASS" override this label, because there was nothing for the
// model to have actually reviewed.
const LabelNotVetted = "not vetted — no diff captured"

// DidNotComplete reports whether the vet pass failed to produce a verdict
// (timeout, backend error, or an empty/unparseable reply) — the caller must
// present this to the user honestly rather than as an implicit pass.
func (r VetResult) DidNotComplete() bool { return r.Label == "did not complete" }

// NotVetted reports whether the pass was skipped because diff capture came
// back empty — see LabelNotVetted.
func (r VetResult) NotVetted() bool { return r.Label == LabelNotVetted }

// reviewerPersona is written for a 14b model: short, imperative, concrete —
// not a character sheet. Adversarial and evidence-first per the vet-loop
// design: the reviewer's job is to find something wrong, not to agree.
const reviewerPersona = `You are a code reviewer. Your only job is to find real problems in the diff below, or say there are none. Be adversarial: assume the change is wrong until the diff proves otherwise. Do not rewrite or fix anything — only report.

Reply in exactly one of these two formats, nothing else:

PASS: no findings

or

FINDINGS:
- <finding 1, naming the file and the concrete problem>
- <finding 2>

A finding must point to something checkable in the diff: a missing error check, an untested branch, a broken invariant, or a claim in the task that the diff does not actually deliver. Do not invent findings to look thorough — if the diff genuinely looks correct and complete for the stated task, reply PASS.`

// RunVetPass runs a one-shot, tool-free adversarial reviewer RunLoop over a
// completed thread's diff + task description. It uses the SAME model
// resolution as any agent run — the reviewer runs on ag's own
// provider/model/endpoint/key — so this works on a 14b local model exactly
// like every other agent turn; a stronger model only makes the review
// better, it is never required for the mechanism to function.
//
// Bounded by VetTimeout. Fails open WITH HONESTY on any error, timeout, or
// unparseable reply: the caller gets Label == "did not complete" rather than
// an invented pass, and the deliverable is never blocked waiting on this.
//
// No recursion by construction: this is a plain RunLoop call, not a
// threadmgr thread. No Thread object is ever created for the vet pass, so it
// can never itself trigger the OnStatusChange hook that spawns vet passes —
// there is nothing to loop on.
func RunVetPass(ctx context.Context, b backend.Backend, ag *agents.Agent, task, diff string) VetResult {
	if b == nil || ag == nil {
		return VetResult{Label: "did not complete"}
	}
	ctx, cancel := context.WithTimeout(ctx, VetTimeout)
	defer cancel()

	diffText := strings.TrimSpace(diff)
	if diffText == "" {
		// HONESTY BACKSTOP: nothing was captured to review (no diff, no
		// untracked-file content) — never let a "PASS: no findings" verdict
		// get printed for a review the model never actually ran. Skip the
		// backend call entirely rather than asking a model to grade a review
		// input it was never given.
		return VetResult{Label: LabelNotVetted}
	}
	userMsg := fmt.Sprintf("## Task\n%s\n\n## Diff\n%s", strings.TrimSpace(task), diffText)

	// A fresh, unregistered reviewer identity — never added to any registry,
	// never persisted. Same model/provider/endpoint/key as the agent whose
	// work is being reviewed; different persona.
	reviewer := &agents.Agent{
		Name:         "vet-reviewer",
		ModelID:      ag.ModelID,
		Provider:     ag.Provider,
		Endpoint:     ag.Endpoint,
		APIKey:       ag.APIKey,
		SystemPrompt: reviewerPersona,
	}

	msgs := []backend.Message{
		{Role: "system", Content: agents.BuildPersonaPrompt(reviewer, "")},
		{Role: "user", Content: userMsg},
	}

	result, err := RunLoop(ctx, RunLoopConfig{
		MaxTurns:  1,
		ModelName: reviewer.GetModelID(),
		Messages:  msgs,
		Backend:   b,
		AgentName: reviewer.Name,
	})
	if err != nil {
		slog.Warn("vet: reviewer pass failed", "agent", ag.Name, "err", err)
		return VetResult{Label: "did not complete"}
	}
	if result == nil {
		slog.Warn("vet: reviewer pass returned nil result", "agent", ag.Name)
		return VetResult{Label: "did not complete"}
	}
	return parseVetVerdict(result.FinalContent)
}

// parseVetVerdict interprets the reviewer's reply per reviewerPersona's
// contract. A reply that doesn't clearly match either format is treated as
// real (if messy) findings rather than silently downgraded to "no
// findings" — an honest, slightly-ugly label beats an invented clean one.
func parseVetVerdict(reply string) VetResult {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return VetResult{Label: "did not complete"}
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), "PASS") {
		return VetResult{Label: "no findings"}
	}

	findings := trimmed
	if idx := strings.Index(strings.ToUpper(trimmed), "FINDINGS:"); idx >= 0 {
		findings = strings.TrimSpace(trimmed[idx+len("FINDINGS:"):])
	}
	count := 0
	for _, line := range strings.Split(findings, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-") {
			count++
		}
	}
	if count == 0 {
		count = 1 // reviewer replied with prose instead of bullets — still real findings, not zero
	}
	plural := "s"
	if count == 1 {
		plural = ""
	}
	return VetResult{Label: fmt.Sprintf("%d finding%s", count, plural), Findings: findings}
}
