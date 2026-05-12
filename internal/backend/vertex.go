package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// VertexConfig is the input to NewVertexBackend.
type VertexConfig struct {
	Project         string
	Location        string
	CredentialsPath string
	Model           string
}

// VertexBackend talks to Google Vertex AI's Gemini and Anthropic publishers.
// It is safe for concurrent use.
type VertexBackend struct {
	project      string
	location     string
	model        string
	client       *http.Client
	tokenSrc     oauth2.TokenSource
	cb           *circuitBreaker
	hostOverride string // empty in production; set by tests via httptest.Server URL
}

// publisherForModel maps a model identifier to its Vertex publisher and
// returns an error for unsupported prefixes.
func publisherForModel(model string) (string, error) {
	switch {
	case strings.HasPrefix(model, "gemini-") || strings.Contains(model, "gemini"):
		return "google", nil
	case strings.HasPrefix(model, "claude-"):
		return "anthropic", nil
	default:
		return "", fmt.Errorf("vertex: unsupported model %q; expected gemini-* or claude-*", model)
	}
}

// methodForPublisher returns the Vertex REST method suffix.
func methodForPublisher(publisher string) string {
	switch publisher {
	case "google":
		return "streamGenerateContent?alt=sse"
	case "anthropic":
		return "streamRawPredict"
	default:
		return ""
	}
}

// ContextWindowForVertexModel returns the known context window for a Vertex
// model identifier. Unknown models receive a conservative 128k default.
// Values are sourced from the public Vertex AI / Anthropic / Google docs
// (https://cloud.google.com/vertex-ai/generative-ai/docs/learn/models and
// https://docs.claude.com/en/docs/about-claude/models/overview) since the
// v1beta1 publisher endpoint does not expose inputTokenLimit.
func ContextWindowForVertexModel(model string) int {
	lower := strings.ToLower(model)
	switch {
	// Gemini 1.5 — 2M for pro, 1M for flash.
	case strings.HasPrefix(lower, "gemini-1.5-pro"):
		return 2_097_152
	case strings.HasPrefix(lower, "gemini-1.5-flash"):
		return 1_048_576

	// Gemini 2.0 — 1M across the family.
	case strings.HasPrefix(lower, "gemini-2.0-"):
		return 1_048_576

	// Gemini 2.5 — TTS specializations are limited to 8K, image is 32K,
	// the chat models (pro / flash / flash-lite) and computer-use are 1M.
	case strings.HasPrefix(lower, "gemini-2.5-pro-tts"),
		strings.HasPrefix(lower, "gemini-2.5-flash-tts"):
		return 8_192
	case strings.HasPrefix(lower, "gemini-2.5-flash-image"):
		return 32_768
	case strings.HasPrefix(lower, "gemini-2.5-"):
		return 1_048_576

	// Gemini live / native-audio — 32K context.
	case strings.HasPrefix(lower, "gemini-live-"):
		return 32_768

	// Gemini 3.x preview families — 1M.
	case strings.HasPrefix(lower, "gemini-3-"),
		strings.HasPrefix(lower, "gemini-3.1-"):
		return 1_048_576

	// Generic gemini fallback.
	case strings.HasPrefix(lower, "gemini-"):
		return 1_048_576

	// Anthropic — sonnet-4-5+ has a 1M-context beta variant on Vertex; the
	// default remains 200K for opus / haiku and for sonnet under the
	// non-beta endpoint.
	case strings.HasPrefix(lower, "claude-sonnet-4-5"),
		strings.HasPrefix(lower, "claude-sonnet-4-6"),
		strings.HasPrefix(lower, "claude-sonnet-4-7"):
		return 1_000_000
	case strings.HasPrefix(lower, "claude-"):
		return 200_000

	default:
		return 128_000
	}
}

