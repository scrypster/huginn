package relay

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDispatcher_ModelProviderList_NilCallback(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID:         "m1",
		Hub:               hub,
		GetModelProviders: nil,
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelProviderListRequest,
		Payload: map[string]any{},
	})

	time.Sleep(50 * time.Millisecond)
	for _, m := range hub.collected() {
		if m.Type == MsgModelProviderListResult {
			t.Errorf("unexpected MsgModelProviderListResult when callback is nil")
		}
	}
}

func TestDispatcher_ModelProviderList_RedactsAPIKey(t *testing.T) {
	hub := &collectHub{}
	providers := []ModelProviderInfo{
		{
			ID:        "ollama",
			Name:      "Ollama (local)",
			Endpoint:  "http://localhost:11434",
			APIKey:    "super-secret-key",
			Connected: true,
			Models:    []ModelInfo{{Name: "llama3:latest", Size: "4.7 GB"}},
		},
	}
	cfg := DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		GetModelProviders: func() []ModelProviderInfo {
			return providers
		},
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelProviderListRequest,
		Payload: map[string]any{},
	})

	got := hub.waitFor(t, MsgModelProviderListResult, 2*time.Second)
	list, ok := got.Payload["providers"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("expected providers slice in payload, got %T: %v", got.Payload["providers"], got.Payload["providers"])
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any entry, got %T", list[0])
	}
	if apiKey, _ := entry["api_key"].(string); apiKey != "" {
		t.Errorf("expected api_key redacted (empty), got %q", apiKey)
	}
	if name, _ := entry["name"].(string); name != "Ollama (local)" {
		t.Errorf("expected name Ollama (local), got %q", name)
	}
}

func TestDispatcher_ModelConfigGet_NilCallback(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{MachineID: "m1", Hub: hub, GetModelConfig: nil}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelConfigGetRequest,
		Payload: map[string]any{"provider": "ollama"},
	})

	time.Sleep(50 * time.Millisecond)
	for _, m := range hub.collected() {
		if m.Type == MsgModelConfigGetResult {
			t.Errorf("unexpected result when callback is nil")
		}
	}
}

func TestDispatcher_ModelConfigGet_ReturnsInfo(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		GetModelConfig: func(provider string) (*ModelProviderInfo, error) {
			if provider != "ollama" {
				return nil, errors.New("unknown provider")
			}
			return &ModelProviderInfo{
				ID:       "ollama",
				Name:     "Ollama (local)",
				Endpoint: "http://localhost:11434",
				APIKey:   "secret",
			}, nil
		},
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelConfigGetRequest,
		Payload: map[string]any{"provider": "ollama"},
	})

	got := hub.waitFor(t, MsgModelConfigGetResult, 2*time.Second)
	entry, ok := got.Payload["provider"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any in payload[provider], got %T", got.Payload["provider"])
	}
	if apiKey, _ := entry["api_key"].(string); apiKey != "[REDACTED]" {
		t.Errorf("expected api_key [REDACTED], got %q", apiKey)
	}
}

func TestDispatcher_ModelConfigUpdate_NilCallback(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{MachineID: "m1", Hub: hub, UpdateModelConfig: nil}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelConfigUpdateRequest,
		Payload: map[string]any{"provider": "ollama", "endpoint": "http://localhost:11434", "api_key": ""},
	})

	time.Sleep(50 * time.Millisecond)
	for _, m := range hub.collected() {
		if m.Type == MsgModelConfigUpdateResult {
			t.Errorf("unexpected result when callback is nil")
		}
	}
}

