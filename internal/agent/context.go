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
	mu              sync.RWMutex
	idx             *repo.Index
	registry        *modelconfig.ModelRegistry
	stats           stats.Collector
	skillsFragment  string
	notepads        []*notepad.Notepad
	gitRoot         string
	searcher        search.Searcher
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

// SetSearcher sets the semantic searcher for context retrieval.
func (cb *ContextBuilder) SetSearcher(s search.Searcher) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.searcher = s
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
		sb.WriteString("**Main channel discipline — speak only when additive:**\n")
		sb.WriteString("After delegating work, respond in the main channel ONLY when you have one of the following:\n")
		sb.WriteString("1. A synthesized recommendation or next step that goes beyond what the team already said — genuine judgment, not a recap.\n")
		sb.WriteString("2. A question that only the user can answer to unblock the team.\n")
		sb.WriteString("3. A blocker or problem the user needs to know about.\n")
		sb.WriteString("Do NOT summarize or narrate what team members said if their responses are already visible in the thread. ")
		sb.WriteString("The user can read the thread. When work completes with nothing to add, stay silent — the thread badge signals completion.\n\n")
		sb.WriteString("**Delegation protocol:** When assigning work to a team member, use @AgentName in your response.\n")
		sb.WriteString("Example: \"@Sam please run the test coverage report and summarize the gaps.\"\n")
		sb.WriteString("The @mention triggers automatic thread creation — the named agent receives the task and works in a dedicated thread visible to the team.\n\n")
		sb.WriteString("**Team members:**\n")
	} else {
		sb.WriteString("**Channel:** ")
		sb.WriteString(spaceName)
		sb.WriteString("\n**Lead Agent:** ")
		sb.WriteString(leadAgent)
		sb.WriteString("\n\n**Team members:**\n")
	}
	for _, m := range members {
		desc := m.Description
		if desc == "" {
			desc = "specialist agent"
		}
		fmt.Fprintf(&sb, "- **%s**: %s\n", m.Name, desc)
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
