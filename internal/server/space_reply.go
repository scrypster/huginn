package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// SpaceThreadRunner runs a mentioned agent inside a Slack-style space thread.
// server.New wires RunSpaceThreadAgent (Orchestrator.ChatWithAgent). Tests
// may inject a fake so they never hit a live model.
type SpaceThreadRunner func(ctx context.Context, spaceID, parentID, agent, task string) (string, error)

// SetSpaceThreadRunner wires the in-thread @ wake runner.
func (s *Server) SetSpaceThreadRunner(fn SpaceThreadRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spaceThreadRunner = fn
}

func teammateMissingFromAgent(ag *agents.Agent, task, speech string) string {
	return agent.TeammateMissingToolFromAgent(ag, task, speech)
}

// RunSpaceThreadAgent is the production SpaceThreadRunner. It runs the named
// agent through Orchestrator.ChatWithAgent — the same loop space chat uses —
// on an ephemeral session so leftover speech cannot land as a hallway root.
// wakeSpaceThreadAgent persists the returned speech via InsertSpaceThreadMessage.
func (s *Server) RunSpaceThreadAgent(ctx context.Context, spaceID, parentID, agentName, task string) (string, error) {
	if s.orch == nil {
		return "", fmt.Errorf("space thread: orchestrator not configured")
	}
	agentName = strings.TrimSpace(agentName)
	task = strings.TrimSpace(task)
	if agentName == "" {
		return "", fmt.Errorf("space thread: agent is required")
	}
	if task == "" {
		return "", fmt.Errorf("space thread: task is required")
	}
	ag := s.resolveNamedSpaceAgent(agentName)
	if ag == nil {
		return "", fmt.Errorf("space thread: agent %q not found", agentName)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	prompt := s.spaceThreadWakePrompt(spaceID, parentID, task)
	// Classify trivial asks on the mention line, not the thread transcript.
	// BuildThreadWakePrompt prepends the parent, which made "@Winston ping"
	// miss the ping short-circuit and silently drop (SNAP-5a).
	userTurn := spaceThreadUserTurn(task, prompt)
	// Tool context gets the real hallway/space session (or a persisted
	// space-thread session with SpaceID). ChatWithAgent still uses an
	// ephemeral orch session so leftover speech cannot land as a hallway root.
	toolSID := s.spaceThreadToolSession(spaceID, strings.TrimSpace(parentID), ag.Name)
	s.ensureDelegateSession(toolSID, spaceID)
	ctx = agent.SetSessionID(ctx, toolSID)
	ctx = agent.SetSpaceID(ctx, spaceID)
	ctx = agent.SetParentMessageID(ctx, parentID)
	ctx = threadmgr.SetCallingAgent(ctx, ag.Name)
	if toolSID != "" {
		ctx = s.InjectSpaceContext(ctx, toolSID, ag)
	}
	sessionID := "space-thread-" + strings.TrimSpace(parentID) + "-" + ag.Name
	var buf strings.Builder
	var lastVisible string
	if err := s.orch.ChatWithAgent(ctx, ag, userTurn, sessionID, func(tok string) {
		buf.WriteString(tok)
		vis := spaces.ReplySpeech(buf.String())
		if vis == "" || strings.HasPrefix(strings.TrimSpace(vis), "{") {
			return
		}
		delta := vis
		if lastVisible != "" && strings.HasPrefix(vis, lastVisible) {
			delta = vis[len(lastVisible):]
		} else if lastVisible != "" && vis == lastVisible {
			return
		}
		if delta == "" {
			return
		}
		lastVisible = vis
		s.emitSpaceReplyToken(spaceID, parentID, agentName, delta)
	}, nil, nil); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// spaceThreadToolSession prefers the real hallway session bound to the space
// so delegate_to_agent / wait_for_threads land on GET /sessions/{id}/threads.
// If none exists, it returns a stable space-thread id that Create accepts
// once ensureDelegateSession persists it with SpaceID.
func (s *Server) spaceThreadToolSession(spaceID, parentID, agentName string) string {
	if s.spaceStore != nil && strings.TrimSpace(spaceID) != "" {
		if refs, err := s.spaceStore.ListSessionsForSpace(spaceID); err == nil && len(refs) > 0 {
			if id := strings.TrimSpace(refs[0].ID); id != "" {
				return id
			}
		}
	}
	return "space-thread-" + strings.TrimSpace(parentID) + "-" + strings.TrimSpace(agentName)
}

// ensureDelegateSession persists a real session with SpaceID so Load does not
// silently stub an empty-space session.
func (s *Server) ensureDelegateSession(id, spaceID string) {
	if s.store == nil || strings.TrimSpace(id) == "" {
		return
	}
	if _, err := session.LoadForDelegate(s.store, id, spaceID); err != nil {
		slog.Warn("space thread: persist delegate session", "session_id", id, "err", err)
	}
}


// spaceThreadUserTurn is the ChatWithAgent user message for an in-thread @.
// Trivial mentions (ping/time/thanks/headcount) use the mention line so the
// hallway short-circuit still fires. Everything else keeps the thread transcript.
func spaceThreadUserTurn(task, prompt string) string {
	if agent.IsTrivialAsk(task) {
		return strings.TrimSpace(task)
	}
	if strings.TrimSpace(prompt) != "" {
		return prompt
	}
	return strings.TrimSpace(task)
}

// resolveNamedSpaceAgent prefers the live orchestrator registry (production),
// then the same agentLoader / agentFromConfig path space chat uses.
func (s *Server) resolveNamedSpaceAgent(name string) *agents.Agent {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if s.orch != nil {
		if reg := s.orch.GetAgentRegistry(); reg != nil {
			if ag, ok := reg.ByName(name); ok && ag != nil {
				return ag
			}
		}
	}
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
	if err != nil || cfg == nil {
		return nil
	}
	return agentFromConfig(cfg, name)
}

// spaceThreadWakePrompt loads the Slack-thread root + prior replies and
// builds the ChatWithAgent user turn so the named agent sees the conversation
// they were pulled into, not a blank session.
func (s *Server) spaceThreadWakePrompt(spaceID, parentID, mention string) string {
	mention = strings.TrimSpace(mention)
	if s.spaceStore == nil || strings.TrimSpace(spaceID) == "" || strings.TrimSpace(parentID) == "" {
		return mention
	}
	var parent *spaces.SpaceMessage
	if p, err := s.spaceStore.GetSpaceMessage(spaceID, parentID); err == nil {
		parent = p
	}
	replies, err := s.spaceStore.ListSpaceReplies(spaceID, parentID)
	if err != nil {
		replies = nil
	}
	if built := spaces.BuildThreadWakePrompt(parent, replies, mention); built != "" {
		return built
	}
	return mention
}

func (s *Server) emitSpaceReply(spaceID, parentID string, msg *spaces.SpaceMessage, replyCount int, lastPreview string) {
	if strings.TrimSpace(spaceID) == "" {
		return
	}
	payload := map[string]any{
		"space_id":     spaceID,
		"parent_id":    parentID,
		"reply_count":  replyCount,
		"last_preview": lastPreview,
	}
	if msg != nil {
		payload["message"] = msg
	}
	s.emitSpaceScoped("space_reply", spaceID, payload)
}

func (s *Server) emitSpaceReplyToken(spaceID, parentID, agent, token string) {
	if strings.TrimSpace(spaceID) == "" || token == "" {
		return
	}
	s.emitSpaceScoped("space_reply_token", spaceID, map[string]any{
		"space_id":  spaceID,
		"parent_id": parentID,
		"agent":     agent,
		"token":     token,
	})
}

func (s *Server) emitSpaceReplyTyping(spaceID, parentID, agent string) {
	if strings.TrimSpace(spaceID) == "" {
		return
	}
	s.emitSpaceScoped("space_reply_typing", spaceID, map[string]any{
		"space_id":  spaceID,
		"parent_id": parentID,
		"agent":     agent,
	})
}

// emitAgentThinking puts hallway / desk-DM ChatWithAgent thinking on the same
// WS/relay wire as in-thread space_reply_typing so left-nav rows pulse
// (isAgentPulsing / agent-pulse) while the named agent is preparing context —
// before any tokens. parent_id is omitted so the thread drawer does not treat
// this as a wake. Empty space_id still broadcasts (desk-like session).
func (s *Server) emitAgentThinking(spaceID, sessionID, agent string, on bool) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return
	}
	event := "space_reply_typing"
	if !on {
		event = "space_reply_typing_done"
	}
	payload := map[string]any{"agent": agent}
	if sid := strings.TrimSpace(sessionID); sid != "" {
		payload["session_id"] = sid
	}
	if strings.TrimSpace(spaceID) != "" {
		s.emitSpaceScoped(event, spaceID, payload)
		return
	}
	msg := WSMessage{Type: event, SessionID: sessionID, Payload: payload}
	if s.onSpaceWS != nil {
		s.onSpaceWS(msg)
	}
	if sessionID != "" {
		s.BroadcastToSession(sessionID, event, payload)
	}
	s.BroadcastWS(msg)
	s.SendRelay(relay.Message{
		Type:      relay.MessageType(event),
		SessionID: sessionID,
		Payload:   payload,
	})
}

