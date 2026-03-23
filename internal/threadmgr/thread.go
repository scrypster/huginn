package threadmgr

import (
	"fmt"
	"time"
)

// ThreadStatus represents the lifecycle state of a thread.
type ThreadStatus string

const (
	StatusQueued    ThreadStatus = "queued"
	StatusThinking  ThreadStatus = "thinking"
	StatusTooling   ThreadStatus = "tooling"
	StatusDone      ThreadStatus = "done"
	StatusBlocked   ThreadStatus = "blocked"
	StatusCancelled ThreadStatus = "cancelled"
	StatusError     ThreadStatus = "error"
)

// Thread represents one delegated task running as a goroutine.
type Thread struct {
	ID              string
	SessionID       string
	AgentID         string
	Task            string
	Rationale       string         // why this agent was chosen (optional, from lead agent)
	ParentMessageID string         // ID of the chat message that triggered this thread (for frontend linkage)
	Status          ThreadStatus
	DependsOn       []string       // resolved thread IDs that must complete first
	DependsOnHints  []string       // LLM-provided agent name hints, resolved by ResolveDependencies
	StartedAt       time.Time
	CompletedAt     time.Time
	CreatedAt       time.Time      // when the thread was registered (monotonic)
	CreatedByUser   string         // actor that requested this thread (e.g. "primary-agent", user ID)
	CreatedReason   string         // free-text rationale for why this thread was created
	ArchivedAt      *time.Time     // non-nil when the thread has been archived
	Summary         *FinishSummary // set on completion
	TokensUsed      int
	TokenBudget     int            // 0 = unlimited
	Timeout         time.Duration  // 0 = no timeout; when > 0, the goroutine is killed after this duration
	cancel          func()         // non-nil after Start()
	InputCh         chan string     // receives human input when status == blocked
}

// AuditEntry is a single immutable record in the ThreadManager audit log.
// Entries are appended on every significant lifecycle event (create, complete, cancel, etc.)
// and bounded to maxAuditEntries via a ring-buffer strategy.
type AuditEntry struct {
	At       time.Time // when the event occurred
	ThreadID string    // thread that the event applies to
	Action   string    // "created" | "completed" | "error" | "timeout" | "cancelled" | "archived"
	Actor    string    // who triggered the event (CreatedByUser or "")
	Reason   string    // optional free-text reason
}

// FinishSummary is the structured output from a completed thread.
type FinishSummary struct {
	Summary       string   // human-readable narrative
	FilesModified []string // paths written
	KeyDecisions  []string // important choices made
	Artifacts     []string // references to outputs
	Status        string   // "completed" | "blocked" | "needs_review" | "completed-with-timeout" | "error"
}

// CreateParams holds arguments for ThreadManager.Create.
type CreateParams struct {
	SessionID       string
	AgentID         string
	Task            string
	Rationale       string        // why this agent was chosen (optional)
	ParentMessageID string        // chat message that triggered this thread (for frontend thread linkage)
	DependsOnHints  []string      // agent name hints from LLM (resolved lazily by ResolveDependencies)
	DependsOn       []string      // explicit thread ID dependencies (skip resolution)
	TokenBudget     int           // 0 = unlimited
	Timeout         time.Duration // 0 = no timeout; when > 0, the goroutine is killed after this duration
	SpaceID         string        // if set, AgentID must be a member of this space
	CreatedByUser   string        // actor that requested this thread (e.g. "primary-agent", user ID)
	CreatedReason   string        // free-text rationale for why this thread was created
}

// checkTokenBudget returns an error if the thread identified by threadID has
// exceeded its per-thread token budget. When TokenBudget is 0, the budget is
// unlimited and this always returns nil.
func checkTokenBudget(tm *ThreadManager, threadID string) error {
	tm.mu.Lock()
	t, ok := tm.threads[threadID]
	tm.mu.Unlock()
	if !ok || t.TokenBudget == 0 {
		return nil
	}
	if t.TokensUsed >= t.TokenBudget {
		return fmt.Errorf("token budget exhausted: used %d of %d tokens", t.TokensUsed, t.TokenBudget)
	}
	return nil
}
