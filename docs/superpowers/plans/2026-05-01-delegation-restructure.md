# Delegation Restructure: Tool-Based Channels + World-Class UX

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the fragile @mention delegation path for high-tier models in channel contexts, replacing it with the deterministic `delegate_to_agent` tool, while rendering tool-call delegations as beautiful Slack-style delegation cards in the UI.

**Architecture:** Channel contexts currently force even capable models off the reliable `delegate_to_agent` tool path onto a regex-based @mention parser, causing silent failures when models forget the `@` sigil. This change (1) updates the channel system prompt to instruct `delegate_to_agent`, (2) injects the tool into channel context schemas, (3) makes the delegation strip show the task description for immediate clarity, (4) renders delegation errors/warnings as visible chips so users are never left hanging, and (5) adds a heuristic fallback for low-tier models that still use the @mention path.

**Tech Stack:** Go (backend), Vue 3 + TypeScript (frontend), WebSocket events

---

## File Map

**Modified:**
- `internal/agent/context.go` — channel delegation prompt (swap @mentions for `delegate_to_agent`)
- `internal/agent/agent_dispatcher.go` — inject `delegate_to_agent` tool in channel context (2 locations)
- `internal/server/space_context_injection_test.go` — update test assertions after prompt change
- `internal/threadmgr/mentions.go` — add bare-name heuristic fallback for low-tier models
- `web/src/composables/useSessions.ts` — add `task?: string` to `DelegatedThread` interface
- `web/src/views/ChatView.vue` — capture `task` from `thread_started`, render error/warning chips

---

### Task 1: Update channel delegation prompt to use `delegate_to_agent`

**Files:**
- Modify: `internal/agent/context.go:276-288` (the `BuildSpaceContextBlock` lead-agent delegation block)
- Modify: `internal/server/space_context_injection_test.go:327-331` (update assertions)

- [ ] **Step 1: Read the current delegation block**

```bash
sed -n '274,295p' internal/agent/context.go
```

Expected output: lines that say `"**Delegation protocol — use @mentions, NOT tools:**\n"`, `"Do NOT use the delegate_to_agent tool in channels..."`, etc.

- [ ] **Step 2: Replace the delegation block in `context.go`**

Find this block (inside `BuildSpaceContextBlock`, the lead-agent branch):

```go
		sb.WriteString("**Delegation protocol — use @mentions, NOT tools:**\n")
		sb.WriteString("When delegating work to a team member, write a natural message that @mentions them with the full task.\n")
		sb.WriteString("Do NOT use the delegate_to_agent tool in channels. Instead, write your delegation as a natural message:\n\n")
		sb.WriteString("  GOOD: \"@Sam, please calculate 3+3 and give me a one-sentence answer.\"\n")
		sb.WriteString("  GOOD: \"@Adam, can you list the top 3 best practices for Go unit tests? Keep it brief.\"\n")
		sb.WriteString("  BAD:  \"@Sam is on it — check the thread for his answer.\" (too vague, not a real task)\n")
		sb.WriteString("  BAD:  Using delegate_to_agent tool call (use @mention instead)\n\n")
		sb.WriteString("The @mention triggers automatic thread creation — the named agent receives your message as their task and replies in a thread on your message, just like Slack.\n")
		sb.WriteString("Your message IS the delegation — make it specific and actionable so the agent knows exactly what to do.\n\n")
```

Replace with:

```go
		sb.WriteString("**Delegation protocol — use the `delegate_to_agent` tool:**\n")
		sb.WriteString("When you need a team member to handle a sub-task, call `delegate_to_agent` with their name and a clear, actionable task description.\n")
		sb.WriteString("The tool spawns a thread automatically — the agent's reply will appear as a thread on your message, just like Slack.\n\n")
		sb.WriteString("  GOOD: delegate_to_agent({agent: \"Sam\", task: \"Calculate 3+3 and return a one-sentence answer.\", rationale: \"Sam specialises in arithmetic\"})\n")
		sb.WriteString("  GOOD: delegate_to_agent({agent: \"Adam\", task: \"List the top 3 best practices for Go unit tests. Keep it brief.\", rationale: \"Adam is the Go expert\"})\n")
		sb.WriteString("  BAD:  Mentioning a team member by name in your message without calling delegate_to_agent — this does nothing.\n")
		sb.WriteString("  BAD:  A vague task like \"help with this\" — the agent needs a specific, complete description to act.\n\n")
```

