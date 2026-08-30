package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/scrypster/huginn/internal/backend"
	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/tools"
)

// Huginn memory modes are harness-side. Muninn has no passive/conversational/
// immersive flag — we map the mode onto WHETHER / HOW OFTEN we call tools.

const (
	MemoryModePassive        = "passive"
	MemoryModeConversational = "conversational"
	MemoryModeImmersive      = "immersive"
)

// muninnWriteToolNames are stripped from the model schema in passive mode.
// The harness never offers them unless the user is in conversational/immersive.
var muninnWriteToolNames = map[string]struct{}{
	"muninn_remember":       {},
	"muninn_evolve":         {},
	"muninn_decide":         {},
	"muninn_trust":          {},
	"muninn_remember_batch": {},
}

const muninnCreateWorkflowVault = "muninn_create_workflow_vault"

type memoryGateCtxKey struct{}

type memoryGateCtx struct {
	Mode      string
	SessionID string
	Agent     string
}

// WithMemoryGate attaches harness memory-mode metadata used by prefetch (guide
// once per immersive session) and one-line memory.* logs. Safe to call with
// empty strings.
func WithMemoryGate(ctx context.Context, mode, sessionID, agent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, memoryGateCtxKey{}, memoryGateCtx{
		Mode:      mode,
		SessionID: sessionID,
		Agent:     agent,
	})
}

func memoryGateFromContext(ctx context.Context) memoryGateCtx {
	if ctx == nil {
		return memoryGateCtx{}
	}
	v, _ := ctx.Value(memoryGateCtxKey{}).(memoryGateCtx)
	return v
}

// normalizeMemoryMode maps empty / unknown onto conversational (runtime default).
func normalizeMemoryMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case MemoryModePassive:
		return MemoryModePassive
	case MemoryModeImmersive:
		return MemoryModeImmersive
	default:
		return MemoryModeConversational
	}
}

func isMuninnWriteTool(name string) bool {
	_, ok := muninnWriteToolNames[strings.TrimSpace(name)]
	return ok
}

// filterMuninnSchemas hides muninn_create_workflow_vault from every schema set
// and strips write tools when memory_mode is passive. Other muninn tools stay.
func filterMuninnSchemas(schemas []backend.Tool, mode string) []backend.Tool {
	if len(schemas) == 0 {
		return schemas
	}
	// Empty mode is conversational at runtime — keep writes. Only explicit
	// "passive" strips remember/evolve/decide/trust/remember_batch.
	passive := strings.EqualFold(strings.TrimSpace(mode), MemoryModePassive)
	out := schemas[:0]
	for _, s := range schemas {
		name := s.Function.Name
		if name == muninnCreateWorkflowVault {
			continue
		}
		if passive && isMuninnWriteTool(name) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// turnHasMuninnWrite reports whether the model already called a write tool.
func turnHasMuninnWrite(messages []backend.Message) bool {
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if isMuninnWriteTool(tc.Function.Name) {
				return true
			}
		}
		if isMuninnWriteTool(m.ToolName) {
			return true
		}
	}
	return false
}

type memoryGateInput struct {
	Mode      string
	Vault     string
	Agent     string
	SessionID string
	UserMsg   string
	Assistant string
	Messages  []backend.Message
	Registry  *tools.Registry
	Prefetch  string // where_left_off / recall text, used to pick evolve
	Home      string // ~/.huginn for MD fallback when Muninn is off
}

type memoryGateDecision struct {
	Emit    bool
	Receipt bool
	Source  string // model | harness | none
	Down    bool   // Muninn unreachable / not registered
	Reason  string // detect / skip reason (no content)
}

