package backend

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestVertex_PublisherForModel(t *testing.T) {
	tests := []struct {
		model     string
		publisher string
	}{
		{"gemini-2.5-pro", "google"},
		{"gemini-2.5-flash", "google"},
		{"gemini-1.5-pro", "google"},
		{"claude-opus-4", "anthropic"},
		{"claude-sonnet-4-5", "anthropic"},
		{"claude-haiku-4-5", "anthropic"},
	}
	for _, tc := range tests {
		got, err := publisherForModel(tc.model)
		if err != nil {
			t.Errorf("publisherForModel(%q) err = %v", tc.model, err)
			continue
		}
		if got != tc.publisher {
			t.Errorf("publisherForModel(%q) = %q, want %q", tc.model, got, tc.publisher)
		}
	}
}

func TestVertex_PublisherForModel_Unknown(t *testing.T) {
	if _, err := publisherForModel("gpt-4o"); err == nil {
		t.Error("expected error for unknown model prefix")
	}
}

func TestVertex_ContextWindow(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"gemini-2.5-pro", 1_048_576},
		{"gemini-2.5-flash", 1_048_576},
		{"gemini-1.5-pro", 2_097_152},
		{"gemini-1.5-flash", 1_048_576},
		{"claude-opus-4", 200_000},
		{"claude-sonnet-4-5", 1_000_000},
		{"claude-haiku-4-5", 200_000},
		{"future-unknown-model", 128_000},
	}
	for _, tc := range tests {
		got := ContextWindowForVertexModel(tc.model)
		if got != tc.want {
			t.Errorf("ContextWindowForVertexModel(%q) = %d, want %d", tc.model, got, tc.want)
		}
	}
}

// writeFakeServiceAccountKey generates a fresh 2048-bit RSA key, writes a
// minimal but syntactically valid service-account JSON to a temp file, and
// returns its path. The key satisfies google.JWTConfigFromJSON parsing in
// tests — no network calls are made with it.
func writeFakeServiceAccountKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	pemEscaped := strings.ReplaceAll(string(pemBlock), "\n", "\\n")
	jsonBody := `{
		"type": "service_account",
		"project_id": "test",
		"private_key_id": "abc",
		"private_key": "` + pemEscaped + `",
		"client_email": "test@test.iam.gserviceaccount.com",
		"client_id": "0",
		"auth_uri": "https://accounts.google.com/o/oauth2/auth",
		"token_uri": "https://oauth2.googleapis.com/token",
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
		"client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/test"
	}`
	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, []byte(jsonBody), 0600); err != nil {
		t.Fatalf("write sa.json: %v", err)
	}
	return path
}

func TestVertex_NewVertexBackend_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  VertexConfig
		want string
	}{
		{"missing project", VertexConfig{Location: "us-central1", CredentialsPath: "/tmp/x", Model: "gemini-2.5-pro"}, "project"},
		{"missing creds", VertexConfig{Project: "p", Location: "us-central1", Model: "gemini-2.5-pro"}, "credentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear the env fallback so tests are deterministic regardless of
			// the developer's shell environment.
			t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
			_, err := NewVertexBackend(context.Background(), tc.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestVertex_NewVertexBackend_DefaultLocation(t *testing.T) {
	tmpKey := writeFakeServiceAccountKey(t)
	b, err := NewVertexBackend(context.Background(), VertexConfig{
		Project:         "test-project",
		CredentialsPath: tmpKey,
		Model:           "gemini-2.5-pro",
	})
	if err != nil {
		t.Fatalf("NewVertexBackend: %v", err)
	}
	if b.location != "us-central1" {
		t.Errorf("default location = %q, want us-central1", b.location)
	}
}

// newTestVertexBackend returns a VertexBackend whose REST calls target the
// given httptest server instead of the real Vertex AI host. The token source
// returns a constant value so tests never hit Google's OAuth2 endpoint.
func newTestVertexBackend(t *testing.T, srv *httptest.Server, project, location, model string) *VertexBackend {
	t.Helper()
	return &VertexBackend{
		project:      project,
		location:     location,
		model:        model,
		client:       srv.Client(),
		tokenSrc:     oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"}),
		cb:           newCircuitBreaker(),
		hostOverride: srv.URL,
	}
}

func TestVertex_Health_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/test/locations/us-central1" {
			t.Errorf("path = %q", r.URL.Path)
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("Authorization header = %q, want Bearer prefix", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/test/locations/us-central1","locationId":"us-central1"}`))
	}))
	defer srv.Close()

	b := newTestVertexBackend(t, srv, "test", "us-central1", "gemini-2.5-pro")
	if err := b.Health(context.Background()); err != nil {
		t.Fatalf("Health = %v, want nil", err)
	}
}