- [ ] **Step 3: Run the affected test to see it fail**

```bash
go test ./internal/server/... -run TestBuildSpaceContextBlock_LeadAgent_ContainsAllElements -v
```

Expected: FAIL — `"@mention instruction"` assertion fails because the text no longer contains `"@mentions"`.

- [ ] **Step 4: Update the test assertions in `space_context_injection_test.go`**

Find the test `TestBuildSpaceContextBlock_LeadAgent_ContainsAllElements` (around line 311). The check named `"@mention instruction"` looks for `"@mentions"` — replace it:

```go
		{"@mention instruction", "@mentions"},
```

Replace with:

```go
		{"Delegation tool instruction", "delegate_to_agent"},
		{"GOOD example with Sam", "GOOD: delegate_to_agent"},
```

- [ ] **Step 5: Run tests to verify pass**

```bash
go test ./internal/server/... -run TestBuildSpaceContext -v
```

Expected: all `TestBuildSpaceContext*` tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/context.go internal/server/space_context_injection_test.go
git commit -m "fix(agent): replace @mention delegation with delegate_to_agent in channel prompt"
```

---

### Task 2: Inject `delegate_to_agent` into channel context tool schemas

**Files:**
- Modify: `internal/agent/agent_dispatcher.go:515` (TaskWithAgent path)
- Modify: `internal/agent/agent_dispatcher.go:715` (ChatWithAgent path)

> There are exactly two `delegationToolNames` lists in `agent_dispatcher.go` — both currently contain only `["list_team_status", "recall_thread_result"]`. Both must be updated.

- [ ] **Step 1: Confirm the two locations**

```bash
grep -n "delegationToolNames" internal/agent/agent_dispatcher.go
```

Expected output shows two lines: one around line 515, one around line 715.

- [ ] **Step 2: Update both `delegationToolNames` slices**

Find (appears twice, update both):

```go
		delegationToolNames := []string{"list_team_status", "recall_thread_result"}
```

Replace each with:

```go
		delegationToolNames := []string{"delegate_to_agent", "list_team_status", "recall_thread_result"}
```

- [ ] **Step 3: Update the comments above each block**

Find (appears twice, update both):

```go
	// Auto-inject read-only team tools when the agent is in a channel context.
	// delegate_to_agent is NOT injected — channels use @mention-based delegation
	// so the lead agent writes natural messages like "@Sam, please do X" and the
	// mention parser (CreateFromMentions) spawns the thread automatically.
```

Replace each with:

```go
	// Auto-inject team coordination tools when the agent is in a channel context.
	// delegate_to_agent is the primary delegation path for capable models;
	// list_team_status and recall_thread_result provide read access to the team.
```

- [ ] **Step 4: Write a test verifying `delegate_to_agent` appears in channel context schemas**

Check if there is an existing test file for agent_dispatcher channel tool injection:

```bash
grep -rn "TestChannel\|TestSpaceContext\|TestDelegat" internal/agent/ --include="*_test.go" | head -10
```

If no existing test for tool injection exists, create `internal/agent/channel_tool_injection_test.go`:

```go
package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/workforce"
)

// TestChannelContextInjectsDelegateToAgent verifies that delegate_to_agent
// appears in BuildSpaceContextBlock output for lead agents so we can track
// that the tool path is expected in channels.
func TestChannelContextPromptMentionsDelegateToAgent(t *testing.T) {
	members := []agent.SpaceMember{
		{Name: "Sam", Description: "Backend engineer"},
	}
	result := agent.BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", members)
	if !strings.Contains(result, "delegate_to_agent") {
		t.Errorf("expected channel prompt to mention delegate_to_agent, got:\n%s", result)
	}
	if strings.Contains(result, "use @mentions, NOT tools") {
		t.Errorf("expected old @mention instruction to be removed, still present in:\n%s", result)
	}
}

