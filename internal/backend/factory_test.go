package backend_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func TestNewFromConfig_Ollama(t *testing.T) {
	b, err := backend.NewFromConfig("ollama", "http://localhost:11434", "", "qwen2.5-coder:14b")
	if err != nil {
		t.Fatalf("NewFromConfig error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for ollama")
	}
}

func TestNewFromConfig_OpenAI(t *testing.T) {
	b, err := backend.NewFromConfig("openai", "", "sk-test", "gpt-4o")
	if err != nil {
		t.Fatalf("NewFromConfig error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for openai")
	}
}

func TestNewFromConfig_Anthropic(t *testing.T) {
	b, err := backend.NewFromConfig("anthropic", "", "sk-ant-test", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("NewFromConfig error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for anthropic")
	}
}

func TestNewFromConfig_OpenRouter(t *testing.T) {
	b, err := backend.NewFromConfig("openrouter", "", "sk-or-test", "anthropic/claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("NewFromConfig error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for openrouter")
	}
}

func TestNewFromConfig_UnknownProvider_ReturnsError(t *testing.T) {
	_, err := backend.NewFromConfig("unknown-provider-xyz", "", "", "some-model")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestNewFromConfig_Anthropic_ResolvesEnvAPIKey(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-from-env")
	b, err := backend.NewFromConfig("anthropic", "", "$TEST_ANTHROPIC_KEY", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("NewFromConfig error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
}
