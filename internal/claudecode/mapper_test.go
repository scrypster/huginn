package claudecode

import (
	"os"
	"strings"
	"testing"
)

func mapFixture(t *testing.T, path string) []Mapped {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	m := NewMapper()
	var out []Mapped
	for _, raw := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		l, ok := ParseLine([]byte(raw))
		if !ok {
			continue
		}
		out = append(out, m.Add(l)...)
	}
	return out
}

func TestMapperProducesThreeMessages(t *testing.T) {
	got := mapFixture(t, "testdata/basic.jsonl")
	// u1 (user), a1 (assistant + tool_use), a2 (assistant).
	// u2 is tool_result-only and must NOT become a message.
	if len(got) != 3 {
		for i, g := range got {
			t.Logf("[%d] role=%q content=%q tools=%d", i, g.Msg.Role, g.Msg.Content, len(g.Msg.ToolCalls))
		}
		t.Fatalf("got %d messages, want 3", len(got))
	}
	if got[0].Msg.Role != "user" || got[0].Msg.Content != "read main.go" {
		t.Errorf("msg[0] = %q/%q, want user/read main.go", got[0].Msg.Role, got[0].Msg.Content)
	}
	if got[1].Msg.Role != "assistant" || got[1].Msg.Content != "Reading it now." {
		t.Errorf("msg[1] = %q/%q", got[1].Msg.Role, got[1].Msg.Content)
	}
	if got[2].Msg.Content != "It is a main package." {
		t.Errorf("msg[2] content = %q", got[2].Msg.Content)
	}
}

func TestMapperAttachesToolCallAndResult(t *testing.T) {
	got := mapFixture(t, "testdata/basic.jsonl")
	if len(got) < 2 {
		t.Fatal("not enough messages")
	}
	calls := got[1].Msg.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].ID != "tu1" || calls[0].Name != "Read" {
		t.Errorf("tool call = %+v, want id=tu1 name=Read", calls[0])
	}
	if calls[0].Args["file_path"] != "/tmp/proj/main.go" {
		t.Errorf("tool args = %v", calls[0].Args)
	}
	if calls[0].Result != "package main" {
		t.Errorf("tool result = %q, want %q", calls[0].Result, "package main")
	}
}

func TestMapperRecordsUsageAndModel(t *testing.T) {
	got := mapFixture(t, "testdata/basic.jsonl")
	if got[1].Msg.PromptTok != 120 || got[1].Msg.CompTok != 45 {
		t.Errorf("usage = %d/%d, want 120/45", got[1].Msg.PromptTok, got[1].Msg.CompTok)
	}
	if got[1].Msg.ModelName != "claude-opus-5" {
		t.Errorf("model = %q", got[1].Msg.ModelName)
	}
}

func TestMapperRoutesSidechainToThread(t *testing.T) {
	m := NewMapper()
	parent, _ := ParseLine([]byte(`{"type":"assistant","uuid":"p1","sessionId":"s","message":{"role":"assistant","content":[{"type":"text","text":"delegating"}]}}`))
	m.Add(parent)

	side, _ := ParseLine([]byte(`{"type":"assistant","uuid":"s1","parentUuid":"p1","isSidechain":true,"sessionId":"s","message":{"role":"assistant","content":[{"type":"text","text":"subagent says hi"}]}}`))
	got := m.Add(side)

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	if got[0].ThreadID != "p1" {
		t.Errorf("ThreadID = %q, want %q", got[0].ThreadID, "p1")
	}
}

func TestMapperAcceptsPlainStringContent(t *testing.T) {
	m := NewMapper()
	l, ok := ParseLine([]byte(`{"type":"user","uuid":"p1","sessionId":"s","message":{"role":"user","content":"I attached the files to that first message"}}`))
	if !ok {
		t.Fatal("ParseLine rejected a plain-string-content user line")
	}
	got := m.Add(l)
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1 — a plain-string user turn must not be dropped", len(got))
	}
	if got[0].Msg.Role != "user" {
		t.Errorf("role = %q, want user", got[0].Msg.Role)
	}
	if got[0].Msg.Content != "I attached the files to that first message" {
		t.Errorf("content = %q", got[0].Msg.Content)
	}
}

func TestMapperToolResultContentAsBlockArray(t *testing.T) {
	m := NewMapper()

	a, _ := ParseLine([]byte(`{"type":"assistant","uuid":"a1","sessionId":"s","message":{"role":"assistant","content":[{"type":"tool_use","id":"t9","name":"Grep","input":{"pattern":"x"}}]}}`))
	out := m.Add(a)
	if len(out) != 1 {
		t.Fatalf("assistant line produced %d messages, want 1", len(out))
	}

	// Real transcripts frequently deliver tool_result.content as an ARRAY of
	// content blocks rather than a string.
	u, _ := ParseLine([]byte(`{"type":"user","uuid":"u1","parentUuid":"a1","sessionId":"s","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t9","content":[{"type":"text","text":"match found"}]}]}}`))
	m.Add(u)

	if got := out[0].Msg.ToolCalls[0].Result; got != "match found" {
		t.Errorf("tool result = %q, want %q — rawToString must handle the block-array form", got, "match found")
	}
}
