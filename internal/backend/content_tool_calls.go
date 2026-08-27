package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode"
)

// contentToolCallID is the synthetic id assigned to a tool call recovered from
// assistant content. Providers that emit native tool_calls supply their own ids.
const contentToolCallID = "content_call_1"

// PromoteContentToolCalls lifts tool invocations that some local models
// (notably qwen2.5-coder:14b on Ollama) emit as assistant content instead of
// structured tool_calls. Native ToolCalls always win.
//
// Content is treated as tool calls only when the entire trimmed message is
// one or more invocations: whitespace-separated JSON objects each with a
// tool name (arguments may be missing or empty — wait_for_threads defaults
// to all spawned threads), a <tool_call> fence, or Qwen XML. If any leftover
// prose remains, nothing is promoted so code samples are not executed.
//
// When calls are promoted, Content is cleared (same mutual-exclusion rule
// buildRequest applies when sending tool_calls back to the model) and
// DoneReason becomes "tool_calls" if it was empty or "stop".
func PromoteContentToolCalls(resp *ChatResponse) {
	if resp == nil || len(resp.ToolCalls) > 0 {
		return
	}
	calls := parseContentToolCalls(resp.Content)
	if len(calls) == 0 {
		return
	}
	resp.ToolCalls = calls
	resp.Content = ""
	if resp.DoneReason == "" || resp.DoneReason == "stop" {
		resp.DoneReason = "tool_calls"
	}
	slog.Debug("backend: promoted content tool calls",
		"count", len(calls),
		"name", calls[0].Function.Name,
		"id", calls[0].ID)
}

// parseContentToolCalls returns tool calls when content is a lone invocation
// or a whitespace-separated sequence of JSON tool objects. It returns nil
// when content is normal prose or any object fails the name check.
func parseContentToolCalls(content string) []ToolCall {
	payload, kind, ok := extractLoneToolCallPayload(content)
	if !ok {
		return nil
	}
	if calls := parseToolCallJSONStream(payload); len(calls) > 0 {
		return calls
	}
	if kind == "fence" || kind == "qwen_xml" {
		if tc, ok := parseQwenXMLFunction(payload); ok {
			return []ToolCall{tc}
		}
		if tc, ok := parseNameParenArgs(payload); ok {
			return []ToolCall{tc}
		}
	}
	return nil
}

func extractLoneToolCallPayload(content string) (payload, kind string, ok bool) {
	s := strings.TrimSpace(content)
	if s == "" {
		return "", "", false
	}
	if body, unwrapped := unwrapWholeMarkdownFence(s); unwrapped {
		s = body
	}
	if body, unwrapped := unwrapWholeTagged(s, "<tool_call>", "</tool_call>"); unwrapped {
		return body, "fence", true
	}
	if looksLikeQwenFunction(s) {
		return s, "qwen_xml", true
	}
	if strings.HasPrefix(s, "{") {
		return s, "json", true
	}
	return "", "", false
}

// unwrapWholeMarkdownFence accepts a message that is only a fenced code block
// (optional json / tool_call / xml language tag). Any leading or trailing prose
// causes a miss.
func unwrapWholeMarkdownFence(s string) (string, bool) {
	if !strings.HasPrefix(s, "```") {
		return "", false
	}
	rest := s[3:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", false
	}
	lang := strings.ToLower(strings.TrimSpace(rest[:nl]))
	switch lang {
	case "", "json", "tool_call", "xml":
	default:
		return "", false
	}
	body := strings.TrimRightFunc(rest[nl+1:], unicode.IsSpace)
	if !strings.HasSuffix(body, "```") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimSuffix(body, "```")), true
}

func unwrapWholeTagged(s, open, close string) (string, bool) {
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, open) || !strings.HasSuffix(lower, close) {
		return "", false
	}
	return strings.TrimSpace(s[len(open) : len(s)-len(close)]), true
}

func looksLikeQwenFunction(s string) bool {
	return strings.HasPrefix(strings.ToLower(s), "<function=")
}

// parseToolCallJSONStream decodes one or more whitespace-separated JSON
// objects from s. Every object must have a tool name (arguments optional);
// any decode error or leftover prose causes a full miss (nothing is promoted).
func parseToolCallJSONStream(s string) []ToolCall {
	calls, leftover, ok := parseLeadingToolCallJSONStream(s)
	if !ok || strings.TrimSpace(leftover) != "" {
		return nil
	}
	return calls
}