func TestDispatcher_ModelConfigUpdate_Success(t *testing.T) {
	hub := &collectHub{}
	var capturedProvider, capturedEndpoint, capturedAPIKey string
	cfg := DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		UpdateModelConfig: func(provider, endpoint, apiKey string) error {
			capturedProvider = provider
			capturedEndpoint = endpoint
			capturedAPIKey = apiKey
			return nil
		},
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelConfigUpdateRequest,
		Payload: map[string]any{"provider": "ollama", "endpoint": "http://localhost:11434", "api_key": "new-key"},
	})

	got := hub.waitFor(t, MsgModelConfigUpdateResult, 2*time.Second)
	if ok, _ := got.Payload["ok"].(bool); !ok {
		t.Errorf("expected ok=true, got %v", got.Payload["ok"])
	}
	if capturedProvider != "ollama" || capturedEndpoint != "http://localhost:11434" || capturedAPIKey != "new-key" {
		t.Errorf("callback received wrong args: provider=%q endpoint=%q apiKey=%q", capturedProvider, capturedEndpoint, capturedAPIKey)
	}
}

func TestDispatcher_ModelConfigUpdate_SkipsRedactedAPIKey(t *testing.T) {
	hub := &collectHub{}
	var receivedAPIKey string
	cfg := DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		UpdateModelConfig: func(provider, endpoint, apiKey string) error {
			receivedAPIKey = apiKey
			return nil
		},
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelConfigUpdateRequest,
		Payload: map[string]any{"provider": "ollama", "endpoint": "http://localhost:11434", "api_key": "[REDACTED]"},
	})

	hub.waitFor(t, MsgModelConfigUpdateResult, 2*time.Second)
	if receivedAPIKey != "" {
		t.Errorf("expected empty apiKey passed to callback when [REDACTED] sent, got %q", receivedAPIKey)
	}
}

func TestDispatcher_ModelConfigUpdate_Error(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		UpdateModelConfig: func(provider, endpoint, apiKey string) error {
			return errors.New("disk full")
		},
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelConfigUpdateRequest,
		Payload: map[string]any{"provider": "anthropic", "endpoint": "https://api.anthropic.com", "api_key": "sk-ant-xxx"},
	})

	got := hub.waitFor(t, MsgModelConfigUpdateResult, 2*time.Second)
	if ok, _ := got.Payload["ok"].(bool); ok {
		t.Errorf("expected ok=false on error")
	}
	if errStr, _ := got.Payload["error"].(string); errStr == "" {
		t.Errorf("expected non-empty error string in payload")
	}
}

func TestDispatcher_ModelPull_NilCallback(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{MachineID: "m1", Hub: hub, PullModel: nil}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelPullRequest,
		Payload: map[string]any{"model": "llama3.2:3b"},
	})

	time.Sleep(50 * time.Millisecond)
	for _, m := range hub.collected() {
		if m.Type == MsgModelPullResult {
			t.Errorf("unexpected result when callback is nil")
		}
	}
}

func TestDispatcher_ModelPull_Success(t *testing.T) {
	hub := &collectHub{}
	var pulledModel string
	cfg := DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		PullModel: func(name string) error {
			pulledModel = name
			return nil
		},
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelPullRequest,
		Payload: map[string]any{"model": "llama3.2:3b"},
	})

	got := hub.waitFor(t, MsgModelPullResult, 2*time.Second)
	if ok, _ := got.Payload["ok"].(bool); !ok {
		t.Errorf("expected ok=true")
	}
	if pulledModel != "llama3.2:3b" {
		t.Errorf("expected pulled model llama3.2:3b, got %q", pulledModel)
	}
}

func TestDispatcher_ModelPull_Error(t *testing.T) {
	hub := &collectHub{}
	cfg := DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		PullModel: func(name string) error {
			return errors.New("ollama not reachable")
		},
	}
	dispatched := NewDispatcher(cfg)

	dispatched(context.Background(), Message{
		Type:    MsgModelPullRequest,
		Payload: map[string]any{"model": "llama3.2:3b"},
	})

	got := hub.waitFor(t, MsgModelPullResult, 2*time.Second)
	if ok, _ := got.Payload["ok"].(bool); ok {
		t.Errorf("expected ok=false on error")
	}
	if errStr, _ := got.Payload["error"].(string); errStr == "" {
		t.Errorf("expected error string in payload")
	}
}
