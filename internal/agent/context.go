package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/stats"
)

const (
	// defaultContextBytes is used when we can't determine the model's context window.
	defaultContextBytes = 96 * 1024

	// treeReserveBytes is space reserved for the repo tree at the top of every context.
	treeReserveBytes = 4 * 1024

	// systemPromptReserveBytes is space reserved for the system prompt template.
	systemPromptReserveBytes = 2 * 1024
)

// ContextBuilder assembles token-budget-aware context for LLM requests.
// It queries the workspace for relevant chunks and trims to fit the model's
// context window. Injected into the Orchestrator.
type ContextBuilder struct {
	mu             sync.RWMutex
	idx            *repo.Index
	registry       *modelconfig.ModelRegistry
	stats          stats.Collector
	skillsFragment string
	notepads       []*notepad.Notepad
	gitRoot        string
	workspaceRoot  string
	searcher       search.Searcher
}

// NewContextBuilder creates a ContextBuilder.
func NewContextBuilder(idx *repo.Index, registry *modelconfig.ModelRegistry, stats stats.Collector) *ContextBuilder {
	return &ContextBuilder{
		idx:      idx,
		registry: registry,
		stats:    stats,
	}
}

// SetSkillsFragment sets the prebuilt skills + workspace rules fragment that is
// appended to the end of every Build() and BuildWithSymbols() result.
// Call this once at startup after loading skills.
func (cb *ContextBuilder) SetSkillsFragment(s string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.skillsFragment = s
}

// SkillsFragment returns the current skills fragment string injected into context.
func (cb *ContextBuilder) SkillsFragment() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.skillsFragment
}

// SetNotepads injects notepads into the context builder to be included in all builds.
func (cb *ContextBuilder) SetNotepads(nps []*notepad.Notepad) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.notepads = nps
}

// SetGitRoot sets the git repository root for injecting git context into builds.
func (cb *ContextBuilder) SetGitRoot(root string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.gitRoot = root
}

// SetWorkspaceRoot sets a fallback workspace root used for loading project
// instructions (.huginn.md) when no git root is set. Callers that operate
// outside a git repository (or that haven't wired git detection at all)
// should set this so .huginn.md still loads in a plain directory.
func (cb *ContextBuilder) SetWorkspaceRoot(root string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.workspaceRoot = root
}

// SetSearcher sets the semantic searcher for context retrieval.
func (cb *ContextBuilder) SetSearcher(s search.Searcher) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.searcher = s
}

// SearchHealth returns search telemetry when the configured searcher reports it.
func (cb *ContextBuilder) SearchHealth() (search.HealthSnapshot, bool) {
	cb.mu.RLock()
	searcher := cb.searcher
	cb.mu.RUnlock()
	if reporter, ok := searcher.(search.HealthReporter); ok {
		return reporter.SearchHealth(), true
	}
	return search.HealthSnapshot{}, false
}

// Build assembles a context string for the given query and model slot.
// It respects the model's context window, leaving room for the system prompt
// and the user message.
//
// Returns a formatted string ready to prepend to the system prompt:
//
//	## Repository Structure
//	```
//	...
//	```
//	## Repository Context
//	### file.go (line 1)
//	```
//	...
//	```
func (cb *ContextBuilder) Build(query string, modelName string) string {
	return cb.BuildCtx(context.Background(), query, modelName)
}

