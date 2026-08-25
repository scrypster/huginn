package backend

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"unicode"
)

// contentToolCallID is the synthetic id assigned to a tool call recovered from
// assistant content. Providers that emit native tool_calls supply their own ids.
const contentToolCallID = "content_call_1"

// PromoteContentToolCalls lifts a tool invocation that some local models
// (notably qwen2.5-coder:14b on Ollama) emit as assistant content instead of
// structured tool_calls. Native ToolCalls always win.
//
// Content is treated as a tool call only when the entire trimmed message is a
// single invocation: a JSON object with name+arguments, a <tool_call> fence,
// or Qwen XML. Surrounding prose is rejected so code samples are not executed.
//
// When a call is promoted, Content is cleared (same mutual-exclusion rule
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
	slog.Debug("backend: promoted content tool call",
		"name", calls[0].Function.Name,
		"id", calls[0].ID)
}

// parseContentToolCalls returns at most one ToolCall when content is a lone
// tool invocation. It returns nil when content is normal prose or ambiguous.
func parseContentToolCalls(content string) []ToolCall {
	payload, kind, ok := extractLoneToolCallPayload(content)
	if !ok {
		return nil
	}
	if tc, ok := parseToolCallJSON(payload); ok {
		return []ToolCall{tc}
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

func parseToolCallJSON(s string) (ToolCall, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return ToolCall{}, false
	}
	if !decoderAtEOF(dec) {
		return ToolCall{}, false
	}
	nameRaw, hasName := raw["name"]
	argsRaw, hasArgs := raw["arguments"]
	if !hasName || !hasArgs {
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
	args, ok := parseArgumentsRaw(argsRaw)
	if !ok {
		return ToolCall{}, false
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
