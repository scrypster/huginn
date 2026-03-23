package tui

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// TestThinkingTokenMsg_Distinct verifies that thinkingTokenMsg is a different
// type from tokenMsg so the TUI Update switch can dispatch them separately.
func TestThinkingTokenMsg_Distinct(t *testing.T) {
	var tm tokenMsg = "regular"
	var th thinkingTokenMsg = "thought"
	if string(tm) != "regular" {
		t.Errorf("tokenMsg = %q, want \"regular\"", string(tm))
	}
	if string(th) != "thought" {
		t.Errorf("thinkingTokenMsg = %q, want \"thought\"", string(th))
	}
}

// TestStreamEventToTUIMsg verifies the conversion from StreamEvent to tea.Msg.
func TestStreamEventToTUIMsg(t *testing.T) {
	cases := []struct {
		event    backend.StreamEvent
		wantType string
	}{
		{backend.StreamEvent{Type: backend.StreamText, Content: "hi"}, "tokenMsg"},
		{backend.StreamEvent{Type: backend.StreamThought, Content: "hmm"}, "thinkingTokenMsg"},
		{backend.StreamEvent{Type: backend.StreamDone}, "streamDoneMsg"},
	}
	for _, tc := range cases {
		msg := streamEventToMsg(tc.event)
		switch msg.(type) {
		case tokenMsg:
			if tc.wantType != "tokenMsg" {
				t.Errorf("event %v: got tokenMsg, want %s", tc.event.Type, tc.wantType)
			}
		case thinkingTokenMsg:
			if tc.wantType != "thinkingTokenMsg" {
				t.Errorf("event %v: got thinkingTokenMsg, want %s", tc.event.Type, tc.wantType)
			}
		case streamDoneMsg:
			if tc.wantType != "streamDoneMsg" {
				t.Errorf("event %v: got streamDoneMsg, want %s", tc.event.Type, tc.wantType)
			}
		default:
			t.Errorf("event %v: unknown msg type", tc.event.Type)
		}
	}
}

// TestStyleThought_Color verifies the thinking token style is non-zero.
func TestStyleThought_Color(t *testing.T) {
	rendered := StyleThought.Render("test")
	if rendered == "" {
		t.Error("StyleThought.Render returned empty string")
	}
}
