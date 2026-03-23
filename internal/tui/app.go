package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/swarm"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/vision"
	"github.com/scrypster/huginn/internal/pricing"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/streaming"
)

// PermissionPromptMsg is sent by main.go's forwarder goroutine when the permission
// gate needs user approval for a tool call. RespCh must receive exactly one Decision.
type PermissionPromptMsg struct {
	Req    permissions.PermissionRequest
	RespCh chan permissions.Decision
}

// writeApprovalMsg is sent by the OnBeforeWrite callback when the agent wants
// to write or edit a file. RespCh must receive exactly one bool (true=allow).
type writeApprovalMsg struct {
	Path       string
	OldContent []byte
	NewContent []byte
	RespCh     chan bool
}

// swarmAgentStatus tracks the state of a single agent in a swarm execution.
type swarmAgentStatus struct {
	name    string
	pct     int
	status  string // "running", "done", "waiting"
	tool    string // current tool being executed
	elapsed string // human-readable elapsed time
	output  string // final output (when done)
}

// artifactOverlayState holds the data and viewport for the full-screen artifact overlay.
type artifactOverlayState struct {
	content  string
	kind     string
	title    string
	viewport viewport.Model
}

// threadOverlayState holds the data for the full-screen thread detail overlay.
type threadOverlayState struct {
	threadID   string
	title      string
	agentChain []string
	lines      []chatLine
	viewport   viewport.Model
}

// observationDeckState holds the data for the narrated observation deck overlay.
type observationDeckState struct {
	title    string
	lines    []string
	viewport viewport.Model
}

// App is the root Bubble Tea model.
type App struct {
	cfg     *config.Config
	orch    *agent.Orchestrator
	models  *modelconfig.Models
	version string

	state         appState
	viewport      viewport.Model
	input         textinput.Model
	wizard        WizardModel
	agentWizard   agentWizardModel
	filePicker    FilePickerModel
	sessionPicker sessionPickerModel
	sessionStore  session.StoreInterface
	activeSession *session.Session

	activeModel  string // model name currently streaming, "" when idle
	agentTurn    int    // current turn in agent loop (0 = not in loop)
	useAgentLoop bool   // true when tool registry is wired

	// chat holds all streaming, history, and channel state.
	chat ChatModel

	// chatLineOffsets maps chat.history index → first viewport line for that entry.
	chatLineOffsets      []int
	chatLineOffsetsDirty bool
	// newLinesWhileScrolled counts lines added during scroll-lock mode.
	newLinesWhileScrolled int

	// dotPhase cycles the typing indicator animation (0–2).
	dotPhase int
	// scrollMode is true when the user has scrolled up (viewport is not at bottom).
	scrollMode bool

	width  int
	height int

	queuedMsg         string               // message typed during streaming, waiting to auto-send
	ctrlCPending      bool                 // true after first ctrl+c — next ctrl+c quits
	attachments       []string             // relative paths of files attached via @ picker (max 10)
	pendingImageParts []backend.ContentPart // image parts built from attachments, sent with next message
	chipFocused  bool        // true when keyboard focus is in the attachment chip row
	chipCursor   int         // index of focused chip (when chipFocused)
	shellContext string      // output of last ! command, prepended to next LLM message
	autoRun      bool        // whether tool calls are auto-approved (default true)
	autoRunAtom  *atomic.Bool // shared with gate promptFunc so the gate can read autoRun atomically

	// Permission prompting (statePermAwait): set when a tool call needs user approval.
	permPending *PermissionPromptMsg // non-nil while awaiting user decision

	// Write approval prompting (stateWriteAwait): set when agent wants to write/edit a file.
	writePending *writeApprovalMsg // non-nil while awaiting write approval

	statsReg      *stats.Registry
	workspaceRoot string
	idx           *repo.Index
	store         *storage.Store

	glamourRenderer *glamour.TermRenderer // markdown renderer; rebuilt on resize

	// Delegation support
	delegationBuf        string // accumulates streaming tokens from the delegatee
	delegationAgent      string // name of the currently-delegated-to agent
	delegationAgentColor string // color of the currently-delegated-to agent
	agentReg             *agents.AgentRegistry
	consultDepth         int32 // atomic depth counter for consult_agent tool; prevents recursive delegation
	notepadMgr           *notepad.Manager

	priceTracker *pricing.SessionTracker

	// Swarm support
	swarmView   *SwarmViewModel
	swarmEvents <-chan swarm.SwarmEvent

	// Overlay states for full-screen views.
	artifactOverlay  artifactOverlayState
	threadOverlay    threadOverlayState
	observationDeck  observationDeckState

	// Primary agent — name displayed in the footer header and toggled by ctrl+p.
	primaryAgent string

	// sessionCostUSD holds the running session cost updated via cost_update WS event or periodic poll.
	// Displayed in the header when > 0.
	sessionCostUSD float64

	// MuninnDB connection info — passed into the agent wizard.
	muninnEndpoint  string
	muninnConnected bool

	// Sidebar, DM picker, channel picker, @-mention autocomplete.
	sidebar        sidebarModel
	dmPicker       dmPickerModel
	channelPicker  channelPickerModel
	atMention      atMentionModel
	activeChannel  string // currently active channel (empty = DM mode)

	// activeAgents tracks agents that are currently active (used by sidebar and briefing).
	activeAgents map[string]bool

	// Top-level navigation screen (screenChat = main chat, others = full-screen views).
	activeScreen   appScreen
	agentsScreen   agentsScreenModel
	modelsScreen   modelsScreenModel
	settingsScreen settingsScreenModel

	// channelLeads maps channel name → lead agent name.
	channelLeads map[string]string

	// appCtx holds shared TUI services for new-style screens.
	appCtx interface{}
}

