package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/backend"
)

// bitbucket.go implements the bitbucket_pr_* belt: a thin Go REST client
// against the Bitbucket Cloud API (api.bitbucket.org/2.0), mirroring the
// gh_pr_*/glab_mr_* tool surface (see github.go, gitlab.go) but talking
// directly to the REST API rather than shelling out to a CLI — there is no
// official, maintained Bitbucket PR CLI to wrap (Atlassian's `acli` does
// not create Bitbucket Cloud PRs; community CLIs are unowned third-party
// wrappers around this same REST API).
//
// Unlike gh_pr_*/glab_mr_*, this belt is not gated on a CLI binary being on
// PATH — it is always registered, and auth is resolved per-call so a
// missing/expired Bitbucket connection surfaces as a clear actionable tool
// error rather than a startup-time decision baked into whether the tools
// exist at all.

// bitbucketAPIBase is the Bitbucket Cloud REST API root.
const bitbucketAPIBase = "https://api.bitbucket.org/2.0"

// bitbucketHTTPTimeout bounds every Bitbucket API call. No retries are
// performed — a retried PR-create could double-create a pull request.
const bitbucketHTTPTimeout = 30 * time.Second

// BitbucketClientFunc resolves an *http.Client whose requests carry a valid
// Bitbucket Authorization header, freshly resolved on every call so token
// refresh (handled by the connections manager) is picked up without
// restarting the tool. Returns an error with an actionable message when no
// Bitbucket connection is configured. Constructed in init_tools.go from the
// connections.Manager/Store — this package deliberately does not import
// internal/connections, to keep the tool belt testable without the full
// connections stack.
type BitbucketClientFunc func(ctx context.Context) (*http.Client, error)

// bbBase is embedded in all bitbucket_pr_* tool structs.
type bbBase struct {
	SandboxRoot string // working directory for resolving the git remote; see workspaceRepo
	ClientFunc  BitbucketClientFunc

	// apiBase overrides bitbucketAPIBase — tests only, points at an
	// httptest server instead of the real Bitbucket API.
	apiBase string
	// remoteFunc overrides workspaceRepo's git-remote resolution — tests
	// only, so unit tests don't need a real git repo with a Bitbucket
	// remote configured.
	remoteFunc func(ctx context.Context) (workspace, repoSlug string, err error)
}

func (b *bbBase) base() string {
	if b.apiBase != "" {
		return b.apiBase
	}
	return bitbucketAPIBase
}

// workspaceRepo derives the Bitbucket workspace and repo_slug from the
// sandbox's "origin" git remote.
func (b *bbBase) workspaceRepo(ctx context.Context) (workspace, repoSlug string, err error) {
	if b.remoteFunc != nil {
		workspace, repoSlug, err = b.remoteFunc(ctx)
	} else {
		var stdout, stderr string
		stdout, stderr, err = runGit(ctx, b.SandboxRoot, "remote", "get-url", "origin")
		if err != nil {
			return "", "", fmt.Errorf("bitbucket: could not read git remote 'origin': %v\n%s", err, strings.TrimSpace(stderr))
		}
		workspace, repoSlug, err = parseBitbucketRemote(stdout)
	}
	if err != nil {
		return "", "", err
	}
	// .git/config is attacker-controlled in a cloned repo — a remote like
	// "ws/repo?x=y" would otherwise pass a query-string-laced workspace/repo
	// straight into an API path (turning a pull-request POST into a
	// repository-update POST). Reject anything outside Bitbucket's own slug
	// charset before it ever reaches a request URL (Opus vet 2026-08-29).
	if verr := validateBitbucketSlug("workspace", workspace); verr != nil {
		return "", "", verr
	}
	if verr := validateBitbucketSlug("repo_slug", repoSlug); verr != nil {
		return "", "", verr
	}
	return workspace, repoSlug, nil
}