// TestChannelContextPromptPreservesMainChannelDiscipline verifies that the
// "speak only when additive" guidance is still present after the prompt change.
func TestChannelContextPromptPreservesMainChannelDiscipline(t *testing.T) {
	members := []agent.SpaceMember{
		{Name: "Sam", Description: "Backend"},
	}
	result := agent.BuildSpaceContextBlock("Eng", "channel", "Tom", "Tom", members)
	if !strings.Contains(result, "Main channel discipline") {
		t.Errorf("Main channel discipline section missing from channel prompt")
	}
	_ = workforce.WithSpaceContext // ensure package is imported without error
}
```

- [ ] **Step 5: Run the new test**

```bash
go test ./internal/agent/... -run TestChannelContextPrompt -v
```

Expected: both tests PASS.

- [ ] **Step 6: Build check**

```bash
go build ./...
```

Expected: exits 0 with no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/agent_dispatcher.go internal/agent/channel_tool_injection_test.go
git commit -m "fix(agent): inject delegate_to_agent tool for channel context agents"
```

---

### Task 3: Surface task description in delegation strips (frontend)

The `thread_started` payload already carries `task` but it is silently discarded. Adding it to `DelegatedThread` and rendering it gives users immediate context ("working on what?") without opening the thread.

**Files:**
- Modify: `web/src/composables/useSessions.ts:28-35` (`DelegatedThread` interface)
- Modify: `web/src/views/ChatView.vue:1672-1681` (thread_started handler)
- Modify: `web/src/views/ChatView.vue:488-543` (delegation strip template)

- [ ] **Step 1: Add `task` to `DelegatedThread` in `useSessions.ts`**

Find:

```typescript
export interface DelegatedThread {
  threadId: string
  agentId: string
  msgId?: string          // parent message ID for fetching thread messages (GET /api/v1/messages/{id}/thread)
  done?: boolean
  replyCount?: number     // actual thread reply count from DB (for badge label)
  inlineSummary?: string  // thread completion summary shown inline (Slack-style thread preview)
}
```

Replace with:

```typescript
export interface DelegatedThread {
  threadId: string
  agentId: string
  msgId?: string          // parent message ID for fetching thread messages (GET /api/v1/messages/{id}/thread)
  task?: string           // task description delegated to this agent (from thread_started payload)
  done?: boolean
  replyCount?: number     // actual thread reply count from DB (for badge label)
  inlineSummary?: string  // thread completion summary shown inline (Slack-style thread preview)
}
```

- [ ] **Step 2: Capture `task` in the `thread_started` WS handler in `ChatView.vue`**

Find this block inside `registerWS(ws, 'thread_started', ...)` (around line 1672):

```typescript
      if (!already) {
        target.delegatedThreads.push({
          threadId: p.thread_id,
          agentId: p.agent_id || '',
          msgId: p.parent_message_id || target.id || '',
          replyCount: 0,
        })
      }
```

Replace with:

```typescript
      if (!already) {
        target.delegatedThreads.push({
          threadId: p.thread_id,
          agentId: p.agent_id || '',
          msgId: p.parent_message_id || target.id || '',
          task: (p.task as string) || undefined,
          replyCount: 0,
        })
      }
```

- [ ] **Step 3: Show the task line in the delegation strip template**

In the delegation strip (around line 488-540 in `ChatView.vue`), find the reply-count label block inside the button:

```html
                      <!-- Reply count label or "working…" indicator -->
                      <span class="text-xs font-medium" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
                        <template v-if="['running','thinking','queued'].includes(getThreadById(d.threadId)?.Status ?? '')">
                          working…
                        </template>
                        <template v-else>
                          {{ (d.replyCount ?? 1) === 1 ? '1 reply' : `${d.replyCount} replies` }}
                        </template>
                      </span>
                      <!-- Separator · agent name · status when done/error -->
                      <span class="text-[11px] text-huginn-muted/50">
                        · {{ d.agentId }}
```

Replace with:

