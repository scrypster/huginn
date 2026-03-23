package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/radar"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/symbol"
)

// convertStorageEdgesToSymbolEdges maps []storage.Edge to []symbol.Edge.
// Both types have identical JSON structure; the symbol package uses typed string aliases.
func convertStorageEdgesToSymbolEdges(in []storage.Edge) []symbol.Edge {
	out := make([]symbol.Edge, len(in))
	for i, e := range in {
		out[i] = symbol.Edge{
			From:       e.From,
			To:         e.To,
			Symbol:     e.Symbol,
			Confidence: symbol.Confidence(e.Confidence),
			Kind:       symbol.EdgeKind(e.Kind),
		}
	}
	return out
}

// runRadar opens the git repo at workspaceRoot, determines changed files from
// the HEAD commit vs its parent, then calls radar.Evaluate and returns findings.
func runRadar(store *storage.Store, workspaceRoot string) ([]radar.Finding, error) {
	r, err := git.PlainOpenWithOptions(workspaceRoot, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("not a git repo: %w", err)
	}
	ref, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("git HEAD: %w", err)
	}
	sha := ref.Hash().String()
	branch := ref.Name().Short()

	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("git commit object: %w", err)
	}

	var totalFiles int
	tree, _ := commit.Tree()
	if tree != nil {
		_ = tree.Files().ForEach(func(f *object.File) error {
			totalFiles++
			return nil
		})
	}

	var changedFiles []string
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err == nil {
			patch, err := parent.Patch(commit)
			if err == nil {
				for _, fp := range patch.FilePatches() {
					from, to := fp.Files()
					if to != nil {
						changedFiles = append(changedFiles, to.Path())
					} else if from != nil {
						changedFiles = append(changedFiles, from.Path())
					}
				}
			}
		}
	}

	if len(changedFiles) == 0 {
		return nil, nil
	}

	return radar.Evaluate(radar.EvaluateInput{
		DB:           store.DB(),
		RepoID:       workspaceRoot,
		SHA:          sha,
		Branch:       branch,
		ChangedFiles: changedFiles,
		TotalFiles:   totalFiles,
		AckStore:     nil,
	})
}