// TagsForVertexModel returns the descriptive tags shown next to a model in
// the picker (fast / recommended / preview / etc.). Derived from the model
// id since the publisher catalog doesn't expose this either.
func TagsForVertexModel(id string) []string {
	lower := strings.ToLower(id)
	var tags []string

	// Capability tier.
	switch {
	case strings.Contains(lower, "opus"), strings.Contains(lower, "-pro") && !strings.Contains(lower, "preview"):
		tags = append(tags, "high-quality")
	case strings.Contains(lower, "haiku"), strings.Contains(lower, "flash-lite"):
		tags = append(tags, "fast", "lightweight")
	case strings.Contains(lower, "flash"):
		tags = append(tags, "fast")
	case strings.Contains(lower, "sonnet"):
		tags = append(tags, "balanced")
	}

	// Modality / specialization.
	switch {
	case strings.Contains(lower, "-tts"):
		tags = append(tags, "tts")
	case strings.Contains(lower, "-audio"), strings.Contains(lower, "-live"):
		tags = append(tags, "audio")
	case strings.Contains(lower, "-image"):
		tags = append(tags, "image")
	case strings.Contains(lower, "computer-use"):
		tags = append(tags, "computer-use")
	case strings.Contains(lower, "embedding"):
		tags = append(tags, "embedding")
	}

	// Recommended flagships.
	switch lower {
	case "gemini-2.5-pro", "claude-sonnet-4-5", "claude-opus-4-7":
		tags = append(tags, "recommended")
	}

	return tags
}

// DisplayNameForVertexModel turns a Vertex model id into a human-readable
// display name (e.g. "gemini-2.5-pro" → "Gemini 2.5 Pro",
// "claude-sonnet-4-5" → "Claude Sonnet 4.5",
// "gemini-3.1-flash-image-preview" → "Gemini 3.1 Flash Image Preview").
//
// The transform splits the id on "-", merges adjacent numeric segments
// separated by an extra "-" back together (so "4-5" becomes "4.5" for
// claude versions), and title-cases each segment with special handling
// for acronyms (TTS) and lowercase tokens (preview, exp).
func DisplayNameForVertexModel(id string) string {
	if id == "" {
		return ""
	}
	parts := strings.Split(id, "-")
	if len(parts) == 0 {
		return id
	}

	// Merge "<int>-<int>" into "<int>.<int>" for Claude version numbers
	// (claude-sonnet-4-5 → Claude Sonnet 4.5).
	merged := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		if i+1 < len(parts) && isAllDigits(parts[i]) && isAllDigits(parts[i+1]) {
			merged = append(merged, parts[i]+"."+parts[i+1])
			i++
			continue
		}
		merged = append(merged, parts[i])
	}

	// Acronyms / special tokens kept as-is or lowercased.
	upperKeep := map[string]string{
		"tts":   "TTS",
		"er":    "ER", // gemini-robotics-er-1.5-preview
		"gpt":   "GPT",
		"o1":    "o1",
		"o3":    "o3",
		"o4":    "o4",
		"ai":    "AI",
	}
	for i, p := range merged {
		lower := strings.ToLower(p)
		if v, ok := upperKeep[lower]; ok {
			merged[i] = v
			continue
		}
		// Already-versioned segments like "2.5" or "4.5" — keep as-is.
		if strings.ContainsAny(p, ".0123456789") && !hasLetters(p) {
			merged[i] = p
			continue
		}
		// Title-case: first rune upper, rest lower. Preserve digits/dots.
		if p == "" {
			continue
		}
		runes := []rune(strings.ToLower(p))
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		merged[i] = string(runes)
	}
	return strings.Join(merged, " ")
}