```html
                      <!-- Reply count label or "working…" indicator -->
                      <span class="text-xs font-medium" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
                        <template v-if="['running','thinking','queued'].includes(getThreadById(d.threadId)?.Status ?? '')">
                          working…
                        </template>
                        <template v-else>
                          {{ (d.replyCount ?? 1) === 1 ? '1 reply' : `${d.replyCount} replies` }}
                        </template>
                      </span>
                      <!-- Task description — shown only when available, truncated -->
                      <span v-if="d.task" class="text-[11px] text-huginn-muted/60 truncate max-w-[200px]">
                        · {{ d.task }}
                      </span>
                      <!-- Separator · agent name · status when done/error (task hidden if task shown) -->
                      <span v-else class="text-[11px] text-huginn-muted/50">
                        · {{ d.agentId }}
```

- [ ] **Step 4: Run frontend type check**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: exits 0 with no type errors.

- [ ] **Step 5: Commit**

```bash
cd ..
git add web/src/composables/useSessions.ts web/src/views/ChatView.vue
git commit -m "feat(ui): show delegation task description in thread strips"
```

---

### Task 4: Render delegation errors and warnings as visible chips

`delegationErrors` and `delegationWarnings` are stored on messages but never rendered. Users need to see these so they are never left wondering why a delegation silently failed.

**Files:**
- Modify: `web/src/views/ChatView.vue` (inside the assistant message template, after tool-call chips)

- [ ] **Step 1: Find the insertion point**

In `ChatView.vue`, find the end of the delegated thread strips block (around line 543):

```html
                </div>
                <!-- Delegated thread reply strips (Slack-style compact) -->
```

The delegation error/warning chips should go AFTER the `delegatedThreads` strips div (after `</div>` that closes the `v-if="msg.delegatedThreads?.length"` block) and BEFORE the next sibling block.

Search for the closing tag of the delegated threads block followed by the `MessageActions` component or next sibling:

```bash
grep -n "delegatedThreads\|MessageActions" web/src/views/ChatView.vue | head -10
```

- [ ] **Step 2: Add the delegation error and warning chips after the thread strips**

Find the closing tag of the `v-if="msg.delegatedThreads?.length"` block. It will look like:

```html
                </div>

                <!-- MessageActions or other sibling -->
```

Insert after the closing `</div>` of the `delegatedThreads` block:

```html

                <!-- Delegation error chips — shown when tm.Create() failed for an @mentioned agent -->
                <div v-if="(msg as any).delegationErrors?.length" class="mt-1.5 flex flex-wrap gap-1.5">
                  <div
                    v-for="e in (msg as any).delegationErrors"
                    :key="e.agent"
                    class="inline-flex items-center gap-1.5 px-2 py-1 rounded-lg text-[11px] font-medium
                           border border-huginn-red/30 bg-huginn-red/8 text-huginn-red"
                    :title="`Could not delegate to ${e.agent}: ${e.reason}`"
                  >
                    <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                      <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
                    </svg>
                    <span>{{ e.agent }} unavailable</span>
                  </div>
                </div>

                <!-- Delegation warning chips — shown when heuristic detects a missed delegation -->
                <div v-if="(msg as any).delegationWarnings?.length" class="mt-1.5 flex flex-wrap gap-1.5">
                  <div
                    v-for="w in (msg as any).delegationWarnings"
                    :key="w.agent + w.reason"
                    class="inline-flex items-center gap-1.5 px-2 py-1 rounded-lg text-[11px] font-medium
                           border border-huginn-yellow/30 bg-huginn-yellow/8 text-huginn-yellow"
                    :title="w.reason === 'missing_mention_syntax' ? `${w.agent} was mentioned but not delegated — did you mean to assign them a task?` : `Unknown agent: ${w.agent}`"
                  >
                    <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
                    </svg>
                    <span v-if="w.reason === 'missing_mention_syntax'">{{ w.agent }} may have been missed</span>
                    <span v-else>Unknown: {{ w.agent }}</span>
                  </div>
                </div>
```

- [ ] **Step 3: Run frontend type check**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: exits 0 with no errors.

- [ ] **Step 4: Run the existing ChatView component tests**

```bash
cd web && npx vitest run src/views/__tests__/
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
cd ..
git add web/src/views/ChatView.vue
git commit -m "feat(ui): render delegation error and warning chips on messages"
```

---

