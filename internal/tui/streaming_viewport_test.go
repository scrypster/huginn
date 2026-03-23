package tui

// streaming_viewport_test.go — tests that verify token-by-token viewport
// updates during streaming.  All message types (tokenMsg, thinkingTokenMsg,
// streamDoneMsg, warningMsg) are unexported so these tests must live in
// package tui (same package), not package tui_test.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// streamingApp returns a minimal App initialised for streaming tests.
// It reuses newMinimalApp() (state=stateChat) and switches the state to
// stateStreaming so the UI correctly shows the streaming affordances.
func streamingApp() *App {
	a := newMinimalApp()
	a.state = stateStreaming
	return a
}

// castApp asserts that the returned tea.Model is an *App and fails the test
// otherwise, making call-sites a single line.
func castApp(t *testing.T, m tea.Model) *App {
	t.Helper()
	app, ok := m.(*App)
	if !ok {
		t.Fatalf("Update() returned %T, expected *App", m)
	}
	return app
}

// ---------------------------------------------------------------------------
// TestStreamingViewport_SingleToken
// ---------------------------------------------------------------------------

// TestStreamingViewport_SingleToken verifies that sending a single tokenMsg
// accumulates the token content in chat.streaming and increments tokenCount.
func TestStreamingViewport_SingleToken(t *testing.T) {
	a := streamingApp()

	model, _ := a.Update(tokenMsg("hello"))
	updated := castApp(t, model)

	if updated.chat.tokenCount != 1 {
		t.Errorf("expected tokenCount=1 after one token, got %d", updated.chat.tokenCount)
	}
	if updated.chat.streaming.String() != "hello" {
		t.Errorf("expected streaming buffer 'hello', got %q", updated.chat.streaming.String())
	}
}

// ---------------------------------------------------------------------------
// TestStreamingViewport_MultipleTokens
// ---------------------------------------------------------------------------

// TestStreamingViewport_MultipleTokens verifies that 5 sequential tokenMsgs
// accumulate their content in the streaming buffer and count every token.
func TestStreamingViewport_MultipleTokens(t *testing.T) {
	a := streamingApp()

	tokens := []string{"The", " quick", " brown", " fox", " jumps"}
	for _, tok := range tokens {
		model, _ := a.Update(tokenMsg(tok))
		a = castApp(t, model)
	}

	if a.chat.tokenCount != 5 {
		t.Errorf("expected tokenCount=5, got %d", a.chat.tokenCount)
	}
	want := "The quick brown fox jumps"
	if a.chat.streaming.String() != want {
		t.Errorf("expected streaming buffer %q, got %q", want, a.chat.streaming.String())
	}
}

// ---------------------------------------------------------------------------
// TestStreamingViewport_ThinkingTokenDistinct
// ---------------------------------------------------------------------------

// TestStreamingViewport_ThinkingTokenDistinct verifies that a thinkingTokenMsg
// is handled without panic and that the thought content ends up in
// chat.thoughtStreaming (not chat.chat.streaming) and tokenCount is incremented.
func TestStreamingViewport_ThinkingTokenDistinct(t *testing.T) {
	a := streamingApp()

	model, _ := a.Update(thinkingTokenMsg("I think therefore I am"))
	updated := castApp(t, model)

	if updated.chat.tokenCount != 1 {
		t.Errorf("expected tokenCount=1 after thinking token, got %d", updated.chat.tokenCount)
	}
	// The thought is rendered through StyleThought.Render(), so the raw string
	// "I think therefore I am" will be contained within the styled output.
	thought := updated.chat.thoughtStreaming.String()
	if !strings.Contains(thought, "I think therefore I am") {
		t.Errorf("expected thought content in thoughtStreaming buffer, got %q", thought)
	}
	// Regular streaming buffer must remain empty.
	if updated.chat.streaming.Len() != 0 {
		t.Errorf("regular streaming buffer must be empty after thinking token, got %q",
			updated.chat.streaming.String())
	}
}

// ---------------------------------------------------------------------------
// TestStreamingViewport_StreamDoneClears
// ---------------------------------------------------------------------------