type chatLine struct {
	role       string // "user", "assistant", "system", "error", "tool-call", "tool-done", "tool-error", "thread-header", "swarm-bar"
	content    string
	duration   string // for tool-done: "1.2s"
	truncated  int    // for tool-done: number of lines hidden (0 = show all)
	fullOutput string // for tool-done: full output before truncation
	expanded   bool   // for tool-done: whether ctrl+o has expanded this line
	toolName   string // for tool-call/tool-done: the tool name

	// Rendered cache fields (assistant messages).
	renderedCache string
	renderWidth   int

	// Agent attribution (assistant messages from delegation).
	agentName  string
	agentColor string
	agentIcon  string

	// Thread-header fields.
	isThreadHeader  bool
	threadID        string
	threadAgentName string
	threadToolsUsed string
	threadElapsed   string
	threadCollapsed bool

	// Artifact-line fields (artifact summaries embedded in thread boxes).
	isArtifactLine bool
	artifactID     string
	artifactKind   string
	artifactTitle  string
	artifactStats  string
	artifactStatus string // "pending", "accepted", "rejected"

	// Swarm-bar fields.
	swarmID     string
	swarmTitle  string
	swarmAgents []swarmAgentStatus
	swarmDone   bool
}

// New creates and initializes the App model.
func New(cfg *config.Config, orch *agent.Orchestrator, models *modelconfig.Models, version string) *App {
	ti := textinput.New()
	ti.Placeholder = "→ message…"
	ti.Focus()
	ti.CharLimit = 8192
	ti.Width = 80

	vp := viewport.New(80, 20)
	vp.SetContent(welcomeMessage())

	gr := newGlamourRenderer(80)

	return &App{
		cfg:             cfg,
		orch:            orch,
		models:          models,
		version:         version,
		state:           stateChat,
		input:           ti,
		viewport:        vp,
		wizard:          newWizardModel(),
		filePicker:      newFilePickerModel(),
		glamourRenderer: gr,
		sidebar:         newSidebarModel(),
		dmPicker:        newDMPicker(),
		channelPicker:   newChannelPicker(),
		agentsScreen:    agentsScreenModel{},
		modelsScreen:    newModelsScreen(),
		settingsScreen:  newSettingsScreen(),
		autoRun:         true,
		autoRunAtom:     func() *atomic.Bool { b := &atomic.Bool{}; b.Store(true); return b }(),
	}
}

// chatWidth returns the width available for the main chat viewport.
// When a sidebar is visible, this is narrower than a.width.
func (a *App) chatWidth() int {
	return a.width
}

// channelPlaceholder returns the input placeholder text to show when the user
// has switched to a named channel context.
func (a *App) channelPlaceholder(channelName string) string {
	return "Message #" + channelName + "…"
}

