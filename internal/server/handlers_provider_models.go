package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

const providerModelsFetchTimeout = 5 * time.Second

// providerModel is the normalized model entry returned to the frontend.
type providerModel struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	ContextLength     int      `json:"context_length,omitempty"`
	PricingPrompt     float64  `json:"pricing_prompt,omitempty"`
	PricingCompletion float64  `json:"pricing_completion,omitempty"`
	Provider          string   `json:"provider,omitempty"` // sub-provider (OpenRouter only)
	CreatedAt         string   `json:"created_at,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type providerModelsCache struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Models    []providerModel `json:"models"`
}

func providerModelsCachePath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".huginn", "cache", fmt.Sprintf("provider-%s-models.json", name))
}

func readProviderModelsCache(name string) ([]providerModel, error) {
	data, err := os.ReadFile(providerModelsCachePath(name))
	if err != nil {
		return nil, err
	}
	var c providerModelsCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return c.Models, nil
}

func writeProviderModelsCache(name string, models []providerModel) {
	c := providerModelsCache{FetchedAt: time.Now(), Models: models}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	path := providerModelsCachePath(name)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
}

// handleProviderModels serves GET /api/v1/providers/{provider}/models.
// Uses network-first with local cache fallback (same pattern as the model catalog).
// OpenRouter is always available (public API); OpenAI/Anthropic require a saved key.
func (s *Server) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")

	var (
		models   []providerModel
		fetchErr error
	)

	switch provider {
	case "openrouter":
		endpoint := "https://openrouter.ai/api/v1"
		if s.cfg.Backend.Provider == "openrouter" && s.cfg.Backend.Endpoint != "" {
			endpoint = strings.TrimSuffix(s.cfg.Backend.Endpoint, "/")
		}
		apiKey := ""
		if s.cfg.Backend.Provider == "openrouter" {
			apiKey = func() string { k, _ := backend.ResolveAPIKey(s.cfg.Backend.APIKey); return k }()
		}
		models, fetchErr = fetchOpenRouterModels(endpoint, apiKey)

	case "openai":
		if s.cfg.Backend.Provider != "openai" {
			jsonOK(w, []providerModel{})
			return
		}
		apiKey := func() string { k, _ := backend.ResolveAPIKey(s.cfg.Backend.APIKey); return k }()
		if apiKey == "" {
			jsonOK(w, []providerModel{})
			return
		}
		endpoint := "https://api.openai.com/v1"
		if s.cfg.Backend.Endpoint != "" {
			endpoint = strings.TrimSuffix(s.cfg.Backend.Endpoint, "/")
		}
		models, fetchErr = fetchOpenAIModels(endpoint, apiKey)

	case "anthropic":
		if s.cfg.Backend.Provider != "anthropic" {
			jsonOK(w, []providerModel{})
			return
		}
		apiKey := func() string { k, _ := backend.ResolveAPIKey(s.cfg.Backend.APIKey); return k }()
		if apiKey == "" {
			jsonOK(w, []providerModel{})
			return
		}
		endpoint := "https://api.anthropic.com"
		if s.cfg.Backend.Endpoint != "" {
			endpoint = strings.TrimSuffix(s.cfg.Backend.Endpoint, "/")
		}
		models, fetchErr = fetchAnthropicModels(endpoint, apiKey)

	default:
		jsonError(w, http.StatusBadRequest, "unknown provider: "+provider)
		return
	}

	if fetchErr != nil {
		// For Anthropic, always fall back to the known model list so the UI
		// remains usable even when the API is unreachable or returns an auth
		// error (which can be transient, not necessarily a bad key).
		if provider == "anthropic" {
			jsonOK(w, anthropicKnownModels)
			return
		}
		cached, cacheErr := readProviderModelsCache(provider)
		if cacheErr != nil {
			jsonError(w, http.StatusBadGateway, "fetch models: "+fetchErr.Error())
			return
		}
		jsonOK(w, cached)
		return
	}

	writeProviderModelsCache(provider, models)
	jsonOK(w, models)
}

// ── OpenRouter ──────────────────────────────────────────────────────────────

func fetchOpenRouterModels(endpoint, apiKey string) ([]providerModel, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := (&http.Client{Timeout: providerModelsFetchTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch OpenRouter models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch OpenRouter models: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read OpenRouter response: %w", err)
	}

	var raw struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse OpenRouter response: %w", err)
	}

	models := make([]providerModel, 0, len(raw.Data))
	for _, m := range raw.Data {
		subProvider := ""
		if idx := strings.Index(m.ID, "/"); idx > 0 {
			subProvider = m.ID[:idx]
		}
		promptPrice, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
		completionPrice, _ := strconv.ParseFloat(m.Pricing.Completion, 64)
		models = append(models, providerModel{
			ID:                m.ID,
			Name:              m.Name,
			Description:       m.Description,
			ContextLength:     m.ContextLength,
			PricingPrompt:     promptPrice * 1_000_000,
			PricingCompletion: completionPrice * 1_000_000,
			Provider:          subProvider,
		})
	}
	return models, nil
}

// ── OpenAI ──────────────────────────────────────────────────────────────────

func fetchOpenAIModels(endpoint, apiKey string) ([]providerModel, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := (&http.Client{Timeout: providerModelsFetchTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch OpenAI models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid OpenAI API key")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch OpenAI models: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	var raw struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse OpenAI response: %w", err)
	}

	models := make([]providerModel, 0)
	for _, m := range raw.Data {
		if !isOpenAIChatModel(m.ID, m.OwnedBy) {
			continue
		}
		ctxLen, desc, tags := openAIModelMeta(m.ID)
		createdAt := ""
		if m.Created > 0 {
			createdAt = time.Unix(m.Created, 0).UTC().Format(time.RFC3339)
		}
		models = append(models, providerModel{
			ID:            m.ID,
			Name:          openAIDisplayName(m.ID),
			Description:   desc,
			ContextLength: ctxLen,
			Tags:          tags,
			CreatedAt:     createdAt,
		})
	}
	return models, nil
}

func isOpenAIChatModel(id, ownedBy string) bool {
	if ownedBy != "openai" && ownedBy != "system" {
		return false
	}
	lower := strings.ToLower(id)
	for _, skip := range []string{"embedding", "whisper", "tts", "dall-e", "moderation", "davinci-002", "babbage-002", "realtime", "audio"} {
		if strings.Contains(lower, skip) {
			return false
		}
	}
	return true
}

func openAIDisplayName(id string) string {
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "gpt-4o-mini"):
		return "GPT-4o Mini"
	case strings.HasPrefix(lower, "gpt-4o"), lower == "chatgpt-4o-latest":
		return "GPT-4o"
	case strings.HasPrefix(lower, "gpt-4-turbo"):
		return "GPT-4 Turbo"
	case strings.HasPrefix(lower, "gpt-4"):
		return "GPT-4"
	case strings.HasPrefix(lower, "gpt-3.5-turbo"):
		return "GPT-3.5 Turbo"
	case strings.HasPrefix(lower, "o3-mini"):
		return "o3 Mini"
	case strings.HasPrefix(lower, "o3"):
		return "o3"
	case strings.HasPrefix(lower, "o1-mini"):
		return "o1 Mini"
	case strings.HasPrefix(lower, "o1-preview"):
		return "o1 Preview"
	case strings.HasPrefix(lower, "o1"):
		return "o1"
	case strings.HasPrefix(lower, "o4"):
		return "o4"
	default:
		return id
	}
}

func openAIModelMeta(id string) (contextLength int, description string, tags []string) {
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "gpt-4o-mini"):
		return 128000, "Affordable and intelligent — fast, capable, cost-effective for most tasks", []string{"recommended", "fast"}
	case strings.HasPrefix(lower, "gpt-4o"), lower == "chatgpt-4o-latest":
		return 128000, "Flagship GPT-4o — multimodal with vision, fast and highly capable", []string{"recommended", "multimodal"}
	case strings.HasPrefix(lower, "o4"):
		return 200000, "Latest generation reasoning model — exceptional on hard problems", []string{"reasoning", "high-quality"}
	case strings.HasPrefix(lower, "o3-mini"):
		return 200000, "Efficient reasoning with adjustable thinking budget — great for coding and STEM", []string{"reasoning", "fast"}
	case strings.HasPrefix(lower, "o3"):
		return 200000, "High-intelligence reasoning model for the most complex multi-step problems", []string{"reasoning", "high-quality"}
	case strings.HasPrefix(lower, "o1-mini"):
		return 128000, "Fast, affordable reasoning for coding and math tasks", []string{"reasoning", "fast"}
	case strings.HasPrefix(lower, "o1"):
		return 200000, "Advanced reasoning model for complex problems requiring careful thinking", []string{"reasoning"}
	case strings.HasPrefix(lower, "gpt-4-turbo"):
		return 128000, "GPT-4 Turbo with vision — powerful, supports images and long contexts", []string{"multimodal"}
	case strings.HasPrefix(lower, "gpt-4"):
		return 8192, "Original GPT-4 — strong reasoning and precise instruction following", []string{}
	case strings.HasPrefix(lower, "gpt-3.5-turbo"):
		return 16385, "Fast and cost-effective for straightforward conversational tasks", []string{"fast", "lightweight"}
	default:
		return 0, "", nil
	}
}

// ── Anthropic ────────────────────────────────────────────────────────────────

// anthropicKnownModels are the current-generation models always shown regardless
// of what the /v1/models API returns (some keys/tiers may not list all models).
var anthropicKnownModels = []providerModel{
	{ID: "claude-opus-4-6", Name: "Claude Opus 4.6", Description: anthropicDescription("claude-opus-4-6"), ContextLength: 200000, Tags: anthropicTags("claude-opus-4-6")},
	{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Description: anthropicDescription("claude-sonnet-4-6"), ContextLength: 200000, Tags: anthropicTags("claude-sonnet-4-6")},
	{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Description: anthropicDescription("claude-haiku-4-5-20251001"), ContextLength: 200000, Tags: anthropicTags("claude-haiku-4-5-20251001")},
}

func fetchAnthropicModels(endpoint, apiKey string) ([]providerModel, error) {
	client := &http.Client{Timeout: providerModelsFetchTimeout}
	type rawModel struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
	}
	type page struct {
		Data    []rawModel `json:"data"`
		HasMore bool       `json:"has_more"`
		LastID  string     `json:"last_id"`
	}

	seen := make(map[string]bool)
	var apiModels []providerModel

	afterID := ""
	for {
		url := endpoint + "/v1/models?limit=100"
		if afterID != "" {
			url += "&after_id=" + afterID
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch Anthropic models: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("Anthropic API returned HTTP %d — key may be invalid or Anthropic is experiencing an auth service issue", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch Anthropic models: HTTP %d", resp.StatusCode)
		}
		if readErr != nil {
			return nil, readErr
		}

		var p page
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("parse Anthropic response: %w", err)
		}

		for _, m := range p.Data {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			apiModels = append(apiModels, providerModel{
				ID:            m.ID,
				Name:          m.DisplayName,
				Description:   anthropicDescription(m.ID),
				ContextLength: 200000,
				Tags:          anthropicTags(m.ID),
				CreatedAt:     m.CreatedAt,
			})
		}

		if !p.HasMore || p.LastID == "" {
			break
		}
		afterID = p.LastID
	}

	// Merge known models first, then append any additional models from the API
	// that aren't already covered. This ensures current-gen models always appear
	// even if the API key/tier doesn't list them.
	merged := make([]providerModel, 0, len(anthropicKnownModels)+len(apiModels))
	knownIDs := make(map[string]bool, len(anthropicKnownModels))
	for _, m := range anthropicKnownModels {
		merged = append(merged, m)
		knownIDs[m.ID] = true
	}
	for _, m := range apiModels {
		if !knownIDs[m.ID] {
			merged = append(merged, m)
		}
	}
	return merged, nil
}

func anthropicDescription(id string) string {
	lower := strings.ToLower(id)
	switch {
	case strings.Contains(lower, "opus"):
		return "Anthropic's most capable model — exceptional at complex analysis, research, and nuanced reasoning"
	case strings.Contains(lower, "sonnet"):
		return "Ideal balance of intelligence and speed — high quality for most production use cases"
	case strings.Contains(lower, "haiku"):
		return "Fast and compact — near-instant responses at the lowest cost per token"
	default:
		return ""
	}
}

func anthropicTags(id string) []string {
	lower := strings.ToLower(id)
	switch {
	case strings.Contains(lower, "opus"):
		return []string{"high-quality", "recommended"}
	case strings.Contains(lower, "sonnet"):
		return []string{"balanced", "recommended"}
	case strings.Contains(lower, "haiku"):
		return []string{"fast", "lightweight"}
	default:
		return nil
	}
}
