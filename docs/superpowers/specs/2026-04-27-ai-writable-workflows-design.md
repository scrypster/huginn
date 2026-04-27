# AI-Writable Workflows & Agents — Design Spec

**Date:** 2026-04-27
**Status:** Approved
**Scope:** Schema discoverability, hot-reload, validation, and dual-format agent support

---

## Problem

Huginn workflows and agents can only be created through the web UI today. An AI system — whether a huginn agent, Claude Code, Cursor, or any external tool — has no reliable way to:

1. Discover the workflow YAML schema without reading Go source
2. Know which agents exist and what external services they can access
3. Write a workflow file and have it automatically scheduled (not just visible in the UI)
4. Write a human-readable agent definition without JSON escaping

The goal is to make both paths — UI and AI — fully first-class, independent, and equally capable.

---

## Scope

**In scope:**
- `workflow-designer` builtin skill (schema reference for any AI consumer)
- `WorkflowsWatcher` (auto-register cron when files are added/changed on disk)
- `POST /api/v1/workflows/validate` (dry-run validation without persisting)
- Agent dual-format support: read `.json` and `.yaml`, write new agents as `.yaml`

**Out of scope:**
- Dedicated "workflow designer" huginn agent preset
- UI changes
- `GET /api/v1/workflows/schema` JSON Schema endpoint
- Agent YAML migration tooling (existing `.json` files are not touched)

---

## Architecture

### Component 1 — `workflow-designer` Builtin Skill

**File:** `internal/skills/builtin/workflow-designer.md`

A Markdown skill file with YAML frontmatter. Follows the same format as `code.md`, `plan.md`, etc. Ships with huginn as a builtin skill.

**Two consumers, one artifact:**

- **Huginn agents** — enable the skill via the UI or `installed.json`; its content is injected into the agent's system prompt automatically by the existing skills machinery
- **External AIs (Claude Code, Cursor, etc.)** — read the file directly from the repo at the known path `internal/skills/builtin/workflow-designer.md`; it is always present, always in sync with the Go structs, versioned with the code

**Content includes:**
- Complete workflow YAML field reference (all fields from `workflow_types.go`)
- All valid enum values: `on_failure` (`stop` | `continue`), notify `type` values, `circuit_state` values
- Template variable reference: `{{prev.output}}`, `{{run.scratch.KEY}}`, `{{inputs.alias}}`
- `when:` conditional semantics and falsy rules
- Step dataflow (`inputs[].from_step` / `inputs[].as`)
- `sub_workflow` semantics
- A minimal working example (2-step workflow)
- **Discovery instruction:** "Before writing a workflow, read `~/.huginn/agents/*.json` (or `*.yaml`) to enumerate available agents and their toolbelt providers. Match agent capabilities to the work each step requires."
- **Placement instruction:** Write completed YAML to `~/.huginn/workflows/{id}.yaml`; the scheduler will pick it up automatically within 2 seconds

---

### Component 2 — `WorkflowsWatcher`

**File:** `internal/scheduler/workflows_watcher.go`

Mirrors `internal/skills/watcher.go` exactly in structure. Polls `~/.huginn/workflows/*.yaml` every 2 seconds, debounces 500ms using FNV-64a hashing of path + size + mtime.

**On change detected:**

```
Load all *.yaml files from dir
  For each file:
    if new or changed → RegisterWorkflow(w)   // upserts cron; respects enabled: false
  For each previously-registered workflow no longer on disk:
    RemoveWorkflow(id)
```

**Why this is necessary:** Without the watcher, an AI writes a file, the UI shows it (because `GET /api/v1/workflows` reads disk fresh), but the cron never fires. The user is told "your workflow will run at 8am" and it silently doesn't. The watcher closes this gap completely.

**Key constraints:**
- Poll-based only — no `fsnotify`; matches the existing pattern, avoids platform-specific inotify/kqueue edge cases
- Does not auto-enable disabled workflows — `enabled: false` is respected; the watcher's job is "disk state = scheduler state", not "override user intent"
- Validation errors (bad cron expression, unknown agent) are logged and skipped; they do not stop other workflows from being registered
- The watcher is started by `Scheduler.Start()` alongside the existing cron runner

**Struct:**

```go
type WorkflowsWatcher struct {
    dir      string
    sched    *Scheduler
    onChange func()  // optional callback for tests
}

func NewWorkflowsWatcher(dir string, sched *Scheduler) *WorkflowsWatcher
func (w *WorkflowsWatcher) Start(ctx context.Context)
```

---

### Component 3 — `POST /api/v1/workflows/validate`

**Handler:** `handleValidateWorkflow` in `internal/server/handlers_workflows.go`

**Route:** `POST /api/v1/workflows/validate` (registered in `server.go` `registerRoutes`)

**Request body:** Same JSON shape as `POST /api/v1/workflows` (a workflow object)

**Behavior:**
1. Decode JSON body → `scheduler.Workflow`
2. Run `validateWorkflow(&wf)` — structural validation
3. Run `validateWorkflowAgentsAndConnections(&wf)` — cross-check agent names and connection IDs
4. **Stop here — do not call `SaveWorkflow` or `RegisterWorkflow`**
5. Return `200 OK` with `{"valid": true}` on success
6. Return `422 Unprocessable Entity` with `{"error": "..."}` on failure