func applyMemoryGate(ctx context.Context, in memoryGateInput) memoryGateDecision {
	mode := normalizeMemoryMode(in.Mode)
	vault := pinMuninnVault(in.Vault)
	slog.Info("memory.mode", "mode", mode, "vault", vault, "agent", in.Agent)

	if mode == MemoryModePassive {
		// v1: strip writes is the whole passive gate. No harness persist.
		dec := memoryGateDecision{Emit: true, Source: "none", Reason: "passive"}
		slog.Info("memory.gate", "action", "emit", "mode", mode, "receipt", false)
		return dec
	}

	if turnHasMuninnWrite(in.Messages) {
		slog.Info("memory.persist", "source", "model", "ok", true, "vault", vault)
		slog.Info("memory.gate", "action", "emit", "mode", mode, "receipt", true)
		return memoryGateDecision{Emit: true, Receipt: true, Source: "model", Reason: "model_write"}
	}

	need, kind := detectNewFact(in.UserMsg, in.Assistant, mode)
	if !need {
		slog.Info("memory.gate", "action", "emit", "mode", mode, "receipt", false)
		return memoryGateDecision{Emit: true, Source: "none", Reason: "no_new_fact"}
	}

	content := atomicMemoryContent(in.UserMsg, in.Assistant, kind)
	if content == "" {
		slog.Info("memory.gate", "action", "emit", "mode", mode, "receipt", false)
		return memoryGateDecision{Emit: true, Source: "none", Reason: "empty_summary"}
	}

	if in.Registry == nil {
		if persistMarkdownFallback(in.Home, in.Agent, content) {
			slog.Info("memory.persist", "source", "markdown", "ok", true, "vault", vault)
			slog.Info("memory.gate", "action", "degrade", "mode", mode, "receipt", true)
			return memoryGateDecision{Emit: true, Receipt: true, Source: "markdown", Down: true, Reason: kind}
		}
		return degradeOrHold(mode, vault, true, "no_registry")
	}

	ok, down := harnessPersist(ctx, in.Registry, vault, content, kind, in.Prefetch, mode)
	if ok {
		slog.Info("memory.persist", "source", "harness", "ok", true, "vault", vault)
		slog.Info("memory.gate", "action", "emit", "mode", mode, "receipt", true)
		return memoryGateDecision{Emit: true, Receipt: true, Source: "harness", Reason: kind}
	}
	if down {
		// Same persist pass: MD only when Muninn is off. Never also-spam MD when Muninn is on.
		if persistMarkdownFallback(in.Home, in.Agent, content) {
			slog.Info("memory.persist", "source", "markdown", "ok", true, "vault", vault)
			slog.Info("memory.gate", "action", "degrade", "mode", mode, "receipt", true)
			return memoryGateDecision{Emit: true, Receipt: true, Source: "markdown", Down: true, Reason: kind}
		}
	}
	return degradeOrHold(mode, vault, down, kind)
}

func persistMarkdownFallback(home, agent, content string) bool {
	home = strings.TrimSpace(home)
	agent = strings.TrimSpace(agent)
	content = strings.TrimSpace(content)
	if home == "" || agent == "" || content == "" {
		return false
	}
	line := "- " + content
	if err := mem.AppendNotes(home, agent, line); err != nil {
		slog.Info("memory.persist", "source", "markdown", "ok", false)
		return false
	}
	return true
}

func degradeOrHold(mode, vault string, down bool, reason string) memoryGateDecision {
	if down {
		// Huginn must work if Muninn is dead. Fail-closed is only when Muninn is UP.
		slog.Info("memory.persist", "source", "harness", "ok", false, "vault", vault)
		slog.Info("memory.gate", "action", "degrade", "mode", mode, "receipt", false)
		return memoryGateDecision{Emit: true, Source: "none", Down: true, Reason: reason}
	}
	if mode == MemoryModeImmersive {
		slog.Info("memory.persist", "source", "harness", "ok", false, "vault", vault)
		slog.Info("memory.gate", "action", "hold", "mode", mode, "receipt", false)
		return memoryGateDecision{Emit: false, Source: "none", Reason: reason}
	}
	// Conversational: retry already happened inside harnessPersist; continue.
	slog.Info("memory.persist", "source", "harness", "ok", false, "vault", vault)
	slog.Info("memory.gate", "action", "emit", "mode", mode, "receipt", false)
	return memoryGateDecision{Emit: true, Source: "none", Reason: reason}
}

