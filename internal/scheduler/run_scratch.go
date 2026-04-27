// Package scheduler — run scratchpad helpers (Phase 2).
//
// The run scratchpad is a per-run key/value store that any step can read via
// `{{run.scratch.KEY}}` placeholders and any agent can write via the
// `set_scratch` PromptTool. It exists ONLY for the duration of one workflow
// run and is never persisted, so it's safe for transient bookkeeping
// (counters, control-flow flags, intermediate values) without touching the
// notification or memory subsystems.
//
// The runner places a writer callback on each step's context using
// WithScratchSetter. Tools can fetch it with ScratchSetter and call setter(k,v)
// to mutate the live run scratchpad.
package scheduler

import "context"

// scratchKey is the context key for the per-run scratchpad writer.
type scratchKey struct{}

// initialInputsKey is the context key for trigger-supplied initial scratch
// values. The runner reads it once at startup and seeds runScratch from it,
// so manual triggers and webhook payloads can supply variables to the very
// first step's prompt without needing a real predecessor step.
type initialInputsKey struct{}

// WithInitialInputs returns a derived context that carries the initial inputs
// map for the run. Nil/empty maps are accepted and behave like a no-op.
func WithInitialInputs(ctx context.Context, inputs map[string]string) context.Context {
	if len(inputs) == 0 {
		return ctx
	}
	cp := make(map[string]string, len(inputs))
	for k, v := range inputs {
		cp[k] = v
	}
	return context.WithValue(ctx, initialInputsKey{}, cp)
}

// initialInputs returns the trigger-supplied initial inputs (read-only) or
// nil. The runner uses this to seed runScratch. Internal-only — external
// callers should use InitialInputs (exported).
func initialInputs(ctx context.Context) map[string]string {
	return InitialInputs(ctx)
}

// InitialInputs returns the trigger-supplied initial inputs (read-only) or
// nil. Exported so HTTP-handler tests can verify the round-trip from
// request body → context without reaching into unexported helpers.
// Callers MUST NOT mutate the returned map.
func InitialInputs(ctx context.Context) map[string]string {
	v, _ := ctx.Value(initialInputsKey{}).(map[string]string)
	return v
}

// ScratchSetterFunc updates the live run scratchpad in a thread-safe manner.
// Returns nil on success; an error indicates the runner has finished and the
// setter is no longer valid (e.g. the run was cancelled).
type ScratchSetterFunc func(key, value string) error

// WithScratchSetter returns a derived context that exposes a writer for the
// run scratchpad. Pass nil to clear the writer (defensive — mostly for tests).
func WithScratchSetter(ctx context.Context, fn ScratchSetterFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, scratchKey{}, fn)
}

// ScratchSetter retrieves the writer placed by WithScratchSetter, or nil if
// none was set. Tools should treat a nil result as "scratchpad unavailable in
// this context" (e.g. running outside a workflow) and skip the write rather
// than panic.
func ScratchSetter(ctx context.Context) ScratchSetterFunc {
	v, _ := ctx.Value(scratchKey{}).(ScratchSetterFunc)
	return v
}
