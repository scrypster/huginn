package backend_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func TestNewFromConfig_DeepSeek_DefaultEndpoint(t *testing.T) {
	b, err := backend.NewFromConfig("deepseek", "", "sk-test", "deepseek-chat")
	if err != nil {
		t.Fatalf("NewFromConfig(deepseek) error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for deepseek")
	}
}

func TestNewFromConfig_DeepSeek_CustomEndpoint(t *testing.T) {
	b, err := backend.NewFromConfig("deepseek", "https://custom.deepseek.example.com", "sk-test", "deepseek-chat")
	if err != nil {
		t.Fatalf("NewFromConfig(deepseek, custom) error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for deepseek with custom endpoint")
	}
}

func TestNewFromConfig_Zai_DefaultEndpoint(t *testing.T) {
	b, err := backend.NewFromConfig("zai", "", "sk-test", "glm-5")
	if err != nil {
		t.Fatalf("NewFromConfig(zai) error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for zai")
	}
}

func TestNewFromConfig_Zai_CustomEndpoint(t *testing.T) {
	b, err := backend.NewFromConfig("zai", "https://api.z.ai/api/paas/v4", "sk-test", "glm-5")
	if err != nil {
		t.Fatalf("NewFromConfig(zai, custom) error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for zai with explicit endpoint")
	}
}

func TestNewFromConfig_Custom_ErrorWithoutEndpoint(t *testing.T) {
	_, err := backend.NewFromConfig("custom", "", "sk-test", "some-model")
	if err == nil {
		t.Fatal("expected error for custom provider without endpoint")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("expected error to mention endpoint, got: %v", err)
	}
}

func TestNewFromConfig_Custom_SucceedsWithEndpoint(t *testing.T) {
	b, err := backend.NewFromConfig("custom", "https://my.provider.example.com/v1", "sk-test", "my-model")
	if err != nil {
		t.Fatalf("NewFromConfig(custom) with endpoint error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend for custom with endpoint")
	}
}
