package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/modelconfig"
)

func mockShowServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/show" {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

func TestDetectVision_LlavaFamily_ReturnsTrue(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"details": map[string]any{"family": "llava", "families": []string{"llava", "clip"}}})
	srv := mockShowServer(t, string(body))
	defer srv.Close()
	got, err := DetectVision(srv.URL, "llava:13b")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !got {
		t.Error("expected vision=true for llava")
	}
}

func TestDetectVision_NoVision_ReturnsFalse(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"details": map[string]any{"family": "qwen2"}})
	srv := mockShowServer(t, string(body))
	defer srv.Close()
	got, _ := DetectVision(srv.URL, "qwen2:14b")
	if got {
		t.Error("expected vision=false")
	}
}

func TestDetectVision_ServerError_GracefulFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()
	got, err := DetectVision(srv.URL, "anymodel")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got {
		t.Error("expected false on 404")
	}
}

func TestDetectVision_ConnectionRefused_GracefulFalse(t *testing.T) {
	got, err := DetectVision("http://127.0.0.1:1", "model")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got {
		t.Error("expected false on connection refused")
	}
}

func TestFetchCapabilities_ToolsFromCapabilitiesArray(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"capabilities": []string{"completion", "tools"},
		"details":      map[string]any{"family": "qwen2"},
	})
	srv := mockShowServer(t, string(body))
	defer srv.Close()

	caps := FetchCapabilities(srv.URL, "qwen2.5-coder:7b")
	if !caps.ToolsKnown {
		t.Fatal("expected ToolsKnown after successful /api/show")
	}
	if !caps.SupportsTools {
		t.Error("Ollama capabilities.tools must set SupportsTools=true even for 7b")
	}
}

func TestFetchCapabilities_ToolsFromTemplate(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"capabilities": []string{"completion"},
		"template":     "{{ if .Tools }}use tools{{ end }}{{ .Prompt }}",
		"details":      map[string]any{"family": "qwen2"},
	})
	srv := mockShowServer(t, string(body))
	defer srv.Close()

	caps := FetchCapabilities(srv.URL, "qwen2.5-coder:14b")
	if !caps.SupportsTools {
		t.Error("expected SupportsTools=true from template .Tools section")
	}
}

func TestFetchCapabilities_NoTools(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"capabilities": []string{"completion"},
		"template":     "{{ .Prompt }}",
		"details":      map[string]any{"family": "qwen2"},
	})
	srv := mockShowServer(t, string(body))
	defer srv.Close()

	caps := FetchCapabilities(srv.URL, "plain:latest")
	if !caps.ToolsKnown {
		t.Fatal("expected ToolsKnown")
	}
	if caps.SupportsTools {
		t.Error("expected SupportsTools=false without tools capability or template")
	}
}

func TestProbeOllamaModels_FillsRegistryFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/show" {
			http.Error(w, "not found", 404)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		caps := []string{"completion"}
		if req.Name == "qwen2.5-coder:7b" || req.Name == "qwen2.5-coder:14b" {
			caps = append(caps, "tools")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"capabilities": caps,
			"details":      map[string]any{"family": "qwen2"},
		})
	}))
	defer srv.Close()

	infos := ProbeOllamaModels(t.Context(), srv.URL, []string{"qwen2.5-coder:7b", "qwen2.5-coder:14b", "plain:latest"})
	if len(infos) != 3 {
		t.Fatalf("probed %d models, want 3", len(infos))
	}
	byName := map[string]modelconfig.ModelInfo{}
	for _, info := range infos {
		byName[info.Name] = info
	}
	seven := byName["qwen2.5-coder:7b"]
	if !seven.SupportsTools {
		t.Error("7b live probe reports tools — do not hardcode SupportsTools=false")
	}
	if seven.SupportsDelegation {
		t.Error("InferCapabilities: 7b should not support delegation")
	}
	if seven.Tier != modelconfig.TierLow {
		t.Errorf("7b tier = %s, want low", seven.Tier)
	}
	fourteen := byName["qwen2.5-coder:14b"]
	if !fourteen.SupportsTools || !fourteen.SupportsDelegation {
		t.Error("14b should support tools and delegation")
	}
	if fourteen.Tier != modelconfig.TierMedium {
		t.Errorf("14b tier = %s, want medium", fourteen.Tier)
	}
	if byName["plain:latest"].SupportsTools {
		t.Error("plain model without tools capability should be SupportsTools=false")
	}
}
