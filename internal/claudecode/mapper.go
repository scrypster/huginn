package claudecode

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// Mapped is one Huginn message derived from a transcript line. A non-empty
// ThreadID means the message belongs on that thread rather than on the main
// timeline; the thread id is the parent message's transcript uuid.
type Mapped struct {
	Msg      session.SessionMessage
	ThreadID string
}

// contentBlock is one element of a Claude Code message.content array.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type innerMessage struct {
	Role    string        `json:"role"`
	Model   string        `json:"model"`
	Content contentBlocks `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// contentBlocks is message.content, which Claude Code writes either as an
// array of typed blocks or — for simple user turns — as a plain string.
// Decoding only the array form silently drops every plain-string message.
type contentBlocks []contentBlock

func (c *contentBlocks) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*c = nil
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		*c = contentBlocks{{Type: "text", Text: s}}
		return nil
	}
	var arr []contentBlock
	if err := json.Unmarshal(trimmed, &arr); err != nil {
		return err
	}
	*c = arr
	return nil
}

// Mapper converts a stream of transcript lines into Huginn messages.
//
// It is stateful by necessity: an assistant line declares tool_use blocks
// whose results only arrive on the *following* user line, keyed by
// tool_use_id. The mapper keeps a direct pointer to each message with an
// OPEN tool call so a later tool_result can be written back into it — NOT a
// list of every message ever mapped. Retaining every message (including
// full ToolCalls[].Result payloads) for the process lifetime is unbounded
// growth; a large backfill would retain essentially the whole corpus. This
// bounds retention to messages currently awaiting a result: openCalls
// shrinks as applyToolResults resolves and deletes entries.
type Mapper struct {
	// openCalls maps tool_use_id to the message awaiting that tool's result
	// and the index of the call within that message's ToolCalls.
	openCalls map[string]callRef
	model     string
}

// callRef points directly at the message and call slot a pending
// tool_use_id belongs to.
//
// INVARIANT: msg must alias the SAME backing array as the ToolCalls slice
// inside the Mapped value the caller already holds. Mapped.Msg is a value
// copy of the SessionMessage, but SessionMessage.ToolCalls is a slice, so
// the copy and this pointer share one backing array — a later in-place
// write through msg.ToolCalls[callIdx] is visible through the Mapped the
// Ingester is holding in its `pending` map. The Ingester's deferred-append
// design depends on this entirely. Never deep-copy or reallocate ToolCalls
// anywhere in this package.
type callRef struct {
	msg     *session.SessionMessage
	callIdx int
}

// NewMapper returns a Mapper with empty state.
func NewMapper() *Mapper {
	return &Mapper{openCalls: map[string]callRef{}}
}

// Model returns the most recent assistant model seen, or "" if none.
func (m *Mapper) Model() string { return m.model }

// Add feeds one transcript line to the mapper.
//
// It returns the messages that became newly available. A user line carrying
// only tool_result blocks completes earlier tool calls and returns nothing —
// the amended message was already returned when its assistant line arrived,
// and the caller is expected to persist the update.
func (m *Mapper) Add(l Line) []Mapped {
	var inner innerMessage
	if len(l.Message) > 0 {
		if err := json.Unmarshal(l.Message, &inner); err != nil {
			return nil
		}
	}
	if inner.Model != "" {
		m.model = inner.Model
	}

	// Complete any tool calls this line reports results for, regardless of role.
	m.applyToolResults(inner)

	text := collectText(inner.Content)
	calls := collectToolCalls(inner.Content)

	// A line with no text and no new tool calls carries only results.
	if text == "" && len(calls) == 0 {
		return nil
	}
	// A tool_result-only user line must not become a message even if the
	// result content happened to be collected as text.
	if l.Type == "user" && len(calls) == 0 && onlyToolResults(inner.Content) {
		return nil
	}

	msg := session.SessionMessage{
		ID:        l.UUID,
		Ts:        parseTS(l.Timestamp),
		Role:      inner.Role,
		Content:   text,
		ModelName: inner.Model,
		PromptTok: inner.Usage.InputTokens,
		CompTok:   inner.Usage.OutputTokens,
		ToolCalls: calls,
	}
	if msg.Role == "" {
		msg.Role = l.Type
	}

	for i, c := range calls {
		m.openCalls[c.ID] = callRef{msg: &msg, callIdx: i}
	}

	out := Mapped{Msg: msg}
	if l.IsSidechain {
		out.ThreadID = l.ParentUUID
	}
	return []Mapped{out}
}

// applyToolResults writes tool_result payloads back into the messages that
// declared the corresponding tool_use. Returns the number applied.
func (m *Mapper) applyToolResults(inner innerMessage) int {
	var n int
	for _, b := range inner.Content {
		if b.Type != "tool_result" || b.ToolUseID == "" {
			continue
		}
		ref, ok := m.openCalls[b.ToolUseID]
		if !ok || ref.callIdx >= len(ref.msg.ToolCalls) {
			continue
		}
		ref.msg.ToolCalls[ref.callIdx].Result = rawToString(b.Content)
		delete(m.openCalls, b.ToolUseID)
		n++
	}
	return n
}

func collectText(blocks []contentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func collectToolCalls(blocks []contentBlock) []session.PersistedToolCall {
	var out []session.PersistedToolCall
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		out = append(out, session.PersistedToolCall{
			ID:   b.ID,
			Name: b.Name,
			Args: b.Input,
		})
	}
	return out
}

func onlyToolResults(blocks []contentBlock) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			return false
		}
	}
	return true
}

// rawToString renders a tool_result content payload, which Claude Code emits
// either as a plain JSON string or as an array of content blocks.
func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return collectText(blocks)
	}
	return string(raw)
}

func parseTS(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now().UTC()
	}
	return t.UTC()
}