func (s *Server) sessionSpaceID(sessionID string) string {
	if s.store == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	sess, err := s.store.Load(sessionID)
	if err != nil || sess == nil {
		return ""
	}
	return sess.SpaceID()
}

func (s *Server) emitSpaceReplyTypingDone(spaceID, parentID, agent string, errMsg string) {
	if strings.TrimSpace(spaceID) == "" {
		return
	}
	payload := map[string]any{
		"space_id":  spaceID,
		"parent_id": parentID,
		"agent":     agent,
	}
	if errMsg != "" {
		payload["error"] = errMsg
		payload["error_human"] = agent + " hit a snag"
	}
	s.emitSpaceScoped("space_reply_typing_done", spaceID, payload)
}

func (s *Server) emitSpaceReplyMention(spaceID, parentID, preview string) {
	if strings.TrimSpace(spaceID) == "" {
		return
	}
	// Persist follow/@me so ListSpaces (TUI / HuginnCloud / Vue reload)
	// returns the same for_you rail flag. Spectator unseen stays a count.
	if marker, ok := s.spaceStore.(interface {
		MarkForYou(spaceID, viewer string) error
	}); ok {
		if err := marker.MarkForYou(spaceID, spaces.LocalViewer); err != nil {
			slog.Warn("space mention: persist for_you", "space_id", spaceID, "err", err)
		}
	}
	s.emitSpaceScoped("space_reply_mention", spaceID, map[string]any{
		"space_id":  spaceID,
		"parent_id": parentID,
		"preview":   preview,
		"for_you":   true,
	})
}