// SetAutoRunAtom injects the shared atomic bool so that main.go's gate promptFunc
// can read the current autoRun state without involving the TUI event loop.
// Must be called before the Bubble Tea program starts.
func (a *App) SetAutoRunAtom(atom *atomic.Bool) {
	a.autoRunAtom = atom
}

// SetSessionStore injects the session store so the picker and session
// management commands can list, load, and save sessions.
func (a *App) SetSessionStore(s session.StoreInterface) { a.sessionStore = s }

// SetActiveSession sets the active session for auto-save and session management.
func (a *App) SetActiveSession(s *session.Session) { a.activeSession = s }

// SetStatsRegistry wires the stats registry into the TUI for /stats display.
func (a *App) SetStatsRegistry(reg *stats.Registry) {
	a.statsReg = reg
}

// SetWorkspace sets the workspace root and index for display in /workspace.
// It also populates the file picker with the indexed paths.
func (a *App) SetWorkspace(root string, idx *repo.Index) {
	a.workspaceRoot = root
	a.idx = idx
	if idx != nil {
		paths := make([]string, 0, len(idx.Chunks))
		seen := make(map[string]bool)
		for _, c := range idx.Chunks {
			if !seen[c.Path] {
				seen[c.Path] = true
				paths = append(paths, c.Path)
			}
		}
		a.filePicker.SetFiles(paths, root)
	}
}

// SetStore wires the Pebble store into the TUI for /impact and other commands.
func (a *App) SetStore(store *storage.Store) {
	a.store = store
}

// SetAgentRegistry injects the agent registry for name→color lookup during delegation.
func (a *App) SetAgentRegistry(reg *agents.AgentRegistry) {
	a.agentReg = reg
}

// SetNotepadManager injects the notepad manager for /notepad commands.
func (a *App) SetNotepadManager(mgr *notepad.Manager) {
	a.notepadMgr = mgr
}

// SetPrimaryAgent sets the primary agent name displayed in the TUI header.
// Called by tests and by the primary_agent_changed WS event handler.
func (a *App) SetPrimaryAgent(name string) {
	a.primaryAgent = name
	a.input.Placeholder = "Message " + name + "..."
}

// HeaderView returns the primary-agent annotation string shown in the footer.
// Returns "" when no primary agent is set. Exposed for tests.
func (a *App) HeaderView() string {
	if a.primaryAgent == "" {
		return ""
	}
	hv := "· " + a.primaryAgent + " ▾"
	if a.sessionCostUSD > 0 {
		hv += fmt.Sprintf(" · $%.2f", a.sessionCostUSD)
	}
	return hv
}

// safeActiveModel returns the current activeModel after validating it still
// exists in the model registry. If the model has been removed (e.g. the user
// switched agent configs mid-stream), it resets to the configured default and
// logs a warning so that the next backend call uses a valid model name.
// When the registry has not been probed yet (Available is empty) the current
// value is returned unchanged.
func (a *App) safeActiveModel() string {
	if a.activeModel == "" {
		if a.cfg != nil {
			return a.cfg.DefaultModel
		}
		return ""
	}
	if a.models != nil {
		reg := modelconfig.NewRegistry(a.models)
		if !reg.HasModel(a.activeModel) {
			slog.Warn("tui: activeModel no longer in registry, resetting to default",
				"model", a.activeModel)
			if a.cfg != nil {
				a.activeModel = a.cfg.DefaultModel
			} else {
				a.activeModel = ""
			}
		}
	}
	return a.activeModel
}

// recalcViewportHeight adjusts the viewport to fill the terminal
// minus whatever chrome is visible (status bar, divider, input, wizard).
func (a *App) recalcViewportHeight() {
	h := a.height - reservedLines
	if a.state == stateWizard {
		h -= wizardLines
	}
	if a.state == stateFilePicker {
		// Picker box: border(2) + breadcrumb(1) + filter(1) + sep(1) + rows + sep(1) + status(1)
		pickerLines := 7 + a.filePicker.maxVisible
		h -= pickerLines
	}
	if a.state == stateSessionPicker {
		// Session picker: header(1) + hint(1) + up to 10 rows.
		pickerRows := len(a.sessionPicker.filtered)
		if pickerRows > 10 {
			pickerRows = 10
		}
		h -= (2 + pickerRows)
	}
	// Chip area: 1 line when any attachments are pending.
	if len(a.attachments) > 0 {
		h -= 1
	}
	// When a follow-up is queued, reserve 3 extra lines for the follow-ups box.
	if a.queuedMsg != "" {
		h -= 3
	}
	if h < 3 {
		h = 3
	}
	a.viewport.Height = h
}

