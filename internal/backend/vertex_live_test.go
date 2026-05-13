package backend

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// requireLive skips the test unless TESTS_LIVE_VERTEX=1 is set in the
// environment. Live tests hit real Google Vertex AI endpoints, consume quota,
// and may incur charges — they are opt-in.
func requireLive(t *testing.T) (project, location, credsPath string) {
	t.Helper()
	if os.Getenv("TESTS_LIVE_VERTEX") != "1" {
		t.Skip("set TESTS_LIVE_VERTEX=1 to run live Vertex AI tests")
	}
	project = os.Getenv("GOOGLE_CLOUD_PROJECT")
	location = os.Getenv("GOOGLE_CLOUD_LOCATION")
	credsPath = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if project == "" {
		t.Fatal("GOOGLE_CLOUD_PROJECT not set")
	}
	if location == "" {
		t.Fatal("GOOGLE_CLOUD_LOCATION not set")
	}
	if credsPath == "" {
		t.Fatal("GOOGLE_APPLICATION_CREDENTIALS not set")
	}
	return
}

func TestVertex_Live_Gemini_Flash(t *testing.T) {
	project, location, credsPath := requireLive(t)
	b, err := NewVertexBackend(context.Background(), VertexConfig{
		Project:         project,
		Location:        location,
		CredentialsPath: credsPath,
		Model:           "gemini-2.5-flash",
	})
	if err != nil {
		t.Fatalf("NewVertexBackend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := b.ChatCompletion(ctx, ChatRequest{
		Model: "gemini-2.5-flash",
		Messages: []Message{
			{Role: "user", Content: "Reply with exactly the word: pong"},
		},
	})
	if err != nil {
		// Some GCP orgs restrict gemini-2.5-flash via the
		// constraints/vertexai.allowedModels org policy. Skip rather than
		// fail when that specific FAILED_PRECONDITION is the cause — the
		// gemini-2.5-pro test still validates the Gemini path.
		if strings.Contains(err.Error(), "allowedModels") {
			t.Skipf("gemini-2.5-flash blocked by org policy in this project: %v", err)
		}
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Content), "pong") {
		t.Errorf("Content = %q, want substring 'pong'", resp.Content)
	}
	t.Logf("gemini-2.5-flash content=%q tokens=%d/%d done=%q",
		resp.Content, resp.PromptTokens, resp.CompletionTokens, resp.DoneReason)
}

func TestVertex_Live_Gemini_Pro(t *testing.T) {
	project, location, credsPath := requireLive(t)
	b, err := NewVertexBackend(context.Background(), VertexConfig{
		Project:         project,
		Location:        location,
		CredentialsPath: credsPath,
		Model:           "gemini-2.5-pro",
	})
	if err != nil {
		t.Fatalf("NewVertexBackend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := b.ChatCompletion(ctx, ChatRequest{
		Model: "gemini-2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "Reply with exactly the word: pong"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Content), "pong") {
		t.Errorf("Content = %q, want substring 'pong'", resp.Content)
	}
	t.Logf("gemini-2.5-pro content=%q tokens=%d/%d done=%q",
		resp.Content, resp.PromptTokens, resp.CompletionTokens, resp.DoneReason)
}

func TestVertex_Live_AnthropicOnVertex_Sonnet(t *testing.T) {
	project, location, credsPath := requireLive(t)
	b, err := NewVertexBackend(context.Background(), VertexConfig{
		Project:         project,
		Location:        location,
		CredentialsPath: credsPath,
		Model:           "claude-sonnet-4-5",
	})
	if err != nil {
		t.Fatalf("NewVertexBackend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := b.ChatCompletion(ctx, ChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "user", Content: "Reply with exactly the word: pong"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Content), "pong") {
		t.Errorf("Content = %q, want substring 'pong'", resp.Content)
	}
	t.Logf("claude-sonnet-4-5 content=%q tokens=%d/%d done=%q",
		resp.Content, resp.PromptTokens, resp.CompletionTokens, resp.DoneReason)
}

func TestVertex_Live_Health(t *testing.T) {
	project, location, credsPath := requireLive(t)
	b, err := NewVertexBackend(context.Background(), VertexConfig{
		Project:         project,
		Location:        location,
		CredentialsPath: credsPath,
		Model:           "gemini-2.5-flash",
	})
	if err != nil {
		t.Fatalf("NewVertexBackend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
}
