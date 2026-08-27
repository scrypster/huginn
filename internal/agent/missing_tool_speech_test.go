package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

func TestTeammateMissingToolSpeech_RewritesHelpDesk(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Use the bash tool to run hostname."}}
	got := TeammateMissingToolSpeech(msgs, nil, "I'm sorry, but I can't assist with that request.")
	if got != "I don't have bash." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolSpeech_KeepsWhenGranted(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Use the bash tool to run hostname."}}
	schemas := []backend.Tool{{Function: backend.ToolFunction{Name: "bash"}}}
	if got := TeammateMissingToolSpeech(msgs, schemas, "I'm sorry, but I can't assist with that request."); got != "" {
		t.Fatalf("rewrote a granted-tool refuse: %q", got)
	}
}

func TestTeammateMissingToolSpeech_KeepsRealProseWhenNotAsked(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "What is 2 plus 2?"}}
	if got := TeammateMissingToolSpeech(msgs, nil, "Steve has bash. I do numbers."); got != "" {
		t.Fatalf("rewrote teammate prose: %q", got)
	}
}

func TestTeammateMissingToolFromAgent_EmptyBelt(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", LocalTools: nil}
	got := TeammateMissingToolFromAgent(ag, "Use the bash tool to run hostname.", "I'm sorry, I can't assist with that.")
	if got != "I don't have bash." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolFromAgent_EmptyBelt_PONG(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", LocalTools: nil}
	got := TeammateMissingToolFromAgent(ag, "Use the bash tool to run hostname.", "PONG")
	if got != "I don't have bash." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolFromAgent_GrantedBash_NoRewrite(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", LocalTools: []string{"bash"}}
	got := TeammateMissingToolFromAgent(ag, "Use the bash tool to run hostname.", "PONG")
	if got != "" {
		t.Fatalf("rewrote granted bash: %q", got)
	}
}

func TestTeammateMissingToolFromAgent_AlreadyTeammateLine(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", LocalTools: nil}
	got := TeammateMissingToolFromAgent(ag, "Use the bash tool to run hostname.", "I don't have bash.")
	if got != "" {
		t.Fatalf("rewrote exact teammate line: %q", got)
	}
}

func TestTeammateMissingToolSpeech_RewritesAWS(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Use the aws tool to list s3 buckets."}}
	got := TeammateMissingToolSpeech(msgs, nil, "I'm sorry, I can't assist with that.")
	if got != "I don't have aws." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolSpeech_RewritesGitHub(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Use the github tool to list issues."}}
	got := TeammateMissingToolSpeech(msgs, nil, "I'm sorry, I can't assist with that.")
	if got != "I don't have github." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolSpeech_RewritesGHIssueCreate(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Use the gh_issue_create tool to open a ticket."}}
	got := TeammateMissingToolSpeech(msgs, nil, "PONG")
	if got != "I don't have gh_issue_create." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolFromAgent_NoAWS(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", LocalTools: nil, Toolbelt: nil}
	got := TeammateMissingToolFromAgent(ag, "Use the aws tool to list s3 buckets.", "I'm sorry, I can't assist.")
	if got != "I don't have aws." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolFromAgent_WildcardToolbeltKeepsAWS(t *testing.T) {
	ag := &agents.Agent{
		Name:     "Reggie",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "*", ConnectionID: "*"}},
	}
	got := TeammateMissingToolFromAgent(ag, "Use the aws tool to list s3 buckets.", "I'm sorry, I can't assist.")
	if got != "" {
		t.Fatalf("rewrote aws under toolbelt *: %q", got)
	}
}

func TestTeammateMissingToolFromAgent_WildcardStillDeniesBash(t *testing.T) {
	ag := &agents.Agent{
		Name:     "Reggie",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "*", ConnectionID: "*"}},
	}
	got := TeammateMissingToolFromAgent(ag, "Use the bash tool to run hostname.", "PONG")
	if got != "I don't have bash." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolFromAgent_NamedGitHubProvider(t *testing.T) {
	ag := &agents.Agent{
		Name:     "Astra",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "github"}},
	}
	if got := TeammateMissingToolFromAgent(ag, "Use the github tool to list PRs.", "sorry"); got != "" {
		t.Fatalf("rewrote granted github: %q", got)
	}
	if got := TeammateMissingToolFromAgent(ag, "Use the aws tool to list s3 buckets.", "sorry"); got != "I don't have aws." {
		t.Fatalf("aws under github-only belt: %q", got)
	}
}

func TestTeammateMissingToolSpeech_PrefersUseTheToolOverDoNotUse(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Use the aws tool to list s3 buckets. Do not use bash."}}
	got := TeammateMissingToolSpeech(msgs, nil, "PONG")
	if got != "I don't have aws." {
		t.Fatalf("got %q, want I don't have aws.", got)
	}
}

func TestTeammateMissingToolFromAgent_WildcardAWS_DoNotUseBash(t *testing.T) {
	ag := &agents.Agent{
		Name:     "Reggie",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "*", ConnectionID: "*"}},
	}
	got := TeammateMissingToolFromAgent(ag, "Use the aws tool to list s3 buckets. Do not use bash.", "I don't have aws.")
	if got != "" {
		t.Fatalf("overwrote loop aws deny: %q", got)
	}
}