// parseLeadingToolCallJSONStream is the display-safe sibling of
// parseToolCallJSONStream: it accepts leftover prose after one or more
// valid tool objects instead of treating leftover as a full miss.
func parseLeadingToolCallJSONStream(s string) (calls []ToolCall, leftover string, ok bool) {
	rest := s
	for {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if rest == "" {
			break
		}
		if rest[0] != '{' {
			if len(calls) == 0 {
				return nil, "", false
			}
			return withContentCallIDs(calls), rest, true
		}
		obj, after, parsed := readJSONObject(rest)
		if !parsed {
			if len(calls) == 0 {
				return nil, "", false
			}
			return withContentCallIDs(calls), rest, true
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(obj), &raw); err != nil {
			return nil, "", false
		}
		tc, valid := toolCallFromRaw(raw)
		if !valid {
			if len(calls) == 0 {
				return nil, "", false
			}
			return withContentCallIDs(calls), rest, true
		}
		calls = append(calls, tc)
		rest = after
	}
	if len(calls) == 0 {
		return nil, "", false
	}
	return withContentCallIDs(calls), "", true
}

func withContentCallIDs(calls []ToolCall) []ToolCall {
	for i := range calls {
		calls[i].ID = fmt.Sprintf("content_call_%d", i+1)
	}
	return calls
}

func readJSONObject(s string) (obj, after string, ok bool) {
	end, found := jsonObjectEnd(s)
	if !found {
		return "", "", false
	}
	raw := s[:end]
	if !json.Valid([]byte(raw)) {
		return "", "", false
	}
	trim := bytes.TrimSpace([]byte(raw))
	if len(trim) == 0 || trim[0] != '{' {
		return "", "", false
	}
	return string(trim), s[end:], true
}

// jsonObjectEnd returns the index just past a leading JSON object in s,
// using a string-aware brace scan so leftover prose after `}` is never
// swallowed by encoding/json Decoder read-ahead (InputOffset can sit past
// the first leftover character on some prefixes). The index is into s.
func jsonObjectEnd(s string) (int, bool) {
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '{' {
			start = i
			break
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return 0, false
		}
	}
	if start < 0 {
		return 0, false
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

func toolCallFromRaw(raw map[string]json.RawMessage) (ToolCall, bool) {
	nameRaw, hasName := raw["name"]
	if !hasName {
		// qwen2.5-coder leftovers after a successful tool: {"function_name":"bash"}
		nameRaw, hasName = raw["function_name"]
	}
	if !hasName {
		return ToolCall{}, false
	}
	var name string
	if err := json.Unmarshal(nameRaw, &name); err != nil {
		return ToolCall{}, false
	}
	name = strings.TrimSpace(name)
	if !validToolName(name) {
		return ToolCall{}, false
	}
	// Missing / empty arguments still execute (wait_for_threads waits on
	// spawned threads; bash then returns a tool error instead of becoming
	// the final answer).
	args := map[string]any{}
	if argsRaw, hasArgs := raw["arguments"]; hasArgs {
		parsed, ok := parseArgumentsRaw(argsRaw)
		if !ok {
			return ToolCall{}, false
		}
		args = parsed
	}
	return newContentToolCall(name, args), true
}

func parseArgumentsRaw(raw json.RawMessage) (map[string]any, bool) {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 || bytes.Equal(trim, []byte("null")) {
		return map[string]any{}, true
	}
	if trim[0] == '{' {
		var m map[string]any
		if err := json.Unmarshal(trim, &m); err != nil {
			return nil, false
		}
		return m, true
	}
	if trim[0] == '"' {
		var inner string
		if err := json.Unmarshal(trim, &inner); err != nil {
			return nil, false
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return map[string]any{}, true
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(inner), &m); err != nil {
			return nil, false
		}
		return m, true
	}
	return nil, false
}

func parseQwenXMLFunction(s string) (ToolCall, bool) {
	const prefix = "<function="
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, prefix) {
		return ToolCall{}, false
	}
	gt := strings.IndexByte(s, '>')
	if gt < 0 {
		return ToolCall{}, false
	}
	name := strings.TrimSpace(s[len(prefix):gt])
	if !validToolName(name) {
		return ToolCall{}, false
	}
	const close = "</function>"
	if !strings.HasSuffix(lower, close) {
		return ToolCall{}, false
	}
	inner := strings.TrimSpace(s[gt+1 : len(s)-len(close)])
	if strings.HasPrefix(inner, "{") {
		args, ok := decodeLoneObject(inner)
		if !ok {
			return ToolCall{}, false
		}
		return newContentToolCall(name, args), true
	}
	args, ok := parseQwenParameters(inner)
	if !ok {
		return ToolCall{}, false
	}
	return newContentToolCall(name, args), true
}

