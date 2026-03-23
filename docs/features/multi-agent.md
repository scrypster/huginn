# Multi-Agent

## What it is

Huginn supports parallel agent delegation — you talk to one primary agent while others work in the background on delegated tasks. Their results stream into the thread panel and post back to your main chat automatically. Agents decide who to delegate to based on the task and each agent's description; there is no fixed pipeline.

Huginn ships with three named agents by default:

| Agent | Default persona | Default model |
|-------|----------------|---------------|
| Chris | Architect / planner | `qwen3-coder:30b` |
| Steve | Senior engineer / coder | `qwen2.5-coder:14b` |
| Mark | Deep thinker / reviewer | `deepseek-r1:14b` |

These are starting points. You can edit their personas, swap their models, add more agents, or remove any of them. See [Custom Agents](custom-agents.md).

---

## How to use it

### Delegate from chat

Just describe the task and name the agent:

```
Ask Steve to implement the payment gateway in internal/payment/gateway.go
```

The primary agent delegates to Steve. Steve's work streams into the **Thread Panel** on the right side of the web UI. When Steve finishes, his summary posts back into your main conversation automatically — no polling, no context switching.

### Run an agent directly

```bash
# Target a specific agent from the command line
huginn --agent Steve "implement internal/payment/gateway.go"
huginn --agent Chris "design the authentication module architecture"
huginn --agent Mark "review the last three commits for security issues"
```

### Watch work in real time

In the web UI (`huginn tray`), the thread panel (right side) shows each sub-agent's output as it streams. You can watch Steve implement a feature while continuing a design conversation with Chris.

### Completion notification

When a sub-agent finishes, a completion summary appears in your main chat thread. You stay in context — no need to switch windows or check a separate terminal.

---

## Configuration

| Key | Where | Description |
|-----|-------|-------------|
| `planner_model` | `~/.huginn/config.json` | Model for Chris (planner slot) |
| `coder_model` | `~/.huginn/config.json` | Model for Steve (coder slot) |
| `reasoner_model` | `~/.huginn/config.json` | Model for Mark (reasoner/reviewer slot) |

Example — use a cloud model for the coder:
```json
{
  "coder_model": "claude-sonnet-4-5",
  "backend": {
    "type": "external",
    "provider": "anthropic",
    "endpoint": "https://api.anthropic.com",
    "api_key": "$ANTHROPIC_API_KEY"
  }
}
```

Agent names can be customized in `huginn.workspace.json` at the project root.

---

## Tips & common patterns

- **Fire agents in parallel for independent tasks** — if design and implementation are genuinely independent, delegate both at once. Chris and Steve work concurrently; neither blocks the other.
- **AutoHelpResolver keeps you unblocked** — if a sub-agent needs clarification mid-task, it queries the primary agent silently. You are not interrupted unless the primary agent itself needs your input.
- **Thread panel is web-UI only** — in the TUI (`huginn` without `tray`), sub-agent output appears inline in the terminal. The thread panel is only available in the web UI.
- **Use `--agent` for focused work** — if you only need one agent for a task, target it directly with `--agent` to skip the delegation overhead.

---

## Troubleshooting

**Sub-agent not appearing in thread panel**

The thread panel only shows in the web UI (`huginn tray`). If you launched Huginn without `tray`, sub-agent output appears inline in the TUI instead.

**Wrong agent used for a task**

Specify the agent by name:
```bash
huginn --agent Steve "implement X"
```
Or address the agent explicitly in the prompt: "Ask Steve to implement X" (not just "implement X").

**Agent falls back to the wrong model**

Check which models are available in Ollama:
```bash
ollama list
```
If the configured model is missing, Ollama falls back to whatever is available. Pull the correct model:
```bash
ollama pull qwen2.5-coder:14b
```
