package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/spaces"
)

// spaceMessageAPI is the Slack-thread surface the TUI uses — the same model as
// GET/POST /api/v1/space-messages (parent_id, reply_count, GET replies).
// Distinct from work-inspector ThreadDetail (ctrl+t).
type spaceMessageAPI interface {
	FindChannelByName(name string) (*spaces.Space, error)
	OpenDM(agentName string) (*spaces.Space, error)
	ListSpaceMessages(spaceID string, before *spaces.SpaceMsgCursor, limit int) (spaces.SpaceMessagesResult, error)
	ListSpaceReplies(spaceID, parentID string) ([]spaces.SpaceMessage, error)
	PostSpaceMessage(spaceID, content, parentID string) (*spaces.SpaceMessage, error)
	GetSpaceMessage(spaceID, msgID string) (*spaces.SpaceMessage, error)
	MarkThreadRead(spaceID, parentID, viewer string) error
}

// replyThreadState is the Slack-style reply drawer (not work-inspector).
type replyThreadState struct {
	spaceID  string
	parent   spaces.SpaceMessage
	replies  []spaces.SpaceMessage
	err      string
	viewport viewport.Model
}

// SetSpaceStore wires the in-process space-messages API so TUI/CLI work
// without the Vue app.
func (a *App) SetSpaceStore(s spaceMessageAPI) { a.spaceStore = s }

// formatReplyChip renders the hallway reply chip. Empty when there are no replies.
func formatReplyChip(count int, preview string, newSince int) string {
	if count < 1 {
		return ""
	}
	label := "1 reply"
	if count != 1 {
		label = fmt.Sprintf("%d replies", count)
	}
	if p := clipChipPreview(preview); p != "" {
		label += " · " + p
	}
	if newSince > 0 {
		if newSince == 1 {
			label += " · 1 new"
		} else {
			label += fmt.Sprintf(" · %d new", newSince)
		}
	}
	return "↳ " + label
}

func clipChipPreview(preview string) string {
	p := strings.TrimSpace(preview)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(p), "delegated to @") {
		return ""
	}
	if utf8.RuneCountInString(p) > 80 {
		return string([]rune(p)[:80]) + "…"
	}
	return p
}

// hallwayLineFromMessage maps a space root onto a chat line (chip metadata included).
func hallwayLineFromMessage(m spaces.SpaceMessage) chatLine {
	role := m.Role
	if role == "" {
		role = "user"
	}
	created := m.CreatedAt
	if created == "" {
		created = m.Ts
	}
	return chatLine{
		role:          role,
		content:       m.Content,
		agentName:     m.Agent,
		spaceMsgID:    m.ID,
		replyCount:    m.ReplyCount,
		lastPreview:   m.LastPreview,
		newSince:      m.NewSince,
		isHallwayRoot: true,
		createdAt:     created,
	}
}

func replySpeaker(m spaces.SpaceMessage) string {
	if m.Role == "user" {
		return "You"
	}
	if strings.TrimSpace(m.Agent) != "" {
		return m.Agent
	}
	if m.Role == "assistant" {
		return "Teammate"
	}
	return "You"
}

func (a *App) loadHallway(spaceID, label string) {
	a.activeSpaceID = spaceID
	if a.spaceStore == nil || strings.TrimSpace(spaceID) == "" {
		a.addLine("system", label)
		return
	}
	res, err := a.spaceStore.ListSpaceMessages(spaceID, nil, 50)
	if err != nil {
		a.addLine("error", "Could not load hallway: "+err.Error())
		return
	}
	a.chat.ClearHistory()
	a.chatLineOffsetsDirty = true
	a.addLine("system", label)
	if len(res.Messages) == 0 {
		a.addLine("system", "No messages yet.")
		return
	}
	for _, m := range res.Messages {
		a.chat.history = append(a.chat.history, hallwayLineFromMessage(m))
	}
	if len(res.Messages) > 0 {
		a.selectedHallwayID = res.Messages[len(res.Messages)-1].ID
	}
}

func (a *App) enterChannelSpace(name string) {
	if a.spaceStore == nil {
		a.activeSpaceID = ""
		a.addLine("system", fmt.Sprintf("Switched to #%s", name))
		return
	}
	sp, err := a.spaceStore.FindChannelByName(name)
	if err != nil || sp == nil {
		a.activeSpaceID = ""
		a.addLine("system", fmt.Sprintf("Switched to #%s", name))
		return
	}
	if lead := strings.TrimSpace(sp.LeadAgent); lead != "" {
		a.primaryAgent = lead
	}
	a.loadHallway(sp.ID, fmt.Sprintf("Switched to #%s", name))
}