// SetUseAgentLoop enables/disables the agentic tool-calling loop.
func (a *App) SetUseAgentLoop(enabled bool) {
	a.useAgentLoop = enabled
}

// SetPriceTracker wires a pricing tracker into the App.
// The tracker accumulates cost from each completed stream and shows it in the footer.
func (a *App) SetPriceTracker(t *pricing.SessionTracker) { a.priceTracker = t }

// SetSkillRegistry wires the skill registry into the wizard picker.
func (a *App) SetSkillRegistry(reg *skills.SkillRegistry) {
	a.wizard.SetRegistry(reg)
}

// SetMuninnConnection stores the MuninnDB endpoint and connection status so the
// agent wizard can offer the memory configuration step.
func (a *App) SetMuninnConnection(endpoint string, connected bool) {
	a.muninnEndpoint = endpoint
	a.muninnConnected = connected
}

// SetChannels populates the sidebar and channel picker with channel names.
// Called at startup after loading channels from the spaces store.
func (a *App) SetChannels(names []string) {
	a.sidebar.SetChannels(names)
	a.channelPicker.SetChannels(names)
}

// SetChannelLeads sets the mapping of channel name → lead agent name.
// Used to pre-select the right primary agent when switching to a channel.
func (a *App) SetChannelLeads(leads map[string]string) {
	a.channelLeads = leads
}

// SetAppContext stores the shared AppContext for use by new-style screens.
func (a *App) SetAppContext(ctx interface{}) {
	a.appCtx = ctx
}

// Init satisfies tea.Model.
func (a *App) Init() tea.Cmd {
	return textinput.Blink
}

// Update satisfies tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		// Save relative scroll position before changing viewport dimensions so
		// we can restore the user's reading position after the resize.
		var scrollFraction float64
		if totalLines := a.viewport.TotalLineCount(); totalLines > 0 {
			scrollFraction = float64(a.viewport.YOffset) / float64(totalLines)
		}

		a.width = msg.Width
		a.height = msg.Height
		a.viewport.Width = msg.Width
		a.input.Width = msg.Width - 6 // account for box border + padding
		a.recalcViewportHeight()
		// Rebuild glamour renderer at new width.
		a.glamourRenderer = newGlamourRenderer(msg.Width - 4)

		// Restore relative scroll position now that height is recalculated.
		if newTotal := a.viewport.TotalLineCount(); newTotal > 0 && scrollFraction > 0 {
			newOffset := int(scrollFraction * float64(newTotal))
			a.viewport.SetYOffset(newOffset)
		}

	case dotTickMsg:
		if a.state == stateStreaming {
			a.dotPhase = (a.dotPhase + 1) % 3
		}
		return a, dotTickCmd()

	case tokenMsg:
		return a.handleTokenMsg(msg)
	case thinkingTokenMsg:
		return a.handleThinkingTokenMsg(msg)
	case warningMsg:
		return a.handleWarningMsg(msg)
	case toolCallMsg:
		return a.handleToolCallMsg(msg)
	case toolDoneMsg:
		return a.handleToolDoneMsg(msg)
	case ctrlCResetMsg:
		return a.handleCtrlCResetMsg(msg)
	case shellResultMsg:
		return a.handleShellResultMsg(msg)
	case streamDoneMsg:
		return a.handleStreamDoneMsg(msg)
	case parallelDoneMsg:
		return a.handleParallelDoneMsg(msg)

	case PermissionPromptMsg:
		return a.handlePermissionPromptMsg(msg)
	case writeApprovalMsg:
		return a.handleWriteApprovalMsg(msg)
	case agentDispatchFallbackMsg:
		return a.handleAgentDispatchFallbackMsg(msg)

	case delegationStartMsg:
		return a.handleDelegationStartMsg(msg)
	case delegationTokenMsg:
		return a.handleDelegationTokenMsg(msg)
	case delegationDoneMsg:
		return a.handleDelegationDoneMsg(msg)
	case swarmEventMsg:
		return a.handleSwarmEventMsg(msg)
	case swarmDoneMsg:
		return a.handleSwarmDoneMsg(msg)
	case wsEventMsg:
		return a.handleWsEventMsg(msg)

	case WizardSelectMsg:
		return a.handleWizardSelectMsg(msg)
	case WizardDismissMsg:
		return a.handleWizardDismissMsg(msg)
	case AgentWizardDoneMsg:
		return a.handleAgentWizardDoneMsg(msg)
	case AgentWizardCancelMsg:
		return a.handleAgentWizardCancelMsg(msg)
	case WizardTabCompleteMsg:
		return a.handleWizardTabCompleteMsg(msg)
	case FilePickerConfirmMsg:
		return a.handleFilePickerConfirmMsg(msg)
	case FilePickerCancelMsg:
		return a.handleFilePickerCancelMsg(msg)
	case SessionPickerMsg:
		return a.handleSessionPickerMsg(msg)
	case sessionResumedMsg:
		return a.handleSessionResumedMsg(msg)
	case SessionPickerDismissMsg:
		return a.handleSessionPickerDismissMsg(msg)

	case AtMentionSelectMsg:
		return a.handleAtMentionSelectMsg(msg)
	case AtMentionDismissMsg:
		return a.handleAtMentionDismissMsg(msg)
	case DMSwitchMsg:
		return a.handleDMSwitchMsg(msg)
	case ChannelSwitchMsg:
		return a.handleChannelSwitchMsg(msg)
	case pickerDismissMsg:
		return a.handlePickerDismissMsg(msg)
	case SidebarSelectMsg:
		return a.handleSidebarSelectMsg(msg)
	case SidebarBlurMsg:
		return a.handleSidebarBlurMsg(msg)

	case startupHealthCheckMsg:
		return a.handleStartupHealthCheckMsg(msg)
	case healthCheckResultMsg:
		return a.handleHealthCheckResultMsg(msg)

	case tea.KeyMsg:
		return a.handleKeyMsg(msg, cmds)
	}

	// Propagate to sub-models.
	if a.state != stateWizard {
		var inputCmd tea.Cmd
		a.input, inputCmd = a.input.Update(msg)
		cmds = append(cmds, inputCmd)
	}

	var vpCmd tea.Cmd
	a.viewport, vpCmd = a.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return a, tea.Batch(cmds...)
}

