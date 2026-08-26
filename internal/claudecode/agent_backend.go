package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// DefaultGatedTools are gated when an agent has no explicit ClaudeGatedTools.
// Every Claude Code tool that can mutate state or reach the network is here.
// An unconfigured agent runs unattended, so the default must be restrictive:
// an empty gated list emits no hooks and gates nothing at all.
//
// These are Claude Code's tool names, not Huginn's LocalTools names — never
// mix the two namespaces.
var DefaultGatedTools = []string{
	"Bash", "Write", "Edit", "NotebookEdit", "WebFetch", "Task",
}

// DefaultAgentTurnTimeoutSecs bounds one agent turn when the config leaves
// TimeoutSecs zero. It matches Delegate's own fallback. Zero must never mean
// "forever": a wedged turn holds the session semaphore, and because backends
// are cached per agent, that would stall every session on that agent.
const DefaultAgentTurnTimeoutSecs = 900

// agentStderrTailBytes caps how much of the CLI's stderr is retained for error
// messages. Bounded on purpose — a chatty or looping CLI must not grow this
// without limit.
const agentStderrTailBytes = 8 << 10

// agentWaitDelay is how long cmd.Wait tolerates a process that has been
// signalled but has not exited before it force-closes the pipes and returns.
// Without it, a child ignoring SIGKILL (or a grandchild holding stdout open)
// holds the session semaphore indefinitely.
const agentWaitDelay = 10 * time.Second

// AgentBackendConfig is everything a Claude Code agent needs for one turn.
type AgentBackendConfig struct {
	Binary       string
	SessionID    string
	CWD          string
	Model        string
	SystemPrompt string   // assembled by AssembleSystemPrompt, rebuilt every turn
	AllowedTools []string // pre-authorised; never invoke the approval hook
	GatedTools   []string // require approval; one PreToolUse hook entry each
	HookCommand  string
	MCPConfig    string // empty in v1 — the seam for the toolbelt MCP server
	// FirstTurn is the INITIAL state only: true when the session does not yet
	// exist on disk. It is not consulted again after the first turn — see
	// AgentBackend.sessionExists, which is what actually drives the flag.
	FirstTurn bool
	// TimeoutSecs bounds a single turn. Zero means DefaultAgentTurnTimeoutSecs.
	TimeoutSecs int
}

// AgentBackend drives one Claude Code session as a Huginn agent's backend.
//
// It implements backend.Backend, with two documented departures from the
// interface's usual semantics:
//
//   - req.Messages is ignored past the newest turn. Claude Code owns the
//     conversation; replaying history would duplicate it.
//   - ToolCalls is always empty. Claude Code executes its own tools, so they
//     are reported via ExecutedTools for persistence and must never be
//     dispatched by the agent loop.
//
// One instance is CACHED AND SHARED per agent, so all mutable state below is
// guarded and every wait is bounded.
type AgentBackend struct {
	cfg AgentBackendConfig

	// sem is a one-slot semaphore serialising turns: two concurrent writers
	// would corrupt the transcript, which is the session's only source of
	// truth. It is a channel rather than a sync.Mutex so a queued caller can
	// abandon the wait when ITS OWN context is cancelled; a mutex is not
	// context-aware and would pin that caller behind an unrelated turn.
	sem chan struct{}

	mu sync.Mutex // guards the fields below; held only for field access
	// sessionExists records whether the Claude Code session has been created.
	// False means the next turn must use --session-id; true means --resume.
	sessionExists bool
	args          []string // last argv, for tests
}

var _ backend.Backend = (*AgentBackend)(nil)

// NewAgentBackend returns a backend bound to one Claude Code session.
func NewAgentBackend(cfg AgentBackendConfig) *AgentBackend {
	return &AgentBackend{
		cfg: cfg,
		sem: make(chan struct{}, 1),
		// FirstTurn seeds the state; from here on the backend tracks it
		// itself, because the same instance serves every later turn.
		sessionExists: !cfg.FirstTurn,
	}
}

func (b *AgentBackend) lastArgs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.args...)
}

func (b *AgentBackend) Health(context.Context) error   { return nil }
func (b *AgentBackend) Shutdown(context.Context) error { return nil }
func (b *AgentBackend) ContextWindow() int             { return 200000 }

