package spaces

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// HumanMentionNames are @tokens that address the local human, not an agent.
// An @ on the human is a notification, never an agent run.
var HumanMentionNames = map[string]bool{
	"user":  true,
	"you":   true,
	"human": true,
	"me":    true,
	"mj":    true,
}

// IsHumanMention reports whether @name addresses the local human, not an agent.
// Covers @you / @MJ / @human plus the actual local USER / LOGNAME / USERNAME.
// Agent names such as @Steve are never treated as human.
func IsHumanMention(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return false
	}
	if HumanMentionNames[key] {
		return true
	}
	for _, envKey := range []string{"USER", "LOGNAME", "USERNAME"} {
		env := strings.ToLower(strings.TrimSpace(os.Getenv(envKey)))
		if env != "" && env == key {
			return true
		}
	}
	return false
}

// WakeError is a fail-closed mention that must not start an agent run.
type WakeError struct {
	Agent   string `json:"agent"`
	Reason  string `json:"reason"` // not_in_company | not_in_roster | mid_text_drop
	Message string `json:"message"`
}

// WakePlan is the result of resolving @mentions in a space thread reply.
type WakePlan struct {
	Agents         []string    // valid space+company members to wake, each once
	Errors         []WakeError // blocked mentions (hover reason)
	MentionedHuman bool        // @user / @you / @human — notify, do not run
}

// NameAppears reports whether name occurs as a whole token in content,
// with or without a leading @. "Steve" matches "Ask Steve hostname" and
// "@Steve". It does not match "Steven" or email-style "alice@Steve".
func NameAppears(content, name string) bool {
	content = strings.ToLower(content)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || content == "" {
		return false
	}
	idx := 0
	for {
		pos := strings.Index(content[idx:], name)
		if pos < 0 {
			return false
		}
		abs := idx + pos
		if nameTokenBoundary(content, abs, len(name)) {
			return true
		}
		idx = abs + len(name)
	}
}

func nameTokenBoundary(s string, start, nlen int) bool {
	if start > 0 {
		c := s[start-1]
		if isAtNameChar(c) {
			return false
		}
		if c == '@' && start >= 2 && isAtNameChar(s[start-2]) {
			return false
		}
	}
	end := start + nlen
	if end < len(s) && isAtNameChar(s[end]) {
		return false
	}
	return true
}

func companySeated(canon, companyID string, inCompany func(agent string) (bool, error)) bool {
	if companyID == "" {
		return true
	}
	if inCompany == nil {
		return false
	}
	seated, err := inCompany(canon)
	return err == nil && seated
}

// WakeOpts tightens ResolveThreadWakes for bidirectional mesh speech.
// Speaker is never woken (self-@ must not loop). ExtraLeads are company
// leads visible from a desk space (empty company_id) only — and only
// when the speaker is the human or desk Winston. ExtraPeers are desk-floor
// names (desk DMs) any speaker may wake. Specialists stay company-scoped.
type WakeOpts struct {
	Speaker    string
	ExtraLeads []string
	ExtraPeers []string
}

// deskCoSSpeaker is true for human posts (empty speaker) and desk Winston.
// A specialist — including a desk-DM LeadAgent — must not ExtraLead-hop
// into another company.
func deskCoSSpeaker(speaker string) bool {
	s := strings.ToLower(strings.TrimSpace(speaker))
	return s == "" || s == "winston"
}

// deskWakeExtras is ExtraLeads (Winston/human only) plus ExtraPeers (anyone).
// Company spaces ignore extras.
func deskWakeExtras(companyID string, opts WakeOpts) []string {
	if strings.TrimSpace(companyID) != "" {
		return nil
	}
	var out []string
	if deskCoSSpeaker(opts.Speaker) {
		out = append(out, opts.ExtraLeads...)
	}
	out = append(out, opts.ExtraPeers...)
	return out
}

// ResolveThreadWakes decides who may be woken in a Slack-style thread.
// No @mention → empty plan (persist + WS only).
// Mid-text @ of a non-member hints and drops (PR 151).
// Non-members are never extra-spawned (PR 154).
// Company spaces require the agent on the company roster AND the space roster.
// Desk (empty company) uses today's space roster / not_in_roster, plus
// ExtraLeads so a desk CoS can wake a company's lead without seating every
// Huginn specialist on the desk.
//
// When an @ already wakes a roster member, other roster names in the same
// text also wake (bare "Steve" or @Steve) so "@Winston Ask Steve hostname"
// does not leave the specialist asleep for the CoS to do their job.
func ResolveThreadWakes(sp *Space, content string, inCompany func(agent string) (bool, error)) WakePlan {
	return ResolveThreadWakesOpts(sp, content, inCompany, WakeOpts{})
}