func TestVertex_Health_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauth", http.StatusUnauthorized)
	}))
	defer srv.Close()

	b := newTestVertexBackend(t, srv, "test", "us-central1", "gemini-2.5-pro")
	err := b.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("Health err = %v, want 401", err)
	}
}

func TestVertex_BuildGeminiBody_Basic(t *testing.T) {
	req := ChatRequest{
		Model: "gemini-2.5-pro",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello."},
		},
	}
	body, err := buildGeminiBody(req)
	if err != nil {
		t.Fatalf("buildGeminiBody: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	si, _ := got["systemInstruction"].(map[string]any)
	parts, _ := si["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("systemInstruction.parts len = %d", len(parts))
	}
	if text := parts[0].(map[string]any)["text"]; text != "You are helpful." {
		t.Errorf("system text = %v", text)
	}
	contents, _ := got["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents len = %d", len(contents))
	}
	c0 := contents[0].(map[string]any)
	if c0["role"] != "user" {
		t.Errorf("contents[0].role = %v", c0["role"])
	}
	if _, has := got["model"]; has {
		t.Error("body should not contain model field")
	}
}

func TestVertex_BuildGeminiBody_AssistantMapped(t *testing.T) {
	req := ChatRequest{
		Model: "gemini-2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello back"},
		},
	}
	body, _ := buildGeminiBody(req)
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	contents := got["contents"].([]any)
	if contents[1].(map[string]any)["role"] != "model" {
		t.Errorf("assistant should map to role=model, got %v", contents[1])
	}
}

func TestVertex_BuildGeminiBody_ToolResult(t *testing.T) {
	req := ChatRequest{
		Model: "gemini-2.5-pro",
		Messages: []Message{
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID: "call-1",
				Function: ToolCallFunction{Name: "list_files", Arguments: map[string]any{"path": "/"}},
			}}},
			{Role: "tool", ToolCallID: "call-1", ToolName: "list_files", Content: `{"files":["a.txt"]}`},
		},
	}
	body, _ := buildGeminiBody(req)
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	contents := got["contents"].([]any)
	mTurn := contents[1].(map[string]any)
	if mTurn["role"] != "model" {
		t.Errorf("assistant role = %v", mTurn["role"])
	}
	mParts := mTurn["parts"].([]any)
	fc := mParts[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "list_files" {
		t.Errorf("functionCall.name = %v", fc["name"])
	}
	tTurn := contents[2].(map[string]any)
	if tTurn["role"] != "user" {
		t.Errorf("tool result role = %v", tTurn["role"])
	}
	tParts := tTurn["parts"].([]any)
	fr := tParts[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "list_files" {
		t.Errorf("functionResponse.name = %v", fr["name"])
	}
}

func TestVertex_BuildGeminiBody_Tools(t *testing.T) {
	req := ChatRequest{
		Model: "gemini-2.5-pro",
		Messages: []Message{{Role: "user", Content: "x"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolProperty{"city": {Type: "string"}}, Required: []string{"city"}},
			},
		}},
	}
	body, _ := buildGeminiBody(req)
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	tools := got["tools"].([]any)
	fds := tools[0].(map[string]any)["functionDeclarations"].([]any)
	d0 := fds[0].(map[string]any)
	if d0["name"] != "get_weather" {
		t.Errorf("tool name = %v", d0["name"])
	}
	if _, ok := d0["parameters"]; !ok {
		t.Error("tool missing parameters")
	}
}