// bitbucketSlugPattern is Bitbucket's own workspace/repo_slug charset.
var bitbucketSlugPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validateBitbucketSlug rejects a workspace or repo_slug value that isn't a
// plain Bitbucket slug — in particular one carrying ?, #, %, @, or : that
// could redirect a request to a different API endpoint (see workspaceRepo).
func validateBitbucketSlug(kind, s string) error {
	if !bitbucketSlugPattern.MatchString(s) {
		return fmt.Errorf("bitbucket: invalid %s %q (must match %s)", kind, s, bitbucketSlugPattern.String())
	}
	return nil
}

// bitbucketRemotePattern matches both SSH (git@bitbucket.org:ws/repo.git)
// and HTTPS (https://bitbucket.org/ws/repo.git or with a trailing slash,
// with or without .git) Bitbucket Cloud remote URLs.
var bitbucketRemotePattern = regexp.MustCompile(`(?i)bitbucket\.org[:/]([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)

// parseBitbucketRemote extracts (workspace, repo_slug) from a git remote
// URL, or returns a clear error when the remote isn't a Bitbucket Cloud URL.
func parseBitbucketRemote(remoteURL string) (workspace, repoSlug string, err error) {
	trimmed := strings.TrimSpace(remoteURL)
	m := bitbucketRemotePattern.FindStringSubmatch(trimmed)
	if m == nil {
		return "", "", fmt.Errorf("bitbucket: remote %q is not a Bitbucket Cloud URL (expected a bitbucket.org remote)", trimmed)
	}
	return m[1], m[2], nil
}

// bitbucketTokenFromEnv reads BITBUCKET_ACCESS_TOKEN from the session env
// layer (see session.EnvFrom, the same mechanism gh_*/glab_* use for
// GH_TOKEN/GITLAB_TOKEN), falling back to the process environment.
func bitbucketTokenFromEnv(ctx context.Context) string {
	for _, kv := range session.EnvFrom(ctx) {
		if v, ok := strings.CutPrefix(kv, "BITBUCKET_ACCESS_TOKEN="); ok && v != "" {
			return v
		}
	}
	return os.Getenv("BITBUCKET_ACCESS_TOKEN")
}

// bitbucketBearerTransport injects a static Bearer token — used only for
// the BITBUCKET_ACCESS_TOKEN env fallback path. The connection-backed path
// (ClientFunc) already returns a client whose transport injects the header
// itself (oauth2.Transport), so this is never layered on top of that.
type bitbucketBearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bitbucketBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	return base.RoundTrip(cloned)
}

// client resolves an authorized HTTP client: the connection-backed
// ClientFunc first, falling back to BITBUCKET_ACCESS_TOKEN. Never panics
// and never returns a client silently missing auth — an error here is
// always meant to be shown to the user verbatim, so it is written as an
// actionable instruction, not a stack trace.
func (b *bbBase) client(ctx context.Context) (*http.Client, error) {
	var connErr error
	if b.ClientFunc != nil {
		c, err := b.ClientFunc(ctx)
		if err == nil && c != nil {
			return c, nil
		}
		connErr = err
	}
	if token := bitbucketTokenFromEnv(ctx); token != "" {
		return &http.Client{Transport: bitbucketBearerTransport{token: token}}, nil
	}
	if connErr != nil {
		// An *oauth2.RetrieveError's Error() embeds the raw token-endpoint
		// response body verbatim — that can carry the token, a client
		// secret, or other server internals. Never let it reach
		// ToolResult.Error (model context + transcript); sanitize to a
		// fixed, actionable message instead (Opus vet 2026-08-29).
		var retrieveErr *oauth2.RetrieveError
		if errors.As(connErr, &retrieveErr) {
			return nil, fmt.Errorf("%s (and no BITBUCKET_ACCESS_TOKEN set)", bitbucketAuthFailedMsg)
		}
		return nil, fmt.Errorf("bitbucket: %v (and no BITBUCKET_ACCESS_TOKEN set)", connErr)
	}
	return nil, fmt.Errorf("bitbucket: no Bitbucket connection configured — connect Bitbucket in Settings → Connections (or set BITBUCKET_ACCESS_TOKEN)")
}

// bitbucketAuthFailedMsg is the sanitized, actionable message shown when the
// connections manager's token exchange fails — deliberately fixed text, no
// interpolation of the underlying oauth2 error (see client, above).
const bitbucketAuthFailedMsg = "bitbucket: authentication failed — reconnect Bitbucket in Settings → Connections"

// bitbucketMaxResponseBytes bounds a Bitbucket API response body. Larger
// than the general maxOutputBytes cap (bash.go) — a PR's description/diff
// stat can legitimately push a create/view/merge response past 100KB, and a
// silently truncated body there doesn't just clip Output, it corrupts the
// JSON so json.Unmarshal fails and a successful create is reported as an
// error (Opus vet 2026-08-29, B6).
const bitbucketMaxResponseBytes = 2 * 1024 * 1024 // 2MB

// bitbucketRequest issues one Bitbucket API call and returns the raw
// response body plus status code. body, when non-nil, is JSON-encoded as
// the request payload. No retries — see bitbucketHTTPTimeout's doc comment.
func bitbucketRequest(ctx context.Context, client *http.Client, method, apiURL string, body any) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, bitbucketHTTPTimeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("bitbucket: marshal request body: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiURL, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("bitbucket: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		// An oauth2 token refresh can fail mid-request; its error embeds the
		// token-endpoint response body verbatim. Sanitize it the same way the
		// connect path does so token-endpoint bodies never reach
		// ToolResult.Error or the model transcript (vet B4b).
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) {
			return nil, 0, errors.New(bitbucketAuthFailedMsg)
		}
		return nil, 0, fmt.Errorf("bitbucket: request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, bitbucketMaxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("bitbucket: read response: %w", err)
	}
	return data, resp.StatusCode, nil
}

// bitbucketTimeoutHint appends an actionable note to a request error when it
// is (or wraps) a context deadline: the request may have already reached
// Bitbucket and created/merged the PR server-side before the client gave up
// waiting, so the caller should check Bitbucket rather than assume nothing
// happened — and never retry blindly, since a retried create could
// double-create a PR (see bitbucketHTTPTimeout's doc comment, B6).
func bitbucketTimeoutHint(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("%v (request timed out — the PR may still have been created/merged; check Bitbucket before retrying)", err)
	}
	return err.Error()
}

// bitbucketAPIError formats a non-2xx Bitbucket response body into a tool
// error message. Bitbucket error bodies look like {"error":{"message":"..."}}.
func bitbucketAPIError(status int, body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	return fmt.Sprintf("HTTP %d: %s", status, truncate(msg, 2000))
}

// bitbucketPRLinks/bitbucketPR mirror only the fields this belt needs from
// Bitbucket's pullrequest resource — deliberately not the full schema.
type bitbucketPR struct {
	ID    int    `json:"id"`
	State string `json:"state"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
	Source struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
	} `json:"source"`
}

