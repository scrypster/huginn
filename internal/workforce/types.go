package workforce

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// delegationCtxKey is the context key for storing a *DelegationContext.
type delegationCtxKey struct{}

// WithDelegationContext attaches a DelegationContext to the context.
func WithDelegationContext(ctx context.Context, dc *DelegationContext) context.Context {
	return context.WithValue(ctx, delegationCtxKey{}, dc)
}

// GetDelegationContext retrieves the DelegationContext from the context, if any.
func GetDelegationContext(ctx context.Context) *DelegationContext {
	if ctx == nil {
		return nil
	}
	dc, _ := ctx.Value(delegationCtxKey{}).(*DelegationContext)
	return dc
}

// spaceContextKey is the context key for storing a space context block (formatted string).
type spaceContextKey struct{}

// WithSpaceContext attaches a formatted space context block to the context.
// The block is a formatted string ready to append to the system prompt.
func WithSpaceContext(ctx context.Context, block string) context.Context {
	return context.WithValue(ctx, spaceContextKey{}, block)
}

// GetSpaceContext retrieves the space context block from the context, if any.
// Returns empty string if no context is attached or ctx is nil.
func GetSpaceContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	block, _ := ctx.Value(spaceContextKey{}).(string)
	return block
}

// channelRecentKey is the context key for the channel-recent summary block.
type channelRecentKey struct{}

// WithChannelRecent attaches a formatted channel-recent summary to the context.
// Used for channels only (not DMs). Contains the last few messages in the space.
func WithChannelRecent(ctx context.Context, block string) context.Context {
	return context.WithValue(ctx, channelRecentKey{}, block)
}

// GetChannelRecent retrieves the channel-recent summary from the context, if any.
// Returns empty string if ctx is nil.
func GetChannelRecent(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	block, _ := ctx.Value(channelRecentKey{}).(string)
	return block
}

// ReplicationMember is one channel participant who receives replicated memories.
type ReplicationMember struct {
	AgentName string
	VaultName string
}

// MemReplicationContext carries channel context for memory replication fan-out.
// Built once per message in ws.go and attached to the chat context.
// Only populated for channel sessions (not DMs, not non-memory sessions).
type MemReplicationContext struct {
	SpaceID   string              // ULID — used as dedup key in SQLite queue
	SpaceName string              // human-readable — used in provenance tags only
	Members   []ReplicationMember // all channel members (producing agent excluded at intercept time)
}

// replicationCtxKey is the context key for storing a *MemReplicationContext.
type replicationCtxKey struct{}

// WithReplicationContext attaches a MemReplicationContext to the context.
func WithReplicationContext(ctx context.Context, rc *MemReplicationContext) context.Context {
	return context.WithValue(ctx, replicationCtxKey{}, rc)
}

// GetReplicationContext retrieves the MemReplicationContext from the context, if any.
// Returns nil if no context is attached (non-channel session or replication not configured).
func GetReplicationContext(ctx context.Context) *MemReplicationContext {
	rc, _ := ctx.Value(replicationCtxKey{}).(*MemReplicationContext)
	return rc
}

// ArtifactKind specifies the type of artifact produced by an agent.
type ArtifactKind string

const (
	KindCodePatch      ArtifactKind = "code_patch"      // Diff/patch file for code changes
	KindDocument       ArtifactKind = "document"        // Markdown or prose document
	KindTimeline       ArtifactKind = "timeline"        // Timestamped event log
	KindStructuredData ArtifactKind = "structured_data" // JSON or CSV structured output
	KindFileBundle     ArtifactKind = "file_bundle"     // Set of files (tree + content)
)

// ArtifactStatus tracks the lifecycle of an artifact.
type ArtifactStatus string

const (
	StatusDraft      ArtifactStatus = "draft"      // Created, pending user review
	StatusAccepted   ArtifactStatus = "accepted"   // User accepted the artifact
	StatusRejected   ArtifactStatus = "rejected"   // User rejected; agent may re-draft
	StatusSuperseded ArtifactStatus = "superseded" // Replaced by a newer artifact
	StatusFailed     ArtifactStatus = "failed"     // Terminal failure state
	StatusDeleted    ArtifactStatus = "deleted"    // Soft-deleted; hidden from all queries
)

// Artifact represents a named, typed output produced by an agent task.
// Artifacts are persisted so they can be reviewed, accepted/rejected, and
// referenced in future sessions.
type Artifact struct {
	ID                  string
	Kind                ArtifactKind
	Title               string
	MimeType            string
	Content             []byte         // inline payload; nil if ContentRef is set
	ContentRef          string         // relative path under artifacts_dir for large payloads
	Metadata            map[string]any // kind-specific metadata (lines added, files changed, etc.)
	AgentName           string
	ThreadID            string
	SessionID           string
	TriggeringMessageID string
	Status              ArtifactStatus
	RejectionReason     string // populated when Status == StatusRejected
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// DelegationContext is threaded through all agent execution contexts.
// It is immutable once created — use WithDelegate to push a new agent.
// Cycle detection and depth limiting are enforced at push time.
type DelegationContext struct {
	RequestID  string
	Stack      []string // ordered agent names: [originator, ..., current]
	MaxDepth   int      // default 5, configurable per space
	Originator string
}

// WithDelegate returns a new DelegationContext with toAgent appended to the
// chain. Returns an error if toAgent is already in the chain (cycle) or if
// the chain would exceed MaxDepth.
func (dc DelegationContext) WithDelegate(toAgent string) (DelegationContext, error) {
	for _, a := range dc.Stack {
		if a == toAgent {
			return dc, fmt.Errorf("%w: %s already in chain %v", ErrDelegationCycle, toAgent, dc.Stack)
		}
	}
	if len(dc.Stack) >= dc.MaxDepth {
		return dc, fmt.Errorf("%w: depth %d", ErrDelegationDepthExceeded, dc.MaxDepth)
	}
	newStack := make([]string, len(dc.Stack)+1)
	copy(newStack, dc.Stack)
	newStack[len(dc.Stack)] = toAgent
	return DelegationContext{
		RequestID:  dc.RequestID,
		Stack:      newStack,
		MaxDepth:   dc.MaxDepth,
		Originator: dc.Originator,
	}, nil
}

// NewDelegationContext creates a root DelegationContext with the originating
// agent as the first entry in the stack.
func NewDelegationContext(requestID, originator string, maxDepth int) DelegationContext {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	return DelegationContext{
		RequestID:  requestID,
		Stack:      []string{originator},
		MaxDepth:   maxDepth,
		Originator: originator,
	}
}

// Sentinel errors for delegation and artifact operations.
var (
	ErrDelegationCycle         = errors.New("cycle detected in delegation chain")
	ErrDelegationDepthExceeded = errors.New("max delegation depth exceeded")
	ErrAgentUnavailable        = errors.New("agent unavailable for delegation")
	ErrArtifactNotFound        = errors.New("artifact not found")
)

// ValidateKind returns true if kind is a recognized ArtifactKind.
func ValidateKind(kind ArtifactKind) bool {
	switch kind {
	case KindCodePatch, KindDocument, KindTimeline, KindStructuredData, KindFileBundle:
		return true
	}
	return false
}

// ValidateStatus returns true if status is a recognized ArtifactStatus.
func ValidateStatus(status ArtifactStatus) bool {
	switch status {
	case StatusDraft, StatusAccepted, StatusRejected, StatusSuperseded, StatusFailed, StatusDeleted:
		return true
	}
	return false
}

func (k ArtifactKind) String() string   { return string(k) }
func (s ArtifactStatus) String() string { return string(s) }
