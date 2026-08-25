package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/modelconfig"
)

type ModelCapabilities struct {
	ContextLength  int
	SupportsVision bool
	SupportsTools  bool
	// ToolsKnown is true when /api/show was reachable and decoded, so
	// SupportsTools reflects the live probe rather than an optimistic default.
	ToolsKnown bool
}

type ollamaShowResponse struct {
	Capabilities []string `json:"capabilities"`
	Template     string   `json:"template"`
	Details      struct {
		Family   string   `json:"family"`
		Families []string `json:"families"`
	} `json:"details"`
	ModelInfo map[string]json.RawMessage `json:"model_info"`
}

const ollamaShowTimeout = 5 * time.Second

func showModel(ctx context.Context, baseURL, model string) (*ollamaShowResponse, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqBody, err := json.Marshal(map[string]string{"name": model})
	if err != nil {
		return nil, false
	}
	reqCtx, cancel := context.WithTimeout(ctx, ollamaShowTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/show", bytes.NewReader(reqBody))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, false
	}
	var show ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&show); err != nil {
		return nil, false
	}
	return &show, true
}

func visionFromShow(show *ollamaShowResponse) bool {
	visionFamilies := []string{"llava", "clip", "moondream", "bakllava"}
	checkFamily := func(s string) bool {
		lower := strings.ToLower(s)
		for _, kw := range visionFamilies {
			if strings.Contains(lower, kw) {
				return true
			}
		}
		return false
	}
	if checkFamily(show.Details.Family) {
		return true
	}
	for _, f := range show.Details.Families {
		if checkFamily(f) {
			return true
		}
	}
	for k, v := range show.ModelInfo {
		if strings.Contains(strings.ToLower(k), "projector") {
			return true
		}
		var s string
		if json.Unmarshal(v, &s) == nil && strings.Contains(strings.ToLower(s), "projector") {
			return true
		}
	}
	return false
}

// toolsFromShow reports whether Ollama advertises tool calling.
// Prefers the capabilities array; falls back to a template `.Tools` section.
func toolsFromShow(show *ollamaShowResponse) bool {
	for _, c := range show.Capabilities {
		if strings.EqualFold(strings.TrimSpace(c), "tools") {
			return true
		}
	}
	tmpl := show.Template
	if tmpl == "" {
		return false
	}
	return strings.Contains(tmpl, ".Tools") || strings.Contains(tmpl, ".tools")
}

func contextLengthFromShow(show *ollamaShowResponse) int {
	for k, v := range show.ModelInfo {
		if !strings.HasSuffix(strings.ToLower(k), "context_length") {
			continue
		}
		var n int
		if json.Unmarshal(v, &n) == nil && n > 0 {
			return n
		}
	}
	return 0
}

func DetectVision(baseURL, model string) (bool, error) {
	show, ok := showModel(context.Background(), baseURL, model)
	if !ok {
		return false, nil
	}
	return visionFromShow(show), nil
}

func FetchCapabilities(baseURL, model string) ModelCapabilities {
	return FetchCapabilitiesContext(context.Background(), baseURL, model)
}

func FetchCapabilitiesContext(ctx context.Context, baseURL, model string) ModelCapabilities {
	show, ok := showModel(ctx, baseURL, model)
	if !ok {
		return ModelCapabilities{}
	}
	return ModelCapabilities{
		ContextLength:  contextLengthFromShow(show),
		SupportsVision: visionFromShow(show),
		SupportsTools:  toolsFromShow(show),
		ToolsKnown:     true,
	}
}

const probeConcurrency = 8

// ProbeOllamaModels hits /api/show for each name and returns ModelInfo
// with SupportsTools set from the live probe (not a 7b name heuristic).
// Models whose show call fails are omitted so the registry stays optimistic.
func ProbeOllamaModels(ctx context.Context, baseURL string, names []string) []modelconfig.ModelInfo {
	if len(names) == 0 {
		return nil
	}
	out := make([]modelconfig.ModelInfo, 0, len(names))
	var mu sync.Mutex
	sem := make(chan struct{}, probeConcurrency)
	var wg sync.WaitGroup
	for _, name := range names {
		if name == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(model string) {
			defer wg.Done()
			defer func() { <-sem }()
			caps := FetchCapabilitiesContext(ctx, baseURL, model)
			if !caps.ToolsKnown {
				return
			}
			info := modelconfig.ModelInfo{
				Name:          model,
				ContextWindow: caps.ContextLength,
				SupportsTools: caps.SupportsTools,
			}
			info.InferCapabilities()
			mu.Lock()
			out = append(out, info)
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return out
}