// DescriptionForVertexModel returns a short, human-readable description for a
// Vertex model id derived from family + capability + modality. Returns an
// empty string for ids that don't match a known pattern so callers can fall
// back to a curated list.
func DescriptionForVertexModel(id string) string {
	lower := strings.ToLower(id)

	// Modality specializations first — most specific.
	switch {
	case strings.Contains(lower, "-tts"):
		return "Text-to-speech variant of " + DisplayNameForVertexModel(strings.TrimSuffix(id, "-tts")) + "."
	case strings.Contains(lower, "native-audio"), strings.HasPrefix(lower, "gemini-live"):
		return "Real-time audio variant of Gemini Live for streaming voice interactions."
	case strings.Contains(lower, "-image"):
		return "Image-generation Gemini variant (Nano Banana)."
	case strings.Contains(lower, "computer-use"):
		return "Gemini agent specialized for computer-use / browser automation tasks."
	case strings.Contains(lower, "embedding"):
		return "Text embedding model for vector / similarity search workloads."
	case strings.Contains(lower, "robotics-er"):
		return "Gemini Robotics ER — embodied-reasoning model for robotics."
	}

	// Anthropic families.
	switch {
	case strings.HasPrefix(lower, "claude-opus"):
		return "Anthropic's flagship Claude Opus — highest intelligence, slower and more expensive than Sonnet."
	case strings.HasPrefix(lower, "claude-sonnet"):
		return "Anthropic Claude Sonnet — balanced quality and speed, the default for most chat workloads."
	case strings.HasPrefix(lower, "claude-haiku"):
		return "Anthropic Claude Haiku — fast and lightweight for low-latency tasks."
	case strings.HasPrefix(lower, "claude-"):
		return "Anthropic Claude served via Google Vertex AI."
	}

	// Gemini families.
	switch {
	case strings.Contains(lower, "flash-lite"):
		return "Lightweight Gemini Flash variant for the lowest latency and cost."
	case strings.HasPrefix(lower, "gemini-1.5-pro"):
		return "Gemini 1.5 Pro — previous-generation flagship with 2M-token context."
	case strings.HasPrefix(lower, "gemini-1.5-flash"):
		return "Gemini 1.5 Flash — fast, low-cost previous-generation model."
	case strings.HasPrefix(lower, "gemini-2.0"):
		return "Gemini 2.0 — fast multimodal model with 1M-token context."
	case strings.HasPrefix(lower, "gemini-2.5-pro"):
		return "Gemini 2.5 Pro — Google's flagship reasoning model with 1M-token context."
	case strings.HasPrefix(lower, "gemini-2.5-flash"):
		return "Gemini 2.5 Flash — fast, cost-efficient Gemini with 1M-token context."
	case strings.HasPrefix(lower, "gemini-3"):
		return "Gemini 3 — next-generation Gemini family (preview)."
	case strings.HasPrefix(lower, "gemini-"):
		return "Google Gemini model served via Vertex AI."
	}
	return ""
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func hasLetters(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

const vertexScope = "https://www.googleapis.com/auth/cloud-platform"

// ResolveVertexCredentials resolves a Vertex credentials reference to the
// raw service-account JSON bytes. The reference follows the same patterns
// as ResolveAPIKey so vertex aligns with other providers:
//
//   - "" (empty)               → fall back to GOOGLE_APPLICATION_CREDENTIALS
//     env var (gcloud convention) and read that file.
//   - "$ENV_VAR"               → resolve env var, treat its value as a path,
//     read that file.
//   - "keyring:<svc>:<user>"   → read from OS keyring; the stored value is
//     the JSON content itself, not a path.
//   - any other string         → treat as a literal filesystem path and
//     read that file.
//
// Returns the JSON bytes ready for google.JWTConfigFromJSON.
func ResolveVertexCredentials(ref string) ([]byte, error) {
	if ref == "" {
		path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		if path == "" {
			return nil, fmt.Errorf("vertex credentials: no reference configured (set BackendConfig.CredentialsPath or GOOGLE_APPLICATION_CREDENTIALS)")
		}
		return os.ReadFile(path)
	}
	if strings.HasPrefix(ref, "$") {
		envVar := strings.TrimPrefix(ref, "$")
		path := os.Getenv(envVar)
		if path == "" {
			return nil, fmt.Errorf("vertex credentials: environment variable %q is empty or unset", envVar)
		}
		return os.ReadFile(path)
	}
	if strings.HasPrefix(ref, "keyring:") {
		// Reuse the same keyring resolver as ResolveAPIKey — the keyring
		// stores the JSON content directly (multi-line strings are fine).
		secret, err := ResolveAPIKey(ref)
		if err != nil {
			return nil, fmt.Errorf("vertex credentials: %w", err)
		}
		return []byte(secret), nil
	}
	return os.ReadFile(ref)
}

// NewVertexBackend constructs a VertexBackend from cfg, resolving the
// credentials reference at cfg.CredentialsPath via ResolveVertexCredentials
// and minting an OAuth2 TokenSource via google.JWTConfigFromJSON.
func NewVertexBackend(ctx context.Context, cfg VertexConfig) (*VertexBackend, error) {
	if cfg.Project == "" {
		return nil, fmt.Errorf("vertex: project is required (set BackendConfig.Project or GOOGLE_CLOUD_PROJECT)")
	}
	location := cfg.Location
	if location == "" {
		location = "us-central1"
	}

	data, err := ResolveVertexCredentials(cfg.CredentialsPath)
	if err != nil {
		return nil, fmt.Errorf("vertex: %w", err)
	}
	jwtCfg, err := google.JWTConfigFromJSON(data, vertexScope)
	if err != nil {
		return nil, fmt.Errorf("vertex: parse credentials: %w", err)
	}

	ts := oauth2.ReuseTokenSource(nil, jwtCfg.TokenSource(ctx))

	return &VertexBackend{
		project:  cfg.Project,
		location: location,
		model:    cfg.Model,
		client:   &http.Client{Timeout: 0, Transport: streamingTransport()},
		tokenSrc: ts,
		cb:       newCircuitBreaker(),
	}, nil
}

// endpoint builds the full Vertex REST URL for a given publisher / model / method.
func (b *VertexBackend) endpoint(publisher, model, method string) string {
	host := b.hostOverride
	if host == "" {
		host = fmt.Sprintf("https://%s-aiplatform.googleapis.com", b.location)
	}
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/%s/models/%s:%s",
		host, b.project, b.location, publisher, model, method)
}

// healthEndpoint builds a cheap GET URL that verifies project + location +
// auth in a single round-trip. The locations resource always exists for a
// valid project / region combination and returns a small JSON body.
func (b *VertexBackend) healthEndpoint() string {
	host := b.hostOverride
	if host == "" {
		host = fmt.Sprintf("https://%s-aiplatform.googleapis.com", b.location)
	}
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s",
		host, b.project, b.location)
}