func harnessPersist(ctx context.Context, reg *tools.Registry, vault, content, kind, prefetch, mode string) (ok, down bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	id := recallEvolveTargetID(ctx, reg, vault, content)
	// Evolve only on an explicit correction ("actually", "not X", "I was
	// wrong") — a complementary fact about the same subject ("my dog is a
	// golden retriever" after "my dog is named Odin") must ADD a memory,
	// never destroy the old one (Opus vet 2026-08-28: data-loss finding).
	useEvolve := id != "" && looksLikeCorrection(content)
	if id == "" {
		id = firstPrefetchID(prefetch)
		useEvolve = id != "" && looksLikeCorrection(content)
	}
	call := func() (tools.ToolResult, bool) {
		if useEvolve {
			if t, has := reg.Get("muninn_evolve"); has {
				return t.Execute(ctx, map[string]any{
					"vault":   vault,
					"id":      id,
					"content": content,
					"summary": clipSummary(content),
					"type":    persistType(kind),
				}), false
			}
		}
		t, has := reg.Get("muninn_remember")
		if !has {
			return tools.ToolResult{}, true
		}
		return t.Execute(ctx, map[string]any{
			"vault":    vault,
			"content":  content,
			"type":     persistType(kind),
			"summary":  clipSummary(content),
			"entities": extractEntities(content),
		}), false
	}

	res, missing := call()
	if missing {
		return false, true
	}
	if persistSucceeded(res) {
		return true, false
	}
	if muninnResultDown(res) {
		// One retry on transient disconnect, then degrade.
		res2, missing2 := call()
		if missing2 {
			return false, true
		}
		if persistSucceeded(res2) {
			return true, false
		}
		return false, muninnResultDown(res2)
	}
	// Muninn is up; write failed. Conversational retries once then continues.
	if mode == MemoryModeConversational {
		res2, missing2 := call()
		if missing2 {
			return false, true
		}
		if persistSucceeded(res2) {
			return true, false
		}
		return false, muninnResultDown(res2)
	}
	return false, false
}

// factSubjectRE captures the possessive/definite subject phrase of a
// declarative fact — "my dog", "our staging server", "the deploy window" —
// so a contradiction ("actually my dog is named Loki") can be checked
// against what's already stored about the SAME subject before writing.
var factSubjectRE = regexp.MustCompile(`(?i)\b((?:our|my|the)\s+[a-z][\w'-]*(?:\s+[a-z][\w'-]*){0,3})\s+(?:is|are|was|were)\s+`)

// factSubject extracts the possessive/definite subject phrase from a
// distilled fact, lowercased for substring matching against recalled
// memory content. Returns "" when the content isn't subject+copula shaped.
func factSubject(content string) string {
	m := factSubjectRE.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(m[1]))
}

// recallHit is one scored memory returned by muninn_recall.
type recallHit struct {
	ID      string
	Content string
	Score   float64
	// Band is muninn_recall's calibrated relevance_band (strong|moderate|
	// weak|...). Preferred over Score when present — raw scores are
	// query-relative and on some vaults use a 0-100 scale, making every hit
	// look "strong" against a 0.75 float threshold.
	Band string
}

// strongRecallBand is the relevance floor above which a recall hit is
// trusted to be about the same thing, not just a loosely related memory.
const strongRecallBand = 0.75

// parseRecallHits best-effort parses a muninn_recall tool output into scored
// hits. Accepts a bare array, or an object wrapping the array under a
// results/memories/items/matches key, or a single hit object.
func parseRecallHits(raw string) []recallHit {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var generic any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return nil
	}
	return recallHitsFromJSON(generic)
}

func recallHitsFromJSON(v any) []recallHit {
	switch t := v.(type) {
	case []any:
		var out []recallHit
		for _, item := range t {
			if h, ok := recallHitFromMap(item); ok {
				out = append(out, h)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"results", "memories", "items", "matches", "hits"} {
			if arr, ok := t[key]; ok {
				return recallHitsFromJSON(arr)
			}
		}
		if h, ok := recallHitFromMap(t); ok {
			return []recallHit{h}
		}
	}
	return nil
}

func recallHitFromMap(v any) (recallHit, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return recallHit{}, false
	}
	id, _ := m["id"].(string)
	content, _ := m["content"].(string)
	if strings.TrimSpace(content) == "" {
		content, _ = m["summary"].(string)
	}
	if id == "" || strings.TrimSpace(content) == "" {
		return recallHit{}, false
	}
	score := numFromAny(m["score"])
	if score == 0 {
		score = numFromAny(m["relevance"])
	}
	band, _ := m["relevance_band"].(string)
	return recallHit{ID: id, Content: content, Score: score, Band: strings.ToLower(strings.TrimSpace(band))}, true
}

func numFromAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	}
	return 0
}

// bestSubjectHit picks the highest-scoring hit at/above strongRecallBand
// whose content mentions subject. Used both by the contradiction-evolve
// path (memory_gates.go) and the forget path (forget_ask.go).
func bestSubjectHit(hits []recallHit, subject string) (recallHit, bool) {
	subject = strings.ToLower(strings.TrimSpace(subject))
	if subject == "" {
		return recallHit{}, false
	}
	subjectRE, reErr := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(subject) + `\b`)
	var best recallHit
	found := false
	for _, h := range hits {
		// Prefer the calibrated relevance_band when the server sent one;
		// fall back to the raw score threshold. Bare Contains matched
		// "my dog" inside "my dogma" (Opus vet) — require word boundaries.
		if h.Band != "" {
			if h.Band != "strong" {
				continue
			}
		} else if h.Score < strongRecallBand {
			continue
		}
		if reErr != nil || !subjectRE.MatchString(h.Content) {
			continue
		}
		if !found || h.Score > best.Score {
			best = h
			found = true
		}
	}
	return best, found
}

