# Permissions & Delegation Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three critical bugs: delegation tools absent for agents with named LocalTools (Bug 2, causes Bug 1 loop exit), and local tool permissions not persisting on page refresh (Bug 3).

**Architecture:** Two independent fixes — (1) add step 5 to `applyToolbelt` in `agent_dispatcher.go` that always injects delegation tools by name regardless of LocalTools config; (2) make `saveLocalAccessModal` in `AgentsView.vue` async and call `save()` matching the pattern of `saveConnectionsModal`.

**Tech Stack:** Go 1.22+, Vue 3, Vitest, Go testing package

---

## File Map

| File | Change |
|------|--------|
| `internal/agent/agent_dispatcher.go` | Add step 5 to `applyToolbelt` (always inject delegation tools) |
| `internal/agent/local_tools_test.go` | Add 4 regression tests for delegation injection |
| `web/src/views/AgentsView.vue` | Fix `saveLocalAccessModal` to call `save()` |
| `web/src/views/__tests__/AgentsView.test.ts` | Add 2 tests for `saveLocalAccessModal` persistence |

---

### Task 1: Fix `applyToolbelt` — always inject delegation tools

**Files:**
- Modify: `internal/agent/agent_dispatcher.go` (lines 53–104, `applyToolbelt` function)
- Test: `internal/agent/local_tools_test.go`

**Context:**
`applyToolbelt` currently has 4 steps: (1) local builtins, (2) toolbelt providers, (3) vault/muninndb tools, (4) fork gate. Delegation tools (`delegate_to_agent`, `list_team_status`, `recall_thread_result`) are tagged `"builtin"` in `main.go` so they're included when `LocalTools = ["*"]` (step 1 calls `AllBuiltinSchemas()`). But agents with named LocalTools (e.g., `["read_file", "bash"]`) only get those explicit tools — delegation is excluded. The fix: add step 5 between vault injection and gate fork that injects any registered delegation tool not already in schemas.

- [ ] **Step 1: Write the failing tests**

In `internal/agent/local_tools_test.go`, append after the existing `TestApplyToolbelt_BothLocalAndExternal` test:

```go
// buildDelegationTestRegistry creates a registry with builtin tools, external tools,
// AND delegation tools registered and tagged "builtin" — matching main.go wiring.
func buildDelegationTestRegistry() *tools.Registry {
	reg := buildLocalTestRegistry() // read_file, bash, git_status (builtin), slack_post (slack)
	for _, name := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools([]string{"delegate_to_agent", "list_team_status", "recall_thread_result"}, "builtin")
	return reg
}

// TestApplyToolbelt_NamedLocalToolsAlwaysIncludesDelegationTools is the primary
// regression test for Bug 2: agents with a named LocalTools list must still
// receive delegation tools so the LLM can call delegate_to_agent.
func TestApplyToolbelt_NamedLocalToolsAlwaysIncludesDelegationTools(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Max", LocalTools: []string{"read_file", "bash"}}

	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("expected delegation tool %q in schemas with named LocalTools, got names=%v", expected, names)
		}
	}
	// Original named tools must still be present
	if !names["read_file"] || !names["bash"] {
		t.Errorf("expected original local tools read_file and bash, got names=%v", names)
	}
}

// TestApplyToolbelt_EmptyLocalToolsAlwaysIncludesDelegationTools verifies that
// even agents with NO local tools configured receive delegation tools (Bug 2).
func TestApplyToolbelt_EmptyLocalToolsAlwaysIncludesDelegationTools(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Max"} // empty LocalTools

	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("expected delegation tool %q even with empty LocalTools, got names=%v", expected, names)
		}
	}
}

// TestApplyToolbelt_DelegationToolsNotInjectedWhenNotRegistered ensures the
// step 5 injection is a safe no-op when delegation tools are not in the registry
// (e.g., TUI mode or test environments that don't register them).
func TestApplyToolbelt_DelegationToolsNotInjectedWhenNotRegistered(t *testing.T) {
	reg := buildLocalTestRegistry() // no delegation tools registered
	ag := &agents.Agent{Name: "Max", LocalTools: []string{"read_file"}}

	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	for _, unexpected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if names[unexpected] {
			t.Errorf("delegation tool %q should NOT be injected when not registered, got names=%v", unexpected, names)
		}
	}
}

// TestApplyToolbelt_WildcardDeduplicatesDelegationTools ensures that when
// LocalTools=["*"] (which already includes delegation via AllBuiltinSchemas),
// step 5 does not produce duplicate entries.
func TestApplyToolbelt_WildcardDeduplicatesDelegationTools(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Max", LocalTools: []string{"*"}}

	schemas, _ := applyToolbelt(ag, reg, nil)

	seen := map[string]int{}
	for _, s := range schemas {
		seen[s.Function.Name]++
	}
	for _, name := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if seen[name] > 1 {
			t.Errorf("delegation tool %q appears %d times in schemas, want exactly 1", name, seen[name])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... -run "TestApplyToolbelt_Named|TestApplyToolbelt_Empty|TestApplyToolbelt_DelegationToolsNot|TestApplyToolbelt_Wildcard" -v 2>&1 | tail -30
```