func parseQwenParameters(inner string) (map[string]any, bool) {
	args := map[string]any{}
	rest := strings.TrimSpace(inner)
	if rest == "" {
		return args, true
	}
	const open, close = "<parameter=", "</parameter>"
	for rest != "" {
		if !strings.HasPrefix(strings.ToLower(rest), open) {
			return nil, false
		}
		gt := strings.IndexByte(rest, '>')
		if gt < 0 {
			return nil, false
		}
		key := strings.TrimSpace(rest[len(open):gt])
		if !validToolName(key) {
			return nil, false
		}
		rest = rest[gt+1:]
		idx := strings.Index(strings.ToLower(rest), close)
		if idx < 0 {
			return nil, false
		}
		args[key] = strings.TrimSpace(rest[:idx])
		rest = strings.TrimSpace(rest[idx+len(close):])
	}
	return args, true
}

func parseNameParenArgs(s string) (ToolCall, bool) {
	paren := strings.IndexByte(s, '(')
	if paren <= 0 || !strings.HasSuffix(s, ")") {
		return ToolCall{}, false
	}
	name := strings.TrimSpace(s[:paren])
	if !validToolName(name) {
		return ToolCall{}, false
	}
	inner := strings.TrimSpace(s[paren+1 : len(s)-1])
	if inner == "" {
		return newContentToolCall(name, map[string]any{}), true
	}
	args, ok := decodeLoneObject(inner)
	if !ok {
		return ToolCall{}, false
	}
	return newContentToolCall(name, args), true
}

func decodeLoneObject(s string) (map[string]any, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, false
	}
	if !decoderAtEOF(dec) {
		return nil, false
	}
	return m, true
}

func decoderAtEOF(dec *json.Decoder) bool {
	_, err := dec.Token()
	return err == io.EOF
}

func validToolName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case i > 0 && ((r >= '0' && r <= '9') || r == '.' || r == '-'):
		default:
			return false
		}
	}
	return true
}

func newContentToolCall(name string, args map[string]any) ToolCall {
	if args == nil {
		args = map[string]any{}
	}
	return ToolCall{
		ID: contentToolCallID,
		Function: ToolCallFunction{
			Name:      name,
			Arguments: args,
		},
	}
}

// VisibleAssistantContent returns the user-visible remainder of assistant
// content after removing a leading tool-call JSON/XML prefix and any
// leftover TOOL_FAIL / DELEGATE_FAIL tokens or bare harness tool-name lines.
// Code samples with leading prose, fenced JSON that is not a lone
// invocation, and other ordinary text are returned unchanged.
//
// This does not promote or execute anything. PromoteContentToolCalls still
// requires the entire trimmed message to be tool objects.
func VisibleAssistantContent(content string) string {
	if leftover, stripped := stripLeadingContentToolCalls(content); stripped {
		content = leftover
	}
	content = stripHarnessVisibleTokens(content)
	if leftover, stripped := stripLeadingContentToolCalls(content); stripped {
		content = stripHarnessVisibleTokens(leftover)
	}
	// Residual playbook speech (wait tags, glue lines, re-typed tool JSON)
	// is never teammate prose regardless of what ran this turn.
	if residual := StripResidualSpeech(content); residual != content {
		content = stripHarnessVisibleTokens(residual)
	}
	// Same rewrite persist already applies: never stream the harness label.
	return stripHarnessClockLabel(content)
}

// RevealContentToolCalls replaces Content with its user-visible remainder so
// harness JSON never lands in the transcript. Safe to call after
// PromoteContentToolCalls (pure tool JSON is already empty).
//
// When the visible remainder equals Content, this is a no-op: it must not
// write the field. SpawnThread / CreateFromMentions can share one
// ChatResponse across goroutines (test backends return the same pointer),
// and a write here races a concurrent read of Content.
func RevealContentToolCalls(resp *ChatResponse) {
	if resp == nil {
		return
	}
	visible := VisibleAssistantContent(resp.Content)
	if visible == resp.Content {
		return
	}
	resp.Content = visible
}