func TestVertex_ParseGeminiSSE_Text(t *testing.T) {
	stream := "" +
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}` + "\n\n" +
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]}}]}` + "\n\n" +
		`data: {"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var collected strings.Builder
	result, err := parseGeminiSSE(context.Background(), resp, ChatRequest{
		OnEvent: func(ev StreamEvent) {
			if ev.Type == StreamText {
				collected.WriteString(ev.Content)
			}
		},
	}, 60*time.Second)
	if err != nil {
		t.Fatalf("parseGeminiSSE: %v", err)
	}
	if result.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello world")
	}
	if result.DoneReason != "STOP" {
		t.Errorf("DoneReason = %q", result.DoneReason)
	}
	if collected.String() != "Hello world" {
		t.Errorf("OnEvent collected = %q", collected.String())
	}
}

func TestVertex_ParseGeminiSSE_FunctionCall(t *testing.T) {
	stream := "" +
		`data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}]}}]}` + "\n\n" +
		`data: {"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(stream))
	}))
	defer srv.Close()

	resp, _ := http.Get(srv.URL)
	result, err := parseGeminiSSE(context.Background(), resp, ChatRequest{}, 60*time.Second)
	if err != nil {
		t.Fatalf("parseGeminiSSE: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("name = %q", tc.Function.Name)
	}
	if tc.Function.Arguments["city"] != "Paris" {
		t.Errorf("args = %v", tc.Function.Arguments)
	}
}

func TestVertex_BuildAnthropicVertexBody(t *testing.T) {
	req := ChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "system", Content: "Be brief."},
			{Role: "user", Content: "Hi."},
		},
	}
	body, err := buildAnthropicVertexBody(req)
	if err != nil {
		t.Fatalf("buildAnthropicVertexBody: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["anthropic_version"] != "vertex-2023-10-16" {
		t.Errorf("anthropic_version = %v", got["anthropic_version"])
	}
	if _, has := got["model"]; has {
		t.Error("body must not contain `model` (it's in the URL for Vertex)")
	}
	if got["system"] != "Be brief." {
		t.Errorf("system = %v", got["system"])
	}
	msgs := got["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "user" {
		t.Errorf("msg[0].role = %v", msgs[0])
	}
}

func TestVertex_ChatCompletion_Gemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/v1/projects/test/locations/us-central1/publishers/google/models/gemini-2.5-pro:streamGenerateContent"
		if !strings.HasPrefix(r.URL.Path, wantPath) {
			t.Errorf("path = %q, want prefix %q", r.URL.Path, wantPath)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("alt query = %q", r.URL.Query().Get("alt"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}]}` + "\n\n"))
	}))
	defer srv.Close()

	b := newTestVertexBackend(t, srv, "test", "us-central1", "gemini-2.5-pro")
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "gemini-2.5-pro",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("Content = %q", resp.Content)
	}
}

func TestVertex_ChatCompletion_AnthropicOnVertex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/v1/projects/test/locations/us-central1/publishers/anthropic/models/claude-sonnet-4-5:streamRawPredict"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(bodyBytes, &body)
		if body["anthropic_version"] != "vertex-2023-10-16" {
			t.Errorf("anthropic_version = %v", body["anthropic_version"])
		}
		if _, has := body["model"]; has {
			t.Error("body must not contain model field")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n" +
				"event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
				"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
				"event: message_stop\ndata: {}\n\n"))
	}))
	defer srv.Close()

	b := newTestVertexBackend(t, srv, "test", "us-central1", "claude-sonnet-4-5")
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.DoneReason != "end_turn" {
		t.Errorf("DoneReason = %q", resp.DoneReason)
	}
}