// formatRadarFindings renders a slice of radar findings as a human-readable
// terminal string, grouped by severity in descending order.
func formatRadarFindings(findings []radar.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Radar — %d finding(s)\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "[%s] %s\n", f.Severity.String(), f.Title)
		if f.Description != "" {
			fmt.Fprintf(&b, "  %s\n", f.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// formatImpactReport renders an ImpactReport as a human-readable string.
func formatImpactReport(r symbol.ImpactReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Impact analysis for '%s'\n\n", r.Symbol)
	if len(r.High) == 0 && len(r.Medium) == 0 && len(r.Low) == 0 {
		b.WriteString("  No callers or dependents found.\n")
		return b.String()
	}
	if len(r.High) > 0 {
		b.WriteString("  HIGH confidence:\n")
		for _, e := range r.High {
			b.WriteString("    " + e.Path + "\n")
		}
	}
	if len(r.Medium) > 0 {
		b.WriteString("  MEDIUM confidence:\n")
		for _, e := range r.Medium {
			b.WriteString("    " + e.Path + "\n")
		}
	}
	if len(r.Low) > 0 {
		b.WriteString("  LOW confidence:\n")
		for _, e := range r.Low {
			b.WriteString("    " + e.Path + "\n")
		}
	}
	return b.String()
}

func (a *App) handleNotepadCmd(raw string) tea.Cmd {
	parts := strings.Fields(raw)
	sub := ""
	if len(parts) >= 2 {
		sub = parts[1]
	}

	if a.notepadMgr == nil {
		a.addLine("system", "Notepads disabled.")
		a.refreshViewport()
		return nil
	}

	switch sub {
	case "list":
		nps, _ := a.notepadMgr.List()
		if len(nps) == 0 {
			a.addLine("system", "No notepads. Create with /notepad create <name>.")
		} else {
			var sb strings.Builder
			for _, np := range nps {
				sb.WriteString(fmt.Sprintf("  • %s [%s]\n", np.Name, np.Scope))
			}
			a.addLine("system", sb.String())
		}
	case "show":
		if len(parts) < 3 {
			a.addLine("error", "Usage: /notepad show <name>")
			break
		}
		np, err := a.notepadMgr.Get(parts[2])
		if err != nil {
			a.addLine("error", err.Error())
			break
		}
		a.addLine("system", fmt.Sprintf("**%s**\n\n%s", np.Name, np.Content))
	case "delete":
		if len(parts) < 3 {
			a.addLine("error", "Usage: /notepad delete <name>")
			break
		}
		if err := a.notepadMgr.Delete(parts[2]); err != nil {
			a.addLine("error", err.Error())
			break
		}
		a.addLine("system", fmt.Sprintf("Deleted %q.", parts[2]))
	default:
		a.addLine("system", "Usage: /notepad list | show <name> | create <name> | delete <name>")
	}
	a.refreshViewport()
	return nil
}

func (a *App) handleSlashCommand(cmd SlashCommand) tea.Cmd {
	a.input.SetValue("")
	switch cmd.Name {
	case "reason":
		a.addLine("system", "Reasoning mode — deep analysis enabled:")
	case "iterate":
		n := strings.TrimSpace(cmd.Args)
		if n == "" {
			n = "1"
		}
		a.addLine("system", fmt.Sprintf("Iterate mode — refining %s time(s):", n))
	case "agents":
		argsTrimmed := strings.TrimSpace(cmd.Args)
		if argsTrimmed == "new" {
			a.agentWizard = newAgentWizardWithMemory(a.muninnEndpoint, a.muninnConnected)
			a.state = stateAgentWizard
			a.recalcViewportHeight()
			return nil
		}
		if argsTrimmed == "" {
			// Always add roster to history so tests and users can see it.
			out := a.handleAgentsCommand("")
			a.addLine("system", out)
			// Also switch to the agents management screen.
			a.agentsScreen.refresh(a.agentReg)
			a.activeScreen = screenAgents
			return nil
		}
		out := a.handleAgentsCommand(cmd.Args)
		a.addLine("system", out)
	case "switch-model":
		a.addLine("system", `Switch — type: "use <model> for planning|coding|reasoning"`)
	case "help":
		a.addLine("system", helpText())
	case "impact":
		sym := strings.TrimSpace(cmd.Args)
		if sym == "" {
			a.addLine("system", "Usage: /impact <symbol-name>")
		} else if a.store == nil {
			a.addLine("system", "Impact analysis unavailable — storage not initialized.")
		} else {
			allEdges := a.store.GetAllEdges()
			symEdges := convertStorageEdgesToSymbolEdges(allEdges)
			report := symbol.ImpactQuery(sym, symEdges)
			if len(report.High)+len(report.Medium)+len(report.Low) == 0 {
				a.addLine("system", fmt.Sprintf("No references found for '%s'.\nRun /workspace to index the repo first.", sym))
			} else {
				a.addLine("system", formatImpactReport(report))
			}
		}
	case "stats":
		var text string
		if a.statsReg != nil {
			snap := a.statsReg.Snapshot()
			text = stats.FormatTable(snap)
		} else {
			text = "  stats registry not connected\n"
		}
		a.addLine("system", "Stats\n\n"+text)
	case "workspace":
		chunks := 0
		if a.idx != nil {
			chunks = len(a.idx.Chunks)
		}
		root := a.workspaceRoot
		if root == "" {
			root = "(not set)"
		}
		a.addLine("system", fmt.Sprintf("Workspace\n\n  root: %s\n  chunks indexed: %d\n", root, chunks))
	case "radar":
		if a.store == nil {
			a.addLine("system", "Radar unavailable — storage not initialized.")
		} else if a.workspaceRoot == "" {
			a.addLine("system", "Radar requires a workspace. Open huginn in a git repo.")
		} else {
			findings, err := runRadar(a.store, a.workspaceRoot)
			if err != nil {
				a.addLine("error", fmt.Sprintf("Radar: %v", err))
			} else if len(findings) == 0 {
				a.addLine("system", "Radar: No findings for the current state.")
			} else {
				a.addLine("system", formatRadarFindings(findings))
			}
		}
	case "swarm":
		// If swarmEvents is already set and no new args given, re-enter the swarm view.
		if cmd.Args == "" && a.swarmEvents != nil {
			a.state = stateSwarm
			a.recalcViewportHeight()
			return readSwarmEvent(a.swarmEvents)
		}
		return a.handleSwarmCommand(cmd.Args)
	case "parallel":
		return a.handleParallelCommand(cmd.Args)
	case "resume":
		return a.openSessionPicker()
	case "save":
		return a.saveSession()
	case "title":
		return a.renameSession(cmd.Args)

	// Space navigation — /dm and /channel open picker overlays.
	case "dm":
		filter := strings.TrimSpace(cmd.Args)
		a.dmPicker.Show(filter)
		a.recalcViewportHeight()
		return nil
	case "channel":
		filter := strings.TrimSpace(cmd.Args)
		a.channelPicker.Show(filter)
		a.recalcViewportHeight()
		return nil

	// Navigation screens — each gets a full-screen placeholder.
	case "models":
		a.activeScreen = screenModels
		return nil
	case "connections":
		a.activeScreen = screenConnections
		return nil
	case "skills":
		a.activeScreen = screenSkills
		return nil
	case "settings":
		a.activeScreen = screenSettings
		return nil
	case "workflows":
		a.activeScreen = screenWorkflows
		return nil
	case "logs":
		a.activeScreen = screenLogs
		return nil
	case "inbox":
		a.activeScreen = screenInbox
		return nil
	}
	a.refreshViewport()
	return nil
}

// handleAgentsCommand processes /agents sub-commands and returns a display string.
func (a *App) handleAgentsCommand(args string) string {
	if a.agentReg == nil {
		return "No agent registry configured. Add agents to ~/.huginn/agents.json to get started."
	}

	args = strings.TrimSpace(args)

	// No args → show roster
	if args == "" {
		return a.renderAgentRoster()
	}

	parts := strings.Fields(args)
	sub := strings.ToLower(parts[0])

	switch sub {
	case "swap":
		// /agents swap <name> <model>
		if len(parts) < 3 {
			return "Usage: /agents swap <name> <model>"
		}
		agName, newModel := parts[1], parts[2]
		ag, ok := a.agentReg.ByName(agName)
		if !ok {
			return fmt.Sprintf("Unknown agent: %q", agName)
		}
		// Persist — update the agent's per-file record
		def := agents.AgentDef{
			Name:         ag.Name,
			Model:        newModel,
			Provider:     ag.Provider,
			Endpoint:     ag.Endpoint,
			APIKey:       ag.APIKey,
			SystemPrompt: ag.SystemPrompt,
			Color:        ag.Color,
			Icon:         ag.Icon,
		}
		if err := agents.SaveAgentDefault(def); err != nil {
			return fmt.Sprintf("Error saving agent: %v", err)
		}
		// Update in-memory
		ag.SwapModel(newModel)
		return fmt.Sprintf("Swapped %s → %s (saved)", agName, newModel)

	case "rename":
		// /agents rename <name> <new-name>
		if len(parts) < 3 {
			return "Usage: /agents rename <name> <new>"
		}
		oldName, newName := parts[1], parts[2]
		ag, ok := a.agentReg.ByName(oldName)
		if !ok {
			return fmt.Sprintf("Unknown agent: %q", oldName)
		}
		if existing, exists := a.agentReg.ByName(newName); exists && existing != ag {
			return fmt.Sprintf("Agent with that name already exists: %q", newName)
		}
		// Write new file with updated name, then remove old file.
		memEnabled := ag.MemoryEnabled
		newDef := agents.AgentDef{
			Name:                newName,
			Model:               ag.GetModelID(),
			Provider:            ag.Provider,
			Endpoint:            ag.Endpoint,
			APIKey:              ag.APIKey,
			SystemPrompt:        ag.SystemPrompt,
			Color:               ag.Color,
			Icon:                ag.Icon,
			IsDefault:           ag.IsDefault,
			VaultName:           ag.VaultName,
			Plasticity:          ag.Plasticity,
			MemoryEnabled:       &memEnabled,
			ContextNotesEnabled: ag.ContextNotesEnabled,
			MemoryMode:          ag.MemoryMode,
			VaultDescription:    ag.VaultDescription,
			Toolbelt:            ag.Toolbelt,
			Skills:              ag.Skills,
		}
		if err := agents.SaveAgentDefault(newDef); err != nil {
			return fmt.Sprintf("Error saving renamed agent: %v", err)
		}
		// Remove old file (best-effort).
		_ = agents.DeleteAgentDefault(oldName)
		// Update in-memory: Rename atomically re-keys the registry map entry.
		ag.Rename(a.agentReg, newName)
		return fmt.Sprintf("Renamed %s → %s (saved)", oldName, newName)

	case "persona":
		// /agents persona <name>
		if len(parts) < 2 {
			return "Usage: /agents persona <name>"
		}
		ag, ok := a.agentReg.ByName(parts[1])
		if !ok {
			return fmt.Sprintf("Unknown agent %q.", parts[1])
		}
		if ag.SystemPrompt == "" {
			return fmt.Sprintf("%s has no custom persona (using default).", ag.Name)
		}
		return fmt.Sprintf("[%s's persona]\n%s", ag.Name, ag.SystemPrompt)

	case "create":
		// /agents create <name> <model>
		if len(parts) < 3 {
			return "Usage: /agents create <name> <model>"
		}
		agName, model := parts[1], parts[2]
		if _, exists := a.agentReg.ByName(agName); exists {
			return fmt.Sprintf("Agent %q already exists", agName)
		}
		runes := []rune(agName)
		if len(runes) == 0 {
			return "Agent name cannot be empty"
		}
		icon := strings.ToUpper(string(runes[:1]))
		def := agents.AgentDef{
			Name:         agName,
			Model:        model,
			SystemPrompt: fmt.Sprintf("You are %s, a helpful AI assistant.", agName),
			Color:        "#8B949E",
			Icon:         icon,
		}
		if err := agents.SaveAgentDefault(def); err != nil {
			return fmt.Sprintf("Error saving agent: %v", err)
		}
		newAgent := agents.FromDef(def)
		a.agentReg.Register(newAgent)
		return fmt.Sprintf("Created agent %s (model: %s, saved)", agName, model)

	case "delete":
		// /agents delete <name>
		if len(parts) < 2 {
			return "Usage: /agents delete <name>"
		}
		agName := parts[1]
		if _, ok := a.agentReg.ByName(agName); !ok {
			return fmt.Sprintf("Unknown agent: %q", agName)
		}
		if err := agents.DeleteAgentDefault(agName); err != nil {
			return fmt.Sprintf("Error deleting agent: %v", err)
		}
		a.agentReg.Unregister(agName)
		return fmt.Sprintf("Deleted agent %s (saved)", agName)

	default:
		return fmt.Sprintf("Unknown sub-command %q. Try: /agents, /agents swap, /agents rename, /agents persona, /agents create", sub)
	}
}

// handleSwarmCommand starts a named-agent swarm from a /swarm command.
//
// Syntax:  /swarm agent1:prompt1 | agent2:prompt2 | ...
//
// Each segment is "agentName:prompt". If the prompt is omitted the segment text
// is used as the prompt for the default first agent. The user is shown an error
// message for any parse or registry error.
func (a *App) handleSwarmCommand(args string) tea.Cmd {
	if args == "" {
		a.addLine("system", "Usage: /swarm agent1:prompt1 | agent2:prompt2 | ...")
		a.refreshViewport()
		return nil
	}

	segments := strings.Split(args, "|")
	var agentNames, prompts []string
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		idx := strings.IndexByte(seg, ':')
		var name, prompt string
		if idx < 0 {
			// No colon — entire segment is both agent name and prompt.
			name = seg
			prompt = seg
		} else {
			name = strings.TrimSpace(seg[:idx])
			prompt = strings.TrimSpace(seg[idx+1:])
		}
		if name == "" {
			continue
		}
		agentNames = append(agentNames, name)
		prompts = append(prompts, prompt)
	}

	if len(agentNames) == 0 {
		a.addLine("error", "swarm: no valid agent:prompt pairs found")
		a.refreshViewport()
		return nil
	}

	if a.orch == nil {
		a.addLine("system", "swarm: orchestrator not initialized")
		a.refreshViewport()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.chat.cancelStream = cancel

	ch, err := a.orch.SwarmWithAgents(ctx, agentNames, prompts, 0 /* default concurrency */)
	if err != nil {
		cancel()
		a.addLine("error", fmt.Sprintf("swarm: %v", err))
		a.refreshViewport()
		return nil
	}

	a.swarmEvents = ch
	a.state = stateSwarm
	a.recalcViewportHeight()
	return readSwarmEvent(a.swarmEvents)
}

// handleParallelCommand parses "|"-separated tasks and runs them concurrently.
// Returns a tea.Cmd that fans out to orch.BatchChat and displays labeled results.
func (a *App) handleParallelCommand(args string) tea.Cmd {
	if a.orch == nil {
		a.addLine("system", "Parallel mode requires an orchestrator.")
		a.refreshViewport()
		return nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		a.addLine("system", "Usage: /parallel <task1> | <task2> | ...")
		a.refreshViewport()
		return nil
	}
	// Split on | to get individual tasks.
	rawTasks := strings.Split(args, "|")
	tasks := make([]string, 0, len(rawTasks))
	for _, t := range rawTasks {
		if t := strings.TrimSpace(t); t != "" {
			tasks = append(tasks, t)
		}
	}
	if len(tasks) == 0 {
		a.addLine("system", "No tasks found. Usage: /parallel <task1> | <task2>")
		a.refreshViewport()
		return nil
	}
	a.addLine("system", fmt.Sprintf("Running %d tasks in parallel…", len(tasks)))
	a.refreshViewport()
	a.state = stateStreaming
	a.activeModel = a.cfg.DefaultModel

	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		results := a.orch.BatchChat(ctx, tasks)
		var sb strings.Builder
		for i, r := range results {
			sb.WriteString(fmt.Sprintf("**Task %d:** %s\n\n", i+1, r.Task))
			if r.Err != nil {
				sb.WriteString(fmt.Sprintf("*Error: %v*\n\n", r.Err))
			} else {
				sb.WriteString(r.Output)
				sb.WriteString("\n\n")
			}
			if i < len(results)-1 {
				sb.WriteString("---\n\n")
			}
		}
		return parallelDoneMsg{output: sb.String()}
	}
}

func helpText() string {
	return `Huginn keybindings:
  /reason       Deep reasoning mode
  /iterate N    Refine N times
  /switch-model Change model
  ctrl+c        Cancel stream / quit
  ctrl+o        Expand/collapse tool output`
}