// VisibleAssistantContentAfterDeny is the display filter used after a tool
// permission deny (and for oneshot agentOutput). It strips a leading
// tool-call prefix (same as VisibleAssistantContent), remaining unfenced
// harness JSON, and leftover TOOL_FAIL / DELEGATE_FAIL tokens so they never
// appear as the visible answer.
func VisibleAssistantContentAfterDeny(content string) string {
	return VisibleAssistantContentAfterTools(content)
}

// VisibleAssistantContentAfterTools is the display filter for a turn in
// which tools already ran (or were denied). On top of VisibleAssistantContent
// it also drops result-shaped JSON the model echoed next to its prose
// ({"pong_response":"PONG","multiplication_result":"56"}), so the speech
// channel reads like a teammate: "Reggie said PONG. 7 times 8 is 56."
func VisibleAssistantContentAfterTools(content string, userAsk ...string) string {
	visible := VisibleAssistantContent(content)
	visible = stripEmbeddedHarnessToolJSON(visible)
	visible = StripResidualSpeechAfterTools(visible)
	visible = stripHarnessVisibleTokens(visible)
	visible = stripHarnessClockLabel(visible)
	ask := ""
	if len(userAsk) > 0 {
		ask = userAsk[0]
	}
	if isTimeAsk(ask) {
		visible = dropTimeExcuseSentences(visible)
	}
	if rewrite := teammateCompanyWallRewrite(visible, content, ask); rewrite != "" {
		return stripHarnessClockLabel(rewrite)
	}
	if rewrite := teammateHostnameFailRewrite(visible, content, ask); rewrite != "" {
		return stripHarnessClockLabel(rewrite)
	}
	if rewrite := teammateTimeFailRewrite(visible, content, ask); rewrite != "" {
		return stripHarnessClockLabel(rewrite)
	}
	if rewrite := teammateInvalidToolPongRewrite(visible, content, ask); rewrite != "" {
		return stripHarnessClockLabel(rewrite)
	}
	return stripHarnessClockLabel(visible)
}

// PersistVisibleAssistantContent is the hallway REST and WS persist filter.
// The turn is over, so AfterTools/AfterDeny leftover speech is stripped even
// when toolsCalled is empty — the live Lab Winston Steve-deny path miss.
// userAsk is the human line for this turn so leftover-empty Sam hostname
// fails still persist a teammate row instead of "".
func PersistVisibleAssistantContent(content string, userAsk ...string) string {
	ask := ""
	if len(userAsk) > 0 {
		ask = userAsk[0]
	}
	visible := VisibleAssistantContentAfterTools(content, ask)
	visible = dropLeftoverClockWhenNotTimeAsk(visible, ask)
	visible = dropLeftoverHireGhost(visible, ask)
	visible = dropLeftoverDelegatedHire(visible, ask)
	return fillTrivialPingPersist(visible, ask)
}

