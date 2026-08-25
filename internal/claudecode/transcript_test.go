package claudecode

import (
	"os"
	"strings"
	"testing"
)

func TestParseLineAcceptsContentTypes(t *testing.T) {
	raw := []byte(`{"type":"assistant","uuid":"a1","sessionId":"s1","message":{"role":"assistant"}}`)
	got, ok := ParseLine(raw)
	if !ok {
		t.Fatal("ParseLine returned ok=false for an assistant line")
	}
	if got.Type != "assistant" || got.UUID != "a1" || got.SessionID != "s1" {
		t.Errorf("ParseLine = %+v, want type=assistant uuid=a1 sessionId=s1", got)
	}
}

func TestParseLineRejectsMetadataTypes(t *testing.T) {
	for _, typ := range []string{
		"last-prompt", "custom-title", "agent-name", "mode",
		"permission-mode", "atis-latch", "attachment",
		"file-history-snapshot", "system", "totally-new-future-type",
	} {
		raw := []byte(`{"type":"` + typ + `","uuid":"x"}`)
		if _, ok := ParseLine(raw); ok {
			t.Errorf("ParseLine(%q) returned ok=true, want false", typ)
		}
	}
}

func TestParseLineRejectsMalformedJSON(t *testing.T) {
	if _, ok := ParseLine([]byte(`{"type":"user"`)); ok {
		t.Error("ParseLine returned ok=true for truncated JSON")
	}
	if _, ok := ParseLine([]byte(``)); ok {
		t.Error("ParseLine returned ok=true for an empty line")
	}
}

func TestParseLineFixtureYieldsFourContentLines(t *testing.T) {
	b, err := os.ReadFile("testdata/basic.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var n int
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if _, ok := ParseLine([]byte(l)); ok {
			n++
		}
	}
	if n != 4 {
		t.Errorf("accepted %d lines, want 4 (2 user + 2 assistant)", n)
	}
}
