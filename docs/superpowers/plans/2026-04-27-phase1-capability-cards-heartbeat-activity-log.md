# Phase 1: Capability Cards + Heartbeat + Activity Log — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire capability cards into all agent-awareness codepaths, add per-agent heartbeat (conversational DM delivery), and rename Inbox → Activity Log in the web UI.

**Architecture:** `BuildCapabilityCard()` in `internal/agents/` generates a deterministic, always-accurate public resume for each agent. `BuildRoster()` and `InjectSpaceContext` (ws.go) both consume it. Heartbeat is an auto-generated workflow YAML under `~/.huginn/workflows/` — the existing WorkflowsWatcher picks it up with no new engine. Activity Log is a nav label change with no backend changes.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, Vue 3 + TypeScript, `web/src/App.vue`, `web/src/views/AgentsView.vue`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/agents/capability_card.go` | **Create** | `BuildCapabilityCard()`, `CapabilityCardInput`, provider display name map |
| `internal/agents/capability_card_test.go` | **Create** | Unit tests for all card formats |
| `internal/agents/roster.go` | **Modify** | Refactor `BuildRoster()` to call `BuildCapabilityCard()` |
| `internal/agents/roster_test.go` | **Modify** | Update existing roster tests for new multi-line card format |
| `internal/agent/context.go` | **No change** | `BuildSpaceContextBlock` / `BuildDMCrossSpaceContextBlock` unchanged — card content comes from call site |
| `internal/server/ws.go` | **Modify** | `InjectSpaceContext`: populate `SpaceMember.Description` via `BuildCapabilityCard()` instead of raw Description field |
| `internal/agents/config.go` | **Modify** | Add `HeartbeatEnabled bool`, `HeartbeatCron string` to `AgentDef` |
| `internal/agents/heartbeat_yaml.go` | **Create** | `SyncHeartbeatYAMLDefault`, `DeleteHeartbeatYAMLDefault`, `RenameHeartbeatYAMLDefault`, `IsManaged`, `HeartbeatYAMLPath` |
| `internal/agents/heartbeat_yaml_test.go` | **Create** | Unit tests for all heartbeat YAML lifecycle operations |
| `internal/server/handlers.go` | **Modify** | `handleUpdateAgent`: call heartbeat sync after save; `handleDeleteAgent`: call heartbeat delete |
| `internal/server/handlers_heartbeat_test.go` | **Create** | Integration tests for heartbeat lifecycle in HTTP handlers |
| `web/src/views/AgentsView.vue` | **Modify** | Add `heartbeat_enabled`/`heartbeat_cron` to form type; add heartbeat toggle UI section |
| `web/src/App.vue` | **Modify** | Nav label `'Inbox'` → `'Activity Log'` |
| `web/src/views/InboxView.vue` | **Modify** | Heading `Inbox` → `Activity Log` |
| `web/src/views/__tests__/InboxView.test.ts` | **Modify** | Update test asserting `'Inbox'` heading text |

---

## Task 1: `BuildCapabilityCard()` function

**Files:**
- Create: `internal/agents/capability_card.go`
- Create: `internal/agents/capability_card_test.go`

- [ ] **Step 1.1: Write failing tests**

```go
// internal/agents/capability_card_test.go
package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
)

func testInfoFn(modelID string) *modelconfig.ModelInfo {
	switch modelID {
	case "capable-model":
		return &modelconfig.ModelInfo{Tier: modelconfig.TierHigh, SupportsTools: true}
	case "medium-model":
		return &modelconfig.ModelInfo{Tier: modelconfig.TierMedium, SupportsTools: true}
	case "low-model":
		return &modelconfig.ModelInfo{Tier: modelconfig.TierLow, SupportsTools: false}
	}
	return nil
}

