package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// askedToolRE captures a request that THIS agent use a tool:
// "use NAME tool", "use the NAME tool", "use the NAME tool to …".
// It does not match delegate sentences like "ask Steve to run hostname with bash".
var askedToolRE = regexp.MustCompile(`(?i)\buse(?:\s+the)?\s+([a-z][a-z0-9_]{1,40})\s+tool\b`)

var askedToolStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "this": true, "that": true,
	"my": true, "your": true, "our": true, "any": true, "no": true,
}

// askedImageRE is a direct ask that THIS agent generate/draw an image.
// It is the image/no-tools twin of askedToolRE — there is no generate_image
// builtin, so an empty grant list must teammate-deny instead of crashing
// or inventing a help-desk line.
var askedImageRE = regexp.MustCompile(`(?i)\b(?:generate|create|draw|make|render)\s+(?:me\s+)?(?:an?\s+)?(?:image|picture|photo|drawing|illustration)\b`)

// askedDelegateRE marks "ask Steve to …" / "tell Nova to …" so image
// phrases in a handoff are not treated as a request of THIS agent.
var askedDelegateRE = regexp.MustCompile(`(?i)\b(?:ask|tell|have)\s+[a-z][a-z0-9_-]{0,40}\s+to\b`)

// imageGrantNames are the only grants that satisfy an image-generation ask.
var imageGrantNames = []string{"image", "generate_image", "dalle", "imagen"}

// externalGrantNames are treated as granted when the toolbelt is allow-all (*).
// Local builtins stay gated by local_tools.
var externalGrantNames = []string{
	"aws", "github", "github_cli",
	"gh_pr_list", "gh_pr_view", "gh_pr_diff", "gh_pr_create",
	"gh_issue_list", "gh_issue_view", "gh_issue_create",
	"slack",
}

// TeammateMissingToolSpeech rewrites speech when the human asked for a tool
// this agent was never granted. 14b otherwise invents a persona line (PONG)
// or a help-desk refusal instead of a teammate no.
func TeammateMissingToolSpeech(msgs []backend.Message, schemas []backend.Tool, speech string) string {
	asked := userAskedMissingTool(lastHumanUserText(msgs), schemas)
	if asked == "" {
		return ""
	}
	want := fmt.Sprintf("I don't have %s.", asked)
	if asked == "image" {
		want = "I can't make images."
	}
	if strings.TrimSpace(speech) == want {
		return ""
	}
	return want
}

func lastHumanUserText(msgs []backend.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		c := strings.TrimSpace(msgs[i].Content)
		if c == "" || strings.HasPrefix(c, "[system]") {
			continue
		}
		return c
	}
	return ""
}

func userAskedMissingTool(user string, schemas []backend.Tool) string {
	if user == "" {
		return ""
	}
	low := strings.ToLower(user)
	granted := map[string]bool{}
	for _, t := range schemas {
		granted[strings.ToLower(t.Function.Name)] = true
	}

	var asked []string
	for _, m := range askedToolRE.FindAllStringSubmatch(low, -1) {
		asked = append(asked, strings.ToLower(m[1]))
	}
	// Prefer "use the X tool" / "use the X tool to". Do not fall back to
	// bare name contains — "ask Steve to run hostname with bash" is not
	// a request that THIS agent use bash.

	seen := map[string]bool{}
	for _, name := range asked {
		if askedToolStopwords[name] || seen[name] {
			continue
		}
		seen[name] = true
		if !granted[name] {
			return name
		}
	}
	// Image/no-tools: same strip as "use the X tool". Empty schemas (no
	// tools / no image grant) deny. Delegate sentences stay untouched.
	if asked := userAskedMissingImage(low, granted); asked != "" {
		return asked
	}
	return ""
}

// imageAskStop is the pre-loop capability deny: a direct "generate an image"
// ask against an agent with no image grant. Stops before create_agent / hire
// can invent a specialist (live Winston → ArtBot). Hire/create-teammate
// wording is left to the model.
func imageAskStop(msgs []backend.Message, schemas []backend.Tool) string {
	user := lastHumanUserText(msgs)
	if user == "" {
		return ""
	}
	if userAskedMissingTool(user, schemas) != "image" {
		return ""
	}
	low := strings.ToLower(user)
	if strings.Contains(low, "create_agent") || strings.Contains(low, "hire") ||
		strings.Contains(low, "add a teammate") {
		return ""
	}
	return "I can't make images."
}

func userAskedMissingImage(low string, granted map[string]bool) string {
	for _, name := range imageGrantNames {
		if granted[name] {
			return ""
		}
	}
	if !askedImageRE.MatchString(low) {
		return ""
	}
	if askedDelegateRE.MatchString(low) {
		return ""
	}
	return "image"
}