func (a *App) enterDMSpace(agent string) {
	if a.spaceStore == nil {
		a.activeSpaceID = ""
		a.addLine("system", fmt.Sprintf("Switched to @%s", agent))
		return
	}
	sp, err := a.spaceStore.OpenDM(agent)
	if err != nil || sp == nil {
		a.activeSpaceID = ""
		a.addLine("system", fmt.Sprintf("Switched to @%s", agent))
		return
	}
	a.loadHallway(sp.ID, fmt.Sprintf("Switched to @%s", agent))
}

// openReplyThreadAtCursor opens the Slack reply drawer for the most recent hallway root.
// Returns false when there is no hallway root (caller should treat `t` as typing).
func (a *App) openReplyThreadAtCursor() bool {
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].isHallwayRoot && a.chat.history[i].spaceMsgID != "" {
			return a.openReplyThread(a.chat.history[i])
		}
	}
	return false
}

func (a *App) openReplyThread(line chatLine) bool {
	if a.spaceStore == nil || a.activeSpaceID == "" || line.spaceMsgID == "" {
		return false
	}
	parent, err := a.spaceStore.GetSpaceMessage(a.activeSpaceID, line.spaceMsgID)
	if err != nil || parent == nil {
		parent = &spaces.SpaceMessage{
			ID:      line.spaceMsgID,
			Content: line.content,
			Role:    line.role,
			Agent:   line.agentName,
		}
	}
	replies, rerr := a.spaceStore.ListSpaceReplies(a.activeSpaceID, line.spaceMsgID)
	errText := ""
	if rerr != nil {
		errText = "Could not load replies."
		replies = nil
	}
	_ = a.spaceStore.MarkThreadRead(a.activeSpaceID, line.spaceMsgID, spaces.LocalViewer)
	sel := -1
	if len(replies) > 0 {
		sel = len(replies) - 1
	}
	a.selectedReplyIdx = sel
	a.selectedHallwayID = line.spaceMsgID
	a.replyThread = replyThreadState{
		spaceID: a.activeSpaceID,
		parent:  *parent,
		replies: replies,
		err:     errText,
	}
	a.replyThread.viewport.Width = 0
	a.state = stateReplyThread
	a.input.SetValue("")
	a.input.Placeholder = "Reply…"
	a.input.Focus()
	a.sidebar.focused = false
	a.chipFocused = false
	a.atMention.Hide()
	// Opening a thread clears the hallway new-since badge.
	for i := range a.chat.history {
		if a.chat.history[i].spaceMsgID == line.spaceMsgID {
			a.chat.history[i].newSince = 0
		}
	}
	return true
}

func (a *App) closeReplyThread() {
	a.state = stateChat
	a.replyThread = replyThreadState{}
	if a.activeChannel != "" {
		a.input.Placeholder = a.channelPlaceholder(a.activeChannel)
	} else if a.primaryAgent != "" {
		a.input.Placeholder = "Message " + a.primaryAgent + "…"
	} else {
		a.input.Placeholder = "→ message…"
	}
	a.input.SetValue("")
	a.atMention.Hide()
	a.refreshViewport()
}

func (a *App) sendSpaceReply(content string) error {
	content = strings.TrimSpace(content)
	if content == "" || a.spaceStore == nil || a.replyThread.parent.ID == "" {
		return fmt.Errorf("nothing to send")
	}
	spaceID := a.replyThread.spaceID
	if spaceID == "" {
		spaceID = a.activeSpaceID
	}
	msg, err := a.spaceStore.PostSpaceMessage(spaceID, content, a.replyThread.parent.ID)
	if err != nil {
		a.replyThread.err = "Could not send reply."
		return err
	}
	if msg != nil {
		dup := false
		for _, existing := range a.replyThread.replies {
			if existing.ID == msg.ID {
				dup = true
				break
			}
		}
		if !dup {
			a.replyThread.replies = append(a.replyThread.replies, *msg)
		}
	}
	a.replyThread.err = ""
	for i := range a.chat.history {
		if a.chat.history[i].spaceMsgID == a.replyThread.parent.ID {
			a.chat.history[i].replyCount++
			a.chat.history[i].lastPreview = content
			a.chat.history[i].newSince = 0
			a.chat.history[i].renderedCache = ""
		}
	}
	return nil
}