func TestBuildCapabilityCard_FullCard(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Ares",
		SystemPrompt: "You are Ares, a security-focused monitoring agent.",
		ModelID:      "capable-model",
		LocalTools:   []string{"filesystem", "web_search"},
		Toolbelt: []agents.ToolbeltEntry{
			{Provider: "github", ConnectionID: "conn1"},
		},
		Skills:     []string{"git-monitor", "pr-reviewer"},
		MemoryMode: "conversational",
	}
	card := agents.BuildCapabilityCard(in, testInfoFn)

	if !strings.Contains(card, "- Ares [capable, tools: yes]") {
		t.Errorf("expected header with tier annotation, got:\n%s", card)
	}
	if !strings.Contains(card, "Role: a security-focused monitoring agent") {
		t.Errorf("expected role line, got:\n%s", card)
	}
	if !strings.Contains(card, "Tools: filesystem, web_search") {
		t.Errorf("expected tools line, got:\n%s", card)
	}
	if !strings.Contains(card, "Connections: GitHub") {
		t.Errorf("expected connections line, got:\n%s", card)
	}
	if !strings.Contains(card, "Skills: git-monitor, pr-reviewer") {
		t.Errorf("expected skills line, got:\n%s", card)
	}
	if !strings.Contains(card, "Memory: conversational") {
		t.Errorf("expected memory line, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_NoInfoFn(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Sam",
		SystemPrompt: "You are Sam, a QA specialist.",
		ModelID:      "some-model",
	}
	card := agents.BuildCapabilityCard(in, nil)

	// No tier annotation when infoFn is nil
	if strings.Contains(card, "[") {
		t.Errorf("expected no tier annotation when infoFn is nil, got:\n%s", card)
	}
	if !strings.Contains(card, "- Sam\n") {
		t.Errorf("expected plain name header, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_DescriptionOverride(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Nova",
		SystemPrompt: "You are Nova, the general assistant with a long persona that should be overridden.",
		Description:  "Personal assistant for scheduling and research",
		MemoryMode:   "passive",
	}
	card := agents.BuildCapabilityCard(in, nil)

	if !strings.Contains(card, "Role: Personal assistant for scheduling and research") {
		t.Errorf("expected description override as role, got:\n%s", card)
	}
	if strings.Contains(card, "the general assistant") {
		t.Errorf("system prompt should be overridden, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_EmptyOptionals(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Ghost",
		SystemPrompt: "You are Ghost.",
	}
	card := agents.BuildCapabilityCard(in, nil)

	if strings.Contains(card, "Tools:") {
		t.Errorf("empty local tools should not emit Tools line, got:\n%s", card)
	}
	if strings.Contains(card, "Connections:") {
		t.Errorf("empty toolbelt should not emit Connections line, got:\n%s", card)
	}
	if strings.Contains(card, "Skills:") {
		t.Errorf("empty skills should not emit Skills line, got:\n%s", card)
	}
	// Memory defaults to conversational when empty
	if !strings.Contains(card, "Memory: conversational") {
		t.Errorf("expected default memory mode, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_LowTierNoTools(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:    "Cheap",
		ModelID: "low-model",
	}
	card := agents.BuildCapabilityCard(in, testInfoFn)

	if !strings.Contains(card, "[low, tools: no]") {
		t.Errorf("expected low tier no-tools annotation, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_SystemPromptTruncation(t *testing.T) {
	long := "You are Verbose, " + strings.Repeat("a", 300)
	in := agents.CapabilityCardInput{
		Name:         "Verbose",
		SystemPrompt: long,
	}
	card := agents.BuildCapabilityCard(in, nil)

	if strings.Contains(card, strings.Repeat("a", 201)) {
		t.Errorf("role line should be truncated to 200 chars, got:\n%s", card)
	}
}
```

- [ ] **Step 1.2: Run tests to verify they fail**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agents/... -run TestBuildCapabilityCard -v 2>&1 | head -20
```

Expected: `FAIL` — `undefined: agents.BuildCapabilityCard`

- [ ] **Step 1.3: Implement `capability_card.go`**

```go
// internal/agents/capability_card.go
package agents

import (
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// CapabilityCardInput holds the data needed to generate a capability card.
// Populate from an *Agent (roster.go) or AgentDef (ws.go) at the call site.
type CapabilityCardInput struct {
	Name         string
	SystemPrompt string
	Description  string // optional user override for the Role line
	ModelID      string // for tier/tools annotation; empty or nil infoFn = no annotation
	LocalTools   []string
	Toolbelt     []ToolbeltEntry
	Skills       []string
	MemoryMode   string
}

// providerDisplayNames maps provider slugs to human-readable display names.
var providerDisplayNames = map[string]string{
	"github":          "GitHub",
	"google":          "Google",
	"google-calendar": "Google Calendar",
	"google-drive":    "Google Drive",
	"notion":          "Notion",
	"slack":           "Slack",
	"jira":            "Jira",
	"linear":          "Linear",
	"twilio":          "Twilio",
	"stripe":          "Stripe",
	"openai":          "OpenAI",
	"anthropic":       "Anthropic",
}

// providerDisplayName returns a human-readable name for a provider slug.
// Falls back to title-casing the first letter if not in the lookup map.
func providerDisplayName(slug string) string {
	if name, ok := providerDisplayNames[slug]; ok {
		return name
	}
	if len(slug) == 0 {
		return slug
	}
	return strings.ToUpper(slug[:1]) + slug[1:]
}

// extractRoleBlurb returns the role text for the card's Role line.
// If descriptionOverride is non-empty, it is used directly.
// Otherwise, the first sentence of systemPrompt is extracted (max 200 chars).
// "You are <Name>, " prefix is stripped.
func extractRoleBlurb(systemPrompt, descriptionOverride string) string {
	if descriptionOverride != "" {
		return descriptionOverride
	}
	prompt := strings.TrimSpace(systemPrompt)
	if prompt == "" {
		return ""
	}
	// Strip "You are <Name>, " prefix (first comma within first 20 chars).
	if i := strings.Index(prompt, ", "); i > 0 && i < 20 {
		prompt = strings.TrimSpace(prompt[i+2:])
	}
	// Take first sentence.
	if i := strings.IndexAny(prompt, ".!?"); i > 0 {
		prompt = prompt[:i]
	}
	if len(prompt) > 200 {
		prompt = prompt[:197] + "..."
	}
	return prompt
}

// BuildCapabilityCard generates a deterministic, multi-line capability card for an agent.
// The card is outward-facing — it describes the agent to other agents for delegation purposes.
//
// Format:
//
//	- AgentName [tier, tools: yes/no]
//	  Role: <role blurb>
//	  Tools: <local tools>        (omitted if empty)
//	  Connections: <providers>    (omitted if empty)
//	  Skills: <skills>            (omitted if empty)
//	  Memory: <mode>
func BuildCapabilityCard(in CapabilityCardInput, infoFn ModelInfoFn) string {
	var sb strings.Builder

	// Header: name + optional tier annotation
	sb.WriteString("- ")
	sb.WriteString(in.Name)
	if infoFn != nil && in.ModelID != "" {
		info := infoFn(in.ModelID)
		if info != nil {
			tier := "capable"
			switch info.Tier {
			case modelconfig.TierMedium:
				tier = "medium"
			case modelconfig.TierLow:
				tier = "low"
			}
			toolsLabel := "yes"
			if !info.SupportsTools {
				toolsLabel = "no"
			}
			fmt.Fprintf(&sb, " [%s, tools: %s]", tier, toolsLabel)
		}
	}
	sb.WriteString("\n")

	// Role
	if role := extractRoleBlurb(in.SystemPrompt, in.Description); role != "" {
		sb.WriteString("  Role: ")
		sb.WriteString(role)
		sb.WriteString("\n")
	}

	// Local tools
	if len(in.LocalTools) > 0 {
		sb.WriteString("  Tools: ")
		sb.WriteString(strings.Join(in.LocalTools, ", "))
		sb.WriteString("\n")
	}

	// Connections (toolbelt provider display names)
	if providers := ToolbeltProviders(in.Toolbelt); len(providers) > 0 {
		names := make([]string, len(providers))
		for i, p := range providers {
			names[i] = providerDisplayName(p)
		}
		sb.WriteString("  Connections: ")
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("\n")
	}

	// Skills
	if len(in.Skills) > 0 {
		sb.WriteString("  Skills: ")
		sb.WriteString(strings.Join(in.Skills, ", "))
		sb.WriteString("\n")
	}

	// Memory mode
	mode := in.MemoryMode
	if mode == "" {
		mode = "conversational"
	}
	sb.WriteString("  Memory: ")
	sb.WriteString(mode)
	sb.WriteString("\n")

	return sb.String()
}
```

- [ ] **Step 1.4: Run tests to verify they pass**

```bash
go test ./internal/agents/... -run TestBuildCapabilityCard -v
```

Expected: all 6 tests PASS

- [ ] **Step 1.5: Commit**

```bash
git add internal/agents/capability_card.go internal/agents/capability_card_test.go
git commit -m "feat(agents): add BuildCapabilityCard for deterministic agent capability cards"
```

---

## Task 2: Refactor `BuildRoster()` to use `BuildCapabilityCard()`

**Files:**
- Modify: `internal/agents/roster.go`
- Modify: `internal/agents/roster_test.go` (update format expectations)

- [ ] **Step 2.1: Check existing roster tests**

```bash
go test ./internal/agents/... -run TestBuildRoster -v
```

Note the current output format. Tests will need updating after the refactor.

- [ ] **Step 2.2: Replace `BuildRoster()` body in `internal/agents/roster.go`**

Replace the entire function body (lines 23–65):

```go
// BuildRoster constructs the agent roster string injected into the primary
// agent's system prompt. It excludes the primary agent itself (primaryName,
// case-insensitive) and returns an empty string if no other agents exist.
//
// Each agent is represented as a capability card (multi-line).
func BuildRoster(reg *AgentRegistry, infoFn ModelInfoFn, primaryName string) string {
	all := reg.All()

	var parts []string
	for _, ag := range all {
		if strings.EqualFold(ag.Name, primaryName) {
			continue // exclude self
		}
		card := BuildCapabilityCard(CapabilityCardInput{
			Name:         ag.Name,
			SystemPrompt: ag.SystemPrompt,
			ModelID:      ag.ModelID,
			LocalTools:   ag.LocalTools,
			Toolbelt:     ag.Toolbelt,
			Skills:       ag.Skills,
			MemoryMode:   ag.MemoryMode,
		}, infoFn)
		parts = append(parts, card)
	}

	if len(parts) == 0 {
		return ""
	}
	return "Available team members:\n" + strings.Join(parts, "")
}
```

Delete `extractPersonaBlurb` from `roster.go` — it is now superseded by `extractRoleBlurb` in `capability_card.go`.

- [ ] **Step 2.3: Update roster tests**

Find all assertions in roster tests that check for the old single-line format (`- Stacy [capable, tools: yes] — Pragmatic senior engineer`) and update them to check for the new multi-line format. Example:

```go
// OLD assertion (remove):
if !strings.Contains(got, "- Stacy [capable, tools: yes] — Pragmatic senior engineer") {

// NEW assertion (replace with):
if !strings.Contains(got, "- Stacy [capable, tools: yes]") {
    t.Errorf(...)
}
if !strings.Contains(got, "  Role: Pragmatic senior engineer") {
    t.Errorf(...)
}
```

- [ ] **Step 2.4: Run all agents tests**

```bash
go test ./internal/agents/... -v 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 2.5: Commit**

```bash
git add internal/agents/roster.go internal/agents/roster_test.go
git commit -m "refactor(agents): BuildRoster uses BuildCapabilityCard for unified agent descriptions"
```

---

## Task 3: Wire capability cards into `InjectSpaceContext`

**Files:**
- Modify: `internal/server/ws.go` (lines ~755–843)

`BuildSpaceContextBlock` and `BuildDMCrossSpaceContextBlock` do not change. The fix is at the call site in `InjectSpaceContext` where `SpaceMember.Description` is populated.

- [ ] **Step 3.1: Update `InjectSpaceContext` in `internal/server/ws.go`**

There are two places where `descMap` is built (one for DM cross-space context, one for channel context). In both places, replace the `descMap` logic with a `cardMap` that uses `BuildCapabilityCard`:

**Replace this pattern (appears twice, ~lines 761–768 and ~807–814):**
```go
descMap := make(map[string]string)
if cfgErr == nil {
    for _, def := range cfg.Agents {
        if def.Description != "" {
            descMap[def.Name] = def.Description
        }
    }
}
```

**With:**
```go
cardMap := make(map[string]string)
if cfgErr == nil {
    for _, def := range cfg.Agents {
        cardMap[def.Name] = agents.BuildCapabilityCard(agents.CapabilityCardInput{
            Name:         def.Name,
            SystemPrompt: def.SystemPrompt,
            Description:  def.Description,
            ModelID:      def.Model,
            LocalTools:   def.LocalTools,
            Toolbelt:     def.Toolbelt,
            Skills:       def.Skills,
            MemoryMode:   def.MemoryMode,
        }, nil) // no model tier resolver at this call site
    }
}
```

Then replace all occurrences of `descMap[` with `cardMap[` in the same function.

- [ ] **Step 3.2: Run server tests**

```bash
go test ./internal/server/... -v 2>&1 | tail -30
```

Expected: all PASS. The `BuildSpaceContextBlock` tests that check member descriptions should now see multi-line card content instead of the raw Description field.

- [ ] **Step 3.3: Run full test suite**

```bash
go test ./... 2>&1 | grep -E "FAIL|ok"
```

Expected: all packages PASS

- [ ] **Step 3.4: Commit**

```bash
git add internal/server/ws.go
git commit -m "feat(server): InjectSpaceContext uses BuildCapabilityCard for full agent capability context"
```

---

## Task 4: Add `HeartbeatEnabled` + `HeartbeatCron` to `AgentDef`

**Files:**
- Modify: `internal/agents/config.go`

- [ ] **Step 4.1: Write a failing test for the new fields**

```go
// internal/agents/heartbeat_fields_test.go
package agents_test

import (
	"encoding/json"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestAgentDef_HeartbeatFields_RoundTrip(t *testing.T) {
	def := agents.AgentDef{
		Name:             "Ares",
		HeartbeatEnabled: true,
		HeartbeatCron:    "0 8 * * *",
	}
	b, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	var out agents.AgentDef
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.HeartbeatEnabled {
		t.Error("HeartbeatEnabled should round-trip")
	}
	if out.HeartbeatCron != "0 8 * * *" {
		t.Errorf("HeartbeatCron: got %q, want %q", out.HeartbeatCron, "0 8 * * *")
	}
}

func TestAgentDef_HeartbeatFields_OmittedByDefault(t *testing.T) {
	def := agents.AgentDef{Name: "Ghost"}
	b, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{}` {
		// heartbeat fields must be omitempty — check they don't appear in minimal output
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if _, ok := m["heartbeat_enabled"]; ok {
			t.Error("heartbeat_enabled should be omitted when false")
		}
		if _, ok := m["heartbeat_cron"]; ok {
			t.Error("heartbeat_cron should be omitted when empty")
		}
	}
}
```

- [ ] **Step 4.2: Run to confirm failure**

```bash
go test ./internal/agents/... -run TestAgentDef_HeartbeatFields -v
```

Expected: FAIL — `unknown field HeartbeatEnabled`

- [ ] **Step 4.3: Add the two fields to `AgentDef` in `internal/agents/config.go`**

Add after the `Description` field (line ~81):

```go
	// HeartbeatEnabled controls whether this agent sends periodic check-in DMs to the user.
	// When true, a workflow YAML is auto-generated at ~/.huginn/workflows/heartbeat-{name}.yaml.
	HeartbeatEnabled bool   `json:"heartbeat_enabled,omitempty" yaml:"heartbeat_enabled,omitempty"`

	// HeartbeatCron is the cron schedule for the heartbeat workflow.
	// Defaults to "0 */4 * * *" (every 4 hours) when empty and HeartbeatEnabled is true.
	HeartbeatCron    string `json:"heartbeat_cron,omitempty"    yaml:"heartbeat_cron,omitempty"`
```

- [ ] **Step 4.4: Run tests**

```bash
go test ./internal/agents/... -run TestAgentDef_HeartbeatFields -v
```

Expected: both tests PASS

- [ ] **Step 4.5: Commit**

```bash
git add internal/agents/config.go internal/agents/heartbeat_fields_test.go
git commit -m "feat(agents): add HeartbeatEnabled and HeartbeatCron fields to AgentDef"
```

---

## Task 5: Heartbeat YAML lifecycle

**Files:**
- Create: `internal/agents/heartbeat_yaml.go`
- Create: `internal/agents/heartbeat_yaml_test.go`

- [ ] **Step 5.1: Write failing tests**

```go
// internal/agents/heartbeat_yaml_test.go
package agents_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestSyncHeartbeatYAML_CreatesFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	def := agents.AgentDef{
		Name:             "Ares",
		HeartbeatEnabled: true,
		HeartbeatCron:    "0 8 * * 1-5",
	}
	if err := agents.SyncHeartbeatYAMLDefault(def); err != nil {
		t.Fatalf("SyncHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, "workflows", "heartbeat-ares.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	s := string(content)
	if !strings.HasPrefix(s, "# MANAGED BY HUGINN") {
		t.Error("expected managed header")
	}
	if !strings.Contains(s, `name: "Heartbeat: Ares"`) {
		t.Errorf("expected name field, got:\n%s", s)
	}
	if !strings.Contains(s, "enabled: true") {
		t.Errorf("expected enabled: true, got:\n%s", s)
	}
	if !strings.Contains(s, `schedule: "0 8 * * 1-5"`) {
		t.Errorf("expected schedule, got:\n%s", s)
	}
	if !strings.Contains(s, "type: agent_dm") {
		t.Errorf("expected agent_dm delivery, got:\n%s", s)
	}
	if !strings.Contains(s, `from: "Ares"`) {
		t.Errorf("expected from field, got:\n%s", s)
	}
	if !strings.Contains(s, `agent: "Ares"`) {
		t.Errorf("expected agent field, got:\n%s", s)
	}
}

func TestSyncHeartbeatYAML_DisablesExistingFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	// First create the file
	enabled := agents.AgentDef{Name: "Ares", HeartbeatEnabled: true, HeartbeatCron: "0 */4 * * *"}
	_ = agents.SyncHeartbeatYAMLDefault(enabled)

	// Now disable
	disabled := agents.AgentDef{Name: "Ares", HeartbeatEnabled: false, HeartbeatCron: "0 */4 * * *"}
	if err := agents.SyncHeartbeatYAMLDefault(disabled); err != nil {
		t.Fatalf("SyncHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, "workflows", "heartbeat-ares.yaml")
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "enabled: false") {
		t.Errorf("expected enabled: false after disable, got:\n%s", content)
	}
	// File must still exist (cron preserved for re-enable)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file should persist when disabled, not be deleted")
	}
}

func TestSyncHeartbeatYAML_NoFileWhenDisabledAndNoExisting(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	def := agents.AgentDef{Name: "Ghost", HeartbeatEnabled: false}
	if err := agents.SyncHeartbeatYAMLDefault(def); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, "workflows", "heartbeat-ghost.yaml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("no file should be created when disabled and no existing file")
	}
}

func TestDeleteHeartbeatYAML_RemovesManagedFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	def := agents.AgentDef{Name: "Ares", HeartbeatEnabled: true}
	_ = agents.SyncHeartbeatYAMLDefault(def)

	if err := agents.DeleteHeartbeatYAMLDefault("Ares"); err != nil {
		t.Fatalf("DeleteHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	path := filepath.Join(home, "workflows", "heartbeat-ares.yaml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("managed file should be deleted")
	}
}

func TestDeleteHeartbeatYAML_DoesNotRemoveUserFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	home := os.Getenv("HUGINN_HOME")
	_ = os.MkdirAll(filepath.Join(home, "workflows"), 0o750)
	userFile := filepath.Join(home, "workflows", "heartbeat-ares.yaml")
	// User-customized file does NOT start with managed header
	_ = os.WriteFile(userFile, []byte("# My custom heartbeat\nname: custom\n"), 0o600)

	if err := agents.DeleteHeartbeatYAMLDefault("Ares"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(userFile); os.IsNotExist(err) {
		t.Error("user-customized file should NOT be deleted")
	}
}

func TestRenameHeartbeatYAML_MovesFile(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())

	old := agents.AgentDef{Name: "Ares", HeartbeatEnabled: true, HeartbeatCron: "0 8 * * *"}
	_ = agents.SyncHeartbeatYAMLDefault(old)

	newDef := agents.AgentDef{Name: "Aries", HeartbeatEnabled: true, HeartbeatCron: "0 8 * * *"}
	if err := agents.RenameHeartbeatYAMLDefault("Ares", newDef); err != nil {
		t.Fatalf("RenameHeartbeatYAMLDefault: %v", err)
	}

	home := os.Getenv("HUGINN_HOME")
	oldPath := filepath.Join(home, "workflows", "heartbeat-ares.yaml")
	newPath := filepath.Join(home, "workflows", "heartbeat-aries.yaml")

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old heartbeat file should be removed")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new heartbeat file should exist: %v", err)
	}
	if !strings.Contains(string(content), `name: "Heartbeat: Aries"`) {
		t.Errorf("new file should reference new name, got:\n%s", content)
	}
}

func TestIsManaged(t *testing.T) {
	tmp := t.TempDir()
	managed := filepath.Join(tmp, "managed.yaml")
	unmanaged := filepath.Join(tmp, "unmanaged.yaml")

	_ = os.WriteFile(managed, []byte("# MANAGED BY HUGINN\nname: test\n"), 0o600)
	_ = os.WriteFile(unmanaged, []byte("# My custom file\nname: test\n"), 0o600)

	if !agents.IsManaged(managed) {
		t.Error("expected managed file to be detected")
	}
	if agents.IsManaged(unmanaged) {
		t.Error("expected unmanaged file to not be detected")
	}
	if agents.IsManaged(filepath.Join(tmp, "nonexistent.yaml")) {
		t.Error("nonexistent file should not be managed")
	}
}
```

- [ ] **Step 5.2: Run to confirm failure**

```bash
go test ./internal/agents/... -run "TestSyncHeartbeat|TestDeleteHeartbeat|TestRenameHeartbeat|TestIsManaged" -v 2>&1 | head -20
```

Expected: FAIL — `undefined: agents.SyncHeartbeatYAMLDefault`

- [ ] **Step 5.3: Implement `heartbeat_yaml.go`**

```go
// internal/agents/heartbeat_yaml.go
package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const managedHeader = "# MANAGED BY HUGINN"

const defaultHeartbeatCron = "0 */4 * * *"

// HeartbeatYAMLPath returns the path to the heartbeat workflow file for an agent.
// Exported for use in tests and the server layer.
func HeartbeatYAMLPath(baseDir, agentName string) string {
	return filepath.Join(baseDir, "workflows", "heartbeat-"+sanitizeAgentName(agentName)+".yaml")
}

// IsManaged returns true if the file at path begins with the managed header comment.
// Returns false for nonexistent files.
func IsManaged(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(managedHeader))
	n, _ := f.Read(buf)
	return string(buf[:n]) == managedHeader
}

// generateHeartbeatYAML produces the YAML content for a heartbeat workflow file.
func generateHeartbeatYAML(name string, cron string, enabled bool) string {
	safeName := sanitizeAgentName(name)
	return fmt.Sprintf(
		"# MANAGED BY HUGINN — changes to cron/enabled will be overwritten by the UI.\n"+
			"# To customize fully: copy to a new filename (e.g. my-%s-heartbeat.yaml) and Huginn will stop managing it.\n"+
			"name: \"Heartbeat: %s\"\n"+
			"description: \"Auto-generated heartbeat for %s\"\n"+
			"enabled: %v\n"+
			"schedule: \"%s\"\n"+
			"notification:\n"+
			"  on_success: true\n"+
			"  on_failure: true\n"+
			"  severity: info\n"+
			"  deliver_to:\n"+
			"    - type: agent_dm\n"+
			"      from: \"%s\"\n"+
			"steps:\n"+
			"  - name: \"Check in\"\n"+
			"    agent: \"%s\"\n"+
			"    prompt: |\n"+
			"      You are checking in with your user. Use your tools and memory to assess whether\n"+
			"      anything warrants their attention right now.\n"+
			"\n"+
			"      Respond as you would in a direct message to a colleague — conversational, direct, 2-4 sentences.\n"+
			"      Do not use bullet points, markdown tables, headers, or report formatting.\n"+
			"      Do not say \"Heartbeat:\" or \"Status update:\" or anything that sounds like a log entry.\n"+
			"      If there is nothing to report, say so in one sentence and stop.\n"+
			"\n"+
			"      Good: \"Nothing unusual in your repos today. The PR you opened yesterday is still waiting on review.\"\n"+
			"      Bad: \"**Heartbeat Report**\\n- Repos: OK\\n- PRs: 1 open\\n- Actions: None required\"\n"+
			"    position: 0\n"+
			"    on_failure: stop\n",
		safeName, name, name, enabled, cron, name, name,
	)
}

func writeHeartbeatYAML(baseDir string, def AgentDef) error {
	workflowsDir := filepath.Join(baseDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0o750); err != nil {
		return fmt.Errorf("mkdir workflows: %w", err)
	}
	cron := def.HeartbeatCron
	if cron == "" {
		cron = defaultHeartbeatCron
	}
	content := generateHeartbeatYAML(def.Name, cron, def.HeartbeatEnabled)
	return os.WriteFile(HeartbeatYAMLPath(baseDir, def.Name), []byte(content), 0o600)
}

// SyncHeartbeatYAMLDefault ensures the heartbeat YAML file matches the agent's config:
//   - HeartbeatEnabled=true:  creates/updates the file with enabled=true
//   - HeartbeatEnabled=false, managed file exists: updates file with enabled=false (preserves cron on re-enable)
//   - HeartbeatEnabled=false, no file: no-op (avoids creating disabled skeleton files)
func SyncHeartbeatYAMLDefault(def AgentDef) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	path := HeartbeatYAMLPath(baseDir, def.Name)
	_, statErr := os.Stat(path)
	fileExists := statErr == nil

	if def.HeartbeatEnabled || (fileExists && IsManaged(path)) {
		return writeHeartbeatYAML(baseDir, def)
	}
	return nil
}

// DeleteHeartbeatYAMLDefault removes the managed heartbeat workflow for an agent.
// No-op if the file does not exist or is not managed (user-customized files are not touched).
func DeleteHeartbeatYAMLDefault(agentName string) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	path := HeartbeatYAMLPath(baseDir, agentName)
	if !IsManaged(path) {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RenameHeartbeatYAMLDefault handles heartbeat file lifecycle when an agent is renamed.
// Removes the old managed file and writes a new file (if enabled).
func RenameHeartbeatYAMLDefault(oldName string, newDef AgentDef) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	// Remove old managed file (best effort)
	oldPath := HeartbeatYAMLPath(baseDir, oldName)
	if IsManaged(oldPath) {
		_ = os.Remove(oldPath)
	}
	// Write new file only if heartbeat is enabled
	if newDef.HeartbeatEnabled {
		return writeHeartbeatYAML(baseDir, newDef)
	}
	return nil
}

// HeartbeatCronOrDefault returns the cron string from the def, or the default if empty.
// Exported for use in the web layer when displaying next-run time.
func HeartbeatCronOrDefault(def AgentDef) string {
	if def.HeartbeatCron != "" {
		return def.HeartbeatCron
	}
	return defaultHeartbeatCron
}

// heartbeatDisableString replaces "enabled: true" with "enabled: false" in YAML content.
// Used to disable a heartbeat workflow without regenerating the entire file
// (preserves any manual edits the user made while ignoring the managed header).
func heartbeatDisableString(content string) string {
	return strings.Replace(content, "enabled: true", "enabled: false", 1)
}
```

- [ ] **Step 5.4: Run heartbeat YAML tests**

```bash
go test ./internal/agents/... -run "TestSyncHeartbeat|TestDeleteHeartbeat|TestRenameHeartbeat|TestIsManaged" -v
```

Expected: all 7 tests PASS

- [ ] **Step 5.5: Run full agents test suite**

```bash
go test ./internal/agents/... 2>&1 | tail -5
```

Expected: PASS

- [ ] **Step 5.6: Commit**

```bash
git add internal/agents/heartbeat_yaml.go internal/agents/heartbeat_yaml_test.go
git commit -m "feat(agents): heartbeat YAML lifecycle — sync, delete, rename managed workflow files"
```

---

## Task 6: Wire heartbeat lifecycle into HTTP handlers

**Files:**
- Modify: `internal/server/handlers.go` (lines ~386–407 and ~665–678)
- Create: `internal/server/handlers_heartbeat_test.go`

- [ ] **Step 6.1: Write failing test**

```go
// internal/server/handlers_heartbeat_test.go
package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/server"
)

func TestHandleUpdateAgent_CreatesHeartbeatYAML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HUGINN_HOME", tmp)

	// Pre-create the agent
	existing := agents.AgentDef{Name: "HeartbeatTestAgent", Model: "claude-sonnet-4-6"}
	_ = agents.SaveAgentDefault(existing)

	srv, _ := server.NewTestServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		srv.HandleUpdateAgent(w, r)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	update := map[string]any{
		"name":              "HeartbeatTestAgent",
		"model":             "claude-sonnet-4-6",
		"heartbeat_enabled": true,
		"heartbeat_cron":    "0 8 * * *",
	}
	body, _ := json.Marshal(update)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/agents/HeartbeatTestAgent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	yamlPath := filepath.Join(tmp, "workflows", "heartbeat-heartbeattestagent.yaml")
	content, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("expected heartbeat YAML at %s: %v", yamlPath, err)
	}
	if !strings.Contains(string(content), "enabled: true") {
		t.Errorf("expected enabled: true in heartbeat YAML, got:\n%s", content)
	}
	if !strings.Contains(string(content), `schedule: "0 8 * * *"`) {
		t.Errorf("expected schedule in heartbeat YAML, got:\n%s", content)
	}
}

func TestHandleDeleteAgent_RemovesHeartbeatYAML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HUGINN_HOME", tmp)

	// Pre-create two agents (can't delete the last one)
	_ = agents.SaveAgentDefault(agents.AgentDef{Name: "AgentA", Model: "claude-sonnet-4-6"})
	target := agents.AgentDef{Name: "HeartbeatDeleteTarget", Model: "claude-sonnet-4-6", HeartbeatEnabled: true}
	_ = agents.SaveAgentDefault(target)
	_ = agents.SyncHeartbeatYAMLDefault(target)

	srv, _ := server.NewTestServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		srv.HandleDeleteAgent(w, r)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agents/HeartbeatDeleteTarget", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	yamlPath := filepath.Join(tmp, "workflows", "heartbeat-heartbeatdeletetarget.yaml")
	if _, err := os.Stat(yamlPath); !os.IsNotExist(err) {
		t.Error("heartbeat YAML should be removed on agent deletion")
	}
}
```

Note: `server.NewTestServer`, `srv.HandleUpdateAgent`, and `srv.HandleDeleteAgent` are test-exported helpers. Check if the test package already has `newTestServer` and unexported method references — adjust to use whatever pattern is already established in `internal/server/builtin_handlers_ws_test.go`. The test in that file uses `srv.handleUpdateAgent` (lowercase) as an unexported method call from `package server_test` via closure — copy that pattern exactly.

- [ ] **Step 6.2: Add heartbeat sync to `handleUpdateAgent` in `internal/server/handlers.go`**

After the `isRename` block (around line 391–393), add:

```go
	// Heartbeat lifecycle: sync or remove the managed workflow YAML.
	if isRename {
		_ = agents.RenameHeartbeatYAMLDefault(name, incoming) // best effort
	} else {
		_ = agents.SyncHeartbeatYAMLDefault(incoming) // best effort
	}
```

- [ ] **Step 6.3: Add heartbeat delete to `handleDeleteAgent` in `internal/server/handlers.go`**

After `agents.DeleteAgentDefault(name)` succeeds (around line 665), add:

```go
	_ = agents.DeleteHeartbeatYAMLDefault(name) // best effort; ignore error
```

- [ ] **Step 6.4: Run handler tests**

```bash
go test ./internal/server/... -run "TestHandleUpdateAgent_CreatesHeartbeatYAML|TestHandleDeleteAgent_RemovesHeartbeatYAML" -v
```

Expected: both PASS

- [ ] **Step 6.5: Run full test suite**

```bash
go test ./... 2>&1 | grep -E "FAIL|ok"
```

Expected: all PASS

- [ ] **Step 6.6: Commit**

```bash
git add internal/server/handlers.go internal/server/handlers_heartbeat_test.go
git commit -m "feat(server): wire heartbeat YAML lifecycle into agent save/rename/delete handlers"
```

---

## Task 7: Web UI — heartbeat toggle in agent editor

**Files:**
- Modify: `web/src/views/AgentsView.vue`

- [ ] **Step 7.1: Add `heartbeat_enabled` and `heartbeat_cron` to the form type and initial value**

In `AgentsView.vue`, find the `AgentForm` type definition (~line 1275) and add the two new fields:

```typescript
type AgentForm = {
  name: string
  model: string
  system_prompt: string
  color: string
  icon: string
  memory_type: string
  memory_enabled: boolean
  context_notes_enabled: boolean
  vault_name: string
  memory_mode: MemoryMode
  vault_description: string
  toolbelt: any[]
  skills: string[]
  local_tools: string[]
  heartbeat_enabled: boolean   // ADD
  heartbeat_cron: string       // ADD
}
```

Update the `form` ref initialization (~line 1294) to add:
```typescript
const form = ref<AgentForm>({
  name: '', model: '', system_prompt: '', color: '#58a6ff', icon: '',
  memory_type: 'none', memory_enabled: false, context_notes_enabled: false,
  vault_name: '', memory_mode: 'conversational', vault_description: '',
  toolbelt: [], skills: [], local_tools: [],
  heartbeat_enabled: false,  // ADD
  heartbeat_cron: '',        // ADD
})
```

Find where agent data is loaded into the form (the function that maps an API response to the form). Add:
```typescript
form.value.heartbeat_enabled = agent.heartbeat_enabled ?? false
form.value.heartbeat_cron = agent.heartbeat_cron ?? ''
```

- [ ] **Step 7.2: Add the heartbeat UI section to the agent editor**

Find the area in the agent editor template where memory mode / skills are configured. Add a heartbeat section below the skills section:

```html
<!-- Heartbeat -->
<div class="mt-4 border-t border-huginn-border/20 pt-4">
  <div class="flex items-center justify-between mb-2">
    <div>
      <div class="text-[11px] font-medium text-huginn-text">Send me regular updates</div>
      <div class="text-[10px] text-huginn-muted/60 mt-0.5">Agent checks in via DM on a schedule</div>
    </div>
    <button
      type="button"
      @click="() => { form.heartbeat_enabled = !form.heartbeat_enabled; markDirty() }"
      :class="form.heartbeat_enabled
        ? 'bg-huginn-blue'
        : 'bg-huginn-muted/20'"
      class="relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none">
      <span
        :class="form.heartbeat_enabled ? 'translate-x-4' : 'translate-x-0'"
        class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out" />
    </button>
  </div>

  <div v-if="form.heartbeat_enabled" class="mt-2">
    <label class="text-[10px] text-huginn-muted/60 font-medium uppercase tracking-wide block mb-1">Frequency</label>
    <select
      v-model="form.heartbeat_cron"
      @change="markDirty"
      class="w-full bg-huginn-bg/50 border border-huginn-border/30 rounded px-2 py-1 text-[11px] text-huginn-text focus:outline-none focus:border-huginn-blue/50">
      <option value="">Every 4 hours (default)</option>
      <option value="0 */12 * * *">Twice daily</option>
      <option value="0 8 * * *">Daily at 8am</option>
      <option value="0 8 * * 1">Weekly (Monday 8am)</option>
    </select>
    <div class="mt-1 text-[10px] text-huginn-muted/40">
      Cron: {{ form.heartbeat_cron || '0 */4 * * *' }}
    </div>
  </div>
</div>
```

- [ ] **Step 7.3: Verify the form sends heartbeat fields on save**

The existing save logic already serializes the entire `form` object to JSON for the `PUT /api/v1/agents/{name}` call. No additional changes needed — `heartbeat_enabled` and `heartbeat_cron` will be included in the payload automatically.

- [ ] **Step 7.4: Manual test**

1. Start the dev server: `cd web && pnpm dev`
2. Open an agent in the editor
3. Scroll to bottom of left panel — verify the "Send me regular updates" toggle appears
4. Toggle it on — verify the frequency selector appears
5. Change frequency — verify the cron expression updates below it
6. Save the agent — verify no errors
7. Check `~/.huginn/workflows/` — verify `heartbeat-{agent-name}.yaml` was created

- [ ] **Step 7.5: Commit**

```bash
git add web/src/views/AgentsView.vue
git commit -m "feat(ui): add heartbeat toggle and cron preset selector to agent editor"
```

---

## Task 8: Web UI — rename Inbox → Activity Log

**Files:**
- Modify: `web/src/App.vue`
- Modify: `web/src/views/InboxView.vue`
- Modify: `web/src/views/__tests__/InboxView.test.ts`

- [ ] **Step 8.1: Update nav item in `web/src/App.vue`**

Find line ~853:
```javascript
{ section: 'inbox', label: 'Inbox', path: '/inbox', icon: 'inbox' },
```

Change to:
```javascript
{ section: 'inbox', label: 'Activity Log', path: '/inbox', icon: 'inbox' },
```

- [ ] **Step 8.2: Update the heading in `web/src/views/InboxView.vue`**

Find line ~5:
```html
<h1 class="text-sm font-semibold text-huginn-text uppercase tracking-widest">Inbox</h1>
```

Change to:
```html
<h1 class="text-sm font-semibold text-huginn-text uppercase tracking-widest">Activity Log</h1>
```

- [ ] **Step 8.3: Update the test in `web/src/views/__tests__/InboxView.test.ts`**

Find the test that asserts the heading text (~line 111):
```typescript
it('shows "Inbox" heading in the header', async () => {
  // ...
  expect(w.text()).toContain('Inbox')
})
```

Update:
```typescript
it('shows "Activity Log" heading in the header', async () => {
  // ...
  expect(w.text()).toContain('Activity Log')
})
```

- [ ] **Step 8.4: Move Activity Log to secondary navigation**

In `web/src/App.vue`, the nav items array at line ~853 controls order and positioning. Move the `inbox` entry so it appears after the primary navigation items (spaces, agents, workflows) and before or after settings. The exact positioning depends on the current nav structure — place it in the secondary section (the lower group of nav items if the layout has two groups).

Look for how the nav is rendered to determine if items have section grouping. If there's a divider or grouping mechanism, place Activity Log in the secondary group. If it's a flat list, move it toward the bottom after workflows/skills.

- [ ] **Step 8.5: Run frontend tests**

```bash
cd web && pnpm test 2>&1 | grep -E "FAIL|pass|fail" | head -20
```

Expected: all tests PASS (the renamed heading test should now pass, others unchanged)

- [ ] **Step 8.6: Commit**

```bash
git add web/src/App.vue web/src/views/InboxView.vue "web/src/views/__tests__/InboxView.test.ts"
git commit -m "feat(ui): rename Inbox to Activity Log and reposition in secondary navigation"
```

---

## Self-Review Checklist

After completing all tasks, verify spec coverage:

| Spec requirement | Covered by |
|-----------------|------------|
| `BuildCapabilityCard()` deterministic, runtime | Task 1 |
| Card includes tier/tools annotation | Task 1 |
| Description field as optional override | Task 1 (`extractRoleBlurb`) |
| Connections from toolbelt provider names | Task 1 |
| BuildRoster uses BuildCapabilityCard | Task 2 |
| BuildSpaceContextBlock call site uses card | Task 3 (`ws.go`) |
| BuildDMCrossSpaceContextBlock call site uses card | Task 3 (`ws.go`) |
| `HeartbeatEnabled` / `HeartbeatCron` on AgentDef | Task 4 |
| Auto-generated YAML written on enable | Task 5+6 |
| YAML disabled (not deleted) on disable | Task 5 `SyncHeartbeatYAMLDefault` |
| Managed header comment | Task 5 `generateHeartbeatYAML` |
| Lifecycle on rename | Task 5+6 `RenameHeartbeatYAMLDefault` |
| Lifecycle on delete | Task 5+6 `DeleteHeartbeatYAMLDefault` |
| `on_success: true`, `on_failure: true`, `deliver_to: agent_dm` | Task 5 YAML template |
| Heartbeat runs NOT in Activity Log | Task 5 (no `inbox` in `deliver_to`) |
| Heartbeat prompt design (conversational, no markdown) | Task 5 YAML template |
| Web UI toggle + cron presets | Task 7 |
| Inbox → Activity Log rename | Task 8 |
| Moved to secondary nav | Task 8 |