**Use case:** AI writes YAML → converts to JSON → `POST /validate` → reads errors → fixes → `POST /workflows` to persist. Clean dry-run loop without creating garbage files.

---

### Component 4 — Agent Dual-Format (JSON + YAML)

**File modified:** `internal/agents/config.go`

#### Reading

`LoadAgents` currently globs `~/.huginn/agents/*.json` (line 346). Change to glob both:

```go
jsonFiles, _ := filepath.Glob(filepath.Join(agentsDir, "*.json"))
yamlFiles, _ := filepath.Glob(filepath.Join(agentsDir, "*.yaml"))
entries := append(jsonFiles, yamlFiles...)
```

For each file, detect format by extension and unmarshal accordingly:

```go
switch filepath.Ext(path) {
case ".json":
    err = json.Unmarshal(data, &agent)
case ".yaml", ".yml":
    err = yaml.Unmarshal(data, &agent)
}
```

`AgentDef` already has `yaml:` struct tags on all fields (they're present in `workflow_types.go` neighboring structs; confirm and add to `AgentDef` if missing).

#### Writing

`SaveAgent` currently writes to `{name}.json`. Change: write to `{name}.yaml`. Use `yaml.Marshal` with the `yaml:` struct tags. Multi-line `system_prompt` gets a YAML block scalar (`|`) automatically — the primary readability win.

**Existing `.json` files:** not touched. They continue to load correctly. No migration step required. Users who prefer JSON can keep their files; new agents created via UI or API will be `.yaml`.

#### Why YAML for agents

The `system_prompt` field is frequently 100–500 words. In JSON this is a single escaped string. In YAML it renders as a natural multi-line block:

```yaml
system_prompt: |
  You are Tom, the technical lead. Your job is to coordinate
  the team, break down complex problems, and delegate to the
  right agent for each task.

  When given a task:
  - Analyze what needs to be done
  - Identify which agents have the right tools
  - Delegate clearly with full context
```

An AI writing an agent definition can produce readable, correct YAML far more reliably than correctly escaped JSON.

---

## File Layout

```
internal/
  skills/
    builtin/
      workflow-designer.md     ← NEW
  scheduler/
    workflows_watcher.go       ← NEW
    workflows_watcher_test.go  ← NEW
  agents/
    config.go                  ← MODIFIED (dual-format read + YAML write)
    config_test.go             ← MODIFIED (add YAML round-trip tests)
  server/
    handlers_workflows.go      ← MODIFIED (add handleValidateWorkflow)
    handlers_workflows_validate_test.go  ← NEW
    server.go                  ← MODIFIED (register validate route)
```

---

## API Changes

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/workflows/validate` | Dry-run validation; no persistence |

No existing endpoints changed. No endpoints removed.

---

## Discovery Flow (External AI)

The complete flow for Claude Code or any external AI to create a workflow:

```
1. Read internal/skills/builtin/workflow-designer.md
   → learn schema, template vars, field options

2. Read ~/.huginn/agents/*.json and ~/.huginn/agents/*.yaml
   → discover agent names, models, toolbelt providers, skills

3. Write ~/.huginn/workflows/{id}.yaml
   → follows schema from step 1, uses agent names from step 2

4. (Optional) POST /api/v1/workflows/validate
   → dry-run before committing

5. WorkflowsWatcher picks up the new file within 2 seconds
   → cron registered if enabled: true
   → workflow appears in UI immediately (already true today)
```

No server restart. No UI interaction required.

---

## Testing

### `WorkflowsWatcher`
- New file with `enabled: true` → `RegisterWorkflow` called within poll window
- New file with `enabled: false` → loaded but not scheduled
- Modified file → `RegisterWorkflow` called again (upsert)
- Deleted file → `RemoveWorkflow` called
- File with invalid cron → error logged, other workflows unaffected
- `ctx.Done()` → watcher exits cleanly

### Agent dual-format
- `LoadAgents` reads `.json` files correctly (existing behavior, regression test)
- `LoadAgents` reads `.yaml` files correctly
- Mixed directory (both `.json` and `.yaml`) — all loaded, no duplicates
- `SaveAgent` writes `.yaml`; file is valid YAML; `system_prompt` is a block scalar
- Round-trip: `SaveAgent` then `LoadAgents` recovers identical `AgentDef`

### Validate endpoint
- Valid workflow body → `200 {"valid": true}`
- Unknown agent name → `422` with descriptive error
- Bad cron expression → `422` with descriptive error
- Missing required field (name) → `422` with descriptive error
- Auth required (no token → `401`)

---

## Success Criteria

- Claude Code can read `internal/skills/builtin/workflow-designer.md`, read `~/.huginn/agents/*.yaml`, write a workflow YAML to `~/.huginn/workflows/`, and have it appear in the UI and fire on schedule — with zero UI interaction and no server restart
- A huginn agent with the `workflow-designer` skill enabled can do the same through chat
- An AI-written agent `.yaml` file with a multi-line `system_prompt` loads correctly and appears in the agents list
- All existing `.json` agent files continue to load without changes
- The validate endpoint catches schema errors before they reach disk