// recallEvolveTargetID recalls the fact's subject and returns the id of an
// existing strong-band memory about the same subject whose content differs
// from the new content — i.e. a contradiction to evolve rather than a
// rival fact to add. Returns "" when there's nothing to evolve (new
// subject, no strong hit, or the recalled memory already says the same
// thing).
func recallEvolveTargetID(ctx context.Context, reg *tools.Registry, vault, content string) string {
	if reg == nil {
		return ""
	}
	subject := factSubject(content)
	if subject == "" {
		return ""
	}
	recallTool, has := reg.Get("muninn_recall")
	if !has {
		return ""
	}
	res := recallTool.Execute(ctx, map[string]any{"vault": vault, "context": subject})
	if res.IsError {
		return ""
	}
	hits := parseRecallHits(res.Output)
	hit, found := bestSubjectHit(hits, subject)
	if !found {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(hit.Content), strings.TrimSpace(content)) {
		return "" // identical fact already stored — nothing to evolve
	}
	return hit.ID
}

func persistSucceeded(res tools.ToolResult) bool {
	if res.IsError {
		return false
	}
	if strings.TrimSpace(res.Error) != "" && strings.TrimSpace(res.Output) == "" {
		return false
	}
	return true
}

func muninnResultDown(res tools.ToolResult) bool {
	msg := res.Error
	if msg == "" {
		msg = res.Output
	}
	low := strings.ToLower(msg)
	for _, n := range []string{
		"connection refused", "connection reset", "broken pipe",
		"i/o timeout", "deadline exceeded", "unavailable",
		"no such host", "network is unreachable", "eof",
		"connect:", "status 502", "status 503", "status 504",
	} {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func persistType(kind string) string {
	if kind == "decision" {
		return "decision"
	}
	return "fact"
}

func detectNewFact(userMsg, assistant, mode string) (bool, string) {
	u := strings.ToLower(userMsg)
	for _, p := range []string{
		"remember this", "remember that", "remember:", "please remember",
		"don't forget", "do not forget", "store this", "save this",
		"make a note", "note that", "keep this in mind",
	} {
		if strings.Contains(u, p) {
			return true, "user_ask"
		}
	}
	blob := strings.ToLower(userMsg + "\n" + assistant)
	for _, p := range []string{
		"we decided", "i decided", "we will go with", "we'll go with",
		"decision:", "the decision is",
	} {
		if strings.Contains(blob, p) {
			return true, "decision"
		}
	}
	if isDeclarativeFactAsk(userMsg) {
		return true, "declarative_fact"
	}
	if normalizeMemoryMode(mode) == MemoryModeImmersive {
		if strings.TrimSpace(assistant) != "" && !isTrivialSpeech(assistant) {
			return true, "immersive_summary"
		}
	}
	return false, ""
}

// declarativeFactPhrases flag an ask that is announcing a fact rather than
// asking a question — "for the record", "FYI", "heads up" and their kin.
// These never had an explicit "remember" verb, so detectNewFact previously
// dropped them on the floor: a natural statement of fact addressed to the
// agent produced no memory write at all.
var declarativeFactPhraseRE = regexp.MustCompile(`(?i)\b(for the record|fyi|f\.y\.i\.?|just so you know|just so you're aware|heads up|just to let you know|worth noting|worth knowing)\b`)

// possessiveFactRE catches plain declarative facts like "our staging server
// is called valkyrie" or "the deploy window is Fridays" — a possessive/
// definite subject followed directly by a copula. Deliberately narrow (no
// intervening prepositional clause) to avoid firing on ordinary questions.
var possessiveFactRE = regexp.MustCompile(`(?i)\b(our|my|the)\s+[a-z][\w'-]*(?:\s+[a-z][\w'-]*){0,3}\s+(?:is|are|was|were)\s+`)

var leadingMentionRE = regexp.MustCompile(`^(?:@[\w.-]+[\s,:]*)+`)

func stripLeadingMentions(s string) string {
	return strings.TrimSpace(leadingMentionRE.ReplaceAllString(strings.TrimSpace(s), ""))
}

// isDeclarativeFactAsk reports whether userMsg is a statement-shaped fact
// addressed to the agent (not a question) worth remembering on its own,
// without the user ever saying "remember".
func isDeclarativeFactAsk(userMsg string) bool {
	body := stripLeadingMentions(userMsg)
	if body == "" {
		return false
	}
	if strings.HasSuffix(strings.TrimSpace(body), "?") {
		return false
	}
	if declarativeFactPhraseRE.MatchString(body) {
		return true
	}
	return possessiveFactRE.MatchString(body)
}

// interrogativeOpenerRE matches a message that opens with a common question
// word/verb, even without a trailing '?' (some chat clients strip it, some
// users just don't type it).
var interrogativeOpenerRE = regexp.MustCompile(`(?i)^(what|who|when|where|why|how|which|is|are|was|were|do|does|did|can|could|would|should|will|has|have|had)\b`)

// isQuestionShaped reports whether userMsg reads as a question — either a
// trailing '?' or an interrogative opener. Used to gate model-initiated
// memory writes: live repro (2026-08-27, Winston DM) shows a question-shaped
// turn must be answered from recall, never by inventing and storing a new
// "fact".
func isQuestionShaped(userMsg string) bool {
	body := stripLeadingMentions(strings.TrimSpace(userMsg))
	if body == "" {
		return false
	}
	if strings.HasSuffix(body, "?") {
		return true
	}
	return interrogativeOpenerRE.MatchString(body)
}

func isTrivialSpeech(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimRight(t, ".! ")
	switch t {
	case "", "ok", "okay", "got it", "thanks", "thank you", "sure", "yep", "yes", "no",
		"ok thanks", "okay thanks", "thanks!", "anytime":
		return true
	}
	toks := significantTokens(s)
	if len(toks) == 0 {
		return true
	}
	for _, tok := range toks {
		if _, ok := ackPullTokens[tok]; !ok {
			return false
		}
	}
	return true
}

var ackPullTokens = map[string]struct{}{
	"ok": {}, "okay": {}, "thanks": {}, "thank": {}, "sure": {},
	"yep": {}, "yes": {}, "yeah": {}, "got": {}, "anytime": {},
}

func atomicMemoryContent(userMsg, assistant, kind string) string {
	text := strings.TrimSpace(assistant)
	if kind == "user_ask" || kind == "declarative_fact" || text == "" || isTrivialSpeech(text) {
		text = strings.TrimSpace(userMsg)
	}
	text = stripLeadingMentions(text)
	text = firstParagraph(text)
	text = distillFactContent(text)
	text = strings.Join(strings.Fields(text), " ")
	if len(text) < 8 {
		return ""
	}
	if len(text) > 400 {
		text = strings.TrimSpace(text[:400])
	}
	_ = kind
	return text
}

// leadingFactWrapperRE matches the imperative wrapper around a fact —
// "please remember this:", "note that", "for the record,", "FYI:" — so the
// distilled memory stores the fact itself, not the instruction to store it.
// Applied repeatedly so stacked wrappers ("Please remember this: for the
// record, …") fully unwrap.
var leadingFactWrapperRE = regexp.MustCompile(`(?i)^(please\s+)?(remember\s+(this|that)|store\s+this|save\s+this|make\s+a\s+note(\s+that)?|note\s+that|keep\s+this\s+in\s+mind(\s+that)?|for\s+the\s+record|fyi|f\.y\.i\.?|just\s+so\s+you\s+know|just\s+so\s+you'?re\s+aware|heads\s+up|just\s+to\s+let\s+you\s+know|worth\s+noting|worth\s+knowing|don'?t\s+forget|do\s+not\s+forget)[:,]?\s*`)

// instructionCruftSentenceRE flags sentences that are instructions to the
// assistant about how to respond ("Confirm in one short sentence.", "Do not
// invent other vaults.") rather than part of the fact being reported.
var instructionCruftSentenceRE = regexp.MustCompile(`(?i)^(please\s+)?(confirm|acknowledge|reply\s+with|respond\s+with|say\s+only|do\s+not\s+invent|don'?t\s+invent)\b`)

// distillFactContent strips the imperative wrapper and any trailing
// meta-instruction sentences off a user message so the stored memory is the
// fact itself: "our staging server is called valkyrie", not "Please
// remember this: our staging server is called valkyrie. Confirm in one
// short sentence."
func distillFactContent(s string) string {
	s = strings.TrimSpace(s)
	for {
		stripped := strings.TrimSpace(leadingFactWrapperRE.ReplaceAllString(s, ""))
		if stripped == s {
			break
		}
		s = stripped
	}
	sentences := splitSentences(s)
	kept := sentences[:0]
	for _, sent := range sentences {
		trim := strings.TrimSpace(sent)
		if trim == "" {
			continue
		}
		if instructionCruftSentenceRE.MatchString(trim) {
			continue
		}
		kept = append(kept, trim)
	}
	if len(kept) == 0 {
		return s
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

// splitSentences splits on sentence-ending punctuation, keeping the
// punctuation attached to each sentence.
func splitSentences(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func firstParagraph(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func clipSummary(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 120 {
		return content
	}
	return strings.TrimSpace(content[:120])
}

func extractEntities(text string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, ".,;:!?()[]\"'")
		if len(w) < 2 {
			continue
		}
		r := []rune(w)
		if !unicode.IsUpper(r[0]) {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
		if len(out) >= 6 {
			break
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func looksLikeCorrection(text string) bool {
	low := strings.ToLower(text)
	for _, p := range []string{"actually", "correction", "not that", "we were wrong", "update that", "that's wrong", "that is wrong"} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func firstPrefetchID(prefetch string) string {
	// Accept "id":"…" / id=… / [id:…] without dumping the neighborhood.
	low := prefetch
	for _, key := range []string{`"id":"`, `"id": "`, "id=", "[id:"} {
		i := strings.Index(low, key)
		if i < 0 {
			continue
		}
		rest := low[i+len(key):]
		end := strings.IndexAny(rest, "\",] \n")
		if end < 0 {
			end = len(rest)
		}
		id := strings.TrimSpace(rest[:end])
		if len(id) >= 6 {
			return id
		}
	}
	return ""
}

func applyLoopMemoryGate(ctx context.Context, cfg RunLoopConfig, result *LoopResult) {
	if result == nil {
		return
	}
	if cfg.MemoryMode == "" && strings.TrimSpace(cfg.MemoryVault) == "" {
		return
	}
	dec := applyMemoryGate(ctx, memoryGateInput{
		Mode:      cfg.MemoryMode,
		Vault:     cfg.MemoryVault,
		Agent:     cfg.MemoryAgent,
		SessionID: cfg.MemorySession,
		UserMsg:   cfg.MemoryUserMsg,
		Assistant: result.FinalContent,
		Messages:  result.Messages,
		Registry:  cfg.Tools,
		Home:      cfg.MemoryHome,
	})
	result.MemoryReceipt = dec.Receipt
	result.HoldClose = !dec.Emit
}

// appendHistoryHonoringGate writes the turn into sess. Immersive fail-closed
// (HoldClose) records the user row only so the session can continue.
func appendHistoryHonoringGate(sess *Session, userMsg, assistant string, newMsgs []backend.Message, holdClose bool) {
	if sess == nil {
		return
	}
	if holdClose {
		sess.appendHistory(backend.Message{Role: "user", Content: userMsg})
		return
	}
	if len(newMsgs) > 0 {
		sess.appendHistory(newMsgs...)
		return
	}
	sess.appendHistory(
		backend.Message{Role: "user", Content: userMsg},
		backend.Message{Role: "assistant", Content: assistant},
	)
}

// topicShifted is a cheap token overlap check. Used so conversational pull
// does not MCP-call every sentence of small talk.
func topicShifted(prev, curr string) bool {
	if isTrivialSpeech(curr) {
		return false
	}
	b := significantTokens(curr)
	if len(b) == 0 {
		return false
	}
	a := significantTokens(prev)
	if len(a) == 0 {
		return true
	}
	aset := make(map[string]struct{}, len(a))
	for _, t := range a {
		aset[t] = struct{}{}
	}
	overlap := 0
	for _, t := range b {
		if _, ok := aset[t]; ok {
			overlap++
		}
	}
	return overlap*4 < len(b)
}

var skipPullTokens = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "you": {}, "that": {}, "this": {},
	"with": {}, "from": {}, "have": {}, "just": {}, "what": {}, "please": {},
	"okay": {}, "yes": {}, "yeah": {}, "are": {}, "was": {}, "were": {},
	"but": {}, "not": {}, "can": {}, "how": {}, "why": {}, "who": {},
	"thanks": {}, "thank": {}, "sure": {}, "got": {}, "anytime": {},
}

func significantTokens(s string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:!?()[]\"'")
		if len(w) < 3 {
			continue
		}
		if _, skip := skipPullTokens[w]; skip {
			continue
		}
		out = append(out, w)
	}
	return out
}
