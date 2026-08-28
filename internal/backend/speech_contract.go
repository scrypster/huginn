package backend

import (
	"log/slog"
	"os"
	"unicode/utf8"
)

// ============================================================================
// THE OUTPUT CONTRACT
// ============================================================================
//
// Everything a model emits in the assistant content channel is a candidate
// for teammate speech, but most of it is not: tool-call JSON a local model
// re-types instead of calling, wait placeholders, playbook instructions the
// model parrots back, engine/harness status leaking into the channel, a
// verbatim echo of the human's own ask, relay frames ("Result from agent
// 'Steve':"), a bare clock stamp on a turn that never asked for the time,
// raw tool errors, and self-referential correction glue ("It seems there
// was a mistake in my response…"). FinalizeSpeech is the single place that
// turns raw assistant content into the text a teammate actually sees.
//
// THE SPEC — what may reach a teammate:
//   - First-person, present/past tense prose about work the model did or is
//     reporting ("I checked the logs.", "Steve reported: DELTA.").
//   - Never a tool identifier or tool-call JSON (name/function_name/arguments).
//   - Never tool-plan narration ("I'll use the muninn_recall function…").
//   - Never engine/harness status (DELEGATE_FAIL, bare tool-name lines, the
//     "Local time now:" label, "Loading model, please wait…").
//   - Never playbook/instruction text the model is echoing back to itself
//     (wait_for_threads usage, "use the following format:", spawned-thread
//     narration).
//   - Never a verbatim echo of the human's own ask, or a trailing echo
//     fragment (digit/capital runs already said earlier in the turn).
//   - Never a relay frame ("Result from agent 'Steve':") — the wrapper is
//     peeled, the body underneath stays.
//   - Never a bare clock stamp UNLESS this turn's ask was a time ask.
//   - Never a raw tool error string standing alone as the "answer".
//   - Never self-referential correction glue ("It seems there was a mistake
//     in my response. I will address the issue directly…").
//
// ORDER OF PASSES (all documented here, nowhere else):
//
//   Stage "stream" (afterTools == false — mid-generation, before any tool in
//   this turn has run):
//     1. leading-tool-call-strip   — drop a leading tool-call JSON/XML prefix
//     2. harness-visible-tokens    — drop TOOL_FAIL/DELEGATE_FAIL + bare tool names
//     3. residual-speech           — wait tags, glue, tool-plan narration, playbook
//     4. harness-clock-label       — drop the injected "Local time now:" label
//
//   No production caller actually reaches this stage through FinalizeSpeech —
//   every stream-stage call site (content_tool_calls.go, residual_speech.go,
//   the live UI's mid-turn render) calls VisibleAssistantContent directly.
//   FinalizeSpeech's `!input.AfterTools` branch below documents the same
//   four passes, in the same order, by delegating straight to
//   VisibleAssistantContent — this doc comment describes what actually
//   executes on that path, not a parallel implementation.
//
//   Stage "display" (afterTools == true, persist == false — tools already
//   ran or were denied this turn, speech is being shown but not yet stored):
//     1. leading-tool-call-strip
//     2. embedded-tool-json        — drop mid-message tool-call JSON
//     3. residual-speech-after-tools — wait/glue/playbook/echo/self-correction/
//                                      relay-frame/leftover-helpdesk (the
//                                      battle-tested pipeline in residual_speech.go)
//     4. harness-visible-tokens
//     5. harness-clock-label
//     6. time-excuse-sentences     — only when the ask is a time ask
//     7. company-wall-rewrite      — rewrite a missing-agent deny to the honest wall line
//     8. hostname-fail-rewrite     — rewrite leftover-empty Sam/hostname fails
//     9. time-fail-rewrite         — rewrite leftover-empty clock fails
//    10. invalid-tool-pong-rewrite — rewrite "not a valid tool" wrapping around a real PONG
//    11. harness-clock-label       — re-applied after any rewrite above
//
//   Stage "persist" (afterTools == true, persist == true — the turn is over,
//   this is what gets written to the transcript): everything in "display",
//   then:
//    12. leading-bare-clock-sentence — drop a leading bare stamp sentence
//    13. leftover-clock-not-time-ask — drop leftover clock speech on a non-time ask
//    14. leftover-hire-ghost         — drop "They're here." leftover on a non-hire turn
//    15. leftover-delegated-hire     — drop "Delegated to @X:" leftover on a hire turn
//    16. echo-ack-rewrite            — rewrite a verbatim echo of the ask into an ack
//    17. statement-fragment-ack-rewrite — rewrite a bare fragment ack
//    18. trivial-ping-fill           — fill/drop "Pong." to match a ping/non-ping ask
//    19. trivial-ack-fill            — fill "Got it."/"You're welcome." for empty acks
//    20. close-incomplete-persist    — add a trailing period to a cut-off clause
//
// Every rule that changes the text is attributed by name in SpeechResult so
// a human — or HUGINN_SPEECH_DEBUG=1 — can see exactly which rule ate or
// rewrote which span. This layer does not re-derive the individual regexes;
// it orders and observes the existing, tested functions.
// ============================================================================