// emitSpaceScoped broadcasts to sessions in the space so a space-mode client
// hears it. Also SendRelay the same type + payload so a remote puppet on the
// HuginnCloud satellite can render thinking pulse / live replies. Cloud hub is
// a transparent pipe (unknown types ForwardToOwner). Clients not viewing that
// space must ignore it (they filter locally on payload space_id).
// Nil hub / empty space_id / nil or disconnected satellite must not panic.
//
// BroadcastToSession(ref) already reaches every wildcard client (session_id
// omitted — what every browser tab's single WS connection actually is; see
// web/src/composables/useHuginnWS.ts) as well as any client scoped to that
// exact session. Falling through to a second, unconditional BroadcastWS on
// top of that double-delivers the same message to the same live connection —
// harmless for one-shot events (an idempotent re-set of reply_count/typing
// state) but fatal for a streaming token: every space_reply_token landed
// twice on the wildcard client, rendering doubled words in the thread drawer
// live stream ("WaitingWaiting for for the the..."). BroadcastWS is now only
// the fallback for when no session exists yet to broadcast to (spaceStore
// nil, a lookup error, or a space with no session rows at all) — the one case
// BroadcastToSession(refs...) cannot reach a wildcard client on its own.
func (s *Server) emitSpaceScoped(eventType, spaceID string, payload map[string]any) {
	if strings.TrimSpace(spaceID) == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["space_id"]; !ok {
		payload["space_id"] = spaceID
	}
	msg := WSMessage{Type: eventType, Payload: payload}
	if s.onSpaceWS != nil {
		s.onSpaceWS(msg)
	}
	delivered := false
	if s.spaceStore != nil {
		if refs, err := s.spaceStore.ListSessionsForSpace(spaceID); err == nil && len(refs) > 0 {
			for _, r := range refs {
				s.BroadcastToSession(r.ID, eventType, payload)
			}
			delivered = true
		}
	}
	if !delivered {
		s.BroadcastWS(msg)
	}
	// Same type string + payload as local WS. SpaceID is set on the envelope
	// so a remote puppet can scope the event without parsing payload.
	s.SendRelay(relay.Message{
		Type:    relay.MessageType(eventType),
		SpaceID: spaceID,
		Payload: payload,
	})
}

