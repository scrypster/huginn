# Slash Commands

Huginn uses `/` as the universal trigger for commands, navigation, and user-installed skills. The same command surface works in both the TUI and the web UI chat input.

---

## How the picker works

Type `/` in the chat input to open the inline picker. The picker filters in real time as you continue typing — for example, typing `/pl` narrows to `/plan`. Use the following keys to operate it:

| Key | Action |
|-----|--------|
| Any character | Narrows the list in real time |
| `Enter` | Selects the highlighted command |
| `Tab` | Autocompletes to the highlighted command |
| `Escape` | Dismisses the picker without selecting |

Built-in commands and user-installed skills share the same picker list. There is no visual distinction between them.

---

## Model-switching commands

These commands switch the active model slot for the current session. Huginn maps three named roles — planner, coder, and reasoner — to specific models configured in your settings.

### `/plan`

Switches the active slot to the **planner model** (Chris by default). Appends `"Using planner mode"` to the system context so the model is primed for structured output.

**Best for:** architectural design, task decomposition, writing specs, breaking down ambiguous requirements before writing any code.

**Typical workflow:**

```
/plan
Describe what you want to build — the agent will produce a structured plan.
Then use /code to hand off implementation to Steve.
```

---

### `/code`

Switches the active slot to the **coder model** (Steve by default).

**Best for:** direct implementation requests, writing functions, refactoring, generating tests, applying a plan that already exists.

---

### `/reason`

Switches the active slot to the **reasoner model** (Mark by default).

**Best for:** debugging hard problems, analyzing tradeoffs, reasoning through edge cases, reviewing logic that is difficult to evaluate quickly.

---

### `/switch-model`

Opens an interactive prompt to switch the model for the current session without assigning it to a named role. Accepts free-form input:

```
use claude-opus-4-6 for planning
```

Use this when you want to temporarily override a slot without changing your saved configuration.

---

## Agent execution commands

### `/iterate [N]`

Runs the agent multiple times on the same task to improve output quality through successive refinement. Prompts you for the number of iterations if `N` is not supplied.

```
/iterate 3
```

Each pass receives the output of the previous pass as context. Useful for code quality, prose polish, or test generation where a single pass is not sufficient.

---

### `/parallel <task1> | <task2> | ...`

Runs multiple tasks concurrently using the orchestrator's `BatchChat` fan-out. Tasks are separated by `|`. All tasks run at the same time; results are displayed sequentially, labeled by task number, when all complete.

```
/parallel Write unit tests for auth.go | Write unit tests for session.go | Write unit tests for relay.go
```

Use this when you have independent work that can safely run in parallel without shared context.

---

### `/swarm <agent>:<prompt> | <agent>:<prompt> | ...`

Dispatches a **named-agent swarm**: each segment assigns a specific agent to a specific prompt. Agents run concurrently and stream progress into the swarm view.

```
/swarm Steve:implement the login handler | Mark:review the auth logic for edge cases
```

- Each segment takes the form `agentName:prompt`.
- If no colon is present, the entire segment is treated as both the agent name and the prompt.
- Results stream into the swarm view in real time. Use `/swarm` with no arguments to re-enter the swarm view if you have navigated away.

**Difference from `/parallel`:** `/parallel` uses the default agent for all tasks; `/swarm` routes each task to a specific named agent.

---

## Agent management — `/agents`

`/agents` with no arguments displays the agent roster and navigates to the agents management screen.

### Sub-commands

| Sub-command | Syntax | Description |
|-------------|--------|-------------|
| _(none)_ | `/agents` | Show agent roster and open the agents screen |
| `new` / `create` | `/agents new` or `/agents create <name> <model>` | `new` opens the agent creation wizard. `create` creates an agent non-interactively with the given name and model ID |
| `swap` | `/agents swap <name> <model>` | Switch the model used by an existing agent and persist the change |
| `rename` | `/agents rename <name> <new-name>` | Rename an agent; saves the new definition and removes the old file |
| `persona` | `/agents persona <name>` | Display the system prompt (persona) configured for a named agent |
| `delete` | `/agents delete <name>` | Permanently delete an agent from the registry |

**Examples:**

```
/agents new
/agents create Petra claude-opus-4-6
/agents swap Steve claude-sonnet-4-6
/agents rename Chris Architect
/agents persona Mark
/agents delete Petra
```

Agent changes made through slash commands are persisted to `~/.huginn/agents/` immediately. They take effect in the current session without requiring a restart.

---

## Analysis and insight commands

### `/impact <symbol>`

Runs call-graph impact analysis on a named symbol (function, type, or identifier) and reports all callers and dependents grouped by confidence level.

```
/impact validateSession
```

Output is organized into **HIGH**, **MEDIUM**, and **LOW** confidence tiers based on the indexed symbol graph. Requires the workspace to be indexed — run `/workspace` to check index status. If no edges are found, the command suggests running `/workspace` to index the repo first.

---

### `/radar`

Runs the **Proactive Impact Radar** against the current git repository. Radar compares the HEAD commit to its parent, identifies changed files, and evaluates which other parts of the codebase may be affected.

Output lists findings grouped by severity. Requires:
- A git repository at the workspace root
- Storage initialized (the symbol index must be populated)