func (a *App) handleReplyThreadKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if a.atMention.Visible() {
		switch msg.String() {
		case "up", "ctrl+p", "down", "ctrl+n", "esc", "enter", "tab":
			var atCmd tea.Cmd
			a.atMention, atCmd = a.atMention.Update(msg)
			if atCmd != nil {
				cmds = append(cmds, atCmd)
			}
			return a, tea.Batch(cmds...)
		}
	}
	switch msg.String() {
	case "esc":
		a.closeReplyThread()
		return a, nil
	case "enter":
		raw := strings.TrimSpace(a.input.Value())
		if raw == "" {
			return a, nil
		}
		a.input.SetValue("")
		if err := a.sendSpaceReply(raw); err != nil {
			return a, nil
		}
		return a, nil
	case "up", "k":
		if a.input.Value() == "" {
			a.moveThreadCursor(-1)
			a.replyThread.viewport.LineUp(1)
			return a, nil
		}
	case "down", "j":
		if a.input.Value() == "" {
			a.moveThreadCursor(1)
			a.replyThread.viewport.LineDown(1)
			return a, nil
		}
	}
	var inputCmd tea.Cmd
	a.input, inputCmd = a.input.Update(msg)
	if inputCmd != nil {
		cmds = append(cmds, inputCmd)
	}
	if atPrefix := ExtractAtPrefix(a.input.Value()); atPrefix != "" {
		a.atMention.Show(atPrefix)
	} else {
		a.atMention.Hide()
	}
	return a, tea.Batch(cmds...)
}

func (a *App) renderReplyThread() string {
	w := a.width
	if w < 20 {
		w = 20
	}
	title := StyleAccent.Width(w).Render("Thread")
	var body strings.Builder
	body.WriteString(StyleDim.Render(replySpeaker(a.replyThread.parent)))
	if a.selectedReplyIdx == -1 {
		if rel := FormatRelativeTime(firstCreated(a.replyThread.parent), time.Now()); rel != "" {
			body.WriteString(StyleDim.Render("  " + rel))
		}
	}
	body.WriteByte('\n')
	body.WriteString(a.replyThread.parent.Content)
	body.WriteString("\n")
	body.WriteString(StyleDim.Render(strings.Repeat("─", max(8, w-4))))
	body.WriteByte('\n')
	if a.replyThread.err != "" {
		body.WriteString(StyleError.Render(a.replyThread.err))
		body.WriteByte('\n')
	}
	if len(a.replyThread.replies) == 0 && a.replyThread.err == "" {
		body.WriteString(StyleDim.Render("No replies yet. Type a reply below."))
		body.WriteByte('\n')
	}
	for i, m := range a.replyThread.replies {
		body.WriteByte('\n')
		body.WriteString(StyleDim.Render(replySpeaker(m)))
		if a.selectedReplyIdx == i {
			if rel := FormatRelativeTime(firstCreated(m), time.Now()); rel != "" {
				body.WriteString(StyleDim.Render("  " + rel))
			}
		}
		body.WriteByte('\n')
		body.WriteString(m.Content)
		body.WriteByte('\n')
	}

	vp := a.replyThread.viewport
	if vp.Width == 0 {
		h := a.height - 8
		if h < 4 {
			h = 4
		}
		vp = viewport.New(w-2, h)
		a.replyThread.viewport = vp
	}
	a.replyThread.viewport.SetContent(body.String())
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBlue)).
		Width(w-2).
		Padding(0, 1).
		Render(a.replyThread.viewport.View())

	sections := []string{title, box}
	if a.atMention.Visible() {
		sections = append(sections, a.atMention.View(w))
	}
	sections = append(sections, StyleDim.Render(strings.Repeat("─", w)))
	sections = append(sections, a.renderInputBox())
	sections = append(sections, StyleDim.Render("  [esc] close  [enter] send reply  [@] mention"))
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func firstCreated(m spaces.SpaceMessage) string {
	if strings.TrimSpace(m.CreatedAt) != "" {
		return m.CreatedAt
	}
	return m.Ts
}

func (a *App) moveHallwayCursor(delta int) {
	var idxs []int
	for i, line := range a.chat.history {
		if line.isHallwayRoot && line.spaceMsgID != "" {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) == 0 {
		return
	}
	cur := len(idxs) - 1
	for n, i := range idxs {
		if a.chat.history[i].spaceMsgID == a.selectedHallwayID {
			cur = n
			break
		}
	}
	next := cur + delta
	if next < 0 {
		next = 0
	}
	if next >= len(idxs) {
		next = len(idxs) - 1
	}
	a.selectedHallwayID = a.chat.history[idxs[next]].spaceMsgID
	a.refreshViewport()
	if idxs[next] < len(a.chatLineOffsets) {
		a.viewport.SetYOffset(a.chatLineOffsets[idxs[next]])
	}
}

func (a *App) moveThreadCursor(delta int) {
	max := len(a.replyThread.replies) - 1
	next := a.selectedReplyIdx + delta
	if next < -1 {
		next = -1
	}
	if next > max {
		next = max
	}
	if max < 0 && next > -1 {
		next = -1
	}
	a.selectedReplyIdx = next
}
