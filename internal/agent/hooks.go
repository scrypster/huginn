package agent

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/scrypster/huginn/internal/tools"
)

// PreToolUseHook runs after the permission gate and before OnBeforeWrite, for
// every tool call (serial or concurrent). Returning allow=false vetoes the
// tool call entirely — it never reaches tool.Execute — and denyReason is
// surfaced to the model as an honest tool result ("<tool> blocked: <reason>").
//
// dispatchTools runs independent tool calls concurrently, so a PreToolUseHook
// may be invoked from many goroutines at once for different tool calls. Hooks
// receive a defensive shallow copy of args (never the live map backing the
// tool call), so they cannot see or cause cross-call interference by mutating
// it. Any state a hook keeps across calls (counters, caches, pending-warning
// tables, ...) MUST be its own responsibility to guard — e.g. sync.Map or an
// internal mutex — the registry itself does not serialize hook execution.
type PreToolUseHook func(ctx context.Context, toolName string, args map[string]any) (allow bool, denyReason string)

// PostToolUseHook observes every tool call that actually ran (including ones
// that returned a tool-level error), firing alongside OnToolDone, before the
// credential-rewrite/truncation pass. It receives a defensive shallow copy of
// args and a pointer to the tool's result so a hook may append information
// (e.g. a non-blocking warning) to Output — the pointer is unique to this
// call's goroutine, never shared, so mutating it is race-safe. Hooks that
// only want to observe should treat it as read-only.
//
// Same concurrency contract as PreToolUseHook: may run on many goroutines at
// once, one per in-flight independent tool call.
type PostToolUseHook func(ctx context.Context, toolName string, args map[string]any, result *tools.ToolResult)

// HookRegistry is the internal (not user-configurable) PreToolUse/PostToolUse
// hook chain that the harness populates at startup. It is the substrate that
// groundings like edit-time syntax validation (G1) attach to via
// RegisterPreToolUse / RegisterPostToolUse. Safe for concurrent registration
// and concurrent Run* calls.
type HookRegistry struct {
	mu   sync.RWMutex
	pre  []PreToolUseHook
	post []PostToolUseHook
}

// NewHookRegistry returns an empty, ready-to-use hook registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{}
}

// RegisterPreToolUse appends a PreToolUseHook to the chain. Hooks run in
// registration order; the first denial short-circuits the rest.
func (r *HookRegistry) RegisterPreToolUse(h PreToolUseHook) {
	if h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pre = append(r.pre, h)
}

// RegisterPostToolUse appends a PostToolUseHook to the chain. All registered
// post hooks run, in registration order, for every completed tool call.
func (r *HookRegistry) RegisterPostToolUse(h PostToolUseHook) {
	if h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.post = append(r.post, h)
}

// runPre evaluates all registered PreToolUse hooks for one tool call. The
// first hook to deny wins; later hooks do not run. A nil registry (or one
// with no pre hooks) always allows.
func (r *HookRegistry) runPre(ctx context.Context, toolName string, args map[string]any) (allow bool, denyReason string) {
	if r == nil {
		return true, ""
	}
	r.mu.RLock()
	hooks := make([]PreToolUseHook, len(r.pre))
	copy(hooks, r.pre)
	r.mu.RUnlock()

	if len(hooks) == 0 {
		return true, ""
	}
	view := copyArgs(args)
	for _, h := range hooks {
		if h == nil {
			continue
		}
		ok, reason := h(ctx, toolName, view)
		if !ok {
			return false, reason
		}
	}
	return true, ""
}

// runPost fires every registered PostToolUse hook for one completed tool
// call. A nil registry (or one with no post hooks) is a no-op.
func (r *HookRegistry) runPost(ctx context.Context, toolName string, args map[string]any, result *tools.ToolResult) {
	if r == nil || result == nil {
		return
	}
	r.mu.RLock()
	hooks := make([]PostToolUseHook, len(r.post))
	copy(hooks, r.post)
	r.mu.RUnlock()

	if len(hooks) == 0 {
		return
	}
	view := copyArgs(args)
	for _, h := range hooks {
		if h != nil {
			h(ctx, toolName, view, result)
		}
	}
}

// copyArgs returns a shallow copy of args so hooks can never mutate the
// live map a concurrent goroutine is about to pass into tool.Execute.
func copyArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

// argsKey builds a deterministic key for one tool call's (toolName, args)
// pair. encoding/json.Marshal sorts map keys, so identical args always
// produce the same key regardless of map iteration order — used by hooks
// (e.g. syntax-validation warn mode) that must correlate their own
// PreToolUse and PostToolUse invocations for the same call.
func argsKey(toolName string, args map[string]any) string {
	b, err := json.Marshal(args)
	if err != nil {
		// Extremely unlikely (args come from decoded tool-call JSON already);
		// fall back to a key that just won't collide with a real one.
		return toolName + "|<unmarshalable>"
	}
	return toolName + "|" + string(b)
}