func grantSchemasFromAgent(ag *agents.Agent) []backend.Tool {
	if ag == nil {
		return nil
	}
	var schemas []backend.Tool
	if len(ag.LocalTools) == 1 && ag.LocalTools[0] == "*" {
		for _, name := range []string{"bash", "shell", "exec"} {
			schemas = append(schemas, backend.Tool{Function: backend.ToolFunction{Name: name}})
		}
	} else {
		for _, name := range ag.LocalTools {
			schemas = append(schemas, backend.Tool{Function: backend.ToolFunction{Name: name}})
		}
	}
	allowed := agents.AllowedProviders(ag.Toolbelt)
	if allowed["*"] {
		for _, name := range externalGrantNames {
			schemas = append(schemas, backend.Tool{Function: backend.ToolFunction{Name: name}})
		}
	} else {
		for p := range allowed {
			if p == "" {
				continue
			}
			schemas = append(schemas, backend.Tool{Function: backend.ToolFunction{Name: p}})
			if p == "github_cli" || p == "github" {
				schemas = append(schemas, backend.Tool{Function: backend.ToolFunction{Name: "github"}})
			}
		}
	}
	return schemas
}

// TeammateMissingToolFromAgent is TeammateMissingToolSpeech using the
// agent's local_tools and toolbelt as the grant list.
// LocalTools ["*"] means bash/shell/exec are present.
// Toolbelt ["*"] means common external names (aws, github, gh_*) are present.
func TeammateMissingToolFromAgent(ag *agents.Agent, user, speech string) string {
	return TeammateMissingToolSpeech([]backend.Message{{Role: "user", Content: user}}, grantSchemasFromAgent(ag), speech)
}

const teammateCredentialDeny = "I don't have that."

// credentialMissRE matches tool-result errors that dump keyring / API-key /
// credential resolution. Never include the secret itself — only the miss.
var credentialMissRE = regexp.MustCompile(`(?i)(?:api[\s_-]?key|keyring|keychain|missing credentials|re-enter your api key|key not found in (?:keychain|keyring|secrets)|secrets:\s+key not found)`)

var teammateTimesSpeechRE = regexp.MustCompile(`(?i)\b\d+\s+times\s+\d+\b`)
var teammateHostValueSpeechRE = regexp.MustCompile(`(?i)\bhostname\b.*['"][^'"]{3,}['"]|['"][^'"]{3,}['"].*\bhostname\b`)

// RewriteCredentialToolResult replaces a keyring/API-key tool error with
// teammate deny so the 14b never sees the dump. Mixed wait/recall reports
// keep their structure; only dump lines are rewritten. Non-credential
// errors pass through. Secrets never appear in the rewrite.
func RewriteCredentialToolResult(content string) string {
	if content == "" || !credentialMissRE.MatchString(content) {
		return content
	}
	if strings.Contains(content, "## ") {
		return redactCredentialLines(content)
	}
	return teammateCredentialDeny
}

func redactCredentialLines(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if !credentialMissRE.MatchString(line) {
			continue
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "**Summary:**") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "**Summary:** " + teammateCredentialDeny
			continue
		}
		lines[i] = teammateCredentialDeny
	}
	return strings.Join(lines, "\n")
}

// TeammateCredentialSpeech rewrites assistant speech that leaked a
// keyring/API-key miss into teammate deny. Residual hostname-fail
// ("Sam couldn't get the hostname.") and real teammate answers stay.
func TeammateCredentialSpeech(speech string) string {
	trim := strings.TrimSpace(speech)
	if trim == "" || trim == teammateCredentialDeny {
		return ""
	}
	if strings.Contains(strings.ToLower(trim), "couldn't get the hostname") {
		return ""
	}
	if speechHasTeammateAnswer(trim) {
		return ""
	}
	if !credentialMissRE.MatchString(trim) {
		return ""
	}
	return teammateCredentialDeny
}

func speechHasTeammateAnswer(s string) bool {
	if strings.Contains(s, "PONG") {
		return true
	}
	if teammateTimesSpeechRE.MatchString(s) {
		return true
	}
	return teammateHostValueSpeechRE.MatchString(s)
}

// applyTeammateSpeech is the visible-speech rewrite after a turn: missing
// granted tool first (Reggie: "I don't have bash."), then a credential
// miss ("I don't have that.").
func applyTeammateSpeech(msgs []backend.Message, schemas []backend.Tool, speech string) string {
	if s := TeammateMissingToolSpeech(msgs, schemas, speech); s != "" {
		return s
	}
	if s := TeammateCredentialSpeech(speech); s != "" {
		return s
	}
	// AfterTools leftover-strip can empty an API-key helpdesk. If the
	// tool-result boundary already rewrote to teammate deny, say that.
	if strings.TrimSpace(speech) == "" && toolResultsHadCredentialDeny(msgs) {
		return teammateCredentialDeny
	}
	return speech
}

func toolResultsHadCredentialDeny(msgs []backend.Message) bool {
	for _, m := range msgs {
		if m.Role == "tool" && strings.TrimSpace(m.Content) == teammateCredentialDeny {
			return true
		}
	}
	return false
}
