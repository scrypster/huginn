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

// MentionRe matches @Word at the start of the string or after whitespace/
// punctuation/markdown wrappers, and ending at end-of-string or another
// non-name character.
//
// The leading and trailing character classes are intentionally permissive
// about characters LLMs commonly emit around mentions in chat responses:
// bold (**, __), italic (*, _), inline code (`), parentheses (()), brackets
// ([]), curly braces ({}), and the standard punctuation/whitespace.
//
// We can't use `\b` for the trailing boundary because `_` belongs to `\w` and
// `_@Bob_` (italic) would fail — `_` after `b` is not a word boundary. We
// also can't use a lookahead (Go's RE2 has no lookaround). Instead the
// trailing class explicitly matches one boundary char (or end-of-string).
//
// Email-style addresses are still rejected: alice@Bob has `e` (a name char)
// immediately before `@`, which is not in the leading boundary class.
var MentionRe = regexp.MustCompile("(?:^|[\\s,;:!?.*_`~()\\[\\]{}'\"<>])@([\\w-]+?)(?:[\\s,;:!?.*_`~()\\[\\]{}'\"<>]|$)")

// mentionRe is the package-internal alias kept for backward compat with existing callers.
var mentionRe = MentionRe

// ParseMentions scans msg for @AgentName patterns and returns a DelegationRequest
// for each mention that matches a known agent name (case-insensitive).
// Duplicate mentions of the same agent produce a single request.
// The Task field is always set to the full original message.
// At most maxMentionsPerMessage unique mentions are returned; excess mentions are
// silently truncated with a warning log.
func ParseMentions(msg string, agentNames []string) []DelegationRequest {
	reqs, _ := parseMentionsInternal(msg, agentNames)
	return reqs
}

// ParseMentionsWithUnknown is like ParseMentions but additionally returns the
// list of @-tokens that were syntactically valid mentions but did not match
// any agent in the registry (case-insensitive). Useful for diagnostic logging
// and for surfacing "hallucinated" agent names to the UI so users see why a
// delegation didn't fire.
func ParseMentionsWithUnknown(msg string, agentNames []string) (matched []DelegationRequest, unknown []string) {
	return parseMentionsInternal(msg, agentNames)
}

func parseMentionsInternal(msg string, agentNames []string) ([]DelegationRequest, []string) {
	if msg == "" {
		return nil, nil
	}

	canonical := make(map[string]string, len(agentNames))
	for _, name := range agentNames {
		canonical[strings.ToLower(name)] = name
	}

	matches := mentionRe.FindAllStringSubmatch(msg, -1)
	seen := make(map[string]bool)
	seenUnknown := make(map[string]bool)
	var requests []DelegationRequest
	var unknown []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawName := m[1]
		lower := strings.ToLower(rawName)
		canon, found := canonical[lower]
		if !found {
			if !seenUnknown[lower] {
				seenUnknown[lower] = true
				unknown = append(unknown, rawName)
			}
			continue
		}
		if seen[canon] {
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

	return requests, unknown
}

// CreateFromMentions parses @AgentName mentions in userMsg, creates a thread for
// each matching agent, and spawns them if ready. Used for low-tier primary agents
// that cannot call delegate_to_agent as a tool.
// parentMsgID, when non-empty, is the session message ID of the triggering message
// so thread replies are linked back and visible in the thread panel.
// callerAgent, when non-empty, is the name of the agent that produced userMsg.
// Mentions of the caller agent are skipped (self-delegation guard).
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
	callerAgent string,
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
	logger.Info("CreateFromMentions", "session_id", sessionID, "msg", userMsg, "known_agents", names, "caller", callerAgent)
	requests, unknown := ParseMentionsWithUnknown(userMsg, names)
	logger.Info("CreateFromMentions: parsed", "requests", len(requests), "unknown", unknown)

	// Surface unknown @-mentions both in logs (so operators can detect prompt
	// regressions where the orchestrator hallucinates agent names) and via a
	// WS event so the UI can render an explanatory badge instead of leaving
	// the user wondering why nothing happened (issue #3).
	if len(unknown) > 0 {
		logger.Warn("CreateFromMentions: unknown @-mentions in agent reply",
			"session_id", sessionID, "caller", callerAgent,
			"unknown", unknown, "known_agents", names)
		if broadcast != nil {
			broadcast(sessionID, "delegation_warning", map[string]any{
				"session_id":    sessionID,
				"parent_msg_id": parentMsgID,
				"caller":        callerAgent,
				"unknown":       unknown,
				"known_agents":  names,
				"reason":        "unknown_agent",
			})
		}
	}

	if len(requests) == 0 {
		logger.Warn("CreateFromMentions: no valid mentions resolved",
			"session_id", sessionID, "caller", callerAgent,
			"unknown", unknown, "raw_msg_len", len(userMsg))
	}
	for i, req := range requests {
		logger.Info("CreateFromMentions: processing mention",
			"index", i, "agent", req.AgentName, "caller", callerAgent,
			"session_id", sessionID, "space_id", spaceID,
			"parent_msg_id", parentMsgID, "ctx_err", ctx.Err())

		// Skip self-delegation: do not create threads for the caller agent mentioning itself.
		if callerAgent != "" && strings.EqualFold(req.AgentName, callerAgent) {
			logger.Info("CreateFromMentions: skipping self-delegation", "agent", req.AgentName, "caller", callerAgent)
			continue
		}

		t, err := tm.Create(CreateParams{
			SessionID:       sessionID,
			AgentID:         req.AgentName,
			Task:            req.Task,
			ParentMessageID: parentMsgID,
			SpaceID:         spaceID,
		})
		if err != nil {
			logger.Warn("CreateFromMentions: create FAILED", "agent", req.AgentName, "err", err,
				"session_id", sessionID, "space_id", spaceID)
			continue
		}
		ready := tm.IsReady(t.ID)
		logger.Info("CreateFromMentions: thread created",
			"thread_id", t.ID, "agent", req.AgentName, "ready", ready,
			"status", t.Status, "parent_msg_id", t.ParentMessageID)
		if ready {
			tid := t.ID
			logger.Info("CreateFromMentions: about to SpawnThread",
				"thread_id", tid, "ctx_err", ctx.Err())
			dagFn := func() {
				tm.EvaluateDAG(ctx, sessionID, store, sess, reg, b, broadcast, ca)
			}
			tm.SpawnThread(ctx, tid, store, sess, reg, b, broadcast, ca, dagFn)
			logger.Info("CreateFromMentions: SpawnThread returned", "thread_id", tid)
		}
	}
}