type postSpaceMessageResponse struct {
	spaces.SpaceMessage
	ReplyCount     int                `json:"reply_count,omitempty"`
	LastPreview    string             `json:"last_preview,omitempty"`
	WakeErrors     []spaces.WakeError `json:"wake_errors,omitempty"`
	MentionedHuman bool               `json:"mentioned_human,omitempty"`
}

// MarshalJSON must live on the wrapper. Embedding SpaceMessage promotes
// SpaceMessage.MarshalJSON, which drops reply_count / last_preview /
// wake_errors / mentioned_human and leaves the chip + hover fail-closed.
func (r postSpaceMessageResponse) MarshalJSON() ([]byte, error) {
	created := r.CreatedAt
	if strings.TrimSpace(created) == "" {
		created = r.Ts
	}
	replyCount := r.ReplyCount
	if replyCount == 0 {
		replyCount = r.SpaceMessage.ReplyCount
	}
	preview := r.LastPreview
	if preview == "" {
		preview = r.SpaceMessage.LastPreview
	}
	type wire struct {
		ID             string                        `json:"id"`
		SessionID      string                        `json:"session_id"`
		Seq            int64                         `json:"seq"`
		Ts             string                        `json:"ts"`
		CreatedAt      string                        `json:"created_at"`
		Role           string                        `json:"role"`
		Content        string                        `json:"content"`
		Agent          string                        `json:"agent"`
		ToolCalls      []spaces.SpaceMessageToolCall `json:"toolCalls,omitempty"`
		ParentID       string                        `json:"parent_id,omitempty"`
		ReplyCount     int                           `json:"reply_count,omitempty"`
		LastPreview    string                        `json:"last_preview,omitempty"`
		NewSince       int                           `json:"new_since,omitempty"`
		WakeErrors     []spaces.WakeError            `json:"wake_errors,omitempty"`
		MentionedHuman bool                          `json:"mentioned_human,omitempty"`
	}
	return json.Marshal(wire{
		ID:             r.ID,
		SessionID:      r.SessionID,
		Seq:            r.Seq,
		Ts:             r.Ts,
		CreatedAt:      created,
		Role:           r.Role,
		Content:        r.Content,
		Agent:          r.Agent,
		ToolCalls:      r.ToolCalls,
		ParentID:       r.ParentID,
		ReplyCount:     replyCount,
		LastPreview:    preview,
		NewSince:       r.NewSince,
		WakeErrors:     r.WakeErrors,
		MentionedHuman: r.MentionedHuman,
	})
}

func (s *Server) afterSpaceReplyPersisted(spaceID string, msg *spaces.SpaceMessage, content string) postSpaceMessageResponse {
	resp := postSpaceMessageResponse{SpaceMessage: *msg}
	if msg.ParentID == "" {
		return resp
	}
	replies, err := s.spaceStore.ListSpaceReplies(spaceID, msg.ParentID)
	if err == nil {
		resp.ReplyCount = len(replies)
		resp.LastPreview = spaces.LastSpeechPreview(replies)
		resp.SpaceMessage.ReplyCount = resp.ReplyCount
		resp.SpaceMessage.LastPreview = resp.LastPreview
	}
	s.emitSpaceReply(spaceID, msg.ParentID, msg, resp.ReplyCount, resp.LastPreview)

	plan := s.resolveSpaceThreadWakes(spaceID, content, "")
	resp.WakeErrors = plan.Errors
	resp.MentionedHuman = plan.MentionedHuman
	if plan.MentionedHuman {
		s.emitSpaceReplyMention(spaceID, msg.ParentID, spaces.SpeechPreview(content))
	}
	if joined, err := s.spaceStore.HasThreadParticipation(spaceID, msg.ParentID, spaces.LocalViewer); err == nil && joined {
		_ = s.spaceStore.MarkThreadRead(spaceID, msg.ParentID, spaces.LocalViewer)
	}
	for _, agent := range plan.Agents {
		s.wakeSpaceThreadAgentHop(spaceID, msg.ParentID, agent, content, 0)
	}
	return resp
}

// maxSpaceWakeHops / maxWakesPerParent stop Steve↔Winston infinite loops.
// User→Steve is hop 0; Steve→Winston is hop 1; Winston @Steve is hop 2.
// Hop 2 speech does not spawn further wakes.
const (
	maxSpaceWakeHops  = 2
	maxWakesPerParent = 8
)