// stripHarnessVisibleTokens removes leftover fail tokens and lines that are
// only a harness tool name. Ordinary prose that happens to mention "bash"
// is left alone — only a line that is exactly the token or tool name goes.
func stripHarnessVisibleTokens(content string) string {
	if content == "" {
		return content
	}
	if isFailSpeech(strings.TrimSpace(content)) || isHarnessToolNameLine(content) {
		return ""
	}
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		if isFailSpeech(strings.TrimSpace(line)) || isHarnessToolNameLine(line) {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	if !changed {
		return content
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func isFailSpeech(s string) bool {
	if s == "" {
		return false
	}
	upper := strings.ToUpper(s)
	for _, tok := range []string{"TOOL_FAIL", "DELEGATE_FAIL"} {
		if upper == tok {
			return true
		}
		if strings.HasPrefix(upper, tok) {
			rest := strings.TrimSpace(upper[len(tok):])
			return rest == "" || strings.HasPrefix(rest, ":")
		}
	}
	return false
}

func isHarnessToolNameLine(s string) bool {
	switch strings.TrimSpace(s) {
	case "wait_for_threads", "delegate_to_agent", "recall_thread_result", "list_team_status", "bash", "create_agent":
		return true
	default:
		return false
	}
}

// stripEmbeddedHarnessToolJSON removes unfenced JSON objects that parse as
// tool calls. Fenced code samples are left alone.
func stripEmbeddedHarnessToolJSON(content string) string {
	if content == "" {
		return content
	}
	parts := strings.Split(content, "```")
	for i, part := range parts {
		if i%2 == 1 {
			continue // inside a fence
		}
		parts[i] = removeToolJSONObjects(part)
	}
	return strings.TrimSpace(strings.Join(parts, "```"))
}

func removeToolJSONObjects(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '{' {
			b.WriteByte(s[i])
			i++
			continue
		}
		rest := s[i:]
		obj, after, ok := readJSONObject(rest)
		if !ok {
			b.WriteByte(s[i])
			i++
			continue
		}
		consumed := len(rest) - len(after)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(obj), &raw); err != nil {
			b.WriteString(s[i : i+consumed])
			i += consumed
			continue
		}
		if _, valid := toolCallFromRaw(raw); !valid {
			b.WriteString(s[i : i+consumed])
			i += consumed
			continue
		}
		i += consumed
		if i < len(s) && (s[i] == '\n' || s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
			// drop one separator so "prose\n{json}\n" does not leave a hole
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
		}
	}
	// Do not trim here: this runs per unfenced segment and trimming would
	// eat the newlines that separate prose from an adjacent code fence.
	return b.String()
}

func stripLeadingContentToolCalls(content string) (leftover string, stripped bool) {
	s := strings.TrimSpace(content)
	if s == "" {
		return content, false
	}
	if strings.HasPrefix(s, "{") {
		calls, rest, ok := parseLeadingToolCallJSONStream(s)
		if ok && len(calls) > 0 {
			return strings.TrimSpace(rest), true
		}
		return content, false
	}
	if inner, after, ok := splitLeadingTagged(s, "<tool_call>", "</tool_call>"); ok {
		if isToolPayload(inner) {
			return strings.TrimSpace(after), true
		}
		return content, false
	}
	if looksLikeQwenFunction(s) {
		if inner, after, ok := splitLeadingQwenFunction(s); ok {
			if _, parsed := parseQwenXMLFunction(inner); parsed {
				return strings.TrimSpace(after), true
			}
		}
	}
	return content, false
}

func isToolPayload(s string) bool {
	if len(parseToolCallJSONStream(s)) > 0 {
		return true
	}
	if _, ok := parseQwenXMLFunction(s); ok {
		return true
	}
	if _, ok := parseNameParenArgs(s); ok {
		return true
	}
	return false
}

func splitLeadingTagged(s, open, close string) (inner, after string, ok bool) {
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, open) {
		return "", "", false
	}
	idx := strings.Index(lower, close)
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[len(open):idx]), s[idx+len(close):], true
}

func splitLeadingQwenFunction(s string) (inner, after string, ok bool) {
	lower := strings.ToLower(s)
	const close = "</function>"
	idx := strings.Index(lower, close)
	if idx < 0 {
		return "", "", false
	}
	end := idx + len(close)
	return s[:end], s[end:], true
}

func couldBeContentToolCallPrefix(content string) bool {
	s := strings.TrimLeftFunc(content, unicode.IsSpace)
	if s == "" {
		return true
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(s, "{") {
		return couldBeJSONToolPrefix(s)
	}
	if strings.HasPrefix(lower, "<tool_call>") || strings.HasPrefix(lower, "<function=") {
		if leftover, stripped := stripLeadingContentToolCalls(s); stripped {
			return leftover == ""
		}
		if strings.HasPrefix(lower, "<tool_call>") && !strings.Contains(lower, "</tool_call>") {
			return true
		}
		if strings.HasPrefix(lower, "<function=") && !strings.Contains(lower, "</function>") {
			return true
		}
		return false
	}
	if strings.HasPrefix(s, "```") {
		return couldBeFenceToolPrefix(s)
	}
	return false
}

func couldBeJSONToolPrefix(s string) bool {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" || s[0] != '{' {
		return s == ""
	}
	obj, _, parsed := readJSONObject(s)
	if !parsed {
		return isIncompleteJSONPrefix(s)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(obj), &raw); err != nil {
		return false
	}
	_, ok := toolCallFromRaw(raw)
	return ok
}

func isIncompleteJSONPrefix(s string) bool {
	dec := json.NewDecoder(strings.NewReader(s))
	var raw json.RawMessage
	return isIncompleteJSON(dec.Decode(&raw))
}

func isIncompleteJSON(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return strings.Contains(err.Error(), "unexpected end of JSON")
}