// TestStreamingViewport_StreamDoneClears verifies that receiving a
// streamDoneMsg when streaming is active:
//   - resets the streaming buffer
//   - transitions state back to stateChat
//   - commits the buffered text to chat.history as an "assistant" line
//
// Note: handleStreamDoneMsg does NOT reset tokenCount — it is left as a
// running total and only cleared by ChatModel.Reset() on the next session.
func TestStreamingViewport_StreamDoneClears(t *testing.T) {
	a := streamingApp()

	// Accumulate some tokens first.
	for _, tok := range []string{"foo", " bar"} {
		model, _ := a.Update(tokenMsg(tok))
		a = castApp(t, model)
	}
	if a.chat.tokenCount != 2 {
		t.Fatalf("pre-condition: expected tokenCount=2, got %d", a.chat.tokenCount)
	}

	// Now signal stream completion.
	model, _ := a.Update(streamDoneMsg{})
	updated := castApp(t, model)

	// tokenCount is NOT reset by streamDoneMsg — it retains the session total.
	if updated.chat.tokenCount != 2 {
		t.Errorf("tokenCount should remain at 2 after streamDoneMsg (not reset), got %d",
			updated.chat.tokenCount)
	}
	if updated.chat.streaming.Len() != 0 {
		t.Errorf("expected streaming buffer cleared after streamDoneMsg, got %q",
			updated.chat.streaming.String())
	}
	if updated.state != stateChat {
		t.Errorf("expected stateChat after streamDoneMsg, got %v", updated.state)
	}
	// The buffered text should have been committed to history.
	found := false
	for _, line := range updated.chat.history {
		if line.role == "assistant" && strings.Contains(line.content, "foo bar") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'foo bar' committed to history as 'assistant' line; history=%v",
			updated.chat.history)
	}
}

// ---------------------------------------------------------------------------
// TestStreamingViewport_WarningMsg
// ---------------------------------------------------------------------------

// TestStreamingViewport_WarningMsg verifies that a warningMsg is appended as a
// "system" chat history line and does not panic. The warning flushes any pending
// streaming content to history first, then appends its own line — it does NOT
// write into chat.streaming (that would cause raw ANSI in the viewport later).
func TestStreamingViewport_WarningMsg(t *testing.T) {
	a := streamingApp()

	model, _ := a.Update(warningMsg("context window is 90% full"))
	updated := castApp(t, model)

	// Warning goes to chat.history as a "system" line, not to chat.chat.streaming.
	var found bool
	for _, line := range updated.chat.history {
		if line.role == "system" && strings.Contains(line.content, "context window is 90% full") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning in chat history as system line, history: %+v", updated.chat.history)
	}
	// streaming buffer should be empty after warning (flushed by handleWarningMsg)
	if updated.chat.streaming.Len() != 0 {
		t.Errorf("expected streaming buffer empty after warningMsg, got %q", updated.chat.streaming.String())
	}
}

// ---------------------------------------------------------------------------
// TestStreamingViewport_InterruptMidToken
// ---------------------------------------------------------------------------

// TestStreamingViewport_InterruptMidToken verifies that a large token followed
// immediately by a streamDoneMsg causes no panic and correctly finalises state.
func TestStreamingViewport_InterruptMidToken(t *testing.T) {
	a := streamingApp()

	largeToken := strings.Repeat("x", 10_000)
	model, _ := a.Update(tokenMsg(largeToken))
	a = castApp(t, model)

	// Immediately done — simulates interrupt mid-stream.
	model, _ = a.Update(streamDoneMsg{})
	updated := castApp(t, model)

	if updated.state != stateChat {
		t.Errorf("expected stateChat after interrupt, got %v", updated.state)
	}
	// tokenCount is not reset by streamDoneMsg — confirm it retains the count from before done.
	if updated.chat.tokenCount != 1 {
		t.Errorf("expected tokenCount=1 (retained) after streamDoneMsg, got %d", updated.chat.tokenCount)
	}
	// History must contain the large token as an assistant line.
	found := false
	for _, line := range updated.chat.history {
		if line.role == "assistant" && len(line.content) >= 10_000 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected large-token content committed to history as assistant line")
	}
}

// ---------------------------------------------------------------------------
// TestStreamingViewport_ViewportResizeDuringStream
// ---------------------------------------------------------------------------

// TestStreamingViewport_ViewportResizeDuringStream verifies that a
// tea.WindowSizeMsg delivered while streaming is active causes no panic and
// that the viewport dimensions are updated accordingly.
func TestStreamingViewport_ViewportResizeDuringStream(t *testing.T) {
	a := streamingApp()

	// Put a token in flight so streaming is active.
	model, _ := a.Update(tokenMsg("streaming content"))
	a = castApp(t, model)

	// Deliver a resize event.
	model, _ = a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updated := castApp(t, model)

	// Width and height must reflect the new size.
	if updated.width != 120 {
		t.Errorf("expected width=120 after resize, got %d", updated.width)
	}
	if updated.height != 40 {
		t.Errorf("expected height=40 after resize, got %d", updated.height)
	}
	// The streaming buffer must still be intact — resize must not flush it.
	if updated.chat.streaming.String() != "streaming content" {
		t.Errorf("streaming buffer must survive resize, got %q", updated.chat.streaming.String())
	}
}
