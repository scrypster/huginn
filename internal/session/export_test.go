package session

import (
	"strings"
	"testing"
	"time"
)

func TestExportMarkdown(t *testing.T) {
	msgs := []SessionMessage{
		{Role: "user", Content: "Hello, agent!", Ts: time.Now()},
		{Role: "assistant", Content: "Hello! How can I help?", Ts: time.Now()},
	}
	out := ExportMarkdown(msgs, "Test Session")
	if !strings.Contains(out, "Test Session") {
		t.Error("expected title in markdown output")
	}
	if !strings.Contains(out, "Hello, agent!") {
		t.Error("expected user message in markdown output")
	}
	if !strings.Contains(out, "Hello! How can I help?") {
		t.Error("expected assistant message in markdown output")
	}
	if !strings.Contains(out, "**User**") || !strings.Contains(out, "**Assistant**") {
		t.Error("expected role headers in markdown output")
	}
}

func TestExportJSON(t *testing.T) {
	msgs := []SessionMessage{
		{Role: "user", Content: "Test message", Ts: time.Now()},
	}
	out, err := ExportJSON(msgs)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	if !strings.Contains(out, "Test message") {
		t.Error("expected message in JSON output")
	}
	if !strings.Contains(out, `"role"`) {
		t.Error("expected 'role' key in JSON output")
	}
}

func TestExportMarkdown_SkipsCostRecords(t *testing.T) {
	msgs := []SessionMessage{
		{Role: "user", Content: "What is 2+2?", Ts: time.Now()},
		{Type: "cost", CostUSD: 0.001, PromptTok: 10, CompTok: 5},
		{Role: "assistant", Content: "The answer is 4.", Ts: time.Now()},
	}
	out := ExportMarkdown(msgs, "Math Session")

	// Cost record should not appear as a formatted section in the output.
	if strings.Contains(out, "0.001") {
		t.Error("cost record dollar amount should not appear in markdown output")
	}
	// Real messages should still be present.
	if !strings.Contains(out, "What is 2+2?") {
		t.Error("expected user message in markdown output")
	}
	if !strings.Contains(out, "The answer is 4.") {
		t.Error("expected assistant message in markdown output")
	}
}

func TestExportMarkdown_EmptyMessages(t *testing.T) {
	out := ExportMarkdown(nil, "Empty Session")
	if !strings.Contains(out, "Empty Session") {
		t.Error("expected title even with no messages")
	}
}

func TestExportJSON_Empty(t *testing.T) {
	out, err := ExportJSON(nil)
	if err != nil {
		t.Fatalf("ExportJSON with nil: %v", err)
	}
	// JSON marshaling of nil slice yields "null"
	if out != "null" {
		t.Errorf("expected 'null' for empty slice, got %q", out)
	}
}