Expected: `TestApplyToolbelt_NamedLocalToolsAlwaysIncludesDelegationTools` and `TestApplyToolbelt_EmptyLocalToolsAlwaysIncludesDelegationTools` FAIL with missing delegation tools. Others may pass.

- [ ] **Step 3: Implement step 5 in `applyToolbelt`**

In `internal/agent/agent_dispatcher.go`, find the block ending at line 83 (closing `}` of the vault step) and insert before the `// 4. Fork the permission gate` comment:

Replace:
```go
	// 4. Fork the permission gate so each agent run gets isolated provider maps.
```

With:
```go
	// 5. Always inject delegation tools when registered in the registry.
	// Delegation tools (delegate_to_agent, list_team_status, recall_thread_result)
	// are tagged "builtin" in main.go so agents with LocalTools:["*"] already get
	// them via step 1. But agents with a named LocalTools list only get those
	// explicit names — delegation is excluded, causing the LLM to never call
	// delegate_to_agent and the loop to exit early (Bug 2 / Bug 1).
	// reg.Get returns (nil, false) when a tool is not registered, making this
	// a safe no-op in environments that don't register delegation tools.
	{
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
	}

	// 4. Fork the permission gate so each agent run gets isolated provider maps.
```

Note: the `{...}` block scopes `delegationNames`, `seenDelegation`, `name`, and `t` to avoid shadowing the `t` variable from the outer gate fork block. Re-number the comment to step 6 for the gate fork (it was step 4):

Actually, to avoid confusion with numbering, just number the new block as step 5 and rename the gate block to step 6. The full replacement:

Find exact text (lines 84–101):
```go
	// 4. Fork the permission gate so each agent run gets isolated provider maps.
	// When gate is nil (no permission gate configured), the forked gate is also nil.
	var agentGate *permissions.Gate
	if gate != nil {
		// Always allow "muninndb" (vault tools) even when the agent has an explicit
		// toolbelt. The vault schemas are already included in step 3 above; without
		// adding "muninndb" to allowedProviders, the gate would reject every vault
		// tool call with "permission denied" for agents that have a non-empty toolbelt.
		allowed := agents.AllowedProviders(ag.Toolbelt)
		if allowed != nil {
			allowed["muninndb"] = true
		}
		agentGate = gate.Fork(
			agents.WatchedProviders(ag.Toolbelt),
			allowed,
		)
	}
```

Replace with:
```go
	// 5. Always inject delegation tools when registered in the registry.
	// Delegation tools (delegate_to_agent, list_team_status, recall_thread_result)
	// are tagged "builtin" in main.go so agents with LocalTools:["*"] already get
	// them via step 1. But agents with a named LocalTools list only get those
	// explicit names — delegation is excluded, causing the LLM to never call
	// delegate_to_agent and the loop to exit early (Bug 2 / Bug 1).
	// reg.Get returns (nil, false) when a tool is not registered, making this
	// a safe no-op in environments that don't register delegation tools.
	{
		delegationNames := []string{"delegate_to_agent", "list_team_status", "recall_thread_result"}
		seenDelegation := make(map[string]bool, len(schemas))
		for _, s := range schemas {
			seenDelegation[s.Function.Name] = true
		}
		for _, dname := range delegationNames {
			if !seenDelegation[dname] {
				if dt, ok := reg.Get(dname); ok {
					schemas = append(schemas, dt.Schema())
				}
			}
		}
	}

	// 6. Fork the permission gate so each agent run gets isolated provider maps.
	// When gate is nil (no permission gate configured), the forked gate is also nil.
	var agentGate *permissions.Gate
	if gate != nil {
		// Always allow "muninndb" (vault tools) even when the agent has an explicit
		// toolbelt. The vault schemas are already included in step 3 above; without
		// adding "muninndb" to allowedProviders, the gate would reject every vault
		// tool call with "permission denied" for agents that have a non-empty toolbelt.
		allowed := agents.AllowedProviders(ag.Toolbelt)
		if allowed != nil {
			allowed["muninndb"] = true
		}
		agentGate = gate.Fork(
			agents.WatchedProviders(ag.Toolbelt),
			allowed,
		)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... -run "TestApplyToolbelt" -v 2>&1 | tail -30
```

Expected: All `TestApplyToolbelt_*` tests PASS.

- [ ] **Step 5: Run full agent package tests**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... -count=1 2>&1 | tail -20
```

Expected: All PASS, no regressions.

- [ ] **Step 6: Verify build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go build ./... 2>&1
```