// --- bitbucket_pr_create ---

type BitbucketPRCreateTool struct {
	bbBase
	// DefaultBranch, when known (resolved once at registration from the
	// local origin/HEAD ref), is surfaced in the tool description so the
	// model knows the destination branch without guessing. Mirrors
	// GHPRCreateTool.DefaultBranch / GlabMRCreateTool.DefaultBranch.
	DefaultBranch string
}

func (t *BitbucketPRCreateTool) Name() string { return "bitbucket_pr_create" }
func (t *BitbucketPRCreateTool) Description() string {
	desc := "Create a new Bitbucket pull request via the Bitbucket Cloud REST API. " +
		"workspace/repo_slug are derived automatically from the git remote 'origin'."
	if t.DefaultBranch != "" {
		desc += fmt.Sprintf(" 'destination_branch' defaults to %q if omitted.", t.DefaultBranch)
	}
	return desc
}
func (t *BitbucketPRCreateTool) Permission() PermissionLevel { return PermWrite }
func (t *BitbucketPRCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_pr_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"title"},
				Properties: map[string]backend.ToolProperty{
					"title":               {Type: "string", Description: "PR title"},
					"body":                {Type: "string", Description: "PR description (markdown)"},
					"source_branch":       {Type: "string", Description: "Source branch (default: current git branch)"},
					"destination_branch":  {Type: "string", Description: "Destination branch (default: repo default branch)"},
					"close_source_branch": {Type: "boolean", Description: "Delete the source branch after merge"},
				},
			},
		},
	}
}
func (t *BitbucketPRCreateTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	title, _ := args["title"].(string)
	if strings.TrimSpace(title) == "" {
		return ToolResult{IsError: true, Error: "bitbucket_pr_create: 'title' argument required"}
	}
	workspace, repoSlug, err := t.workspaceRepo(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	sourceBranch, _ := args["source_branch"].(string)
	if strings.TrimSpace(sourceBranch) == "" {
		stdout, stderr, err := runGit(ctx, t.SandboxRoot, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_create: could not determine current branch: %v\n%s", err, strings.TrimSpace(stderr))}
		}
		sourceBranch = strings.TrimSpace(stdout)
	}
	destBranch, _ := args["destination_branch"].(string)
	if strings.TrimSpace(destBranch) == "" {
		destBranch = t.DefaultBranch
	}
	if strings.TrimSpace(destBranch) == "" {
		return ToolResult{IsError: true, Error: "bitbucket_pr_create: 'destination_branch' argument required (no repo default branch could be detected)"}
	}
	body, _ := args["body"].(string)
	closeSource, _ := args["close_source_branch"].(bool)

	client, err := t.client(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	payload := map[string]any{
		"title":               title,
		"description":         body,
		"close_source_branch": closeSource,
		"source":              map[string]any{"branch": map[string]string{"name": sourceBranch}},
		"destination":         map[string]any{"branch": map[string]string{"name": destBranch}},
	}
	apiURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests", t.base(), workspace, repoSlug)
	data, status, err := bitbucketRequest(ctx, client, http.MethodPost, apiURL, payload)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_create: %s", bitbucketTimeoutHint(err))}
	}
	if status >= 300 {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_create: %s", bitbucketAPIError(status, data))}
	}
	var pr bitbucketPR
	if err := json.Unmarshal(data, &pr); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_create: decode response: %v", err)}
	}
	// Output is a concise human line carrying the PR's web link — mirrors
	// gh_pr_create/glab_mr_create (extractForgeURL) and is what the web PR
	// card's prInfo.ts regex actually parses. The raw JSON body used to be
	// dumped here instead, and its links.self API URL (not the web link)
	// was the first https:// URL the frontend's regex found (Opus vet
	// 2026-08-29, B3). Full details stay out of Output; Metadata already
	// carries the structured {url, number}.
	result := ToolResult{}
	if pr.Links.HTML.Href != "" {
		if pr.ID != 0 {
			result.Output = fmt.Sprintf("Created PR #%d: %s", pr.ID, pr.Links.HTML.Href)
		} else {
			result.Output = fmt.Sprintf("Created PR: %s", pr.Links.HTML.Href)
		}
		md := map[string]any{"url": pr.Links.HTML.Href}
		if pr.ID != 0 {
			md["number"] = fmt.Sprintf("%d", pr.ID)
		}
		result.Metadata = md
	} else {
		// Unexpected API shape (no html link) — fall back to the raw body
		// so there's still something to inspect.
		result.Output = strings.TrimSpace(string(data))
	}
	return result
}