// ResolveThreadWakesOpts is ResolveThreadWakes plus speaker exclusion and
// desk-only company-lead extras.
func ResolveThreadWakesOpts(sp *Space, content string, inCompany func(agent string) (bool, error), opts WakeOpts) WakePlan {
	var plan WakePlan
	if sp == nil {
		return plan
	}
	names := extractAtNames(content)
	if len(names) == 0 {
		return plan
	}
	roster := map[string]string{}
	for _, n := range RosterNames(sp) {
		roster[strings.ToLower(n)] = n
	}
	companyID := strings.TrimSpace(sp.CompanyID)
	// Desk: Winston/human may wake company leads; any speaker may wake
	// desk-floor peers. Company spaces stay seated-members-only.
	for _, n := range deskWakeExtras(companyID, opts) {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if _, ok := roster[key]; !ok {
			roster[key] = n
		}
	}
	speaker := strings.ToLower(strings.TrimSpace(opts.Speaker))
	seen := map[string]bool{}
	for _, raw := range names {
		key := strings.ToLower(raw)
		if IsHumanMention(key) {
			plan.MentionedHuman = true
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if speaker != "" && key == speaker {
			continue // never self-wake
		}
		canon, onRoster := roster[key]
		if !onRoster {
			plan.Errors = append(plan.Errors, WakeError{
				Agent:   raw,
				Reason:  "not_in_roster",
				Message: raw + " isn't in this space.",
			})
			continue
		}
		if !companySeated(canon, companyID, inCompany) {
			plan.Errors = append(plan.Errors, WakeError{
				Agent:   canon,
				Reason:  "not_in_company",
				Message: canon + " isn't in this company.",
			})
			continue
		}
		plan.Agents = append(plan.Agents, canon)
	}
	// A valid @ already started a run. Also wake other roster members
	// named in the same text without @. Bare non-members stay silent.
	// Extra desk leads are included so "@Winston Ask Ava hostname" can
	// wake a company lead named in the same breath.
	if len(plan.Agents) > 0 {
		named := append([]string{}, RosterNames(sp)...)
		named = append(named, deskWakeExtras(companyID, opts)...)
		for _, n := range named {
			n = strings.TrimSpace(n)
			key := strings.ToLower(n)
			if n == "" || seen[key] || IsHumanMention(key) {
				continue
			}
			if speaker != "" && key == speaker {
				continue
			}
			if !NameAppears(content, n) {
				continue
			}
			seen[key] = true
			canon := n
			if r, ok := roster[key]; ok {
				canon = r
			}
			if !companySeated(canon, companyID, inCompany) {
				continue
			}
			plan.Agents = append(plan.Agents, canon)
		}
	}
	return plan
}

// InsertSpaceThreadMessage persists a message already attached to a Slack-style
// thread (typically an assistant wake). parentID is required and flattened to
// the root. Does not update session.updated_at so hallway unseen stays still.
func (s *SQLiteSpaceStore) InsertSpaceThreadMessage(spaceID, content, parentID, role, agent string) (*SpaceMessage, error) {
	content = strings.TrimSpace(content)
	parentID = strings.TrimSpace(parentID)
	if content == "" {
		return nil, &SpaceError{Code: "invalid_content", Message: "content is required"}
	}
	if len(content) > MaxSpaceMessageBytes {
		return nil, &SpaceError{Code: "invalid_content", Message: "content too long"}
	}
	if parentID == "" {
		return nil, &SpaceError{Code: "invalid_parent", Message: "parent_id is required"}
	}
	if _, err := s.requireActiveSpace(spaceID); err != nil {
		return nil, err
	}
	parent, err := s.loadSpaceMessage(spaceID, parentID)
	if err != nil {
		return nil, err
	}
	root, err := s.resolveReplyRoot(spaceID, parent)
	if err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	if role != "assistant" && role != "user" {
		role = "assistant"
	}
	id := newID()
	tx, err := s.db.Write().Begin()
	if err != nil {
		return nil, fmt.Errorf("spaces: begin thread insert: %w", err)
	}
	defer tx.Rollback()
	var maxSeq int64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE container_id = ?`,
		root.SessionID,
	).Scan(&maxSeq); err != nil {
		return nil, fmt.Errorf("spaces: next seq: %w", err)
	}
	seq := maxSeq + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`
		INSERT INTO messages
			(id, container_type, container_id, seq, ts, role, content, agent, parent_id)
		VALUES (?, 'session', ?, ?, ?, ?, ?, ?, ?)`,
		id, root.SessionID, seq, now, role, content, agent, root.ID,
	); err != nil {
		return nil, fmt.Errorf("spaces: insert thread message: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("spaces: commit thread message: %w", err)
	}
	return &SpaceMessage{
		ID:        id,
		SessionID: root.SessionID,
		Seq:       seq,
		Ts:        now,
		Role:      role,
		Content:   content,
		Agent:     agent,
		ParentID:  root.ID,
	}, nil
}

// CountSpaceReplies returns the Slack-style reply count under parentID.
func (s *SQLiteSpaceStore) CountSpaceReplies(spaceID, parentID string) (int, error) {
	replies, err := s.ListSpaceReplies(spaceID, parentID)
	if err != nil {
		return 0, err
	}
	return len(replies), nil
}

func extractAtNames(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for i := 0; i < len(content); i++ {
		if content[i] != '@' {
			continue
		}
		if i > 0 && isAtNameChar(content[i-1]) {
			continue
		}
		rest := content[i+1:]
		if len(rest) == 0 || !isAtNameStart(rest[0]) {
			continue
		}
		end := 1
		for end < len(rest) && isAtNameChar(rest[end]) {
			end++
		}
		if end > 64 {
			continue
		}
		name := rest[:end]
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
		i += end
	}
	return names
}

func isAtNameStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAtNameChar(c byte) bool {
	return isAtNameStart(c) || (c >= '0' && c <= '9') || c == '-' || c == '_'
}