Expected: no output (clean build).

- [ ] **Step 7: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add internal/agent/agent_dispatcher.go internal/agent/local_tools_test.go
git commit -m "fix(agent): always inject delegation tools regardless of LocalTools config

applyToolbelt step 5: inject delegate_to_agent, list_team_status,
recall_thread_result when registered, regardless of whether LocalTools
is empty, named, or wildcard. Previously only wildcard (LocalTools=[*])
agents got delegation tools via AllBuiltinSchemas — agents with named
local tool lists had no delegation tools in their schema, so the LLM
could not call delegate_to_agent (Bug 2) and the loop exited after the
last tool call instead of delegating (Bug 1).

Regression tests: 4 new TestApplyToolbelt_* cases cover named list,
empty list, unregistered (no-op), and wildcard dedup."
```

---

### Task 2: Fix `saveLocalAccessModal` — persist local tool permissions

**Files:**
- Modify: `web/src/views/AgentsView.vue` (line 1827, `saveLocalAccessModal`)
- Test: `web/src/views/__tests__/AgentsView.test.ts`

**Context:**
`saveLocalAccessModal` updates `form.value.local_tools` in-memory but never calls `save()`. Refreshing the page reloads from disk, losing all changes. `saveConnectionsModal` and `saveSkillsModal` both call `await save()` for existing agents and `markDirty()` for new agents — `saveLocalAccessModal` must do the same.

- [ ] **Step 1: Write the failing tests**

In `web/src/views/__tests__/AgentsView.test.ts`, append inside the `describe('AgentsView', () => {` block (after the last test, before the closing `})`):

```ts
  // ── saveLocalAccessModal persistence ──────────────────────────────
  it('saveLocalAccessModal calls save() for existing agent', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Max',
      model: 'claude-3',
      system_prompt: '',
      color: '#3fb950',
      icon: 'M',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'Max' })
    await flushPromises()

    // Directly call saveLocalAccessModal via the component's exposed vm
    // (shallowMount exposes all methods on vm)
    const vm = w.vm as unknown as Record<string, unknown>
    // Set up modalLocalTools state first
    ;(vm.modalLocalTools as { value: string[] }).value = ['read_file', 'bash']
    await (vm.saveLocalAccessModal as () => Promise<void>)()
    await flushPromises()

    expect(mockApiAgentsUpdate).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ local_tools: ['read_file', 'bash'] })
    )
  })

  it('saveLocalAccessModal marks dirty for new agent (does not call save)', async () => {
    const w = mountAgent({ agentName: 'new' })
    await flushPromises()

    const vm = w.vm as unknown as Record<string, unknown>
    ;(vm.modalLocalTools as { value: string[] }).value = ['bash']
    // Clear call count from initial mount
    mockApiAgentsUpdate.mockClear()
    await (vm.saveLocalAccessModal as () => Promise<void>)()
    await flushPromises()

    // For new agents, save() is NOT called — markDirty() sets dirty flag instead
    expect(mockApiAgentsUpdate).not.toHaveBeenCalled()
    // dirty state means "Unsaved changes" banner is visible
    expect(w.text()).toContain('Unsaved changes')
  })
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npm run test -- AgentsView --reporter=verbose 2>&1 | grep -E "PASS|FAIL|saveLocalAccessModal" | head -20
```

Expected: `saveLocalAccessModal calls save()` FAIL (mockApiAgentsUpdate not called).

- [ ] **Step 3: Implement the fix in `AgentsView.vue`**

In `web/src/views/AgentsView.vue`, find the `saveLocalAccessModal` function (line 1827):

Replace:
```ts
function saveLocalAccessModal() {
  form.value.local_tools = [...modalLocalTools.value]
  showLocalAccessModal.value = false
}
```

With:
```ts
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

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npm run test -- AgentsView --reporter=verbose 2>&1 | grep -E "PASS|FAIL|saveLocalAccessModal" | head -20
```

Expected: Both `saveLocalAccessModal` tests PASS.

- [ ] **Step 5: Run full frontend test suite**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npm run test -- --reporter=verbose 2>&1 | tail -30
```

Expected: All tests PASS.

- [ ] **Step 6: TypeScript type check**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npm run type-check 2>&1 | tail -20
```

Expected: No errors.

- [ ] **Step 7: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add web/src/views/AgentsView.vue web/src/views/__tests__/AgentsView.test.ts
git commit -m "fix(ui): saveLocalAccessModal now persists local tool permissions to disk

saveLocalAccessModal was updating form.value.local_tools in memory but
never calling save(). Refreshing the page reloaded from disk, losing all
local tool assignments. Made function async and added save()/markDirty()
matching the pattern of saveConnectionsModal and saveSkillsModal."
```