// --- bitbucket_pr_view ---

type BitbucketPRViewTool struct{ bbBase }

func (t *BitbucketPRViewTool) Name() string { return "bitbucket_pr_view" }
func (t *BitbucketPRViewTool) Description() string {
	return "View a Bitbucket pull request by ID via the Bitbucket Cloud REST API."
}
func (t *BitbucketPRViewTool) Permission() PermissionLevel { return PermRead }
func (t *BitbucketPRViewTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_pr_view",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"number"},
				Properties: map[string]backend.ToolProperty{
					"number": {Type: "integer", Description: "PR ID"},
				},
			},
		},
	}
}
func (t *BitbucketPRViewTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "number")
	if !ok {
		return ToolResult{IsError: true, Error: "bitbucket_pr_view: 'number' argument required"}
	}
	workspace, repoSlug, err := t.workspaceRepo(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	client, err := t.client(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	apiURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d", t.base(), workspace, repoSlug, num)
	data, status, err := bitbucketRequest(ctx, client, http.MethodGet, apiURL, nil)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_view: %v", err)}
	}
	if status >= 300 {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_view: %s", bitbucketAPIError(status, data))}
	}
	return ToolResult{Output: strings.TrimSpace(string(data))}
}

// --- bitbucket_pr_checks ---

