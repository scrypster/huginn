package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// DelegateRequest is one delegated Claude Code task.
type DelegateRequest struct {
	Prompt         string
	CWD            string
	Model          string
	PermissionMode string
	MaxTurns       int
	AllowedTools   []string

	// The fields below exist so the agent backend (agent_backend.go) can share
	// this one argument builder instead of forking it. A one-shot delegation
	// leaves them zero and gets exactly the argv it always got.

	// Resume continues an existing session with `--resume <id>` instead of
	// creating one with `--session-id <id>`. The two are mutually exclusive,
	// which is why this is a flag here rather than an extra argument appended
	// by the caller.
	Resume bool
	// AppendSystemPrompt is passed to --append-system-prompt.
	AppendSystemPrompt string
	// Settings is inline JSON for --settings; see BuildHookSettings.
	Settings string
	// MCPConfig is passed to --mcp-config.
	MCPConfig string
}

// DelegateResult is the outcome of a delegated run.
type DelegateResult struct {
	SessionID  string
	Text       string
	CostUSD    float64
	DurationMS int
	NumTurns   int
	IsError    bool
	ErrorText  string

	// ReportedSessionID is the session id the CLI itself echoed back, on any
	// line that carries one. It is the only trustworthy evidence that a
	// session actually EXISTS on disk: Delegate assigns SessionID up front and
	// never learns whether the CLI got far enough to create it. AgentBackend
	// keys its --session-id/--resume decision off this.
	ReportedSessionID string

	// InputTokens/OutputTokens are accumulated from assistant-line usage.
	// Output is summed across the run; input is the LAST reported value, since
	// each assistant message restates the whole context rather than a delta —
	// summing it would multiply-count the same prompt.
	InputTokens  int
	OutputTokens int
}

// StreamEvent is a live update emitted while the delegated run proceeds.
// Type is "text" or "tool_use".
type StreamEvent struct {
	Type     string
	Text     string
	ToolName string
}

// BuildArgs assembles the claude CLI arguments for a delegated run.
//
// The session id is assigned by Huginn rather than read back afterwards, so
// the transcript watcher can correlate the run to a session without racing
// the CLI.
func BuildArgs(cfg DelegateConfig, req DelegateRequest, sessionID string) []string {
	mode := req.PermissionMode
	if mode == "" {
		mode = cfg.PermissionMode
	}
	model := req.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	turns := req.MaxTurns
	if turns <= 0 {
		turns = cfg.MaxTurns
	}

	sessionFlag := "--session-id"
	if req.Resume {
		sessionFlag = "--resume"
	}

	args := []string{
		"-p", req.Prompt,
		sessionFlag, sessionID,
		"--output-format", "stream-json",
		"--verbose",
	}
	if mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	// --max-turns is absent from `claude --help` but IS accepted by the CLI.
	// Verified empirically: this build rejects genuinely unknown flags with
	// "error: unknown option", and `--max-turns 3` was accepted and ran
	// normally. It is undocumented, not nonexistent — do not remove it, and do
	// not assume other undocumented flags behave the same way without testing.
	if turns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(turns))
	}
	// ASSUMPTION (not verified against the real CLI): --allowedTools accepts
	// its tools as separate space-separated argv elements. The comma-separated
	// single-argument form is the other candidate. This predates the agent
	// backend and has never been exercised in production, so if pre-authorised
	// tools appear to be ignored, check this form FIRST.
	if len(req.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, req.AllowedTools...)
	}
	// ASSUMPTION (not verified against the real CLI): the three flags below
	// take a single following argument, and --settings accepts inline JSON as
	// well as a file path. Unlike --max-turns above, none of these has been
	// empirically confirmed — do not upgrade this marker to VERIFIED without
	// actually running the CLI.
	if req.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.AppendSystemPrompt)
	}
	if req.Settings != "" {
		args = append(args, "--settings", req.Settings)
	}
	if req.MCPConfig != "" {
		args = append(args, "--mcp-config", req.MCPConfig)
	}
	// Only ever set from explicit configuration, never inferred.
	if cfg.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args
}