If no findings are returned, Huginn reports that the current state appears clean.

---

### `/stats`

Displays session statistics from the live stats registry:

- Tokens used (input and output)
- Estimated cost
- Model latency (median and p95)
- Cache hit rate
- Files indexed

---

### `/workspace`

Shows the active workspace state:

- Root path
- Number of chunks indexed

If the root is not set, the output shows `(not set)`. Useful for confirming that Huginn has picked up the correct project directory before running analysis commands.

---

## Session management commands

### `/resume`

Opens the session picker overlay. Select a previous session by number or name to restore the full message history.

---

### `/save`

Force-saves the current session state to disk immediately. Huginn auto-saves periodically, but use this before closing the terminal or switching projects if you want to be certain.

---

### `/title <text>`

Renames the current session.

```
/title Auth refactor — March sprint
```

Session names appear in the session sidebar (web UI) and in `/resume` picker lists. Sessions are automatically named from the first message but can be renamed at any time.

---

## Space navigation commands

### `/dm [agent-name]`

Opens or creates a **Direct Message** space with a specific agent. Providing an agent name pre-filters the picker. DM spaces are persistent, separate from main chat sessions.

```
/dm Steve
```

---

### `/channel [name]`

Opens or creates a **Channel** space for multi-agent collaboration. Providing a name pre-filters the picker.

```
/channel backend-review
```

---

## Screen navigation commands

These commands navigate to a specific screen in the TUI or web UI without any additional arguments.

| Command | Navigates to |
|---------|-------------|
| `/models` | Model management screen — view, pull, and delete models |
| `/connections` | Connections manager — OAuth provider setup and token status |
| `/skills` | Skills screen — browse, install, and manage skills |
| `/settings` | Settings screen — model selection, theme, backend config |
| `/workflows` | Workflow manager — create, trigger, and view run history |
| `/logs` | Application log viewer |
| `/inbox` | Notification inbox — workflow results and alerts |

---

### `/help`

Displays the keybindings reference and a summary of slash commands in the current terminal session.

**TUI keybindings (also shown by `/help`):**

| Key | Action |
|-----|--------|
| `a` | Approve plan |
| `e` | Edit plan |
| `c` | Cancel plan |
| `ctrl+c` | Cancel active stream / quit |
| `ctrl+o` | Expand or collapse tool output |

---

## Notepad management — `/notepad`

Manages persistent notepads that survive across sessions.

| Sub-command | Syntax | Description |
|-------------|--------|-------------|
| `list` | `/notepad list` | List all notepads (name and scope) |
| `show` | `/notepad show <name>` | Display the full content of a notepad |
| `create` | `/notepad create <name>` | Create a new notepad |
| `delete` | `/notepad delete <name>` | Delete a notepad permanently |

If no sub-command is given, the usage summary is shown.

---

## User-installed skills as commands

Any skill that is installed and enabled automatically appears in the `/` picker alongside built-in commands. Skills and built-ins are listed together with no visual distinction.

**Install a skill from the registry:**

```bash
huginn skill install <skill-name>
```

**Override a built-in command:** Create a skill with the same name as a built-in command. User-installed skills always take precedence. No flags or configuration required.

```
# Example: replace /plan with a custom planning skill
# Create ~/.huginn/skills/plan.md with your custom skill definition
```

To browse the skills registry, see [Skills](skills.md).

---

## Command reference summary

| Command | Category | Description |
|---------|----------|-------------|
| `/plan` | Model | Switch to planner model (Chris) |
| `/code` | Model | Switch to coder model (Steve) |
| `/reason` | Model | Switch to reasoner model (Mark) |
| `/switch-model` | Model | Interactively change the model for the current session |
| `/iterate [N]` | Execution | Run the agent N times for quality improvement |
| `/parallel <tasks>` | Execution | Run multiple tasks concurrently with the default agent |
| `/swarm <agent:prompt>` | Execution | Dispatch tasks to specific named agents concurrently |
| `/agents [sub]` | Agents | List, create, rename, swap, persona, or delete agents |
| `/impact <symbol>` | Analysis | Call-graph impact analysis for a symbol |
| `/radar` | Analysis | Proactive Impact Radar findings for recent git changes |
| `/stats` | Analysis | Token usage, cost, latency, cache stats |
| `/workspace` | Analysis | Show workspace root and index status |
| `/resume` | Session | Open session picker to restore a previous session |
| `/save` | Session | Force-save current session immediately |
| `/title <text>` | Session | Rename the current session |
| `/dm [agent]` | Spaces | Open or create a DM space with an agent |
| `/channel [name]` | Spaces | Open or create a Channel space |
| `/models` | Navigation | Open model management screen |
| `/connections` | Navigation | Open connections manager |
| `/skills` | Navigation | Open skills screen |
| `/settings` | Navigation | Open settings screen |
| `/workflows` | Navigation | Open workflow manager |
| `/logs` | Navigation | Open application log viewer |
| `/inbox` | Navigation | Open notification inbox |
| `/notepad [sub]` | Notepads | List, show, create, or delete persistent notepads |
| `/help` | Help | Show keybindings and command reference |