func (s *Server) resolveSpaceThreadWakes(spaceID, content, speaker string) spaces.WakePlan {
	var plan spaces.WakePlan
	if s.spaceStore == nil {
		return plan
	}
	sp, err := s.spaceStore.GetSpace(spaceID)
	if err != nil || sp == nil {
		return plan
	}
	inCompany := func(agent string) (bool, error) {
		if strings.TrimSpace(sp.CompanyID) == "" {
			return true, nil
		}
		return s.spaceStoreInCompany(agent, sp.CompanyID)
	}
	opts := spaces.WakeOpts{Speaker: speaker}
	if strings.TrimSpace(sp.CompanyID) == "" {
		opts.ExtraLeads = s.deskCompanyLeads()
		if spaces.IsDeskDM(sp) {
			opts.ExtraPeers = s.deskPeerNames()
		}
	}
	return spaces.ResolveThreadWakesOpts(sp, content, inCompany, opts)
}

func (s *Server) deskPeerNames() []string {
	type deskFloor interface {
		DeskPeerNames() ([]string, error)
	}
	if s.spaceStore == nil {
		return nil
	}
	floor, ok := s.spaceStore.(deskFloor)
	if !ok {
		return nil
	}
	peers, err := floor.DeskPeerNames()
	if err != nil {
		return nil
	}
	return peers
}

func (s *Server) deskCompanyLeads() []string {
	cs := s.companyAPI()
	if cs == nil {
		return nil
	}
	list, err := cs.ListCompanies()
	if err != nil || len(list) == 0 {
		return nil
	}
	var leads []string
	seen := map[string]bool{}
	for _, c := range list {
		if c == nil {
			continue
		}
		lead := c.EffectiveLead()
		key := strings.ToLower(lead)
		if lead == "" || seen[key] {
			continue
		}
		seen[key] = true
		leads = append(leads, lead)
	}
	return leads
}

func (s *Server) reserveParentWake(parentID string) bool {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return false
	}
	s.spaceWakeMu.Lock()
	defer s.spaceWakeMu.Unlock()
	if s.spaceWakeCounts == nil {
		s.spaceWakeCounts = map[string]int{}
	}
	if s.spaceWakeCounts[parentID] >= maxWakesPerParent {
		return false
	}
	s.spaceWakeCounts[parentID]++
	return true
}

func (s *Server) spaceStoreInCompany(agent, companyID string) (bool, error) {
	type companyGate interface {
		AgentInCompany(agent, companyID string) (bool, error)
	}
	if g, ok := s.spaceStore.(companyGate); ok {
		return g.AgentInCompany(agent, companyID)
	}
	return false, nil
}

func (s *Server) wakeSpaceThreadAgent(spaceID, parentID, agent, task string) {
	s.wakeSpaceThreadAgentHop(spaceID, parentID, agent, task, 0)
}

func (s *Server) wakeSpaceThreadAgentHop(spaceID, parentID, agent, task string, hop int) {
	if hop > maxSpaceWakeHops {
		return
	}
	if !s.reserveParentWake(parentID) {
		return
	}
	s.emitSpaceReplyTyping(spaceID, parentID, agent)
	s.mu.Lock()
	runner := s.spaceThreadRunner
	s.mu.Unlock()
	if runner == nil {
		s.emitSpaceReplyTypingDone(spaceID, parentID, agent, "")
		return
	}
	// Return to the HTTP handler immediately. The drawer must never wait on
	// a live model — persist + space_reply + typing already happened.
	s.spaceThreadWG.Add(1)
	go func() {
		defer s.spaceThreadWG.Done()
		s.finishSpaceThreadWake(spaceID, parentID, agent, task, runner, hop)
	}()
}

func (s *Server) waitSpaceThreadWakes() {
	s.spaceThreadWG.Wait()
}

func (s *Server) companyStillSeats(spaceID, agent string) bool {
	if s.spaceStore == nil {
		return false
	}
	sp, err := s.spaceStore.GetSpace(spaceID)
	if err != nil || sp == nil {
		return false
	}
	cid := strings.TrimSpace(sp.CompanyID)
	if cid == "" {
		return true // desk: roster already authorized the wake
	}
	if cs := s.companyAPI(); cs != nil {
		if _, err := cs.GetCompany(cid); err != nil {
			return false
		}
	}
	seated, err := s.spaceStoreInCompany(agent, cid)
	return err == nil && seated
}