type BitbucketPRChecksTool struct{ bbBase }

func (t *BitbucketPRChecksTool) Name() string { return "bitbucket_pr_checks" }
func (t *BitbucketPRChecksTool) Description() string {
	return "Show build/check status for a Bitbucket pull request's latest commit. " +
		"Unlike gh_pr_checks, 'number' has no current-branch default — Bitbucket has no " +
		"per-branch PR lookup shortcut, so the PR ID must be given explicitly."
}
func (t *BitbucketPRChecksTool) Permission() PermissionLevel { return PermRead }
func (t *BitbucketPRChecksTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_pr_checks",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"number"},
				Properties: map[string]backend.ToolProperty{
					"number": {Type: "integer", Description: "PR ID"},
				},
			},
		},
	}
}

// bitbucketStatus is one entry from GET .../commit/{hash}/statuses.
type bitbucketStatus struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	State string `json:"state"` // SUCCESSFUL | FAILED | INPROGRESS | STOPPED
	URL   string `json:"url"`
}

type bitbucketStatusesResponse struct {
	Values []bitbucketStatus `json:"values"`
	// Next is the URL of the following page, when the statuses list is
	// paginated (Bitbucket defaults to ~10 per page). Must be followed —
	// see fetchBitbucketStatuses (B2).
	Next string `json:"next"`
}

// bitbucketMaxStatusPages/bitbucketMaxStatusesCollected bound how much of a
// paginated statuses list this belt will follow. Chosen generously (5 pages
// / 500 statuses covers any realistic PR); if a commit's statuses run past
// this bound, fetchBitbucketStatuses reports truncation rather than silently
// verdicting off a partial set.
const (
	bitbucketMaxStatusPages       = 5
	bitbucketMaxStatusesCollected = 500
)

// fetchBitbucketStatuses follows a paginated GET .../statuses response's
// "next" links, aggregating every page's Values. Bitbucket paginates that
// endpoint (~10 per page by default); reading only page 1 (as this belt
// originally did) means a later-page FAILED status is invisible and a
// passing page 1 alone can report a false green (Opus vet 2026-08-29, B2).
// truncated is true when more pages remained beyond the bound above — the
// caller must never report "passed" in that case, since the verdict would
// be based on incomplete data.
func fetchBitbucketStatuses(ctx context.Context, client *http.Client, firstURL string) (statuses []bitbucketStatus, truncated bool, err error) {
	nextURL := firstURL
	for page := 0; page < bitbucketMaxStatusPages && nextURL != ""; page++ {
		data, status, ferr := bitbucketRequest(ctx, client, http.MethodGet, nextURL, nil)
		if ferr != nil {
			return nil, false, ferr
		}
		if status >= 300 {
			return nil, false, errors.New(bitbucketAPIError(status, data))
		}
		var resp bitbucketStatusesResponse
		if jerr := json.Unmarshal(data, &resp); jerr != nil {
			return nil, false, fmt.Errorf("decode statuses: %w", jerr)
		}
		statuses = append(statuses, resp.Values...)
		if len(statuses) > bitbucketMaxStatusesCollected {
			return statuses, true, nil
		}
		nextURL = resp.Next
	}
	if nextURL != "" {
		truncated = true
	}
	return statuses, truncated, nil
}

