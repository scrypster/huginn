package agent

import (
	"context"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/tools"
)

// Forget is a harness-side gate, same shape as the named-hire fast path:
// "forget my dog's name" is imperative, not a chat turn for the model to
// interpret. The harness recalls the subject, forgets the best strong-band
// match, and speaks the outcome — no model round trip.

// forgetVerbRE matches the imperative opener, captured in two shapes with
// very different trust levels:
//   - "forget ..." — forget is memory-domain by itself.
//   - "delete/remove ..." — ONLY with explicit memory framing ("what I told
//     you about X", "that memory", "... from (your) memory"). Bare
//     "delete the channel" / "remove Steve from the company" are roster and
//     channel COMMANDS; hijacking them into a memory deletion destroys an
//     unrelated memory and lies that the command succeeded (Opus vet,
//     2026-08-28 — BLOCK-level finding).
var forgetVerbRE = regexp.MustCompile(`(?i)^(?:please\s+|can\s+you\s+|could\s+you\s+)?forget\b\s*(.*)$`)

// deleteMemoryFramedRE is the delete/remove form, valid only with memory
// framing around the subject.
var deleteMemoryFramedRE = regexp.MustCompile(`(?i)^(?:please\s+|can\s+you\s+|could\s+you\s+)?(?:delete|remove)\s+(?:what\s+(?:i|we)\s+(?:said|told\s+you)\s+about\s+(.+)|(?:the\s+)?(?:note|fact|memor(?:y|ies))s?\s+(?:about|on|of)\s+(.+)|(.+?)\s+from\s+(?:your\s+)?memory|(my\s+.+))\s*$`)

// forgetVagueSubjectRE rejects wildcard subjects ("it", "that", "this",
// "everything") — a recall on those matches arbitrary memories.
var forgetVagueSubjectRE = regexp.MustCompile(`(?i)^(?:it|that|this|them|everything|all(?:\s+of\s+it)?)$`)

// forgetAboutWrapperRE strips the "what I told you about" / "that I said
// about" / "about" framing so the captured subject is the bare noun phrase.
var forgetAboutWrapperRE = regexp.MustCompile(`(?i)^(?:what\s+(?:i|we)\s+(?:said|told\s+you)\s+about\s+|that\s+(?:i|we)\s+(?:said|told\s+you)\s+about\s+|about\s+)`)

// forgetNameSuffixRE strips a trailing "'s name" / " name" so "my dog's
// name" resolves to the same subject phrase ("my dog") a stored fact about
// it would use ("my dog is named Odin").
var forgetNameSuffixRE = regexp.MustCompile(`(?i)('s)?\s+name$`)

// isForgetAsk reports whether userMsg is an imperative request to forget
// something — never a question (even one that mentions "forget").
func isForgetAsk(userMsg string) bool {
	_, ok := forgetSubject(userMsg)
	return ok
}

// forgetSubject extracts the normalized subject phrase from an imperative
// forget/delete/remove ask. Returns ok=false for questions or asks with no
// extractable subject.
func forgetSubject(userMsg string) (string, bool) {
	body := stripLeadingMentions(strings.TrimSpace(userMsg))
	if body == "" {
		return "", false
	}
	if strings.HasSuffix(body, "?") {
		return "", false
	}
	var rest string
	if m := forgetVerbRE.FindStringSubmatch(body); len(m) >= 2 {
		rest = strings.TrimSpace(m[1])
	} else if m := deleteMemoryFramedRE.FindStringSubmatch(body); len(m) >= 5 {
		for _, g := range m[1:] {
			if strings.TrimSpace(g) != "" {
				rest = strings.TrimSpace(g)
				break
			}
		}
	} else {
		return "", false
	}
	rest = forgetAboutWrapperRE.ReplaceAllString(rest, "")
	rest = strings.TrimSpace(rest)
	rest = strings.Trim(rest, " \t.!")
	rest = forgetNameSuffixRE.ReplaceAllString(rest, "")
	rest = strings.TrimSpace(rest)
	if rest == "" || forgetVagueSubjectRE.MatchString(rest) {
		return "", false
	}
	return rest, true
}

// tryForgetFastPath handles an imperative forget ask entirely in the
// harness: recall the subject, forget the best strong-band match, and speak
// the outcome. Returns false (no-op) when the turn isn't forget-shaped or
// the muninn recall/forget tools aren't available, so the caller falls
// through to the normal model turn.
func (o *Orchestrator) tryForgetFastPath(ctx context.Context, ag *agents.Agent, userMsg string, sess *Session, reg *tools.Registry, onToken func(string)) bool {
	if o == nil || ag == nil || reg == nil || sess == nil {
		return false
	}
	subject, ok := forgetSubject(userMsg)
	if !ok {
		return false
	}
	recallTool, hasRecall := reg.Get("muninn_recall")
	forgetTool, hasForget := reg.Get("muninn_forget")
	if !hasRecall || !hasForget {
		return false
	}
	vault := pinMuninnVault(ag.VaultName)
	res := recallTool.Execute(ctx, map[string]any{"vault": vault, "context": subject})
	var speech string
	if !res.IsError {
		hits := parseRecallHits(res.Output)
		if hit, found := bestSubjectHit(hits, subject); found {
			fres := forgetTool.Execute(ctx, map[string]any{"vault": vault, "id": hit.ID})
			if !fres.IsError {
				speech = "Forgotten — " + subject + "."
			}
		}
	}
	if speech == "" {
		speech = "I don't have anything stored about " + subject + "."
	}
	if onToken != nil {
		onToken(speech)
	}
	appendHistoryHonoringGate(sess, userMsg, speech, nil, false)
	o.compactHistoryAsync(sess)
	return true
}
