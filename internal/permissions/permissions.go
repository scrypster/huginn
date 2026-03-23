package permissions

import (
	"container/list"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// relayEntry pairs a response channel with the wall-clock time it was
// registered. The createdAt timestamp is used by the periodic sweep to evict
// entries whose contexts were never cancelled and whose responses never arrived
// (e.g. the network dropped before HuginnCloud could deliver the decision).
type relayEntry struct {
	ch        chan bool
	createdAt time.Time
}

// promptFuncTimeout is the maximum time we wait for a promptFunc to return
// before treating the request as denied (safe default). Declared as a var so
// package-internal tests can override it without the 30s wait.
var promptFuncTimeout = 30 * time.Second

const (
	// sessionAllowedMaxEntries caps how many tool names can accumulate in sessionAllowed.
	// When the cap is reached, the least-recently-used entries are evicted (true LRU).
	sessionAllowedMaxEntries = 1000

	// maxSessionAllowed is an alias kept for backwards compatibility with existing code.
	maxSessionAllowed = sessionAllowedMaxEntries
)

// Decision is the user's response to a permission prompt.
type Decision int

const (
	Allow     Decision = iota // Allow this one time
	AllowOnce                 // Alias for Allow
	AllowAll                  // Always allow this tool for this session
	Deny                      // Deny this call
)

// PermissionRequest describes what the model wants to do.
type PermissionRequest struct {
	ToolName string
	Level    tools.PermissionLevel
	Args     map[string]any
	Summary  string // human-readable one-liner
	Provider string // provider tag from tool registry; empty if untagged
}

// Gate controls whether tool calls proceed.
type Gate struct {
	mu               sync.Mutex
	skipAll          bool            // --dangerously-skip-permissions
	watchedProviders map[string]bool // prompt for these even in skipAll mode
	allowedProviders map[string]bool // nil = all providers allowed; non-nil = toolbelt restriction
	sessionAllowed   map[string]bool // tool name → always allow this session
	sessionOrder     []string        // kept for backwards compat; unused when lruList is non-nil
	// lruList is the doubly-linked list for true LRU eviction (front = MRU, back = LRU).
	lruList *list.List
	// lruItems maps tool name → *list.Element for O(1) touch/eviction.
	lruItems   map[string]*list.Element
	promptFunc func(PermissionRequest) Decision

	// relayChans holds in-flight relay permission requests.
	// Each entry pairs the response channel with its registration time so the
	// background sweep can evict entries that are older than the sweep threshold
	// even if their context was never cancelled (e.g. network-level drops where
	// HuginnCloud never delivers a response).
	relayChans map[string]relayEntry // requestID → entry

	// sweepDone is closed by Close() to stop the background sweep goroutine.
	sweepDone chan struct{}
	// sweepOnce ensures the sweep goroutine is started exactly once.
	sweepOnce sync.Once
}

// NewGate creates a Gate.
// If skipAll is true, all tools are allowed without prompting.
// promptFunc is called for tools that need user approval.
//
// NewGate starts a background sweep goroutine that evicts relay-permission
// entries older than promptFuncTimeout*3. This is a second line of defence
// against memory leaks: the primary cleanup is the context-based goroutine in
// RegisterRelayResponse, but if the context is never cancelled (e.g. a
// context.Background() passed by a caller) the sweep ensures eventual cleanup.
// Call Gate.Close() when the gate is no longer needed to stop the goroutine.
func NewGate(skipAll bool, promptFunc func(PermissionRequest) Decision) *Gate {
	g := &Gate{
		skipAll:          skipAll,
		watchedProviders: make(map[string]bool),
		sessionAllowed:   make(map[string]bool),
		lruList:          list.New(),
		lruItems:         make(map[string]*list.Element),
		promptFunc:       promptFunc,
		relayChans:       make(map[string]relayEntry),
		sweepDone:        make(chan struct{}),
	}
	g.startSweep()
	return g
}

// startSweep launches the background goroutine that periodically removes stale
// relay-permission entries. It is idempotent (sync.Once guards it) so that Fork
// can also call it safely.
func (g *Gate) startSweep() {
	g.sweepOnce.Do(func() {
		// Capture once so the goroutine never races against test code that
		// mutates the package-level promptFuncTimeout variable.
		timeout := promptFuncTimeout
		go func() {
			// Sweep interval: every timeout*2 so stale entries are
			// removed within timeout*3 of registration (worst case).
			ticker := time.NewTicker(timeout * 2)
			defer ticker.Stop()
			for {
				select {
				case <-g.sweepDone:
					return
				case <-ticker.C:
					g.sweepRelayChans(timeout)
				}
			}
		}()
	})
}

// sweepRelayChans removes relay entries whose registration time is older than
// timeout*3. Each evicted channel receives false (deny) so that any
// goroutine blocking on it is unblocked. Caller must NOT hold g.mu.
func (g *Gate) sweepRelayChans(timeout time.Duration) {
	threshold := timeout * 3
	now := time.Now()

	g.mu.Lock()
	var stale []relayEntry
	for id, entry := range g.relayChans {
		if now.Sub(entry.createdAt) > threshold {
			stale = append(stale, entry)
			delete(g.relayChans, id)
		}
	}
	g.mu.Unlock()

	// Send deny outside the lock so we never block the mutex.
	for _, entry := range stale {
		select {
		case entry.ch <- false:
		default:
			// Channel already had a value (race with DeliverRelayResponse or
			// the context goroutine); nothing to do.
		}
	}
}

// Close stops the background sweep goroutine and drains all pending relay
// channels by sending false (deny). After Close returns, no goroutines are
// blocked waiting on relayChans entries created by this Gate.
// Safe to call multiple times (idempotent after the first call).
func (g *Gate) Close() {
	// Close sweepDone exactly once. Use a non-blocking select to be idempotent.
	select {
	case <-g.sweepDone:
		// already closed
	default:
		close(g.sweepDone)
	}

	// Drain all remaining relay entries, unblocking any waiting goroutines.
	g.mu.Lock()
	remaining := g.relayChans
	g.relayChans = make(map[string]relayEntry)
	g.mu.Unlock()

	for _, entry := range remaining {
		select {
		case entry.ch <- false:
		default:
		}
	}
}

// SetWatchedProviders configures providers for which write tools always require
// approval, even when skipAll is true. Pass nil to clear all watched providers.
func (g *Gate) SetWatchedProviders(providers map[string]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if providers == nil {
		g.watchedProviders = make(map[string]bool)
	} else {
		g.watchedProviders = providers
	}
}

// SetAllowedProviders configures the set of connection providers whose tools
// this gate will allow. Pass nil to allow all providers (no toolbelt restriction).
func (g *Gate) SetAllowedProviders(providers map[string]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.allowedProviders = providers
}

// Fork creates a new Gate that inherits skipAll, promptFunc, and a snapshot of
// sessionAllowed from g, but uses the provided watchedProviders and
// allowedProviders instead of mutating g. Fork is safe to call concurrently.
//
// Use Fork when you need per-agent-run gate isolation — concurrent agent runs
// each receive their own forked gate, eliminating the race condition that
// arises from sharing a single mutable gate across concurrent applyToolbelt
// calls.
func (g *Gate) Fork(watchedProviders, allowedProviders map[string]bool) *Gate {
	g.mu.Lock()
	sessionCopy := make(map[string]bool, len(g.sessionAllowed))
	for k, v := range g.sessionAllowed {
		sessionCopy[k] = v
	}
	skipAll := g.skipAll
	promptFunc := g.promptFunc
	// Copy LRU order: iterate front-to-back (MRU to LRU).
	newList := list.New()
	newItems := make(map[string]*list.Element, g.lruList.Len())
	for e := g.lruList.Front(); e != nil; e = e.Next() {
		name := e.Value.(string)
		newItems[name] = newList.PushBack(name)
	}
	// Re-insert front element at front to preserve MRU position.
	// list.New() + PushBack preserves original order (front = MRU stays front).
	g.mu.Unlock()

	if watchedProviders == nil {
		watchedProviders = make(map[string]bool)
	}
	child := &Gate{
		skipAll:          skipAll,
		watchedProviders: watchedProviders,
		allowedProviders: allowedProviders,
		sessionAllowed:   sessionCopy,
		lruList:          newList,
		lruItems:         newItems,
		promptFunc:       promptFunc,
		relayChans:       make(map[string]relayEntry),
		sweepDone:        make(chan struct{}),
	}
	child.startSweep()
	return child
}

// NewRelayRequestID generates a cryptographically random 16-byte hex string
// suitable for use as a permission request ID. Using unguessable IDs prevents
// a rogue relay server from pre-computing IDs and approving arbitrary requests.
func NewRelayRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("permissions: crypto/rand unavailable: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// RegisterRelayResponse registers a channel that will receive the decision for
// a permission request identified by requestID. The channel must be buffered
// with capacity >= 1 so that DeliverRelayResponse never blocks.
//
// ctx controls the lifetime of the registration. When ctx is cancelled or times
// out, the entry is automatically removed and false (deny) is sent to ch so the
// waiting goroutine is never stuck. This prevents the relayChans map from
// accumulating entries if HuginnCloud never delivers a response (e.g. network
// drop, server restart).
//
// A second cleanup path exists in the background sweep goroutine (started by
// NewGate / Fork) which removes entries older than promptFuncTimeout*3 even if
// the caller passed a context that is never cancelled. Gate.Close() provides a
// final synchronous drain that releases all remaining entries.
func (g *Gate) RegisterRelayResponse(ctx context.Context, requestID string, ch chan bool) {
	g.mu.Lock()
	g.relayChans[requestID] = relayEntry{ch: ch, createdAt: time.Now()}
	g.mu.Unlock()

	// Primary auto-cleanup: deny when context expires so callers never block.
	go func() {
		<-ctx.Done()
		g.mu.Lock()
		_, still := g.relayChans[requestID]
		if still {
			delete(g.relayChans, requestID)
		}
		g.mu.Unlock()
		if still {
			ch <- false // deny — timeout or cancellation
		}
	}()
}

// DeliverRelayResponse sends approved to the channel registered for requestID
// and removes the entry so it cannot be delivered twice. It returns true if
// the requestID was known, false otherwise.
func (g *Gate) DeliverRelayResponse(requestID string, approved bool) bool {
	g.mu.Lock()
	entry, ok := g.relayChans[requestID]
	if ok {
		delete(g.relayChans, requestID)
	}
	g.mu.Unlock()
	if !ok {
		return false
	}
	entry.ch <- approved
	return true
}

// Check returns true if the tool call should proceed.
// PermRead tools are always allowed unless blocked by provider restriction.
// PermWrite/PermExec respect skipAll, sessionAllowed, and promptFunc.
func (g *Gate) Check(req PermissionRequest) bool {
	// Toolbelt enforcement: reject calls from providers not in the allowed set.
	// Only applies when allowedProviders is non-nil (agent has an explicit toolbelt)
	// and req.Provider is non-empty (connection tool, not an internal tool).
	if req.Provider != "" {
		g.mu.Lock()
		allowed := g.allowedProviders
		g.mu.Unlock()
		if allowed != nil && !allowed[req.Provider] {
			return false
		}
	}
	// Read-only tools are always allowed
	if req.Level == tools.PermRead {
		return true
	}
	// --dangerously-skip-permissions bypasses everything UNLESS the provider
	// is in watchedProviders (per-connection approval gate).
	if g.skipAll {
		g.mu.Lock()
		watched := g.watchedProviders[req.Provider]
		g.mu.Unlock()
		if !watched {
			return true
		}
		// Fall through to prompt for watched providers
	}
	g.mu.Lock()
	// Check session allow-list
	if g.sessionAllowed[req.ToolName] {
		g.mu.Unlock()
		return true
	}
	g.mu.Unlock()

	// No prompt function — deny by default
	if g.promptFunc == nil {
		return false
	}

	// Call promptFunc with a timeout. If it doesn't respond within
	// promptFuncTimeout, treat as denied (safe default) and log a warning.
	type result struct{ d Decision }
	ch := make(chan result, 1)
	pf := g.promptFunc
	go func() {
		ch <- result{pf(req)}
	}()

	var decision Decision
	timer := time.NewTimer(promptFuncTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		decision = r.d
	case <-timer.C:
		slog.Warn("permissions: promptFunc timed out, denying request",
			"tool", req.ToolName, "timeout", promptFuncTimeout)
		return false
	}

	switch decision {
	case AllowAll:
		g.mu.Lock()
		if !g.sessionAllowed[req.ToolName] {
			g.sessionAllowed[req.ToolName] = true
			g.lruTouch(req.ToolName)
			// Evict LRU entry when cap is exceeded (evict exactly one entry).
			if len(g.sessionAllowed) > sessionAllowedMaxEntries {
				if back := g.lruList.Back(); back != nil {
					evicted := back.Value.(string)
					g.lruList.Remove(back)
					delete(g.lruItems, evicted)
					delete(g.sessionAllowed, evicted)
				}
			}
		} else {
			// Already in the allow-list — touch it to record recent use.
			g.lruTouch(req.ToolName)
		}
		g.mu.Unlock()
		return true
	case Allow, AllowOnce:
		return true
	default:
		return false
	}
}

// lruTouch moves toolName to the front of the LRU list (marking it as most-recently used).
// If toolName is not yet in lruItems it is added at the front.
// Caller must hold g.mu.
func (g *Gate) lruTouch(toolName string) {
	if elem, ok := g.lruItems[toolName]; ok {
		g.lruList.MoveToFront(elem)
	} else {
		elem = g.lruList.PushFront(toolName)
		g.lruItems[toolName] = elem
	}
}

// FormatRequest returns a human-readable one-liner for a permission request.
func FormatRequest(req PermissionRequest) string {
	if req.Summary != "" {
		return req.Summary
	}
	switch req.ToolName {
	case "bash":
		if cmd, ok := req.Args["command"].(string); ok {
			return fmt.Sprintf("bash: %s", truncateLine(cmd, 80))
		}
	case "write_file":
		if path, ok := req.Args["file_path"].(string); ok {
			content, _ := req.Args["content"].(string)
			return fmt.Sprintf("write_file: %s (%d bytes)", path, len(content))
		}
	case "edit_file":
		if path, ok := req.Args["file_path"].(string); ok {
			return fmt.Sprintf("edit_file: %s", path)
		}
	}
	return fmt.Sprintf("%s: %v", req.ToolName, req.Args)
}

// FormatPromptOptions returns the key hint shown in the TUI permission prompt.
func FormatPromptOptions() string {
	return "[a]llow  [d]eny  [A]lways allow for this session"
}

func truncateLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
