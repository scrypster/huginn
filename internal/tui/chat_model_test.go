package tui

import (
	"context"
	"testing"
)

func TestChatModel_AddLine(t *testing.T) {
	var cm ChatModel
	cm.AddLine("user", "hello")
	cm.AddLine("assistant", "hi there")
	if len(cm.history) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(cm.history))
	}
	if cm.history[0].role != "user" || cm.history[0].content != "hello" {
		t.Errorf("line 0: got %q/%q", cm.history[0].role, cm.history[0].content)
	}
	if cm.history[1].role != "assistant" || cm.history[1].content != "hi there" {
		t.Errorf("line 1: got %q/%q", cm.history[1].role, cm.history[1].content)
	}
}

func TestChatModel_StreamingState(t *testing.T) {
	var cm ChatModel
	if cm.IsStreaming() {
		t.Error("expected not streaming initially")
	}

	cm.streaming.WriteString("token1")
	cm.tokenCount++
	_, cancel := context.WithCancel(context.Background())
	cm.cancelStream = cancel
	defer cancel()

	if !cm.IsStreaming() {
		t.Error("expected streaming after cancel set")
	}
	if cm.tokenCount != 1 {
		t.Errorf("expected tokenCount 1, got %d", cm.tokenCount)
	}
	if cm.streaming.String() != "token1" {
		t.Errorf("expected streaming content 'token1', got %q", cm.streaming.String())
	}
}

func TestChatModel_ThoughtStreaming(t *testing.T) {
	var cm ChatModel
	cm.thoughtStreaming.WriteString("thinking...")
	if cm.thoughtStreaming.String() != "thinking..." {
		t.Errorf("unexpected thought content: %q", cm.thoughtStreaming.String())
	}
}

func TestChatModel_Reset(t *testing.T) {
	cm := ChatModel{tokenCount: 5}
	cm.streaming.WriteString("some tokens")
	cm.thoughtStreaming.WriteString("thoughts")
	cm.AddLine("user", "preserved")

	_, cancel := context.WithCancel(context.Background())
	cm.cancelStream = cancel
	defer cancel()

	cm.Reset()

	if cm.streaming.Len() != 0 {
		t.Error("streaming not reset")
	}
	if cm.thoughtStreaming.Len() != 0 {
		t.Error("thoughtStreaming not reset")
	}
	if cm.tokenCount != 0 {
		t.Error("tokenCount not reset")
	}
	if cm.cancelStream != nil {
		t.Error("cancelStream not cleared")
	}
	if cm.runner != nil {
		t.Error("runner not cleared")
	}
	if cm.eventCh != nil {
		t.Error("eventCh not cleared")
	}
	// History is preserved across Reset.
	if len(cm.history) != 1 {
		t.Errorf("expected history preserved, got %d lines", len(cm.history))
	}
}

func TestChatModel_ClearHistory(t *testing.T) {
	var cm ChatModel
	cm.AddLine("user", "hello")
	cm.AddLine("assistant", "world")
	cm.ClearHistory()
	if len(cm.history) != 0 {
		t.Errorf("expected empty history, got %d", len(cm.history))
	}
}

func TestChatModel_AppInChatField(t *testing.T) {
	// Verify the App struct uses ChatModel through the chat field.
	a := New(nil, nil, nil, "test")
	a.chat.AddLine("user", "via chat model")
	if len(a.chat.history) != 1 {
		t.Fatalf("expected 1 line via chat, got %d", len(a.chat.history))
	}
	if a.chat.history[0].content != "via chat model" {
		t.Errorf("unexpected content: %q", a.chat.history[0].content)
	}
}
