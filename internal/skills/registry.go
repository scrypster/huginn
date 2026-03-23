package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// SkillLoadError records a single skill file that failed to load.
type SkillLoadError struct {
	File string
	Err  error
}

func (e *SkillLoadError) Error() string {
	return fmt.Sprintf("skills: %s: %s", e.File, e.Err)
}

func (e *SkillLoadError) Unwrap() error { return e.Err }

//go:embed builtin/*.md
var builtinSkillsFS embed.FS

type SkillRegistry struct {
	mu              sync.RWMutex
	skills          []Skill
	toolOwners      map[string]string  // toolName → skillName
	combinedCache   string             // cached CombinedPromptFragment result
	combinedDirty   bool               // true when cache must be regenerated
	reloadCallback  func()             // optional callback invoked after hot reload
	invocations     map[string]int64   // toolName → invocation count
	lastReloadedAt  int64              // unix nano of last hot reload; 0 = never
}

func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills:        make([]Skill, 0),
		toolOwners:    make(map[string]string),
		invocations:   make(map[string]int64),
		combinedDirty: true, // no skills yet → treat as dirty
	}
}

// SetReloadCallback registers a callback that will be invoked after each hot
// reload. The callback is called without any registry locks held. Pass nil to
// clear an existing callback.
func (r *SkillRegistry) SetReloadCallback(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reloadCallback = fn
}

// NotifyReload marks the combined-fragment cache as stale, records the reload
// timestamp, and invokes the registered reload callback (if any).
// Call this after any bulk skill reload.
func (r *SkillRegistry) NotifyReload() {
	r.mu.Lock()
	r.combinedDirty = true
	r.lastReloadedAt = time.Now().UnixNano()
	cb := r.reloadCallback
	r.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// RecordInvocation increments the invocation counter for the given tool name.
// Safe for concurrent use; counter increments are best-effort (no overflow guard).
func (r *SkillRegistry) RecordInvocation(toolName string) {
	r.mu.Lock()
	r.invocations[toolName]++
	r.mu.Unlock()
}

// InvocationCounts returns a snapshot of per-tool invocation counts.
func (r *SkillRegistry) InvocationCounts() map[string]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]int64, len(r.invocations))
	for k, v := range r.invocations {
		out[k] = v
	}
	return out
}

// LastReloadedAt returns the Unix nanosecond timestamp of the last hot reload,
// or 0 if no reload has occurred since the registry was created.
func (r *SkillRegistry) LastReloadedAt() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastReloadedAt
}

// Register adds a skill to the registry. If a skill with the same name is
// already registered (e.g. after a hot reload), the old version is replaced
// atomically — the new version always wins. Returns an error only if a tool
// name is owned by a *different* skill (cross-skill collision).
func (r *SkillRegistry) Register(s Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for cross-skill tool name collisions (different skill, same tool).
	if tooler, ok := s.(interface{ Tools() []tools.Tool }); ok {
		for _, t := range tooler.Tools() {
			if owner, exists := r.toolOwners[t.Name()]; exists && owner != s.Name() {
				return fmt.Errorf("skills: tool %q already registered by skill %q", t.Name(), owner)
			}
		}
	}

	// Remove any existing skill with the same name (version upgrade / hot reload).
	// This ensures the new version replaces the old one instead of coexisting.
	for i, existing := range r.skills {
		if existing.Name() == s.Name() {
			// Log version transitions for observability.
			if existingVS, ok1 := existing.(VersionedSkill); ok1 {
				if newVS, ok2 := s.(VersionedSkill); ok2 && existingVS.Version() != newVS.Version() {
					slog.Info("skills: upgrading skill version",
						"skill", s.Name(),
						"from", existingVS.Version(),
						"to", newVS.Version())
				}
			}
			// Remove tool ownership for the old skill's tools.
			if tooler, ok := existing.(interface{ Tools() []tools.Tool }); ok {
				for _, t := range tooler.Tools() {
					delete(r.toolOwners, t.Name())
				}
			}
			r.skills = append(r.skills[:i], r.skills[i+1:]...)
			break
		}
	}

	// Register the new skill's tools.
	if tooler, ok := s.(interface{ Tools() []tools.Tool }); ok {
		for _, t := range tooler.Tools() {
			r.toolOwners[t.Name()] = s.Name()
		}
	}
	r.skills = append(r.skills, s)
	r.combinedDirty = true // invalidate the combined-fragment cache
	return nil
}

func (r *SkillRegistry) All() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, len(r.skills))
	copy(out, r.skills)
	return out
}