func (s *Server) finishSpaceThreadWake(spaceID, parentID, agent, task string, runner SpaceThreadRunner, hop int) {
	if !s.companyStillSeats(spaceID, agent) {
		s.emitSpaceReplyTypingDone(spaceID, parentID, agent, agent+" isn't in this company.")
		return
	}
	speech, err := runner(context.Background(), spaceID, parentID, agent, task)
	if err != nil {
		slog.Warn("space thread wake failed", "agent", agent, "err", err)
		if backend.IsKeyMiss(err) {
			speech = backend.PersistKeyMissSpeech(agent, "", err)
			if speech != "" && s.spaceStore != nil {
				inserted, insErr := s.spaceStore.InsertSpaceThreadMessage(spaceID, speech, parentID, "assistant", agent)
				if insErr != nil {
					slog.Warn("space thread key-miss insert failed", "agent", agent, "err", insErr)
					s.emitSpaceReplyTypingDone(spaceID, parentID, agent, "")
					return
				}
				replies, _ := s.spaceStore.ListSpaceReplies(spaceID, parentID)
				s.emitSpaceReply(spaceID, parentID, inserted, len(replies), spaces.LastSpeechPreview(replies))
				s.emitSpaceReplyTypingDone(spaceID, parentID, agent, "")
				return
			}
		}
		s.emitSpaceReplyTypingDone(spaceID, parentID, agent, "")
		return
	}
	// Same AfterTools leftover strip as hallway REST/WS persist.
	speech = strings.TrimSpace(backend.PersistVisibleAssistantContent(speech, task))
	if ag := s.resolveNamedSpaceAgent(agent); ag != nil {
		if rewrite := teammateMissingFromAgent(ag, task, speech); rewrite != "" {
			speech = rewrite
		}
	}
	if speech == "" {
		s.emitSpaceReplyTypingDone(spaceID, parentID, agent, "")
		return
	}
	if !s.companyStillSeats(spaceID, agent) {
		s.emitSpaceReplyTypingDone(spaceID, parentID, agent, agent+" isn't in this company.")
		return
	}
	inserted, insErr := s.spaceStore.InsertSpaceThreadMessage(spaceID, speech, parentID, "assistant", agent)
	if insErr != nil {
		slog.Warn("space thread insert failed", "agent", agent, "err", insErr)
		s.emitSpaceReplyTypingDone(spaceID, parentID, agent, insErr.Error())
		return
	}
	replies, _ := s.spaceStore.ListSpaceReplies(spaceID, parentID)
	s.emitSpaceReply(spaceID, parentID, inserted, len(replies), spaces.LastSpeechPreview(replies))
	s.emitSpaceReplyTypingDone(spaceID, parentID, agent, "")
	// Bidirectional mesh: assistant speech uses the same wake resolver.
	// Never wake the speaker. Hop cap + per-parent cap stop ping-pong.
	if hop >= maxSpaceWakeHops {
		return
	}
	plan := s.resolveSpaceThreadWakes(spaceID, speech, agent)
	if plan.MentionedHuman {
		s.emitSpaceReplyMention(spaceID, parentID, spaces.SpeechPreview(speech))
	}
	for _, next := range plan.Agents {
		s.wakeSpaceThreadAgentHop(spaceID, parentID, next, speech, hop+1)
	}
}

func (s *Server) handleMarkSpaceThreadRead(w http.ResponseWriter, r *http.Request) {
	if s.spaceStore == nil {
		jsonError(w, 503, "spaces not configured")
		return
	}
	spaceID := r.PathValue("id")
	if err := validateSpaceID(spaceID); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	var body struct {
		ParentID string `json:"parent_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}
	}
	if strings.TrimSpace(body.ParentID) == "" {
		body.ParentID = strings.TrimSpace(r.URL.Query().Get("parent_id"))
	}
	if err := s.spaceStore.MarkThreadRead(spaceID, body.ParentID, spaces.LocalViewer); err != nil {
		jsonSpaceError(w, err)
		return
	}
	unseen, err := s.spaceStore.ThreadUnseenForViewer(spaceID, body.ParentID, spaces.LocalViewer)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	jsonOK(w, map[string]any{"ok": true, "unseen": unseen})
}
