package oneshot

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/tools"
)

// fakeBackend emits scripted ChatCompletion responses. The first response is
// a bash tool_call; the second is a final assistant answer.
type fakeBackend struct {
	mu        sync.Mutex
	responses []*backend.ChatResponse
	requests  []backend.ChatRequest
	call      int
}

func (f *fakeBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	// --no-tools sends no schemas; answer in one shot instead of emitting a tool_call.
	if len(req.Tools) == 0 {
		resp := &backend.ChatResponse{Content: "The hostname is testhost.", DoneReason: "stop"}
		if req.OnToken != nil {
			req.OnToken(resp.Content)
		}
		return resp, nil
	}
	idx := f.call
	f.call++
	var resp *backend.ChatResponse
	if idx < len(f.responses) {
		resp = f.responses[idx]
	} else {
		resp = &backend.ChatResponse{Content: "done", DoneReason: "stop"}
	}
	if req.OnToken != nil && resp != nil && resp.Content != "" {
		req.OnToken(resp.Content)
	}
	return resp, nil
}

func (f *fakeBackend) Health(_ context.Context) error   { return nil }
func (f *fakeBackend) Shutdown(_ context.Context) error { return nil }
func (f *fakeBackend) ContextWindow() int               { return 8192 }

func (f *fakeBackend) lastTools() []backend.Tool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return nil
	}
	return f.requests[0].Tools
}

func (f *fakeBackend) lastModel() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return ""
	}
	return f.requests[0].Model
}

type stubTool struct {
	name   string
	output string
}

func (t *stubTool) Name() string                      { return t.name }
func (t *stubTool) Description() string               { return t.name }
func (t *stubTool) Permission() tools.PermissionLevel { return tools.PermExec }
func (t *stubTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *stubTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: t.output}
}

func bashThenAnswerBackend() *fakeBackend {
	return &fakeBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{{
					ID: "call_bash_1",
					Function: backend.ToolCallFunction{
						Name:      "bash",
						Arguments: map[string]any{"command": "hostname"},
					},
				}},
			},
			{Content: "The hostname is testhost.", DoneReason: "stop"},
		},
	}
}

func steveRegistry() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:       "Steve",
		ModelID:    "qwen3.6:35b",
		LocalTools: []string{"*"},
	})
	return reg
}

func bashToolReg() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "bash", output: "testhost\n"})
	reg.TagTools([]string{"bash"}, "builtin")
	return reg
}

func TestRun_JSON_ToolsCalledNonEmpty(t *testing.T) {
	b := bashThenAnswerBackend()
	res, err := Run(context.Background(), Config{
		Prompt:          "Use bash to run hostname",
		AgentName:       "Steve",
		SkipPermissions: true,
		Backend:         b,
		Registry:        steveRegistry(),
		Tools:           bashToolReg(),
		Models:          modelconfig.DefaultModels(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.AgentOutput != "The hostname is testhost." {
		t.Errorf("agentOutput = %q", res.AgentOutput)
	}
	if len(res.ToolsCalled) == 0 {
		t.Fatal("toolsCalled is empty; want at least one bash call")
	}
	tc := res.ToolsCalled[0]
	if tc.Name != "bash" {
		t.Errorf("toolsCalled[0].name = %q, want bash", tc.Name)
	}
	if cmd, _ := tc.Args["command"].(string); cmd != "hostname" {
		t.Errorf("toolsCalled[0].args.command = %v, want hostname", tc.Args["command"])
	}
	if !strings.Contains(tc.Result, "testhost") {
		t.Errorf("toolsCalled[0].result = %q, want testhost", tc.Result)
	}

	var buf bytes.Buffer
	if err := WriteResult(&buf, io.Discard, res, true, false); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	var parsed Result
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json: %v\nraw: %s", err, buf.String())
	}
	if parsed.AgentOutput == "" {
		t.Error("JSON agentOutput is empty")
	}
	if len(parsed.ToolsCalled) == 0 {
		t.Error("JSON toolsCalled is empty")
	}
}

func TestRun_NoTools_ToolsCalledEmpty(t *testing.T) {
	b := bashThenAnswerBackend()
	res, err := Run(context.Background(), Config{
		Prompt:    "Use bash to run hostname",
		AgentName: "Steve",
		NoTools:   true,
		Backend:   b,
		Registry:  steveRegistry(),
		Tools:     bashToolReg(),
		Models:    modelconfig.DefaultModels(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.ToolsCalled) != 0 {
		t.Fatalf("toolsCalled = %#v, want empty under --no-tools", res.ToolsCalled)
	}
	if res.AgentOutput == "" {
		t.Error("agentOutput should still be set under --no-tools")
	}
	if len(b.lastTools()) != 0 {
		t.Errorf("--no-tools still sent tool schemas: %v", b.lastTools())
	}

	var buf bytes.Buffer
	if err := WriteResult(&buf, io.Discard, res, true, false); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	var parsed Result
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json: %v", err)
	}
	if parsed.ToolsCalled == nil {
		t.Fatal("JSON toolsCalled should be [] not omitted")
	}
	if len(parsed.ToolsCalled) != 0 {
		t.Errorf("JSON toolsCalled = %#v, want []", parsed.ToolsCalled)
	}
}

func TestRun_UnknownAgent(t *testing.T) {
	_, err := Run(context.Background(), Config{
		Prompt:    "hello",
		AgentName: "Nobody",
		Backend:   bashThenAnswerBackend(),
		Registry:  steveRegistry(),
		Models:    modelconfig.DefaultModels(),
	})
	if err == nil {
		t.Fatal("expected unknown-agent error")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("error = %v, want unknown agent", err)
	}
	if !strings.Contains(err.Error(), "Steve") {
		t.Errorf("error should list available agents, got %v", err)
	}
}

func TestRun_SwapModel(t *testing.T) {
	b := bashThenAnswerBackend()
	_, err := Run(context.Background(), Config{
		Prompt:          "hello",
		AgentName:       "Steve",
		Model:           "qwen3.6:35b",
		SkipPermissions: true,
		NoTools:         true,
		Backend:         b,
		Registry:        steveRegistry(),
		Models:          modelconfig.DefaultModels(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := b.lastModel(); got != "qwen3.6:35b" {
		t.Errorf("model = %q, want qwen3.6:35b (SwapModel)", got)
	}
}

func TestRun_PrintWithoutAgent_UsesFallback(t *testing.T) {
	b := bashThenAnswerBackend()
	res, err := Run(context.Background(), Config{
		Prompt:          "Use bash to run hostname",
		SkipPermissions: true,
		Backend:         b,
		Registry:        agents.NewRegistry(), // empty — no named agents
		Tools:           bashToolReg(),
		Models:          modelconfig.DefaultModels(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.ToolsCalled) == 0 {
		t.Fatal("--print without --agent should still run the tool loop")
	}
}

func TestFormatToolSummary_AndWriteText(t *testing.T) {
	summary := FormatToolSummary([]ToolCall{{
		Name:   "bash",
		Args:   map[string]any{"command": "hostname"},
		Result: "testhost\n",
	}})
	if !strings.Contains(summary, "bash") || !strings.Contains(summary, "hostname") {
		t.Errorf("summary missing tool details: %q", summary)
	}

	var out, errOut bytes.Buffer
	res := &Result{AgentOutput: "done", ToolsCalled: []ToolCall{{Name: "bash", Result: "ok"}}}
	if err := WriteResult(&out, &errOut, res, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "done") {
		t.Errorf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "bash") {
		t.Errorf("stderr tool summary = %q", errOut.String())
	}
}