// BuildCtx assembles context with explicit context (for semantic search support).
func (cb *ContextBuilder) BuildCtx(ctx context.Context, query string, modelName string) string {
	// Snapshot mutable fields under the read lock to avoid data races.
	cb.mu.RLock()
	gitRoot := cb.gitRoot
	workspaceRoot := cb.workspaceRoot
	skillsFragment := cb.skillsFragment
	notepads := cb.notepads
	searcher := cb.searcher
	cb.mu.RUnlock()

	// Determine byte budget for context.
	// Context window size (in tokens) × ~4 bytes/token, minus reserves.
	// If registry is nil or context window is 0, use default.
	contextBytes := defaultContextBytes
	if cb.registry != nil {
		cw := cb.registry.ModelContextWindow(modelName)
		if cw > 0 {
			// Convert token count to bytes (rough: 1 token ≈ 4 bytes).
			// Reserve 30% for system prompt + user message + response.
			available := int(float64(cw) * 4.0 * 0.70)
			if available > 0 {
				contextBytes = available
			}
		}
	}

	// Reserve space for tree and system overhead.
	chunkBudget := contextBytes - treeReserveBytes - systemPromptReserveBytes
	if chunkBudget < 8*1024 {
		chunkBudget = 8 * 1024 // minimum useful context
	}

	var sb strings.Builder

	// 1. Git context (if gitRoot is set and repo is a git repository).
	if gitRoot != "" {
		if gitCtx := buildGitContext(gitRoot); gitCtx != "" {
			sb.WriteString(gitCtx)
			sb.WriteString("\n")
		}
	}

	// 2. Repo tree (always included, small).
	if cb.idx != nil {
		tree := cb.idx.BuildTree()
		sb.WriteString(tree)
	}

	// 3. Relevant chunks from semantic search or BM25 scoring.
	if query != "" {
		var chunks string
		if searcher != nil {
			// Use semantic search (hybrid keyword+vector)
			maxChunks := (chunkBudget / 1024) + 1
			if maxChunks < 3 {
				maxChunks = 3
			}
			searchResults, err := searcher.Search(ctx, query, maxChunks)
			if err == nil && len(searchResults) > 0 {
				chunks = formatSearchResults(searchResults, chunkBudget)
			}
		}
		// Fallback to BM25 if searcher is not available or returns no results
		if chunks == "" && cb.idx != nil {
			chunks = cb.idx.BuildContext(query, chunkBudget)
		}
		if chunks != "" {
			sb.WriteString(chunks)
		}
	}

	result := sb.String()

	// Skills fragment (system prompt injections + workspace rule files).
	if skillsFragment != "" {
		result += "\n\n## Skills & Workspace Rules\n" + skillsFragment
	}

	// Project instructions (.huginn.md / .huginn/instructions.md), loaded here so
	// every context-build path (web chat, delegated threads, scheduled agents)
	// gets them consistently — not just the one call site that used to load them
	// directly (mcp_agent_chat.go).
	//
	// Prefer gitRoot (also used for git context above), but fall back to
	// workspaceRoot when there's no git root — e.g. a plain, non-git
	// directory, or a caller that only wired workspace detection. Without
	// this fallback .huginn.md silently stops loading outside a git repo,
	// even though the older direct-load path (o.workspaceRoot) never had
	// that restriction.
	instructionsRoot := gitRoot
	if instructionsRoot == "" {
		instructionsRoot = workspaceRoot
	}
	if instructionsRoot != "" {
		if projectInstructions := LoadProjectInstructions(instructionsRoot); projectInstructions != "" {
			result += "\n\n## Project Instructions\n" + projectInstructions
		}
	}

	// Active notepads (persistent user-managed context).
	if len(notepads) > 0 {
		const maxNotepadsChars = 32768
		var npSb strings.Builder
		remaining := maxNotepadsChars
		for _, np := range notepads {
			entry := "### " + np.Name + "\n" + np.Content + "\n"
			if len(entry) > remaining {
				continue
			}
			npSb.WriteString(entry)
			remaining -= len(entry)
		}
		if npSb.Len() > 0 {
			result += "\n\n## Active Notepads\n" + npSb.String()
		}
	}

	// Record stats.
	if cb.stats != nil {
		cb.stats.Record("agent.context_bytes", float64(len(result)), fmt.Sprintf("model:%s", modelName))
	}

	return result
}

// formatSearchResults converts semantic search results to a formatted context string.
func formatSearchResults(chunks []search.Chunk, budget int) string {
	if len(chunks) == 0 {
		return ""
	}

	var sb strings.Builder
	remaining := budget
	added := 0

	for _, chunk := range chunks {
		entry := fmt.Sprintf("### %s (line %d)\n```\n%s\n```\n",
			chunk.Path, chunk.StartLine, chunk.Content)

		if len(entry) > remaining {
			break
		}

		sb.WriteString(entry)
		remaining -= len(entry)
		added++
	}

	if added == 0 {
		return ""
	}

	return "## Repository Context\n" + sb.String()
}