// DroppedSpan is a span of raw text a rule removed entirely.
type DroppedSpan struct {
	Text string
	Rule string
}

// Rewrite is a span of raw text a rule replaced with different text.
type Rewrite struct {
	From string
	To   string
	Rule string
}

// SpeechInput is everything FinalizeSpeech needs to turn raw assistant
// content into teammate-visible speech.
type SpeechInput struct {
	// Raw is the assistant content exactly as the model emitted it.
	Raw string
	// UserAsk is the human's line for this turn (used to detect trivial
	// asks, time asks, hire asks, and to keep an honest wall-deny answer).
	UserAsk string
	// AfterTools is true once at least one tool ran or was denied this
	// turn (selects the "display"/"persist" stage over "stream").
	AfterTools bool
	// Persist is true when this is the final, stored transcript row (the
	// "persist" stage) rather than an in-flight display-only render.
	Persist bool
	// ToolsRan names the tools that executed this turn. Not required by
	// any rule today, but part of the contract surface so a future rule
	// can condition on it without widening SpeechInput again.
	ToolsRan []string
	// DeniedAgent is the agent name a delegate/consult call was denied
	// for this turn, if any.
	DeniedAgent string
}

// SpeechResult is FinalizeSpeech's output: the teammate-visible speech plus
// an attributed record of every span dropped or rewritten to produce it.
type SpeechResult struct {
	Speech   string
	Dropped  []DroppedSpan
	Rewrites []Rewrite
}

// speechDebug, when true, logs every dropped/rewritten span via slog.Debug.
// Enabled by HUGINN_SPEECH_DEBUG=1 at process start, or by tests via
// SetSpeechDebug.
var speechDebug = os.Getenv("HUGINN_SPEECH_DEBUG") == "1"

// SetSpeechDebug overrides the debug hook for tests. Returns a restore func.
func SetSpeechDebug(on bool) (restore func()) {
	prev := speechDebug
	speechDebug = on
	return func() { speechDebug = prev }
}

// FinalizeSpeech is the single entry point for turning raw assistant
// content into teammate-visible speech. See the package doc comment above
// for the full contract spec and the explicit, ordered list of passes.
func FinalizeSpeech(input SpeechInput) SpeechResult {
	r := &SpeechResult{}
	s := input.Raw

	if !input.AfterTools {
		// VisibleAssistantContent already is the ordered leading-tool-call
		// strip + harness-token + residual-speech + clock-label pipeline for
		// this stage; wrapped whole so its internal ordering (it re-checks
		// for a leading tool call after the first token strip) is not
		// re-derived here.
		s = r.step(s, "visible-assistant-content", VisibleAssistantContent)
		r.Speech = s
		r.debugLog()
		return *r
	}

	ask := input.UserAsk

	s = r.step(s, "visible-assistant-content", VisibleAssistantContent)
	s = r.step(s, "embedded-tool-json", stripEmbeddedHarnessToolJSON)
	s = r.step(s, "residual-speech-after-tools", StripResidualSpeechAfterTools)
	s = r.step(s, "harness-visible-tokens", stripHarnessVisibleTokens)
	s = r.step(s, "harness-clock-label", stripHarnessClockLabel)

	if isTimeAsk(ask) {
		s = r.step(s, "time-excuse-sentences", dropTimeExcuseSentences)
	}

	if rewrite := teammateCompanyWallRewrite(s, input.Raw, ask); rewrite != "" {
		s = r.step(s, "company-wall-rewrite", func(string) string { return rewrite })
	} else if rewrite := teammateHostnameFailRewrite(s, input.Raw, ask); rewrite != "" {
		s = r.step(s, "hostname-fail-rewrite", func(string) string { return rewrite })
	} else if rewrite := teammateTimeFailRewrite(s, input.Raw, ask); rewrite != "" {
		s = r.step(s, "time-fail-rewrite", func(string) string { return rewrite })
	} else if rewrite := teammateInvalidToolPongRewrite(s, input.Raw, ask); rewrite != "" {
		s = r.step(s, "invalid-tool-pong-rewrite", func(string) string { return rewrite })
	}
	s = r.step(s, "harness-clock-label", stripHarnessClockLabel)

	if !input.Persist {
		r.Speech = s
		r.debugLog()
		return *r
	}

	s = r.step(s, "leading-bare-clock-sentence", func(s string) string {
		return stripLeadingBareClockSentence(s, ask)
	})
	s = r.step(s, "leftover-clock-not-time-ask", func(s string) string {
		return dropLeftoverClockWhenNotTimeAsk(s, ask)
	})
	s = r.step(s, "leftover-hire-ghost", func(s string) string {
		return dropLeftoverHireGhost(s, ask)
	})
	s = r.step(s, "leftover-delegated-hire", func(s string) string {
		return dropLeftoverDelegatedHire(s, ask)
	})
	s = r.step(s, "echo-ack-rewrite", func(s string) string {
		return EchoAckRewrite(s, ask)
	})
	s = r.step(s, "statement-fragment-ack-rewrite", func(s string) string {
		return StatementFragmentAckRewrite(s, ask)
	})
	s = r.step(s, "trivial-ping-fill", func(s string) string {
		return fillTrivialPingPersist(s, ask)
	})
	s = r.step(s, "trivial-ack-fill", func(s string) string {
		return fillTrivialAckPersist(s, ask)
	})
	s = r.step(s, "close-incomplete-persist", closeIncompletePersist)

	r.Speech = s
	r.debugLog()
	return *r
}