// Delegate runs a one-shot Claude Code task and returns its result.
//
// onEvent, if non-nil, is called for each assistant text and tool_use event as
// it streams, so callers can mirror progress into a thread panel. It is called
// from the reading goroutine and must not block for long.
//
// On timeout the process group is killed and the partial result is returned
// alongside a non-nil error.
func Delegate(
	ctx context.Context,
	cfg DelegateConfig,
	binary string,
	req DelegateRequest,
	sessionID string,
	onEvent func(StreamEvent),
) (DelegateResult, error) {
	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binary, BuildArgs(cfg, req, sessionID)...)
	if req.CWD != "" {
		cmd.Dir = req.CWD
	}
	// Put the child in its own process group so a timeout kills any tools it
	// spawned, not just the CLI itself. Platform-specific; see delegate_unix.go.
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return DelegateResult{IsError: true, ErrorText: err.Error()},
			fmt.Errorf("claudecode: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return DelegateResult{IsError: true, ErrorText: err.Error()},
			fmt.Errorf("claudecode: start %s: %w", binary, err)
	}

	res := DelegateResult{SessionID: sessionID}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		applyStreamLine(sc.Bytes(), &res, onEvent)
	}

	waitErr := cmd.Wait()
	if runCtx.Err() != nil {
		res.IsError = true
		res.ErrorText = "claude run timed out after " + timeout.String()
		return res, fmt.Errorf("claudecode: delegate timed out: %w", runCtx.Err())
	}
	if waitErr != nil {
		res.IsError = true
		if res.ErrorText == "" {
			res.ErrorText = waitErr.Error()
		}
		return res, fmt.Errorf("claudecode: delegate failed: %w", waitErr)
	}
	return res, nil
}

// streamLine is one line of claude's stream-json output.
//
// VERIFIED against the real CLI, not just the test fixture: a live
// `claude -p ... --output-format json` run emits exactly these keys —
// result, total_cost_usd, duration_ms, num_turns, subtype ("success"),
// session_id, type ("result"). Field names here match that output.
type streamLine struct {
	Type      string  `json:"type"`
	Subtype   string  `json:"subtype"`
	Result    string  `json:"result"`
	IsError   bool    `json:"is_error"`
	Cost      float64 `json:"total_cost_usd"`
	Duration  int     `json:"duration_ms"`
	NumTurns  int     `json:"num_turns"`
	SessionID string  `json:"session_id"`
	Message   struct {
		Content contentBlocks `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func applyStreamLine(raw []byte, res *DelegateResult, onEvent func(StreamEvent)) {
	var sl streamLine
	if err := json.Unmarshal(raw, &sl); err != nil {
		return
	}
	// VERIFIED against a live `claude -p --output-format stream-json` run: the
	// stream also carries `system` (hook events) and `rate_limit_event` lines.
	// Both are deliberately ignored here — the default switch arm drops any
	// line type this bridge does not consume, so new line types in future CLI
	// versions cannot break a delegation.
	// Recorded regardless of line type: the CLI emits session_id on its
	// `system`/init line too, which is the earliest proof the session exists.
	if sl.SessionID != "" {
		res.ReportedSessionID = sl.SessionID
	}
	switch sl.Type {
	case "assistant":
		if u := sl.Message.Usage; u.InputTokens > 0 {
			res.InputTokens = u.InputTokens
		}
		res.OutputTokens += sl.Message.Usage.OutputTokens
		if onEvent == nil {
			return
		}
		for _, b := range sl.Message.Content {
			switch b.Type {
			case "text":
				onEvent(StreamEvent{Type: "text", Text: b.Text})
			case "tool_use":
				onEvent(StreamEvent{Type: "tool_use", ToolName: b.Name})
			}
		}
	case "result":
		res.Text = sl.Result
		res.CostUSD = sl.Cost
		res.DurationMS = sl.Duration
		res.NumTurns = sl.NumTurns
		// `is_error` is the CLI's canonical failure signal; `subtype` carries
		// the reason. Both are checked: a run can report is_error with a
		// subtype we do not recognise, and older builds may set only subtype.
		if sl.IsError || (sl.Subtype != "" && sl.Subtype != "success") {
			res.IsError = true
			res.ErrorText = sl.Subtype
			if res.ErrorText == "" {
				res.ErrorText = "claude reported is_error"
			}
		}
	}
}