// SpaceMember represents a member agent in a channel space, with an optional
// human-readable description for inclusion in team context prompts.
type SpaceMember struct {
	Name        string
	Description string
}

func delegationExampleAgents(selfName string, members []SpaceMember) (string, string) {
	names := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, m := range members {
		name := strings.TrimSpace(m.Name)
		if name == "" || strings.EqualFold(name, selfName) {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
		if len(names) == 2 {
			break
		}
	}
	if len(names) == 0 {
		return "<teammate_name>", "<another_teammate_name>"
	}
	if len(names) == 1 {
		return names[0], "<another_teammate_name>"
	}
	return names[0], names[1]
}

// BuildSpaceContextBlock generates a system prompt addendum for a multi-agent channel.
// selfName is the name of the agent receiving this context; leadAgent is the channel lead.
// When selfName matches leadAgent, the block describes the agent as the lead with routing
// responsibilities. Otherwise it provides a generic space context block.
//
// Returns an empty string for non-channel contexts (kind != "channel") or when there
// are no members to list.
func BuildSpaceContextBlock(spaceName, spaceKind, selfName, leadAgent string, members []SpaceMember) string {
	if spaceKind != "channel" || len(members) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[Team Context]\n")
	if strings.EqualFold(selfName, leadAgent) {
		sb.WriteString("You are ")
		sb.WriteString(leadAgent)
		sb.WriteString(", the lead agent for the \"")
		sb.WriteString(spaceName)
		sb.WriteString("\" channel. Route specialized tasks to the right team member and synthesize results.\n\n")
		sb.WriteString("**Delegation protocol — use the `delegate_to_agent` tool:**\n")
		sb.WriteString("When you need a team member to handle a sub-task, call `delegate_to_agent` with their name and a clear, actionable task description.\n")
		sb.WriteString("The tool spawns a thread automatically — the agent's reply will appear as a thread on your message, just like Slack.\n\n")
		sb.WriteString("Prefer delegate-first routing when a specialist exists for the request. Only do the task yourself when no specialist is clearly better suited.\n\n")
		exampleAgentA, exampleAgentB := delegationExampleAgents(selfName, members)
		fmt.Fprintf(&sb, "  GOOD: Call delegate_to_agent with agent=%q, task=%q, rationale=%q\n",
			exampleAgentA,
			"Calculate 3+3 and return a one-sentence answer.",
			fmt.Sprintf("%s specialises in arithmetic", exampleAgentA),
		)
		fmt.Fprintf(&sb, "  GOOD: Call delegate_to_agent with agent=%q, task=%q, rationale=%q\n",
			exampleAgentB,
			"List the top 3 best practices for Go unit tests. Keep it brief.",
			fmt.Sprintf("%s is the Go expert", exampleAgentB),
		)
		sb.WriteString("A user @mention addresses that agent for the turn — they receive the message, not you. When YOU need a teammate, call delegate_to_agent; writing @Name in your own reply does not assign work.\n")
		sb.WriteString("  BAD:  Writing @Name in your reply and expecting it to assign work — use delegate_to_agent instead.\n")
		sb.WriteString("  BAD:  A vague task like \"help with this\" — the agent needs a specific, complete description to act.\n\n")
		sb.WriteString("**Collecting results — use `wait_for_threads`:**\n")
		sb.WriteString("After delegating, call `wait_for_threads` (with the thread IDs, or no arguments for all) to block until your delegates finish and receive their full results in one response.\n")
		sb.WriteString("If it times out, the response shows each pending thread's live activity and a stall warning when an agent has gone quiet — call it again if they are still working, or `list_team_status` to check progress without blocking.\n\n")
		sb.WriteString("**Main channel discipline — speak only when additive:**\n")
		sb.WriteString("After delegating, the thread badge shows progress. Only post again when you have:\n")
		sb.WriteString("1. A synthesized recommendation that goes beyond what the team said.\n")
		sb.WriteString("2. A question only the user can answer.\n")
		sb.WriteString("3. A blocker the user needs to know about.\n")
		sb.WriteString("Do NOT summarize or narrate what team members said — the user can read the thread.\n\n")
		sb.WriteString("**Team members:**\n")
		sb.WriteString("Select delegates by matching each member card's Role, Tools, Connections, and Skills to the request; do not guess specialist fit.\n")
	} else {
		sb.WriteString("**Channel:** ")
		sb.WriteString(spaceName)
		sb.WriteString("\n**Lead Agent:** ")
		sb.WriteString(leadAgent)
		sb.WriteString("\n\n**Team members:**\n")
	}
	for _, m := range members {
		if m.Description != "" {
			// Description is a pre-formatted capability card — output directly.
			sb.WriteString(m.Description)
			if !strings.HasSuffix(m.Description, "\n") {
				sb.WriteString("\n")
			}
		} else {
			fmt.Fprintf(&sb, "- **%s**: specialist agent\n", m.Name)
		}
	}
	return sb.String()
}

// ChannelRoster summarizes a channel for cross-space awareness in DMs.
type ChannelRoster struct {
	Name      string
	LeadAgent string
	Members   []SpaceMember
}

// BuildDMCrossSpaceContextBlock generates context for a lead agent in a DM,
// listing the channels they participate in and the team members in each.
// This gives the agent Slack-like awareness: "I lead these channels with
// these team members, and I can delegate work to them."
func BuildDMCrossSpaceContextBlock(selfName string, channels []ChannelRoster) string {
	if len(channels) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[Your Channels & Teams]\n")
	sb.WriteString("You participate in the following team channels. When the user asks you to do something that involves a team member, ")
	sb.WriteString("call the delegate_to_agent tool to spawn work and keep the handoff visible in thread activity.\n\n")
	for _, ch := range channels {
		sb.WriteString("**#")
		sb.WriteString(ch.Name)
		sb.WriteString("**")
		if strings.EqualFold(selfName, ch.LeadAgent) {
			sb.WriteString(" (you are the lead)")
		}
		sb.WriteString("\n")
		for _, m := range ch.Members {
			if m.Description != "" {
				// Description is a pre-formatted capability card — output directly.
				sb.WriteString(m.Description)
				if !strings.HasSuffix(m.Description, "\n") {
					sb.WriteString("\n")
				}
			} else {
				fmt.Fprintf(&sb, "  - **%s**: specialist agent\n", m.Name)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// BuildDeskFloorContextBlock tells a desk-DM lead they can A2A any other
// desk-floor agent. peers should include capability cards. Returns empty
// when there is no one else on the floor (so a lone DM stays quiet).
func BuildDeskFloorContextBlock(selfName string, peers []SpaceMember) string {
	others := make([]SpaceMember, 0, len(peers))
	for _, m := range peers {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		if strings.EqualFold(m.Name, selfName) {
			continue
		}
		others = append(others, m)
	}
	if len(others) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[Desk Floor]\n")
	sb.WriteString("This is a 1:1 desk DM with the human. The desk is a shared floor: you may talk to any other desk agent with delegate_to_agent or consult_agent, then wait_for_threads and relay their answer.\n")
	sb.WriteString("Do not do their job yourself when they exist, and do not say they are not a member of this space.\n\n")
	sb.WriteString("**Desk agents you can reach:**\n")
	for _, m := range others {
		if m.Description != "" {
			sb.WriteString(m.Description)
			if !strings.HasSuffix(m.Description, "\n") {
				sb.WriteString("\n")
			}
		} else {
			fmt.Fprintf(&sb, "- **%s**: desk agent\n", m.Name)
		}
	}
	return sb.String()
}

// BuildWithSymbols adds symbol context (future: from symbol store).
// For now delegates to Build, reserving the method name for the future
// when the symbol extractor is wired into the context pipeline.
func (cb *ContextBuilder) BuildWithSymbols(query string, modelName string, symbolRefs []string) string {
	base := cb.Build(query, modelName)
	if len(symbolRefs) == 0 {
		return base
	}

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n## Referenced Symbols\n")
	for _, ref := range symbolRefs {
		sb.WriteString("- ")
		sb.WriteString(ref)
		sb.WriteString("\n")
	}
	return sb.String()
}

// correlationIDKey is the context key for correlation IDs used in request tracing.
type correlationIDKey struct{}

// SetCorrelationID attaches a correlation ID to the context.
func SetCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// GetCorrelationID retrieves the correlation ID from the context.
// Returns empty string if none is set.
func GetCorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}
