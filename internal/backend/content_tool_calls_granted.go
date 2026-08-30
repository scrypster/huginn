package backend

import (
	"encoding/json"
	"log/slog"
	"strings"
	"unicode"
)

// PromoteGrantedContentToolCalls lifts tool invocations that a model writes as
// fenced or bare JSON *mixed with prose* — the qwen2.5-coder "playbook" shape
// (fenced delegate_to_agent JSON, glue prose, fenced wait_for_threads JSON,
// toolsCalled empty). PromoteContentToolCalls only promotes when the entire
// message is tool objects; this promotes embedded objects too, but only when
// the tool name is granted this turn, so ordinary code samples with unknown
// names stay inert and visible.
//
// Promoted objects (and their fences) are removed from Content in document
// order; the surrounding prose remains user-visible. Native ToolCalls always
// win. DoneReason becomes "tool_calls" if it was empty or "stop" so the agent
// loop keeps running instead of treating the playbook as the final answer.
func PromoteGrantedContentToolCalls(resp *ChatResponse, granted []Tool) {
	if resp == nil || len(resp.ToolCalls) > 0 {
		return
	}
	names := grantedToolNameSet(granted)
	if len(names) == 0 {
		return
	}
	calls, visible, _ := scanGrantedToolContent(resp.Content, names, false)
	if len(calls) == 0 {
		return
	}
	resp.ToolCalls = withContentCallIDs(calls)
	resp.Content = strings.TrimSpace(visible)
	if resp.DoneReason == "" || resp.DoneReason == "stop" {
		resp.DoneReason = "tool_calls"
	}
	slog.Debug("backend: promoted granted content tool calls",
		"count", len(calls),
		"name", calls[0].Function.Name,
		"id", calls[0].ID)
}

func grantedToolNameSet(granted []Tool) map[string]bool {
	if len(granted) == 0 {
		return nil
	}
	names := make(map[string]bool, len(granted))
	for _, t := range granted {
		if t.Function.Name != "" {
			names[t.Function.Name] = true
		}
	}
	return names
}

// scanGrantedToolContent walks content once, collecting granted tool
// invocations from fenced blocks and bare JSON objects while building the
// user-visible remainder. Unknown tool names and non-tool fences are kept
// verbatim.
//
// In streaming mode it additionally reports holding=true when the tail is an
// incomplete candidate (unterminated tool-compatible fence or incomplete JSON
// object) that must not be emitted yet. The visible output is monotonic across
// growing input, which the token gate's prefix-emit logic relies on.
func scanGrantedToolContent(s string, granted map[string]bool, streaming bool) (calls []ToolCall, visible string, holding bool) {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if strings.HasPrefix(s[i:], "```") {
			closeIdx := strings.Index(s[i+3:], "```")
			if closeIdx < 0 {
				if streaming && couldBeFenceToolPrefix(s[i:]) {
					return calls, out.String(), true
				}
				out.WriteString(s[i:])
				break
			}
			part := s[i+3 : i+3+closeIdx]
			after := i + 3 + closeIdx + 3
			if cs := fenceGrantedToolCalls(part, granted); len(cs) > 0 {
				calls = append(calls, cs...)
				// swallow one separator so "prose\n```…```\nprose" closes up
				if after < len(s) && s[after] == '\n' {
					after++
				}
			} else {
				out.WriteString(s[i:after])
			}
			i = after
			continue
		}
		if s[i] == '{' {
			rest := s[i:]
			obj, afterStr, ok := readJSONObject(rest)
			if !ok {
				if streaming && isIncompleteJSONPrefix(rest) {
					return calls, out.String(), true
				}
				out.WriteByte(s[i])
				i++
				continue
			}
			consumed := len(rest) - len(afterStr)
			if tc, valid := grantedToolCallFromJSON(obj, granted); valid {
				calls = append(calls, tc)
				i += consumed
				if i < len(s) && s[i] == '\n' {
					i++
				}
				continue
			}
			out.WriteString(s[i : i+consumed])
			i += consumed
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return calls, out.String(), false
}

func grantedToolCallFromJSON(obj string, granted map[string]bool) (ToolCall, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(obj), &raw); err != nil {
		return ToolCall{}, false
	}
	tc, valid := toolCallFromRaw(raw)
	if !valid || !granted[tc.Function.Name] {
		return ToolCall{}, false
	}
	return tc, true
}

// fenceGrantedToolCalls parses the body of one complete markdown fence. The
// fence promotes only when its language tag is tool-compatible ("", json,
// tool_call, xml), its body parses as tool invocations, and every name is
// granted. Anything else — go/python samples, prose, unknown tool names —
// returns nil so the fence stays visible.
func fenceGrantedToolCalls(part string, granted map[string]bool) []ToolCall {
	body := strings.TrimSpace(part)
	if body == "" {
		return nil
	}
	lang, rest := splitFenceLang(body)
	switch lang {
	case "", "json", "tool_call", "xml":
		body = rest
	default:
		return nil
	}
	if body == "" {
		return nil
	}
	calls := parseToolCallJSONStream(body)
	if len(calls) == 0 {
		if inner, ok := unwrapWholeTagged(body, "<tool_call>", "</tool_call>"); ok {
			calls = parseToolCallJSONStream(inner)
		}
	}
	if len(calls) == 0 {
		if tc, ok := parseQwenXMLFunction(body); ok {
			calls = []ToolCall{tc}
		}
	}
	if len(calls) == 0 {
		return nil
	}
	for _, tc := range calls {
		if !granted[tc.Function.Name] {
			return nil
		}
	}
	return calls
}

// splitFenceLang splits an optional fence language tag off the body. A body
// that starts straight into JSON/XML has no tag ("```json {…}```" inline
// fences put the tag and payload on one line).
func splitFenceLang(body string) (lang, rest string) {
	if body[0] == '{' || body[0] == '<' {
		return "", body
	}
	idx := strings.IndexFunc(body, unicode.IsSpace)
	if idx < 0 {
		return strings.ToLower(body), ""
	}
	return strings.ToLower(body[:idx]), strings.TrimSpace(body[idx:])
}