// Health verifies project, location, and credentials by fetching the
// project's location resource. A 2xx response indicates the credentials are
// valid, the project exists, and the region is enabled for Vertex AI.
func (b *VertexBackend) Health(ctx context.Context) error {
	tok, err := b.tokenSrc.Token()
	if err != nil {
		return fmt.Errorf("vertex health: token: %w", err)
	}
	hc := *b.client
	hc.Timeout = 5 * time.Second
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.healthEndpoint(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("vertex health: %w", err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("vertex health: authentication failed (401)")
	case http.StatusForbidden:
		return fmt.Errorf("vertex health: forbidden (403)")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("vertex health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// ChatCompletion dispatches to the appropriate Vertex AI publisher (Gemini or
// Anthropic) based on the model name, then streams and parses the SSE response.
// It shares the circuit-breaker and retry behaviour used by AnthropicBackend.
func (b *VertexBackend) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = b.model
	}
	publisher, err := publisherForModel(model)
	if err != nil {
		return nil, err
	}
	method := methodForPublisher(publisher)

	var body []byte
	switch publisher {
	case "google":
		body, err = buildGeminiBody(req)
	case "anthropic":
		body, err = buildAnthropicVertexBody(req)
	}
	if err != nil {
		return nil, fmt.Errorf("vertex: build request: %w", err)
	}

	if !b.cb.Allow() {
		return nil, ErrCircuitOpen
	}

	tok, err := b.tokenSrc.Token()
	if err != nil {
		b.cb.RecordFailure()
		return nil, fmt.Errorf("vertex: token: %w", err)
	}

	url := b.endpoint(publisher, model, method)
	var lastErr error
	for attempt := 0; attempt <= chatMaxRetries; attempt++ {
		if attempt > 0 {
			delay := chatRetryBase * (1 << uint(attempt-1))
			jitter := time.Duration(rand.Int63n(int64(delay / 4)))
			if rand.Intn(2) == 0 {
				jitter = -jitter
			}
			select {
			case <-time.After(delay + jitter):
			case <-ctx.Done():
				return nil, fmt.Errorf("vertex: %w", ctx.Err())
			}
			slog.Warn("vertex: retrying after transient error", "attempt", attempt, "err", lastErr)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := b.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("vertex: %w", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				rl := &RateLimitError{Body: string(bytes.TrimSpace(errBody))}
				rl.RetryAfter = parseRetryAfter(resp.Header)
				return nil, rl
			}
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				if len(errBody) > 0 {
					return nil, fmt.Errorf("vertex: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(errBody))
				}
				return nil, fmt.Errorf("vertex: HTTP %d", resp.StatusCode)
			}
			lastErr = fmt.Errorf("vertex: HTTP %d", resp.StatusCode)
			continue
		}

		var result *ChatResponse
		switch publisher {
		case "google":
			result, err = parseGeminiSSE(ctx, resp, req, streamStallTimeout)
		case "anthropic":
			result, err = parseAnthropicSSE(ctx, resp, req, streamStallTimeout)
		}
		if err != nil {
			b.cb.RecordFailure()
			return nil, err
		}
		b.cb.RecordSuccess()
		return result, nil
	}
	b.cb.RecordFailure()
	return nil, lastErr
}

// Shutdown is a no-op for VertexBackend (we don't own the service).
func (b *VertexBackend) Shutdown(_ context.Context) error { return nil }

// ContextWindow returns the model's context window in tokens.
func (b *VertexBackend) ContextWindow() int {
	return ContextWindowForVertexModel(b.model)
}

// BackendStatus implements StatusReporter.
func (b *VertexBackend) BackendStatus() string {
	if b.cb == nil {
		return "closed"
	}
	return b.cb.State()
}

// Gemini request schema (subset Huginn uses).
type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	Tools             []geminiTool            `json:"tools,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  ToolParameters `json:"parameters"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
}

// buildGeminiBody translates a Huginn ChatRequest into the Vertex AI Gemini
// streamGenerateContent body. System messages collapse into
// systemInstruction.parts. Assistant turns with ToolCalls map to model-role
// functionCall parts. Tool result messages map to user-role functionResponse
// parts (Gemini convention). The body intentionally omits a `model` field —
// the model is encoded in the request URL.
func buildGeminiBody(req ChatRequest) ([]byte, error) {
	gReq := geminiRequest{
		Contents: make([]geminiContent, 0, len(req.Messages)),
	}
	var sysParts []geminiPart
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			sysParts = append(sysParts, geminiPart{Text: m.Content})
		case "user":
			gReq.Contents = append(gReq.Contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: m.Content}},
			})
		case "assistant":
			parts := make([]geminiPart, 0)
			if m.Content != "" {
				parts = append(parts, geminiPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Function.Arguments
				if args == nil {
					args = map[string]any{}
				}
				parts = append(parts, geminiPart{FunctionCall: &geminiFunctionCall{
					Name: tc.Function.Name,
					Args: args,
				}})
			}
			gReq.Contents = append(gReq.Contents, geminiContent{Role: "model", Parts: parts})
		case "tool":
			var respMap map[string]any
			if m.Content != "" {
				if err := json.Unmarshal([]byte(m.Content), &respMap); err != nil {
					// Non-JSON tool results are wrapped so the structure is still valid.
					respMap = map[string]any{"result": m.Content}
				}
			} else {
				respMap = map[string]any{}
			}
			gReq.Contents = append(gReq.Contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{{FunctionResponse: &geminiFunctionResponse{
					Name:     m.ToolName,
					Response: respMap,
				}}},
			})
		}
	}
	if len(sysParts) > 0 {
		gReq.SystemInstruction = &geminiContent{Parts: sysParts}
	}

	for _, t := range req.Tools {
		gReq.Tools = append(gReq.Tools, geminiTool{
			FunctionDeclarations: []geminiFunctionDeclaration{{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			}},
		})
	}

	return json.Marshal(gReq)
}

type geminiStreamResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
}

// parseGeminiSSE consumes the streamGenerateContent SSE response, accumulating
// text into ChatResponse.Content and surfacing functionCall parts as ToolCalls.
// Gemini does not assign tool-call ids, so each call gets a synthesized id of
// the form "gemini-tool-N".
func parseGeminiSSE(ctx context.Context, resp *http.Response, req ChatRequest, stallTimeout time.Duration) (*ChatResponse, error) {
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	activityCh := make(chan struct{}, 1)
	go func() {
		timer := time.NewTimer(stallTimeout)
		defer timer.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-activityCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(stallTimeout)
			case <-timer.C:
				slog.Warn("vertex: gemini SSE idle timeout, aborting", "timeout", stallTimeout)
				streamCancel()
				resp.Body.Close()
				return
			}
		}
	}()

	result := &ChatResponse{}
	toolCallIdx := 0

	scanner := bufio.NewScanner(resp.Body)
	// Allow large data lines — Gemini can send big function-call argument blobs.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}
		select {
		case activityCh <- struct{}{}:
		default:
		}

		var gsr geminiStreamResponse
		if err := json.Unmarshal([]byte(data), &gsr); err != nil {
			slog.Warn("vertex: gemini SSE chunk parse failed", "err", err)
			continue
		}
		for _, cand := range gsr.Candidates {
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					result.Content += part.Text
					if req.OnEvent != nil {
						req.OnEvent(StreamEvent{Type: StreamText, Content: part.Text})
					}
					if req.OnToken != nil {
						req.OnToken(part.Text)
					}
				}
				if part.FunctionCall != nil {
					toolCallIdx++
					args := part.FunctionCall.Args
					if args == nil {
						args = map[string]any{}
					}
					result.ToolCalls = append(result.ToolCalls, ToolCall{
						ID: fmt.Sprintf("gemini-tool-%d", toolCallIdx),
						Function: ToolCallFunction{
							Name:      part.FunctionCall.Name,
							Arguments: args,
						},
					})
				}
			}
			if cand.FinishReason != "" {
				result.DoneReason = cand.FinishReason
			}
		}
		if gsr.UsageMetadata != nil {
			result.PromptTokens = gsr.UsageMetadata.PromptTokenCount
			result.CompletionTokens = gsr.UsageMetadata.CandidatesTokenCount
		}
	}

	if req.OnEvent != nil {
		req.OnEvent(StreamEvent{Type: StreamDone})
	}

	if err := scanner.Err(); err != nil {
		if streamCtx.Err() != nil {
			return nil, fmt.Errorf("gemini SSE aborted: idle timeout after %s", stallTimeout)
		}
		return nil, fmt.Errorf("reading gemini SSE: %w", err)
	}
	if streamCtx.Err() != nil {
		return nil, fmt.Errorf("gemini SSE aborted: idle timeout after %s", stallTimeout)
	}
	return result, nil
}