func TestTeammateMissingToolFromAgent_DelegateBashMention_NoRewrite(t *testing.T) {
	ag := &agents.Agent{Name: "Winston", LocalTools: nil, Toolbelt: nil}
	got := TeammateMissingToolFromAgent(ag, "Ask Steve to run hostname with bash", "Steve said MJs-MacBook-Pro. 56.")
	if got != "" {
		t.Fatalf("rewrote delegate-bash prompt: %q", got)
	}
}

func TestTeammateMissingToolFromAgent_UseTheBashTool_EmptyBelt(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", LocalTools: nil, Toolbelt: nil}
	got := TeammateMissingToolFromAgent(ag, "Use the bash tool to run hostname.", "PONG")
	if got != "I don't have bash." {
		t.Fatalf("got %q, want I don't have bash.", got)
	}
}

func TestTeammateMissingToolSpeech_ImageAsk_EmptySchemas(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Generate an image of a cat."}}
	got := TeammateMissingToolSpeech(msgs, nil, "I'm sorry, I can't create images.")
	if got != "I don't have image." {
		t.Fatalf("got %q, want I don't have image.", got)
	}
}

func TestTeammateMissingToolSpeech_DrawAPicture_NoTools(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Draw a picture of the office."}}
	got := TeammateMissingToolSpeech(msgs, nil, "PONG")
	if got != "I don't have image." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolSpeech_UseGenerateImageTool(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Use the generate_image tool to make a logo."}}
	got := TeammateMissingToolSpeech(msgs, nil, "I cannot do that.")
	if got != "I don't have generate_image." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolSpeech_ImageAsk_GrantedImage_NoRewrite(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Generate an image of a cat."}}
	schemas := []backend.Tool{{Function: backend.ToolFunction{Name: "generate_image"}}}
	if got := TeammateMissingToolSpeech(msgs, schemas, "working on it"); got != "" {
		t.Fatalf("rewrote granted image: %q", got)
	}
}

func TestTeammateMissingToolFromAgent_EmptyBelt_ImageAsk(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", LocalTools: nil, Toolbelt: nil}
	got := TeammateMissingToolFromAgent(ag, "Create an image of a sunset.", "PONG")
	if got != "I don't have image." {
		t.Fatalf("got %q", got)
	}
}

func TestTeammateMissingToolFromAgent_DelegateImage_NoRewrite(t *testing.T) {
	ag := &agents.Agent{Name: "Winston", LocalTools: nil, Toolbelt: nil}
	got := TeammateMissingToolFromAgent(ag, "Ask Steve to generate an image of the hostname.", "Steve said he doesn't have image.")
	if got != "" {
		t.Fatalf("rewrote delegate-image prompt: %q", got)
	}
}

func TestRewriteCredentialToolResult_Keyring(t *testing.T) {
	in := `error: api key: keyring lookup failed for service "huginn"`
	got := RewriteCredentialToolResult(in)
	if got != "I don't have that." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{"API key", "api key", "keyring", "huginn"} {
		if containsFold(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestRewriteCredentialToolResult_SecretsFile(t *testing.T) {
	in := `error: secrets: key not found in keychain or secrets file for slot "openai" — re-enter your API key in Settings`
	got := RewriteCredentialToolResult(in)
	if got != "I don't have that." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{"API key", "keychain", "openai", "Settings", "secrets"} {
		if containsFold(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestRewriteCredentialToolResult_NeverPrintsSecret(t *testing.T) {
	in := `error: api key: keyring lookup failed for service "huginn" value=sk-secret-SHOULD-NOT-LEAK`
	got := RewriteCredentialToolResult(in)
	if got != "I don't have that." {
		t.Fatalf("got %q", got)
	}
	if containsFold(got, "sk-") || containsFold(got, "SHOULD-NOT-LEAK") || containsFold(got, "API") {
		t.Fatalf("leaked secret or API: %q", got)
	}
}

func TestRewriteCredentialToolResult_KeepsPermissionDenied(t *testing.T) {
	in := "error: permission denied"
	if got := RewriteCredentialToolResult(in); got != in {
		t.Fatalf("rewrote non-credential: %q", got)
	}
}

func TestRewriteCredentialToolResult_KeepsUnknownTool(t *testing.T) {
	in := `error: unknown tool "bash"`
	if got := RewriteCredentialToolResult(in); got != in {
		t.Fatalf("rewrote unknown-tool: %q", got)
	}
}

func TestRewriteCredentialToolResult_WaitReportRedactsDump(t *testing.T) {
	in := "## Finished threads (1)\n\n## Result from agent \"Sam\"\n\n**Summary:** LLM API error: chat completion: resolve api key: api key: keyring lookup failed for service \"huginn\"\n\n**Status:** error\nThread ID: `th_1`\n"
	got := RewriteCredentialToolResult(in)
	if !containsFold(got, "## Finished threads") || !containsFold(got, "Sam") {
		t.Fatalf("lost wait structure: %q", got)
	}
	if got == "I don't have that." {
		t.Fatal("wiped wait report")
	}
	if !containsFold(got, "I don't have that.") {
		t.Fatalf("missing teammate deny: %q", got)
	}
	for _, leak := range []string{"API key", "api key", "keyring", "sk-"} {
		if containsFold(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestTeammateCredentialSpeech_RewritesHelpDesk(t *testing.T) {
	got := TeammateCredentialSpeech("I'm sorry, the system encountered an API key resolution issue.")
	if got != "I don't have that." {
		t.Fatalf("got %q", got)
	}
	if containsFold(got, "API") || containsFold(got, "sorry") || containsFold(got, "apologize") {
		t.Fatalf("leaked helpdesk: %q", got)
	}
}

func TestTeammateCredentialSpeech_KeepsHostnameFail(t *testing.T) {
	if got := TeammateCredentialSpeech("Sam couldn't get the hostname."); got != "" {
		t.Fatalf("stomped residual hostname fail: %q", got)
	}
}

func TestTeammateCredentialSpeech_KeepsHostnameAnswer(t *testing.T) {
	in := "The hostname of the machine is 'MJs-MacBook-Pro'. The system encountered an API key resolution issue."
	if got := TeammateCredentialSpeech(in); got != "" {
		t.Fatalf("stomped hostname answer: %q", got)
	}
}

func TestTeammateCredentialSpeech_AlreadyDeny(t *testing.T) {
	if got := TeammateCredentialSpeech("I don't have that."); got != "" {
		t.Fatalf("rewrote exact deny: %q", got)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func TestRunLoop_KeyringToolError_TeammateDenySpeech(t *testing.T) {
	tool := &mockTool{name: "bash", result: tools.ToolResult{
		IsError: true,
		Error:   `api key: keyring lookup failed for service "huginn" value=sk-secret-SHOULD-NOT-LEAK`,
	}}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("bash", "call-1"),
			{Content: "I'm sorry, the system encountered an API key resolution issue. If you have access to the API key, please provide it.", DoneReason: "stop"},
		},
	}
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		Backend:     mb,
		Tools:       newRegistryWith(tool),
		Messages:    []backend.Message{{Role: "user", Content: "What is the hostname?"}},
		ToolSchemas: []backend.Tool{{Function: backend.ToolFunction{Name: "bash"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinalContent != "I don't have that." {
		t.Fatalf("visible speech = %q, want teammate deny", result.FinalContent)
	}
	for _, leak := range []string{"API key", "api key", "keyring", "apologize", "sorry", "sk-secret", "SHOULD-NOT-LEAK"} {
		if containsFold(result.FinalContent, leak) {
			t.Errorf("speech leaked %q: %q", leak, result.FinalContent)
		}
	}

	var toolMsg string
	for _, msg := range result.Messages {
		if msg.Role == "tool" {
			toolMsg = msg.Content
		}
	}
	if toolMsg != "I don't have that." {
		t.Fatalf("tool result = %q, want teammate deny (14b must not see dump)", toolMsg)
	}

	if len(mb.lastRequests) < 2 {
		t.Fatalf("backend calls = %d, want at least 2", len(mb.lastRequests))
	}
	for i, req := range mb.lastRequests {
		if i == 0 {
			continue
		}
		for _, msg := range req.Messages {
			blob := msg.Content
			for _, leak := range []string{"keyring", "api key", "API key", "sk-secret", "SHOULD-NOT-LEAK"} {
				if containsFold(blob, leak) {
					t.Errorf("turn %d %s message leaked %q: %q", i+1, msg.Role, leak, blob)
				}
			}
		}
	}
}

func TestRunLoop_PermissionDeniedNotCredentialDeny(t *testing.T) {
	tool := &mockTool{name: "bash", result: tools.ToolResult{
		IsError: true,
		Error:   "permission denied",
	}}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("bash", "call-1"),
			{Content: "I couldn't run that.", DoneReason: "stop"},
		},
	}
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		Backend:     mb,
		Tools:       newRegistryWith(tool),
		Messages:    []backend.Message{{Role: "user", Content: "run hostname"}},
		ToolSchemas: []backend.Tool{{Function: backend.ToolFunction{Name: "bash"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinalContent != "I couldn't run that." {
		t.Fatalf("stomped permission-denied speech: %q", result.FinalContent)
	}
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "permission denied") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected permission denied tool result to pass through")
	}
}

func TestTeammateMissingToolFromAgent_WinstonCreateAgentStillDeniesImage(t *testing.T) {
	ag := &agents.Agent{Name: "Winston", LocalTools: []string{"create_agent"}}
	got := TeammateMissingToolFromAgent(ag, "@Winston generate an image of a red cube", "created")
	if got != "I don't have image." {
		t.Fatalf("got %q", got)
	}
}

func TestImageAskStop_DirectAsk(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "@Winston generate an image of a red cube"}}
	schemas := []backend.Tool{{Function: backend.ToolFunction{Name: "create_agent"}}}
	if got := imageAskStop(msgs, schemas); got != "I don't have image." {
		t.Fatalf("got %q", got)
	}
}

func TestImageAskStop_HireWordingStillRuns(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "Hire a teammate who can generate an image of a logo."}}
	if got := imageAskStop(msgs, nil); got != "" {
		t.Fatalf("hire+image short-circuited: %q", got)
	}
}
