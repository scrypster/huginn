# Permissions & Delegation Hardening — Design Spec

**Date:** 2026-04-27
**Status:** Approved
**Branch:** feature/permissions

---

## Problem

Three critical bugs prevent the multi-agent system from working reliably:

### Bug 1 — Agent loop stops after tool calls (no delegation)
Max uses MuninnDB tools, produces final text, loop exits. The user expected Max to delegate to Elena/Maya. Root cause: Bug 2. When the LLM never sees `delegate_to_agent` in its tool schema, it cannot call it, so after completing its own work it writes a response and the loop exits normally.

### Bug 2 — Delegation tools absent for agents with named LocalTools
`applyToolbelt` in `agent_dispatcher.go` only includes delegation tools (`delegate_to_agent`, `list_team_status`, `recall_thread_result`) when `LocalTools = ["*"]` (wildcard). Agents with a specific named list — or no local tools at all — never receive delegation tools in their schema. This is the root cause of Bug 1.

The delegation tools are tagged `"builtin"` in `main.go`, so `AllBuiltinSchemas()` returns them. But `AllBuiltinSchemas()` is only called for the wildcard case. Named-list agents go through `SchemasByNames(ag.LocalTools)`, which only returns the explicitly named tools.

### Bug 3 — Local tool permissions vanish on page refresh
`saveLocalAccessModal()` in `AgentsView.vue` updates `form.value.local_tools` in-memory but never calls `save()`. The agent YAML is never written to disk. Refreshing reloads from disk, losing changes.

Counterpart functions `saveConnectionsModal` and `saveSkillsModal` both call `await save()` when editing an existing agent. `saveLocalAccessModal` was simply missing this call.

---

## Scope

**In scope:**
- Fix `applyToolbelt` to always inject delegation tools (step 5) — `internal/agent/agent_dispatcher.go`
- Fix `saveLocalAccessModal` to persist on save — `web/src/views/AgentsView.vue`
- Regression tests: `applyToolbelt` delegation injection, frontend save behavior

**Out of scope:**
- Space-context injection blocks (lines 488-501 / 688-701) — these add `list_team_status` and `recall_thread_result` for channel contexts; they become redundant but harmless (dedup prevents duplicates) and are left in place to preserve documented intent
- Agent YAML schema — no changes
- Any UI changes beyond the `saveLocalAccessModal` fix

---

## Architecture

### Fix 1: `applyToolbelt` step 5

After step 3 (vault tools), add step 5 that iterates over the three delegation tool names and injects any that are registered but not yet in `schemas`:

```go
// 5. Always inject delegation tools when registered.
// These are registered and tagged "builtin" in main.go. Agents with named
// LocalTools lists miss them because SchemasByNames only returns explicit names.
// Agents in space (channel) contexts already get list_team_status /
// recall_thread_result via the space-context block, but we still inject here
// for dedup-safety and so DM-mode agents see all three tools.
delegationNames := []string{"delegate_to_agent", "list_team_status", "recall_thread_result"}
seenDelegation := make(map[string]bool, len(schemas))
for _, s := range schemas {
    seenDelegation[s.Function.Name] = true
}
for _, name := range delegationNames {
    if !seenDelegation[name] {
        if t, ok := reg.Get(name); ok {
            schemas = append(schemas, t.Schema())
        }
    }
}
```

This is purely additive and safe: `reg.Get` returns false when the tool isn't registered (e.g., outside server mode), so the loop is a no-op in that case.

### Fix 2: `saveLocalAccessModal`

Change from synchronous one-liner to match `saveConnectionsModal` pattern:

```go
// Before:
function saveLocalAccessModal() {
  form.value.local_tools = [...modalLocalTools.value]
  showLocalAccessModal.value = false
}

// After:
async function saveLocalAccessModal() {
  form.value.local_tools = [...modalLocalTools.value]
  showLocalAccessModal.value = false
  if (props.agentName && props.agentName !== 'new') {
    await save()
  } else {
    markDirty()
  }
}
```

---

## Tests

### Backend — `internal/agent/local_tools_test.go`

| Test | Asserts |
|------|---------|
| `TestApplyToolbelt_NamedLocalToolsAlwaysIncludesDelegationTools` | Named LocalTools + delegation tools registered → all three injected |
| `TestApplyToolbelt_EmptyLocalToolsAlwaysIncludesDelegationTools` | Empty LocalTools + delegation tools registered → all three injected |
| `TestApplyToolbelt_DelegationToolsNotInjectedWhenNotRegistered` | Delegation tools not registered → none injected |
| `TestApplyToolbelt_WildcardDeduplicatesDelegationTools` | LocalTools=["*"] → no duplicate delegation tools |

### Frontend — `web/src/views/__tests__/AgentsView.test.ts`

| Test | Asserts |
|------|---------|
| `saveLocalAccessModal calls save() for existing agent` | `mockApiAgentsUpdate` called after saving local tools modal |
| `saveLocalAccessModal marks dirty for new agent` | `dirty` state set for `agentName='new'` |

---

## Success Criteria

- `go test ./internal/agent/... -run TestApplyToolbelt` passes
- `npm run test -- AgentsView` passes
- `go build ./...` passes
- Agent with named LocalTools receives delegation tools in its schema
- Saving local tools persists to disk (no refresh regression)