// anthropicVertexWrapper is the body shape for Vertex AI's anthropic publisher.
// Differs from the direct Anthropic Messages API in two ways: (1) the model
// field is omitted (the model is in the URL), and (2) anthropic_version is
// set to vertex-2023-10-16.
type anthropicVertexWrapper struct {
	AnthropicVersion string             `json:"anthropic_version"`
	MaxTokens        int                `json:"max_tokens"`
	System           string             `json:"system,omitempty"`
	Messages         []anthropicMessage `json:"messages"`
	Tools            []anthropicTool    `json:"tools,omitempty"`
	Stream           bool               `json:"stream"`
}

func buildAnthropicVertexBody(req ChatRequest) ([]byte, error) {
	sys, msgs, tools, err := anthropicMessagesAndTools(req)
	if err != nil {
		return nil, err
	}
	maxTok := modelconfig.MaxOutputTokensForModel(req.Model)
	wrap := anthropicVertexWrapper{
		AnthropicVersion: "vertex-2023-10-16",
		MaxTokens:        maxTok,
		System:           sys,
		Messages:         msgs,
		Tools:            tools,
		Stream:           true,
	}
	return json.Marshal(wrap)
}

// compile-time interface checks
var (
	_ Backend        = (*VertexBackend)(nil)
	_ StatusReporter = (*VertexBackend)(nil)
)