// CombinedPromptFragment returns the combined system-prompt fragment for all
// registered skills. The result is cached and only recomputed when a skill is
// added, removed, or the registry is hot-reloaded (combinedDirty == true).
func (r *SkillRegistry) CombinedPromptFragment() string {
	// Fast path: cache hit under read lock.
	r.mu.RLock()
	if !r.combinedDirty {
		cached := r.combinedCache
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	// Slow path: recompute under write lock.
	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock.
	if !r.combinedDirty {
		return r.combinedCache
	}
	parts := make([]string, 0, len(r.skills))
	for _, s := range r.skills {
		if frag := s.SystemPromptFragment(); frag != "" {
			parts = append(parts, wrapSkillContent(s.Name(), frag))
		}
	}
	r.combinedCache = strings.Join(parts, "\n\n")
	r.combinedDirty = false
	return r.combinedCache
}

func (r *SkillRegistry) CombinedRuleContent() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	parts := make([]string, 0, len(r.skills))
	for _, s := range r.skills {
		if rc := s.RuleContent(); rc != "" {
			parts = append(parts, rc)
		}
	}
	return strings.Join(parts, "\n\n")
}

// AllTools collects all tools from all registered skills into a single slice.
// It is available for future use (e.g., dynamic skill reload without restarting
// the tool registry). The current production path uses InjectAgentExecutor,
// which iterates the flat tool registry after startup wiring is complete.
// Returns an empty non-nil slice if no skills have tools.
func (r *SkillRegistry) AllTools() []tools.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []tools.Tool
	for _, s := range r.skills {
		skillTools := s.Tools()
		result = append(result, skillTools...)
	}
	return result
}

// LoadBuiltins loads the embedded built-in skills into the registry.
// Call before LoadUserSkills so user skills with the same name override built-ins.
// Returns a slice of per-file load errors (nil if all succeeded). A non-empty
// []error does NOT mean the registry is unusable — successfully parsed skills
// are still registered.
func (r *SkillRegistry) LoadBuiltins() (errs []error) {
	entries, err := fs.ReadDir(builtinSkillsFS, "builtin")
	if err != nil {
		return []error{fmt.Errorf("skills: LoadBuiltins: %w", err)}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := fs.ReadFile(builtinSkillsFS, "builtin/"+entry.Name())
		if err != nil {
			errs = append(errs, &SkillLoadError{File: entry.Name(), Err: err})
			continue
		}
		s, err := ParseMarkdownSkillBytes(data)
		if err != nil {
			errs = append(errs, &SkillLoadError{File: entry.Name(), Err: err})
			continue
		}
		if err := r.Register(s); err != nil {
			errs = append(errs, &SkillLoadError{File: entry.Name(), Err: err})
		}
	}
	return errs
}

// FindByName returns the skill with the given name.
// Last registered wins — user skills (loaded after built-ins) override built-ins.
// Returns nil if no skill with that name exists.
func (r *SkillRegistry) FindByName(name string) Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.skills) - 1; i >= 0; i-- {
		if r.skills[i].Name() == name {
			return r.skills[i]
		}
	}
	return nil
}

// FilteredSkillsFragment returns the combined system prompt fragment for the
// given skill names. The names argument controls injection:
//
//   nil       → CombinedPromptFragment() — global fallback, all enabled skills
//   []string{} → "" — agent explicitly opts out of all skills
//   ["x","y"] → only skills named "x" and "y"
//
// Unknown names log a warning and are skipped. Duplicates are deduplicated.
func (r *SkillRegistry) FilteredSkillsFragment(names []string) string {
	if names == nil {
		return r.CombinedPromptFragment()
	}
	var parts []string
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		sk := r.FindByName(name)
		if sk == nil {
			slog.Warn("skills: unknown skill in agent list", "name", name)
			continue
		}
		// Combine prompt + rules for this skill, then wrap with delimiters.
		var inner []string
		if frag := sk.SystemPromptFragment(); frag != "" {
			inner = append(inner, frag)
		}
		if rules := sk.RuleContent(); rules != "" {
			inner = append(inner, rules)
		}
		if len(inner) > 0 {
			parts = append(parts, wrapSkillContent(name, strings.Join(inner, "\n\n")))
		}
	}
	return strings.Join(parts, "\n\n")
}

// wrapSkillContent wraps skill content with clear delimiters so the model can
// identify skill boundaries. This is the primary prompt-injection defense for
// user-supplied skill content.
//
// Trust boundary: skill content is user-authored markdown loaded from disk or
// a remote registry. It is NOT trusted system instructions. The delimiters
// allow the model to distinguish skill context from core system instructions.
func wrapSkillContent(name, content string) string {
	return fmt.Sprintf("<skill name=%q>\n%s\n</skill>", name, content)
}