func (b *AgentBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	prompt, err := newestUserMessage(req.Messages)
	if err != nil {
		return nil, err
	}

	// Acquire the session before doing anything expensive, and give up if the
	// caller goes away while we are queued.
	//
	// NOTE FOR TASK 8: the whole turn — including every onEvent callback below
	// — runs while holding this slot. The `claude-approve` callback MUST NOT
	// route back into this backend (or into anything that itself waits on this
	// agent's turn), or the approval deadlocks, and Claude Code's 30s
	// PreToolUse hook timeout then FAILS OPEN and runs the tool unapproved.
	// See ClaudeHookTimeoutSecs in hooks.go.
	select {
	case b.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("claudecode agent: gave up waiting for the session: %w", ctx.Err())
	}
	defer func() { <-b.sem }()

	timeout := time.Duration(b.cfg.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = DefaultAgentTurnTimeoutSecs * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args, err := b.buildArgs(prompt)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.args = args
	b.mu.Unlock()

	cmd := exec.CommandContext(runCtx, b.cfg.Binary, args...)
	if b.cfg.CWD != "" {
		cmd.Dir = b.cfg.CWD
	}
	// Own process group, so cancelling the turn kills the tools the CLI
	// spawned rather than orphaning them. See delegate_unix.go.
	setProcessGroup(cmd)
	cmd.WaitDelay = agentWaitDelay

	// Only read after cmd.Wait returns, which happens-after the copying
	// goroutine exec starts for a non-*os.File stderr — no extra locking.
	stderr := &tailBuffer{max: agentStderrTailBytes}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claudecode agent: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		// The process never ran, so no session was created: leave
		// sessionExists alone so the next turn still uses --session-id.
		return nil, fmt.Errorf("claudecode agent: start %s: %w", b.cfg.Binary, err)
	}

	var (
		res   DelegateResult
		tools []backend.ExecutedTool
	)
	onEvent := func(e StreamEvent) {
		switch e.Type {
		case "text":
			// BOTH callbacks fire, never else-if. The relay builds its live
			// "token" messages from OnToken and ignores StreamText in its
			// OnEvent handler, so an else-if here means no text ever reaches
			// the user. anthropic_sse.go carries the same warning after this
			// exact bug shipped once already.
			if req.OnEvent != nil {
				req.OnEvent(backend.StreamEvent{Type: backend.StreamText, Content: e.Text})
			}
			if req.OnToken != nil {
				req.OnToken(e.Text)
			}
		case "tool_use":
			// Surfaced for live display only; persistence happens via
			// ExecutedTools, which the loop must not dispatch.
			if req.OnEvent != nil {
				req.OnEvent(backend.NewToolCallEvent(e.ToolName))
			}
		}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		applyStreamLine(line, &res, onEvent)
		tools = appendExecutedTools(tools, line)
	}
	// A scan error (typically a line past the buffer cap) leaves the pipe
	// unread, which would block Wait forever. Record it, then always Wait.
	scanErr := sc.Err()

	waitErr := cmd.Wait()

	// The CLI echoing a session id is the only trustworthy evidence that the
	// session now exists on disk, and it holds even when the turn later
	// failed. Recording it here — before any error return — is what stops a
	// failed first turn from wedging the agent in either direction: no id
	// means the next turn still creates the session, an id means it resumes.
	b.markSessionExists(res.ReportedSessionID != "" || (waitErr == nil && scanErr == nil))

	// Cancellation and timeout are distinguished deliberately: the relay keys
	// idle-timeout handling off errors.Is(err, context.Canceled), which never
	// matched while these surfaced as a bare "signal: killed".
	if ctx.Err() != nil {
		return nil, b.wrap(stderr, fmt.Errorf("claudecode agent: turn cancelled: %w", ctx.Err()))
	}
	if runCtx.Err() != nil {
		return nil, b.wrap(stderr, fmt.Errorf("claudecode agent: turn timed out after %s: %w", timeout, runCtx.Err()))
	}
	if scanErr != nil {
		return nil, b.wrap(stderr, fmt.Errorf("claudecode agent: reading %s output: %w", b.cfg.Binary, scanErr))
	}
	if waitErr != nil {
		return nil, b.wrap(stderr, fmt.Errorf("claudecode agent: %s failed: %w", b.cfg.Binary, waitErr))
	}
	if res.IsError {
		return nil, b.wrap(stderr, fmt.Errorf("claudecode agent: %s", res.ErrorText))
	}

	// CostUSD and DurationMS have nowhere to go: backend.ChatResponse has no
	// field for either, and widening that shared interface for one backend is
	// out of scope here. Logged so the numbers are at least observable.
	slog.Debug("claudecode agent: turn complete",
		"session_id", b.cfg.SessionID,
		"cost_usd", res.CostUSD,
		"num_turns", res.NumTurns,
		"duration_ms", res.DurationMS,
		"tools", len(tools))

	// ToolCalls is deliberately left nil: Claude Code already ran these.
	return &backend.ChatResponse{
		Content:          res.Text,
		DoneReason:       "stop",
		ExecutedTools:    tools,
		PromptTokens:     res.InputTokens,
		CompletionTokens: res.OutputTokens,
	}, nil
}