// classifyBitbucketStatuses aggregates a commit's build statuses into a
// single ChecksStatus-compatible verdict, or "" when there is nothing to
// go on (no statuses at all, or one or more statuses in a state this belt
// doesn't recognize). This is default-deny, not default-pass: "passed" is
// returned ONLY when every status is the explicit SUCCESSFUL state.
// FAILED/ERROR/STOPPED fail the whole set outright (checked first, so a
// failure is never masked by an unrelated unrecognized status).
// INPROGRESS/NOTRUN/PENDING mark the set pending. Anything else — an empty
// state, a missing state field, or a value this belt has never seen —
// abstains: it does NOT count toward "passed", because a status this belt
// can't classify is exactly the shape of data an attacker (or a new
// Bitbucket check type) could use to slip a false green past the model
// (Opus vet 2026-08-29, B1).
func classifyBitbucketStatuses(statuses []bitbucketStatus) string {
	if len(statuses) == 0 {
		return ""
	}
	pending := false
	unknown := false
	for _, s := range statuses {
		switch strings.ToUpper(s.State) {
		case "SUCCESSFUL":
			// contributes toward "passed"
		case "FAILED", "ERROR", "STOPPED":
			return "failed"
		case "INPROGRESS", "NOTRUN", "PENDING":
			pending = true
		default:
			unknown = true
		}
	}
	if unknown {
		return ""
	}
	if pending {
		return "pending"
	}
	return "passed"
}

func (t *BitbucketPRChecksTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "number")
	if !ok {
		return ToolResult{IsError: true, Error: "bitbucket_pr_checks: 'number' argument required"}
	}
	workspace, repoSlug, err := t.workspaceRepo(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	client, err := t.client(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	prURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d", t.base(), workspace, repoSlug, num)
	prData, status, err := bitbucketRequest(ctx, client, http.MethodGet, prURL, nil)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_checks: %v", err)}
	}
	if status >= 300 {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_checks: %s", bitbucketAPIError(status, prData))}
	}
	var pr bitbucketPR
	if err := json.Unmarshal(prData, &pr); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_checks: decode PR: %v", err)}
	}
	if pr.Source.Commit.Hash == "" {
		return ToolResult{IsError: true, Error: "bitbucket_pr_checks: PR response had no source commit hash"}
	}

	statusesURL := fmt.Sprintf("%s/repositories/%s/%s/commit/%s/statuses", t.base(), workspace, repoSlug, pr.Source.Commit.Hash)
	statuses, truncated, err := fetchBitbucketStatuses(ctx, client, statusesURL)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_checks: %v", err)}
	}

	verdict := classifyBitbucketStatuses(statuses)
	if truncated && verdict == "passed" {
		// Never report passed on a truncated set — more pages of statuses
		// existed than this belt fetched, so an all-SUCCESSFUL prefix isn't
		// the whole story (B2). Abstain toward pending rather than a false
		// green.
		verdict = "pending"
	}
	outBytes, _ := json.Marshal(statuses)
	output := strings.TrimSpace(string(outBytes))
	if truncated {
		output += "\n(truncated: more check-status pages exist than were fetched — this verdict may be incomplete)"
	}
	if verdict == "" {
		if len(statuses) == 0 {
			return ToolResult{Output: "no checks reported for this pull request"}
		}
		return ToolResult{Output: output}
	}
	result := ToolResult{
		Output:   output,
		Metadata: map[string]any{"status": verdict},
	}
	// A failed build status is a tool-level error, not just informational —
	// mirrors gh_pr_checks: the model should treat "checks failed" as
	// something to act on, not a quiet pass-through result.
	if verdict == "failed" {
		result.IsError = true
		result.Error = "bitbucket_pr_checks: one or more checks failed"
	}
	return result
}

// --- bitbucket_pr_comment ---

type BitbucketPRCommentTool struct{ bbBase }

