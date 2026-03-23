# Getting Started with Huginn

This guide takes you from zero to productive in about 15 minutes. It assumes you are a developer comfortable with a terminal and a text editor.

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Go | 1.25+ | `go version` to verify |
| Ollama | any | Required for local model use. Download at [ollama.com](https://ollama.com). |
| Cloud API key | — | Alternative to Ollama. Anthropic, OpenAI, or OpenRouter. |
| Node.js | 20+ | Optional. Only needed if you want to build the web UI from source. |

You need either Ollama or a cloud API key. You do not need both to get started.

---

## Install

### From source (recommended for contributors)

```bash
git clone https://github.com/scrypster/huginn
cd huginn
go build -tags embed_frontend -o huginn .
sudo mv huginn /usr/local/bin/huginn
```

The `-tags embed_frontend` flag bundles the web UI into the binary so it works from any directory.

### Via go install

```bash
go install github.com/scrypster/huginn@latest
```

The binary is self-contained. No external runtime dependencies beyond your chosen backend.

### Pull local models (Ollama)

Huginn's three default agent slots map to these models:

```bash
ollama pull qwen3-coder:30b    # planner — Chris
ollama pull qwen2.5-coder:14b  # coder   — Steve
ollama pull deepseek-r1:14b    # reasoner — Mark
```

Pulling all three is recommended but not required. If a slot's model is missing, Huginn falls back to whichever model is available. You can also configure each slot to use a different model in `~/.huginn/config.json`.

---

## First Run: TUI Mode

Start Ollama, then launch Huginn from inside a project directory:

```bash
ollama serve          # in a separate terminal, or as a background service
cd ~/my-project
huginn
```

**What you see when the TUI opens:**

- A status bar at the top showing the active agent name, model, and backend health.
- A scrollable chat area in the center.
- A model indicator in the bottom-right corner showing which slot is active.
- An input prompt at the bottom.

**On first run**, Huginn indexes your repository automatically. You will see a brief "indexing..." indicator. This builds the BM25 and HNSW vector indexes that power semantic code search. Subsequent runs reuse the index and start instantly.

**Try your first question:**

```
> what does the authentication flow do in this codebase?
```

Huginn will retrieve the most relevant file chunks from the index, pass them as context to the LLM, and stream the answer. You did not paste any files — it found them automatically.

**Useful TUI shortcuts:**

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Ctrl+C` | Cancel current generation |
| `Ctrl+D` | Exit Huginn |
| `Tab` | Cycle through input history |

---

## Using the Web UI

The web UI gives you a richer multi-agent experience with a persistent session sidebar, live sub-agent thread panel, and an Inbox for automation results.

Start the web server:

```bash
huginn tray
```

Then open your browser to the address shown in the terminal output (typically `http://localhost:8421`).

**What you see:**

- **Left sidebar** — session list. Each conversation is a persistent session with its own history.
- **Main chat area** — your primary conversation with the active agent.
- **Thread panel (right)** — when an agent spawns a sub-agent, its work appears here in real time: tokens streaming, tool calls completing, status updates. You can watch Steve write code without leaving your conversation with Chris.
- **Inbox** — landing spot for completed automation results (once Routines ship).

**Creating a session:**

Click "New Session" in the sidebar. Give it a name (e.g., "payment module"). Each session maintains its own message history and can have a different active agent.

**Watching sub-agent threads:**

When you ask an agent to delegate work (e.g., "ask Steve to implement the gateway"), the Thread panel on the right activates and shows a live card for the sub-agent. Each card displays the sub-agent's name, current status, and a live token stream. When it finishes, a summary is automatically posted back into the main chat — you do not need to read the thread card unless you want to.

---

## Your First Multi-Agent Task

### TUI: run agents in parallel

The fastest way to use all three agents at once is to fire non-interactive commands from your shell simultaneously:

```bash
huginn --agent Chris "design the interface for internal/payment/gateway.go" &
huginn --agent Steve "implement internal/payment/gateway.go based on Chris's design" &
huginn --agent Mark  "review internal/payment/ for error handling gaps" &
wait
```

Each agent runs its own session independently. The swarm semaphore in Huginn prevents runaway resource use if you chain many agents.

### Web UI: delegate from a conversation

Start a session in the web UI and ask your primary agent:

```
> Delegate the implementation of internal/payment/gateway.go to Steve,
  and ask Mark to review it when Steve is done.
```

The agent will use the `delegate_to_agent` tool to spin up sub-agents. You will see their work appear in the Thread panel on the right as it happens. When they finish, the summaries arrive in your main chat.

---

## Configuring a Cloud Backend (Optional)

If you prefer Anthropic, OpenAI, or OpenRouter instead of local Ollama models, update `~/.huginn/config.json`.

The config file is created automatically on first run. Open it in your editor:

```bash
$EDITOR ~/.huginn/config.json
```

### Anthropic

```json
{
  "backend": {
    "type": "external",
    "provider": "anthropic",
    "endpoint": "https://api.anthropic.com",
    "api_key": "$ANTHROPIC_API_KEY"
  },
  "coder_model": "claude-sonnet-4-5"
}
```

Set your key in the environment:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

### OpenAI

```json
{
  "backend": {
    "type": "external",
    "provider": "openai",
    "endpoint": "https://api.openai.com",
    "api_key": "$OPENAI_API_KEY"
  },
  "coder_model": "gpt-4o"
}
```

### OpenRouter

OpenRouter gives you access to hundreds of models through a single API key, including many that work well for code:

```json
{
  "backend": {
    "type": "external",
    "provider": "openrouter",
    "api_key": "$OPENROUTER_API_KEY"
  },
  "coder_model": "anthropic/claude-sonnet-4-5"
}
```

**API key resolution:** any `api_key` value starting with `$` is read from the environment variable of that name. Literal key strings are also accepted but environment variables are preferred — they keep secrets out of config files.

---

## Enabling Long-term Memory (Optional)

Huginn integrates with **MuninnDB** to give agents vault-scoped memory that persists across sessions. An agent that reviewed your auth module last week still knows what it decided.

### Step 1: Install and start MuninnDB

Follow the MuninnDB setup documentation for your platform. The MuninnDB process must be running and reachable before Huginn can connect.

### Step 2: Configure vault mapping

Create or edit `huginn.workspace.json` in your project root:

```json
{
  "memory_vault": "my-project"
}
```

This tells Huginn which MuninnDB vault to use for this workspace. If no workspace file is present, Huginn falls back to the `"default"` vault.

### Step 3: Test the connection

```bash
huginn memory status
```

Expected output:

```
Memory: connected  vault: my-project  entries: 0
```

### Step 4: Store and recall

```bash
# Store a fact
huginn memory store "the payments service uses idempotency keys on all POST endpoints"

# List stored memories
huginn memory list

# Agents recall relevant memories automatically at the start of each session
huginn --print "how does the payment service handle duplicate requests?"
```

---

## Connecting to HuginnCloud (Optional)

HuginnCloud lets you access your agents from any machine or browser. In the web UI,
click the profile icon in the bottom-left corner and click "Connect to Huginn Cloud."
A browser window opens for you to approve the connection. Once approved, your machine
appears as "Online" in the HuginnCloud dashboard.

For more, see [docs/features/huginncloud.md](features/huginncloud.md).

---

## Skills: Teaching Agents New Tricks (Optional)

Skills inject reusable instructions into agent system prompts without touching any code. They are YAML files that live in `~/.huginn/skills/`.

### Create a skill

```bash
mkdir -p ~/.huginn/skills/nil-guard
```

Create `~/.huginn/skills/nil-guard/skill.yaml`:

```yaml
name: nil-guard
description: "Always add nil guards before dereferencing pointers"
mode: prompt
system_prompt_fragment: |
  Before writing any code that dereferences a pointer, always add a nil
  check and return a descriptive error rather than panicking.
```

Huginn loads all skills in `~/.huginn/skills/` at startup. No registration step required — drop the directory in and restart.

### Modes

| Mode | What it does |
|---|---|
| `prompt` | Appends `system_prompt_fragment` verbatim to the system message |
| `template` | Same as prompt, but renders `{{ .Variable }}` Go template syntax first |
| `shell` | Runs `command` and injects stdout into context (e.g., live linting output) |

### Auto-discovered rule files

Huginn also automatically picks up project-level instructions from these files in your workspace root, in order of precedence:

- `.huginn/rules.md`
- `CLAUDE.md`
- `.cursorrules`
- `.github/copilot-instructions.md`

You do not need to configure anything — if the file exists, it is loaded.

---

## Next Steps

You are up and running. Here is where to go from here:

| Resource | What it covers |
|---|---|
| [README.md](../README.md) | Full CLI reference, configuration schema, all supported backends |
| [docs/CONTRIBUTING.md](CONTRIBUTING.md) | How to build, test, add tools, add backends, and open PRs |
| [Multi-Agent](features/multi-agent.md) | Chris, Steve, Mark — parallel delegation and the thread panel |
| [Routines & Scheduling](features/routines.md) | YAML Routines, cron scheduling, Inbox integration |
| [Workflows](features/workflows.md) | Chain Routines into ordered pipelines with variables |
| [Custom Agents](features/custom-agents.md) | Create agents beyond Chris, Steve, and Mark |
| [Permissions](features/permissions.md) | How Huginn controls what agents can and cannot do |
| [Memory](features/memory.md) | Context notes and MuninnDB cross-session memory |
| `huginn --help` | Authoritative flag and subcommand reference for your installed version |
| [HuginnCloud](features/huginncloud.md) | Connect to HuginnCloud, access agents from anywhere |
| [Headless Mode](features/headless.md) | Run Huginn on a server or in Docker |
| [Troubleshooting](troubleshooting.md) | Common problems and fixes |

If something breaks, `huginn report-bug` prints the issue URL and the location of any crash files on your machine.
