package threadmgr

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/session"
)

// DelegationRequest represents a single parsed @AgentName mention.
type DelegationRequest struct {
	AgentName string // canonical name from the registry
	Task      string // full original user message
}

// NOTE(localhost-app): maxMentionsPerMessage caps how many @agent mentions are
// processed from a single message. This is a usability guard — not a DoS defence —
// since Huginn binds exclusively to 127.0.0.1. It prevents log-spam from messages
// that accidentally contain many @mentions (e.g. a pasted email thread).
const maxMentionsPerMessage = 10

// ErrTooManyMentions is returned by ParseMentions (via the caller) when a
// message contains more unique @agent mentions than maxMentionsPerMessage.
var ErrTooManyMentions = errors.New("threadmgr: too many @mentions in one message (max 10)")

// MentionRe matches @Word at the start of the string or after whitespace/punctuation.
// This prevents false positives on email-style addresses (alice@Bob) and requires
// mentions to appear as standalone tokens. \b enforces a word boundary so @StacyFoo
// does not match agent "Stacy".
// Exported so callers can build consistent exclusion sets without duplicating the pattern.
var MentionRe = regexp.MustCompile(`(?:^|[\s,;:!?])@([\w-]+)\b`)

// mentionRe is the package-internal alias kept for backward compat with existing callers.
var mentionRe = MentionRe

// ParseMentions scans msg for @AgentName patterns and returns a DelegationRequest
// for each mention that matches a known agent name (case-insensitive).
// Duplicate mentions of the same agent produce a single request.
// The Task field is always set to the full original message.
// At most maxMentionsPerMessage unique mentions are returned; excess mentions are
// silently truncated with a warning log.
func ParseMentions(msg string, agentNames []string) []DelegationRequest {
	if msg == "" || len(agentNames) == 0 {
		return nil
	}

	// Build case-insensitive lookup: lowercase → canonical
	canonical := make(map[string]string, len(agentNames))
	for _, name := range agentNames {
		canonical[strings.ToLower(name)] = name
	}

	matches := mentionRe.FindAllStringSubmatch(msg, -1)
	seen := make(map[string]bool)
	var requests []DelegationRequest
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawName := m[1]
		lower := strings.ToLower(rawName)
		canon, found := canonical[lower]
		if !found || seen[canon] {
			continue
		}
		seen[canon] = true
		requests = append(requests, DelegationRequest{
			AgentName: canon,
			Task:      msg,
		})
	}

	if len(requests) > maxMentionsPerMessage {
		logger.Warn("threadmgr: mention cap reached, truncating", "total", len(requests), "cap", maxMentionsPerMessage)
		requests = requests[:maxMentionsPerMessage]
	}

	return requests
}

// CreateFromMentions parses @AgentName mentions in userMsg, creates a thread for
// each matching agent, and spawns them if ready. Used for low-tier primary agents
// that cannot call delegate_to_agent as a tool.
// parentMsgID, when non-empty, is the session message ID of the triggering message
// so thread replies are linked back and visible in the thread panel.
func CreateFromMentions(
	ctx context.Context,
	sessionID string,
	userMsg string,
	parentMsgID string,
	reg *agents.AgentRegistry,
	store session.StoreInterface,
	sess *session.Session,
	b backend.Backend,
	broadcast BroadcastFn,
	ca *CostAccumulator,
	tm *ThreadManager,
) {
	// Collect canonical names from the registry.
	all := reg.All()
	names := make([]string, 0, len(all))
	for _, ag := range all {
		names = append(names, ag.Name)
	}

	var spaceID string
	if sess != nil {
		spaceID = sess.SpaceID()
	}
	logger.Info("CreateFromMentions", "session_id", sessionID, "msg", userMsg, "known_agents", names)
	requests := ParseMentions(userMsg, names)
	logger.Info("CreateFromMentions: parsed", "requests", len(requests))
	for _, req := range requests {
		t, err := tm.Create(CreateParams{
			SessionID:       sessionID,
			AgentID:         req.AgentName,
			Task:            req.Task,
			ParentMessageID: parentMsgID,
			SpaceID:         spaceID,
		})
		if err != nil {
			logger.Warn("CreateFromMentions: create failed", "agent", req.AgentName, "err", err)
			continue
		}
		if tm.IsReady(t.ID) {
			tid := t.ID
			dagFn := func() {
				tm.EvaluateDAG(ctx, sessionID, store, sess, reg, b, broadcast, ca)
			}
			tm.SpawnThread(ctx, tid, store, sess, reg, b, broadcast, ca, dagFn)
		}
	}
}