// wrap appends the tail of the CLI's stderr to err. Without it a misconfigured
// run surfaces as a bare "exit status 1" with the actual cause discarded.
func (b *AgentBackend) wrap(stderr *tailBuffer, err error) error {
	tail := stderr.String()
	if tail == "" {
		return err
	}
	return fmt.Errorf("%w: stderr: %s", err, tail)
}

func (b *AgentBackend) markSessionExists(exists bool) {
	if !exists {
		return
	}
	b.mu.Lock()
	b.sessionExists = true
	b.mu.Unlock()
}

// buildArgs delegates to the package's single argument builder. The agent-only
// flags live on DelegateRequest so there is exactly one place that knows the
// CLI's argv shape.
func (b *AgentBackend) buildArgs(prompt string) ([]string, error) {
	settings, err := BuildHookSettings(b.cfg.GatedTools, b.cfg.HookCommand)
	if err != nil {
		return nil, fmt.Errorf("claudecode agent: build hook settings: %w", err)
	}
	b.mu.Lock()
	resume := b.sessionExists
	b.mu.Unlock()

	// A zero DelegateConfig is load-bearing: it guarantees no inherited
	// permission mode, no --max-turns, and above all SkipPermissions=false, so
	// an agent backend can never emit --dangerously-skip-permissions.
	return BuildArgs(DelegateConfig{}, DelegateRequest{
		Prompt:             prompt,
		Model:              b.cfg.Model,
		AllowedTools:       b.cfg.AllowedTools,
		Resume:             resume,
		AppendSystemPrompt: b.cfg.SystemPrompt,
		Settings:           settings,
		MCPConfig:          b.cfg.MCPConfig,
	}, b.cfg.SessionID), nil
}

// newestUserMessage returns the newest user turn. Claude Code owns the rest.
//
// A blank newest turn is an ERROR, never a reason to fall further back:
// replaying an older message would silently re-send a prompt the user already
// answered, which is worse than sending nothing at all.
func newestUserMessage(msgs []backend.Message) (string, error) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		if strings.TrimSpace(msgs[i].Content) == "" {
			return "", fmt.Errorf("claudecode agent: newest user message is empty")
		}
		return msgs[i].Content, nil
	}
	return "", fmt.Errorf("claudecode agent: no user message to send")
}

// appendExecutedTools collects tool_use blocks from assistant lines and fills
// their results from the following user line's tool_result blocks.
func appendExecutedTools(acc []backend.ExecutedTool, raw []byte) []backend.ExecutedTool {
	var sl streamLine
	if err := json.Unmarshal(raw, &sl); err != nil {
		return acc
	}
	for _, blk := range sl.Message.Content {
		switch blk.Type {
		case "tool_use":
			acc = append(acc, backend.ExecutedTool{
				Call: backend.ToolCall{
					ID:       blk.ID,
					Function: backend.ToolCallFunction{Name: blk.Name, Arguments: blk.Input},
				},
			})
		case "tool_result":
			// An empty tool_use_id correlates to nothing; without this guard it
			// matches the first call that also has an empty id and attaches the
			// wrong output. mapper.go's applyToolResults guards it the same way.
			if blk.ToolUseID == "" {
				continue
			}
			for i := range acc {
				if acc[i].Call.ID == blk.ToolUseID {
					acc[i].Result = rawToString(blk.Content)
					// Ids are unique; stop so a duplicate id cannot fan one
					// result out across several calls.
					break
				}
			}
		}
	}
	return acc
}

// tailBuffer is an io.Writer keeping only the last max bytes written to it.
type tailBuffer struct {
	max       int
	buf       []byte
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if t.max <= 0 {
		return n, nil
	}
	if len(p) >= t.max {
		t.truncated = true
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return n, nil
	}
	if len(t.buf)+len(p) > t.max {
		drop := len(t.buf) + len(p) - t.max
		t.truncated = true
		t.buf = t.buf[:copy(t.buf, t.buf[drop:])]
	}
	t.buf = append(t.buf, p...)
	return n, nil
}

func (t *tailBuffer) String() string {
	s := strings.TrimSpace(string(t.buf))
	if s == "" {
		return ""
	}
	if t.truncated {
		return "...(truncated) " + s
	}
	return s
}