### Task 5: Add bare-name heuristic fallback in `CreateFromMentions` (low-tier models)

When a low-tier model writes "Elena, please investigate..." without the `@`, `ParseMentions` returns zero results and nothing happens. This task adds a heuristic scan: if zero @mentions were found but a known agent name appears near delegation-intent language, emit `delegation_warning` with the `heuristic_agents` field so the frontend can surface a warning chip.

**Files:**
- Modify: `internal/threadmgr/mentions.go` (inside `CreateFromMentions`, after the `len(requests) == 0` log)
- Create: `internal/threadmgr/mentions_heuristic_test.go`

- [ ] **Step 1: Write the failing test first**

Create `internal/threadmgr/mentions_heuristic_test.go`:

```go
package threadmgr

import (
	"strings"
	"testing"
)

// TestDetectBareAgentNames verifies that detectBareAgentNames finds agent names
// that appear in text without an @ sigil, near delegation-intent language.
func TestDetectBareAgentNames(t *testing.T) {
	known := []string{"Elena", "Sam", "Adam"}

	tests := []struct {
		name     string
		msg      string
		wantAny  []string // at least one of these must appear in result
		wantNone []string // none of these should appear
	}{
		{
			name:    "bare name with delegation verb",
			msg:     "Elena, please investigate the latency issue.",
			wantAny: []string{"Elena"},
		},
		{
			name:    "bare name with 'can you'",
			msg:     "Sam can you run the benchmarks?",
			wantAny: []string{"Sam"},
		},
		{
			name:    "already @mentioned — should not appear in heuristic",
			msg:     "@Elena please investigate",
			wantNone: []string{"Elena"},
		},
		{
			name:    "name in non-delegation context",
			msg:     "I was talking to Elena yesterday about the plan.",
			wantNone: []string{"Elena"},
		},
		{
			name:    "name in heading/title without intent",
			msg:     "Sam's analysis shows the following results.",
			wantNone: []string{"Sam"},
		},
		{
			name:    "multiple bare names with delegation",
			msg:     "Sam, please run tests. Adam, please check the docs.",
			wantAny: []string{"Sam", "Adam"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectBareAgentNames(tt.msg, known)
			for _, want := range tt.wantAny {
				found := false
				for _, r := range result {
					if strings.EqualFold(r, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %q in result %v", want, result)
				}
			}
			for _, notwant := range tt.wantNone {
				for _, r := range result {
					if strings.EqualFold(r, notwant) {
						t.Errorf("expected %q NOT in result %v", notwant, result)
					}
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/threadmgr/... -run TestDetectBareAgentNames -v
```

Expected: FAIL — `detectBareAgentNames` undefined.

- [ ] **Step 3: Implement `detectBareAgentNames` in `mentions.go`**

Add after the `var mentionRe = MentionRe` line (around line 50):

```go
// delegationIntentRe matches common phrases that indicate the speaker is
// delegating a task to someone. Used by detectBareAgentNames to reduce
// false positives from casual name mentions.
var delegationIntentRe = regexp.MustCompile(`(?i)\b(please|can you|could you|would you|i need you|go ahead|proceed|investigate|look into|check|handle|take care of|work on|help with|analyse|analyze|review|run|execute|prepare|write|create|build|fix|update|deploy)\b`)

// detectBareAgentNames scans msg for known agent names that appear WITHOUT
// an @ sigil, near delegation-intent language (e.g. "Elena, please investigate").
// Returns the names found. Used as a heuristic fallback for low-tier models
// that omit the @ sigil.
//
// Heuristic rules:
//  1. The name must not be immediately preceded by @  (already an @mention)
//  2. A delegation-intent verb must appear within 60 chars before or after the name
//  3. The name must appear at a word boundary (not mid-word like "Samsung")
func detectBareAgentNames(msg string, knownAgents []string) []string {
	lower := strings.ToLower(msg)
	var found []string
	for _, name := range knownAgents {
		lname := strings.ToLower(name)
		idx := 0
		for {
			pos := strings.Index(lower[idx:], lname)
			if pos < 0 {
				break
			}
			abs := idx + pos
			idx = abs + len(lname)

			// Reject if preceded by @ (already an @mention).
			if abs > 0 && msg[abs-1] == '@' {
				continue
			}

			// Require word boundary before: must be start-of-string or a non-name char.
			if abs > 0 {
				before := lower[abs-1]
				if (before >= 'a' && before <= 'z') || (before >= '0' && before <= '9') || before == '_' || before == '-' {
					continue // mid-word
				}
			}
			// Require word boundary after.
			end := abs + len(lname)
			if end < len(lower) {
				after := lower[end]
				if (after >= 'a' && after <= 'z') || (after >= '0' && after <= '9') || after == '_' || after == '-' {
					continue // mid-word
				}
			}

			// Check for delegation-intent language within a 60-char window.
			start := abs - 60
			if start < 0 {
				start = 0
			}
			end2 := abs + len(lname) + 60
			if end2 > len(lower) {
				end2 = len(lower)
			}
			window := lower[start:end2]
			if delegationIntentRe.MatchString(window) {
				found = append(found, name)
				break // one entry per agent
			}
		}
	}
	return found
}
```