func (t *BitbucketPRCommentTool) Name() string { return "bitbucket_pr_comment" }
func (t *BitbucketPRCommentTool) Description() string {
	return "Post a comment on a Bitbucket pull request via the Bitbucket Cloud REST API."
}
func (t *BitbucketPRCommentTool) Permission() PermissionLevel { return PermWrite }
func (t *BitbucketPRCommentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_pr_comment",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"number", "body"},
				Properties: map[string]backend.ToolProperty{
					"number": {Type: "integer", Description: "PR ID"},
					"body":   {Type: "string", Description: "Comment body (markdown)"},
				},
			},
		},
	}
}
func (t *BitbucketPRCommentTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "number")
	if !ok {
		return ToolResult{IsError: true, Error: "bitbucket_pr_comment: 'number' argument required"}
	}
	body, _ := args["body"].(string)
	if strings.TrimSpace(body) == "" {
		return ToolResult{IsError: true, Error: "bitbucket_pr_comment: 'body' argument required"}
	}
	workspace, repoSlug, err := t.workspaceRepo(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	client, err := t.client(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	apiURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d/comments", t.base(), workspace, repoSlug, num)
	payload := map[string]any{"content": map[string]string{"raw": body}}
	data, status, err := bitbucketRequest(ctx, client, http.MethodPost, apiURL, payload)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_comment: %v", err)}
	}
	if status >= 300 {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_comment: %s", bitbucketAPIError(status, data))}
	}
	return ToolResult{Output: strings.TrimSpace(string(data))}
}

// --- bitbucket_pr_merge ---

type BitbucketPRMergeTool struct{ bbBase }

func (t *BitbucketPRMergeTool) Name() string { return "bitbucket_pr_merge" }
func (t *BitbucketPRMergeTool) Description() string {
	return "Merge a Bitbucket pull request via the Bitbucket Cloud REST API."
}
func (t *BitbucketPRMergeTool) Permission() PermissionLevel { return PermWrite }
func (t *BitbucketPRMergeTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_pr_merge",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"number"},
				Properties: map[string]backend.ToolProperty{
					"number":         {Type: "integer", Description: "PR ID"},
					"message":        {Type: "string", Description: "Merge commit message (optional)"},
					"merge_strategy": {Type: "string", Description: "merge_commit (default), squash, or fast_forward"},
				},
			},
		},
	}
}
func (t *BitbucketPRMergeTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "number")
	if !ok {
		return ToolResult{IsError: true, Error: "bitbucket_pr_merge: 'number' argument required"}
	}
	workspace, repoSlug, err := t.workspaceRepo(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	client, err := t.client(ctx)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}
	payload := map[string]any{}
	if msg, _ := args["message"].(string); strings.TrimSpace(msg) != "" {
		payload["message"] = msg
	}
	if strategy, _ := args["merge_strategy"].(string); strings.TrimSpace(strategy) != "" {
		payload["merge_strategy"] = strategy
	}
	apiURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d/merge", t.base(), workspace, repoSlug, num)
	data, status, err := bitbucketRequest(ctx, client, http.MethodPost, apiURL, payload)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_merge: %s", bitbucketTimeoutHint(err))}
	}
	if status >= 300 {
		return ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_pr_merge: %s", bitbucketAPIError(status, data))}
	}
	// See bitbucket_pr_create's Execute: Output is a concise line carrying
	// the PR's web link, not the raw JSON body (B3).
	var pr bitbucketPR
	result := ToolResult{}
	if json.Unmarshal(data, &pr) == nil && pr.Links.HTML.Href != "" {
		if pr.ID != 0 {
			result.Output = fmt.Sprintf("Merged PR #%d: %s", pr.ID, pr.Links.HTML.Href)
		} else {
			result.Output = fmt.Sprintf("Merged PR: %s", pr.Links.HTML.Href)
		}
		md := map[string]any{"url": pr.Links.HTML.Href}
		if pr.ID != 0 {
			md["number"] = fmt.Sprintf("%d", pr.ID)
		}
		result.Metadata = md
	} else {
		result.Output = strings.TrimSpace(string(data))
	}
	return result
}