// View satisfies tea.Model.
func (a *App) View() string {
	if a.width == 0 {
		return "Loading…"
	}

	// ── Viewport (no border box — raw scrollable content) ───────────────────
	vp := a.viewport.View()

	// ── Divider ─────────────────────────────────────────────────────────────
	divider := StyleDim.Render(strings.Repeat("─", a.width))

	sections := []string{vp, divider}

	// ── Wizard overlay (sits between divider and input) ──────────────────────
	if a.state == stateWizard {
		sections = append(sections, a.wizard.View(a.width))
	}

	// ── Agent creation wizard overlay ────────────────────────────────────────
	if a.state == stateAgentWizard {
		sections = append(sections, a.agentWizard.View())
	}

	// ── File picker overlay ───────────────────────────────────────────────────
	if a.state == stateFilePicker {
		sections = append(sections, a.filePicker.View(a.width))
	}

	// ── Session picker overlay ────────────────────────────────────────────────
	if a.state == stateSessionPicker {
		sections = append(sections, a.sessionPicker.View())
	}

	// ── Swarm view overlay ────────────────────────────────────────────────────
	if a.state == stateSwarm && a.swarmView != nil {
		sections = append(sections, a.swarmView.View())
	}

	// ── Attachment chips (shown when files are attached) ──────────────────────
	if len(a.attachments) > 0 {
		sections = append(sections, a.renderChips())
	}

	// ── Follow-ups queue box (shown when a message is queued during streaming) ──
	if a.queuedMsg != "" {
		sections = append(sections, a.renderFollowUpBox())
	}

	// ── Input or approval prompt ─────────────────────────────────────────────
	switch a.state {
	case statePermAwait:
		sections = append(sections, a.renderPermissionPrompt())
	case stateWriteAwait:
		if a.writePending != nil {
			sections = append(sections, a.renderWriteApprovalPrompt())
		}
	default:
		sections = append(sections, a.renderInputBox())
	}

	// ── Footer status bar (always pinned at bottom) ──────────────────────────
	sections = append(sections, a.renderFooter())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// --- private helpers ---

// addLine appends a chat line to the chat history.
func (a *App) addLine(role, content string) {
	a.chat.history = append(a.chat.history, chatLine{role: role, content: content})
	a.chatLineOffsetsDirty = true
}

// addAssistantLine appends an assistant message chat line, recording agent attribution
// from the current delegation state when active.
func (a *App) addAssistantLine(content string) {
	line := chatLine{role: "assistant", content: content}
	if a.delegationAgent != "" {
		line.agentName = a.delegationAgent
		line.agentColor = a.delegationAgentColor
		line.agentIcon = agentIconFromName(a.delegationAgent)
	} else if a.primaryAgent != "" && a.agentReg != nil {
		if ag, ok := a.agentReg.ByName(a.primaryAgent); ok {
			line.agentName = ag.Name
			line.agentColor = ag.Color
			line.agentIcon = ag.Icon
		}
	}
	a.chat.history = append(a.chat.history, line)
	a.chatLineOffsetsDirty = true
}

func (a *App) submitMessage(raw string) tea.Cmd {
	// Show attachment header in the user bubble if files are queued.
	displayMsg := raw
	if len(a.attachments) > 0 {
		names := make([]string, len(a.attachments))
		for i, p := range a.attachments {
			names[i] = "@" + filepath.Base(p)
		}
		displayMsg = strings.Join(names, "  ") + "\n\n" + raw
	}
	a.addLine("user", displayMsg)
	a.chat.tokenCount = 0  // reset token counter for new stream
	a.state = stateStreaming // set before refreshViewport so generating spinner renders
	a.refreshViewport()

	lower := strings.ToLower(raw)

	// /notepad commands
	if strings.HasPrefix(lower, "/notepad") {
		a.state = stateChat
		return a.handleNotepadCmd(raw)
	}

	// /iterate N <section>
	if strings.HasPrefix(lower, "/iterate") {
		parts := strings.Fields(raw)
		n := 5
		sectionStart := 1
		if len(parts) > 1 {
			if num, err := strconv.Atoi(parts[1]); err == nil && num > 0 {
				n = num
				sectionStart = 2
			}
		}
		section := strings.Join(parts[sectionStart:], " ")
		if section == "" {
			section = "the current implementation"
		}
		a.addLine("system", fmt.Sprintf("Iterating %d times on: %s", n, section))
		a.refreshViewport()
		ctx, cancel := context.WithCancel(context.Background())
		a.chat.cancelStream = cancel
		return a.streamIterate(ctx, n, section)
	}

	// ! prefix: run a shell command and display output (no LLM involved).
	if strings.HasPrefix(raw, "!") {
		shellCmd := strings.TrimSpace(strings.TrimPrefix(raw, "!"))
		if shellCmd == "" {
			a.state = stateChat
			a.addLine("system", "Shell: type !<command> to run  e.g. !ls -la")
			a.refreshViewport()
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.chat.cancelStream = cancel
		return a.runShellCmd(ctx, shellCmd)
	}

	// # prefix typed directly: open the file picker.
	// @AgentName is now reserved for delegation (parsed elsewhere).
	if isFileAttachmentInput(raw) {
		a.state = stateFilePicker
		a.filePicker.maxVisible = max(6, a.height/3)
		a.filePicker.width = a.width
		a.filePicker.Show()
		a.recalcViewportHeight()
		// Restore input to empty (user typed #something but picker handles it).
		a.input.SetValue("")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.chat.cancelStream = cancel

	// Build context: shell output + attached files, both prepended at send time.
	var contextParts []string
	if a.shellContext != "" {
		contextParts = append(contextParts, a.shellContext)
		a.shellContext = ""
	}
	if len(a.attachments) > 0 {
		for _, rel := range a.attachments {
			fullPath := rel
			if a.workspaceRoot != "" && !filepath.IsAbs(rel) {
				fullPath = filepath.Join(a.workspaceRoot, rel)
			}
			if vision.IsImage(fullPath) {
				dataURI, err := vision.ReadImageAsDataURI(fullPath, 20480)
				if err != nil {
					contextParts = append(contextParts, fmt.Sprintf("<attached_file path=%q>\n(error reading image: %v)\n</attached_file>", rel, err))
				} else {
					a.pendingImageParts = append(a.pendingImageParts, backend.ContentPart{
						Type:     "image_url",
						ImageURL: dataURI,
					})
				}
			} else {
				data, err := os.ReadFile(fullPath)
				if err != nil {
					contextParts = append(contextParts, fmt.Sprintf("<attached_file path=%q>\n(error reading: %v)\n</attached_file>", rel, err))
				} else {
					contextParts = append(contextParts, fmt.Sprintf("<attached_file path=%q>\n%s\n</attached_file>", rel, string(data)))
				}
			}
		}
		a.attachments = nil
		a.chipFocused = false
		a.chipCursor = 0
	}
	msgForLLM := raw
	if len(contextParts) > 0 {
		msgForLLM = strings.Join(contextParts, "\n\n") + "\n\n" + raw
		a.recalcViewportHeight()
	}

	a.activeModel = a.activeAgentModel()

	// Dispatch to named agents if input contains a directive.
	if a.agentReg != nil && agents.ContainsAgentName(msgForLLM, a.agentReg) {
		// Try to dispatch — if handled, we're done.
		// If not handled (no directive matched), fall through to regular routing.
		if cmd := a.tryDispatch(ctx, msgForLLM); cmd != nil {
			return cmd
		}
	}
	if a.useAgentLoop {
		a.addLine("system", "Agent mode — using tools")
		a.refreshViewport()
		return a.streamAgentChat(ctx, msgForLLM)
	}
	return a.streamChat(ctx, msgForLLM)
}

func (a *App) streamIterate(ctx context.Context, n int, section string) tea.Cmd {
	r := streaming.NewRunner()
	a.chat.runner = r
	r.Start(ctx, func(emit func(string)) error {
		return a.orch.Iterate(ctx, n, section, func(token string) { emit(token) })
	})
	return waitForToken(r.TokenCh(), r.ErrCh())
}

// renderAgentRoster produces a formatted agent table for display in the TUI.
func (a *App) renderAgentRoster() string {
	all := a.agentReg.All()
	if len(all) == 0 {
		return "No agents configured. Add agents to ~/.huginn/agents.json to get started."
	}

	var sb strings.Builder
	sb.WriteString("  Agents ─────────────────────────────────────────────────\n")
	for _, ag := range all {
		icon := ag.Icon
		if icon == "" {
			icon = string([]rune(ag.Name)[0:1])
		}
		line := fmt.Sprintf("  %s  %-10s  %-26s  %s",
			icon, ag.Name, ag.GetModelID(), ag.Color)
		if ag.Color != "" {
			sb.WriteString(StyleAgentLabel(ag.Color).Render(line))
		} else {
			sb.WriteString(line)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("  ─────────────────────────────────────────────────────────\n")
	sb.WriteString(StyleDim.Render("  swap <name> <model>  · rename <name> <new>  · persona <name>"))
	return sb.String()
}

// activeAgentModel returns the model name for the currently active agent.
// It falls back to cfg.DefaultModel when no agent is selected or the agent
// has no model configured. Note: safeActiveModel() provides the runtime
// validation layer during streaming (registry lookup + fallback); this method
// is called before streaming begins to set a.activeModel for display purposes.
func (a *App) activeAgentModel() string {
	if a.agentReg != nil && a.primaryAgent != "" {
		if ag, ok := a.agentReg.ByName(a.primaryAgent); ok {
			if m := ag.GetModelID(); m != "" {
				return m
			}
		}
	}
	if a.cfg != nil && a.cfg.DefaultModel != "" {
		return a.cfg.DefaultModel
	}
	return ""
}

func (a *App) handleModelCommand(raw string) tea.Cmd {
	a.input.SetValue("")
	modelName, err := parseModelCommandIfAny(raw)
	if err != nil {
		return nil
	}
	a.models.Reasoner = modelName
	a.addLine("system", fmt.Sprintf("Model updated: %s", modelName))
	a.refreshViewport()
	return nil
}

func parseModelCommandIfAny(input string) (string, error) {
	return modelconfig.ParseModelCommand(input)
}

// isFileAttachmentInput returns true if the input string starts with '#',
// indicating a file attachment request (replaces the former '@' syntax).
func isFileAttachmentInput(s string) bool {
	return len(s) > 0 && s[0] == '#'
}

// Run starts the Bubble Tea program.
func Run(cfg *config.Config, orch *agent.Orchestrator, models *modelconfig.Models, version string) error {
	app := New(cfg, orch, models, version)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