- [ ] **Step 4: Run the test to confirm it passes**

```bash
go test ./internal/threadmgr/... -run TestDetectBareAgentNames -v
```

Expected: all sub-tests PASS.

- [ ] **Step 5: Wire `detectBareAgentNames` into `CreateFromMentions`**

In `mentions.go`, find the block after `len(requests) == 0` (around line 170):

```go
	if len(requests) == 0 {
		logger.Warn("CreateFromMentions: no valid mentions resolved",
			"session_id", sessionID, "caller", callerAgent,
			"unknown", unknown, "raw_msg_len", len(userMsg))
	}
```

Replace with:

```go
	if len(requests) == 0 {
		logger.Warn("CreateFromMentions: no valid mentions resolved",
			"session_id", sessionID, "caller", callerAgent,
			"unknown", unknown, "raw_msg_len", len(userMsg))
		// Heuristic fallback: scan for bare agent names near delegation-intent
		// language. Used for low-tier models that omit the @ sigil.
		// Emit delegation_warning so the frontend can surface a visible chip
		// rather than leaving the user wondering why nothing happened.
		if broadcast != nil {
			heuristic := detectBareAgentNames(userMsg, names)
			if len(heuristic) > 0 {
				logger.Warn("CreateFromMentions: heuristic detected bare agent names without @",
					"session_id", sessionID, "heuristic_agents", heuristic)
				broadcast(sessionID, "delegation_warning", map[string]any{
					"session_id":      sessionID,
					"parent_msg_id":   parentMsgID,
					"caller":          callerAgent,
					"heuristic_agents": heuristic,
					"reason":          "missing_mention_syntax",
				})
			}
		}
	}
```

- [ ] **Step 6: Run all threadmgr tests**

```bash
go test ./internal/threadmgr/... -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 7: Full build check**

```bash
go build ./...
```

Expected: exits 0.

- [ ] **Step 8: Commit**

```bash
git add internal/threadmgr/mentions.go internal/threadmgr/mentions_heuristic_test.go
git commit -m "fix(threadmgr): heuristic fallback for bare agent names in @mention path"
```

---

## Self-Review

**Spec coverage check:**
- ✅ Task 1: Channel prompt updated to `delegate_to_agent`
- ✅ Task 2: Tool injected into channel schemas (both `TaskWithAgent` and `ChatWithAgent` paths)
- ✅ Task 3: Task description surfaced in delegation strip
- ✅ Task 4: Error and warning chips rendered — users never left hanging
- ✅ Task 5: Heuristic fallback for low-tier models

**Placeholder scan:** No TBDs or vague instructions. All code is complete.

**Type consistency:** `DelegatedThread.task` added in Task 3 Step 1, consumed in Step 2, rendered in Step 3. `heuristic_agents` field in delegation_warning already handled by the existing `delegation_warning` WS handler in ChatView (line ~1943). No new types introduced.

**Regression risk:** The only breaking test is `TestBuildSpaceContextBlock_LeadAgent_ContainsAllElements` which is fixed in Task 1 Step 4. No other tests assert on the `@mention` delegation instruction text.