// step runs fn, and if it changed s, records the change as a Dropped span
// (fn removed text outright) or a Rewrite (fn replaced text with other
// text), attributed to name. The recorded span is the minimal middle
// section that differs once common prefix/suffix are trimmed off, so a
// rule that only touches one sentence in a long message is not blamed for
// the whole message.
func (r *SpeechResult) step(s, name string, fn func(string) string) string {
	out := fn(s)
	if out == s {
		return out
	}
	before, after := diffMiddle(s, out)
	if before == "" && after == "" {
		return out
	}
	if after == "" {
		r.Dropped = append(r.Dropped, DroppedSpan{Text: before, Rule: name})
	} else {
		r.Rewrites = append(r.Rewrites, Rewrite{From: before, To: after, Rule: name})
	}
	return out
}

// diffMiddle trims the common prefix and suffix off a and b and returns
// what's left of each — the minimal span that actually differs.
//
// The cut points are snapped outward to UTF-8 rune boundaries. Comparing
// byte-wise is what makes the scan cheap, but slicing on a raw byte index
// splits a multi-byte rune whenever the difference starts or ends mid-rune
// ("日本語テキスト" -> "日本語スト" cuts inside 三 bytes), which would put
// invalid UTF-8 into the attribution spans that reach slog and any caller
// marshalling SpeechResult. Widening the reported span by at most a couple
// of bytes keeps every span valid text.
func diffMiddle(a, b string) (before, after string) {
	prefix := 0
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for prefix < limit && a[prefix] == b[prefix] {
		prefix++
	}
	// Back the prefix cut up to the start of the rune it landed inside.
	// a[:prefix] and b[:prefix] are identical, so one adjustment fits both.
	for prefix > 0 && !(runeStartsAt(a, prefix) && runeStartsAt(b, prefix)) {
		prefix--
	}
	suffix := 0
	for suffix < limit-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	// Shrink the suffix so its first byte is a rune start. The common suffix
	// bytes are identical in a and b, so a's boundary is b's boundary too,
	// and shrinking keeps it a valid common suffix.
	for suffix > 0 && !(runeStartsAt(a, len(a)-suffix) && runeStartsAt(b, len(b)-suffix)) {
		suffix--
	}
	before = a[prefix : len(a)-suffix]
	after = b[prefix : len(b)-suffix]
	return before, after
}

// runeStartsAt reports whether index i in s is a valid rune boundary.
func runeStartsAt(s string, i int) bool {
	return i >= len(s) || utf8.RuneStart(s[i])
}

func (r *SpeechResult) debugLog() {
	if !speechDebug {
		return
	}
	for _, d := range r.Dropped {
		slog.Debug("speechcontract: dropped span", "rule", d.Rule, "text", truncateForLog(d.Text))
	}
	for _, rw := range r.Rewrites {
		slog.Debug("speechcontract: rewrote span", "rule", rw.Rule, "from", truncateForLog(rw.From), "to", truncateForLog(rw.To))
	}
}

func truncateForLog(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ============================================================================
// THIN ADAPTERS — existing entry points now call FinalizeSpeech.
// ============================================================================

// finalizeDisplaySpeech is VisibleAssistantContentAfterTools's body,
// expressed as a FinalizeSpeech call (stage "display").
func finalizeDisplaySpeech(content, userAsk string) string {
	return FinalizeSpeech(SpeechInput{Raw: content, UserAsk: userAsk, AfterTools: true}).Speech
}

// finalizePersistSpeech is PersistVisibleAssistantContent's core, expressed
// as a FinalizeSpeech call (stage "persist").
func finalizePersistSpeech(content, userAsk string) string {
	return FinalizeSpeech(SpeechInput{Raw: content, UserAsk: userAsk, AfterTools: true, Persist: true}).Speech
}