func couldBeFenceToolPrefix(s string) bool {
	if !strings.HasPrefix(s, "```") {
		return false
	}
	if body, ok := unwrapWholeMarkdownFence(s); ok {
		return isToolPayload(body)
	}
	rest := s[3:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		lang := strings.ToLower(strings.TrimSpace(rest))
		if lang == "" {
			return true
		}
		for _, cand := range []string{"json", "tool_call", "xml"} {
			if strings.HasPrefix(cand, lang) || strings.HasPrefix(lang, cand) {
				return true
			}
		}
		return false
	}
	lang := strings.ToLower(strings.TrimSpace(rest[:nl]))
	switch lang {
	case "", "json", "tool_call", "xml":
	default:
		return false
	}
	body := rest[nl+1:]
	closeIdx := strings.Index(body, "```")
	if closeIdx < 0 {
		return true
	}
	if strings.TrimSpace(body[closeIdx+3:]) != "" {
		return false
	}
	return isToolPayload(strings.TrimSpace(body[:closeIdx]))
}

func visibleOrHeldAssistantContent(content string) (visible string, holding bool) {
	if leftover, stripped := stripLeadingContentToolCalls(content); stripped {
		return leftover, leftover == ""
	}
	if couldBeContentToolCallPrefix(content) {
		return "", true
	}
	return content, false
}

// ContentToolCallTokenGate holds back streamed tokens that still look like a
// leading tool-call JSON/XML prefix so the chat bubble never paints harness
// JSON. Leftover prose is emitted as soon as it is recognized.
type ContentToolCallTokenGate struct {
	downstreamToken func(string)
	downstreamEvent func(StreamEvent)
	raw             string
	emitted         string
	granted         map[string]bool
}

// SetGrantedTools enables mid-message stripping of fenced / bare JSON
// invocations of tools granted this turn (the playbook shape
// PromoteGrantedContentToolCalls executes). Nil-safe; without a granted set
// only leading tool prefixes are held (original behavior).
func (g *ContentToolCallTokenGate) SetGrantedTools(granted []Tool) {
	if g == nil {
		return
	}
	g.granted = grantedToolNameSet(granted)
}

// NewContentToolCallTokenGate wraps OnToken / OnEvent StreamText emitters.
// A nil gate is returned when both callbacks are nil.
func NewContentToolCallTokenGate(onToken func(string), onEvent func(StreamEvent)) *ContentToolCallTokenGate {
	if onToken == nil && onEvent == nil {
		return nil
	}
	return &ContentToolCallTokenGate{downstreamToken: onToken, downstreamEvent: onEvent}
}

// OnToken implements the ChatRequest.OnToken callback.
func (g *ContentToolCallTokenGate) OnToken(tok string) {
	if g == nil || tok == "" {
		return
	}
	g.raw += tok
	g.flush(false)
}

// Push is an alias for OnToken used by the SSE parser.
func (g *ContentToolCallTokenGate) Push(delta string) {
	g.OnToken(delta)
}

// Finish emits any remaining user-visible text. Pass the authoritative
// visible Content after PromoteContentToolCalls + RevealContentToolCalls
// so a promoted lone fence/JSON never leaks at end-of-stream.
func (g *ContentToolCallTokenGate) Finish(visible string) {
	if g == nil || visible == "" {
		return
	}
	// Only emit a true suffix. A conflicting replacement (visible != emitted
	// and not a prefix) forks a second stream event — live #mention-proof
	// painted Steve `PONG` then an orphan `ONG` after StreamDone closed the
	// first row.
	if strings.HasPrefix(visible, g.emitted) {
		g.emit(visible[len(g.emitted):])
		g.emitted = visible
	}
}

func (g *ContentToolCallTokenGate) flush(final bool) {
	vis, holding := visibleOrHeldAssistantContent(g.raw)
	if holding && !final {
		return
	}
	if final {
		vis = VisibleAssistantContent(g.raw)
	}
	if len(g.granted) > 0 {
		// Drop complete granted tool fences/objects; a held tail (incomplete
		// candidate) is simply truncated off vis until it resolves.
		_, vis, _ = scanGrantedToolContent(vis, g.granted, !final)
	}
	if strings.HasPrefix(vis, g.emitted) {
		g.emit(vis[len(g.emitted):])
		g.emitted = vis
	}
}

func (g *ContentToolCallTokenGate) emit(s string) {
	if s == "" {
		return
	}
	if g.downstreamEvent != nil {
		g.downstreamEvent(StreamEvent{Type: StreamText, Content: s})
	}
	if g.downstreamToken != nil {
		g.downstreamToken(s)
	}
}
