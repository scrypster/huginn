package backend

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func requireGoogleAILive(t *testing.T) string {
	t.Helper()
	if os.Getenv("TESTS_LIVE_GOOGLE_AI") != "1" {
		t.Skip("set TESTS_LIVE_GOOGLE_AI=1 to run live Google AI Studio tests")
	}
	key := os.Getenv("GOOGLE_AI_API_KEY")
	if key == "" {
		t.Fatal("GOOGLE_AI_API_KEY not set")
	}
	return key
}

func TestGoogleAI_Live_Flash(t *testing.T) {
	key := requireGoogleAILive(t)
	b := NewGoogleAIBackend(NewKeyResolver(key), "gemini-2.5-flash")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := b.ChatCompletion(ctx, ChatRequest{
		Model:    "gemini-2.5-flash",
		Messages: []Message{{Role: "user", Content: "Reply with exactly the word: pong"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Content), "pong") {
		t.Errorf("Content = %q, want substring 'pong'", resp.Content)
	}
	t.Logf("gemini-2.5-flash content=%q tokens=%d/%d done=%q",
		resp.Content, resp.PromptTokens, resp.CompletionTokens, resp.DoneReason)
}

func TestGoogleAI_Live_Pro(t *testing.T) {
	key := requireGoogleAILive(t)
	b := NewGoogleAIBackend(NewKeyResolver(key), "gemini-2.5-pro")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := b.ChatCompletion(ctx, ChatRequest{
		Model:    "gemini-2.5-pro",
		Messages: []Message{{Role: "user", Content: "Reply with exactly the word: pong"}},
	})
	if err != nil {
		// Free-tier API keys often have zero pro-model quota. Skip rather
		// than fail when that's the specific reason — gemini-2.5-flash still
		// validates the Google AI Studio path on the same key.
		if IsRateLimited(err) {
			t.Skipf("gemini-2.5-pro rate-limited (likely free-tier quota): %v", err)
		}
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Content), "pong") {
		t.Errorf("Content = %q, want substring 'pong'", resp.Content)
	}
	t.Logf("gemini-2.5-pro content=%q tokens=%d/%d done=%q",
		resp.Content, resp.PromptTokens, resp.CompletionTokens, resp.DoneReason)
}

func TestGoogleAI_Live_Health(t *testing.T) {
	key := requireGoogleAILive(t)
	b := NewGoogleAIBackend(NewKeyResolver(key), "gemini-2.5-flash")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
}
