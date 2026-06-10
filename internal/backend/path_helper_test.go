package backend

import (
	"testing"
)

func TestOpenAICompatiblePath(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		subPath  string
		want     string
	}{
		{
			name:     "ollama — no version segment",
			endpoint: "http://localhost:11434",
			subPath:  "chat/completions",
			want:     "http://localhost:11434/v1/chat/completions",
		},
		{
			name:     "ollama — models",
			endpoint: "http://localhost:11434",
			subPath:  "models",
			want:     "http://localhost:11434/v1/models",
		},
		{
			name:     "openai — no version segment",
			endpoint: "https://api.openai.com",
			subPath:  "chat/completions",
			want:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "deepseek — no version segment",
			endpoint: "https://api.deepseek.com",
			subPath:  "chat/completions",
			want:     "https://api.deepseek.com/v1/chat/completions",
		},
		{
			name:     "zai — endpoint already ends with v4",
			endpoint: "https://api.z.ai/api/paas/v4",
			subPath:  "chat/completions",
			want:     "https://api.z.ai/api/paas/v4/chat/completions",
		},
		{
			name:     "zai — models endpoint",
			endpoint: "https://api.z.ai/api/paas/v4",
			subPath:  "models",
			want:     "https://api.z.ai/api/paas/v4/models",
		},
		{
			name:     "endpoint already ending in /v1",
			endpoint: "https://api.openai.com/v1",
			subPath:  "chat/completions",
			want:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "openrouter — endpoint ends in v1",
			endpoint: "https://openrouter.ai/api/v1",
			subPath:  "chat/completions",
			want:     "https://openrouter.ai/api/v1/chat/completions",
		},
		{
			name:     "trailing slash stripped",
			endpoint: "https://api.openai.com/",
			subPath:  "chat/completions",
			want:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "trailing slash stripped with version segment",
			endpoint: "https://api.z.ai/api/paas/v4/",
			subPath:  "models",
			want:     "https://api.z.ai/api/paas/v4/models",
		},
		{
			name:     "custom endpoint no version",
			endpoint: "https://my.provider.example.com",
			subPath:  "chat/completions",
			want:     "https://my.provider.example.com/v1/chat/completions",
		},
		{
			name:     "v2 version segment",
			endpoint: "https://some.api.com/v2",
			subPath:  "chat/completions",
			want:     "https://some.api.com/v2/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openAICompatiblePath(tt.endpoint, tt.subPath)
			if got != tt.want {
				t.Errorf("openAICompatiblePath(%q, %q) = %q; want %q", tt.endpoint, tt.subPath, got, tt.want)
			}
		})
	}
}