// VertexPublisherModel is one entry from the publisher-models listing.
type VertexPublisherModel struct {
	ID          string // bare model id (e.g. "gemini-2.5-pro")
	Publisher   string // "google" | "anthropic"
	LaunchStage string // "GA" | "PUBLIC_PREVIEW" | "EXPERIMENTAL" | ""
	Name        string // full resource name (publishers/.../models/...)
}

// ListVertexPublisherModels queries Vertex AI's v1beta1 publisher-models
// listing endpoint and returns the chat-capable gemini / claude models. The
// global endpoint (aiplatform.googleapis.com, no region prefix) is used so
// the catalogue reflects every model Vertex offers across regions — a
// region-specific listing in some regions (e.g. us-east5) returns only a
// small subset of what the user actually has access to in other regions.
//
// The caller's configured location still determines where chat-completion
// requests land; a model that's in this list but unavailable in the
// configured region surfaces as a clear HTTP error on the first
// ChatCompletion call.
//
// location is accepted for API parity (other Vertex helpers take it) but is
// intentionally unused here.
//
// credentialsRef follows the same patterns as ResolveVertexCredentials
// (literal path, "$ENV_VAR", "keyring:<svc>:<user>", or empty for the
// GOOGLE_APPLICATION_CREDENTIALS env fallback).
func ListVertexPublisherModels(ctx context.Context, project, location, credentialsRef string) ([]VertexPublisherModel, error) {
	_ = location // intentionally unused — global endpoint covers all regions
	if project == "" {
		return nil, fmt.Errorf("vertex models: project is required")
	}
	data, err := ResolveVertexCredentials(credentialsRef)
	if err != nil {
		return nil, fmt.Errorf("vertex models: %w", err)
	}
	jwtCfg, err := google.JWTConfigFromJSON(data, vertexScope)
	if err != nil {
		return nil, fmt.Errorf("vertex models: parse credentials: %w", err)
	}
	tok, err := jwtCfg.TokenSource(ctx).Token()
	if err != nil {
		return nil, fmt.Errorf("vertex models: token: %w", err)
	}

	host := "https://aiplatform.googleapis.com"
	client := &http.Client{Timeout: 10 * time.Second}

	var out []VertexPublisherModel
	for _, pub := range []string{"google", "anthropic"} {
		url := fmt.Sprintf("%s/v1beta1/publishers/%s/models", host, pub)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("vertex models: %s: %w", pub, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("vertex models: %s: HTTP %d: %s", pub, resp.StatusCode, body)
		}
		if readErr != nil {
			return nil, readErr
		}
		var doc struct {
			PublisherModels []struct {
				Name        string `json:"name"`
				LaunchStage string `json:"launchStage"`
			} `json:"publisherModels"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("vertex models: parse %s: %w", pub, err)
		}
		for _, m := range doc.PublisherModels {
			idx := strings.LastIndex(m.Name, "/")
			if idx < 0 {
				continue
			}
			id := m.Name[idx+1:]
			if !isVertexChatModel(pub, id) {
				continue
			}
			out = append(out, VertexPublisherModel{
				ID:          id,
				Publisher:   pub,
				LaunchStage: m.LaunchStage,
				Name:        m.Name,
			})
		}
	}
	return out, nil
}

// isVertexChatModel filters a publisher model id to the gemini-* and claude-*
// families. Other publisher products (imagen, embeddings, gemma open models)
// are excluded but all gemini / claude variants — including tts, audio, and
// live specializations — are kept so the user sees their full project access.
func isVertexChatModel(publisher, id string) bool {
	lower := strings.ToLower(id)
	switch publisher {
	case "google":
		return strings.HasPrefix(lower, "gemini-") || strings.HasPrefix(lower, "gemini")
	case "anthropic":
		return strings.HasPrefix(lower, "claude-")
	}
	return false
}
