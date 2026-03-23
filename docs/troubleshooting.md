# Troubleshooting

This guide covers common issues across all parts of Huginn. Each feature's documentation also has a Troubleshooting section with issues specific to that feature — check there if the problem is clearly related to one area (e.g., a Routine not running, an OAuth flow failing).

To generate a bug report with crash file locations and system information:

```bash
huginn report-bug
```

---

## Quick diagnostics

Run these first before digging into a specific issue:

```bash
# Verify the installed version
huginn --version

# Check memory connection status (if using MuninnDB)
huginn memory status

# Verify Ollama is running and which models are available (if using Ollama)
ollama list

# Check for crash files
ls ~/.huginn/logs/
```

Crash logs live in `~/.huginn/logs/`. If Huginn panicked, a file there will contain the stack trace. Include it when filing a bug report.

---

## Installation & startup

**`go build` fails**

Confirm you are on Go 1.25 or later:

```bash
go version
```

If you are building with the embedded web UI, you must include the build tag:

```bash
go build -tags embed_frontend -o huginn .
```

Without `-tags embed_frontend`, the binary will not serve the web UI. The build will succeed but opening a browser to the server address will return an error.

**Web UI shows a blank page**

The binary was built without the `embed_frontend` tag. Rebuild:

```bash
go build -tags embed_frontend -o huginn .
sudo mv huginn /usr/local/bin/huginn
```

Alternatively, run `huginn tray` from the repo root so the development file server can find the `web/dist/` directory.

**Port conflict on startup**

Huginn defaults to port 8421. If another process is using that port, change it in `~/.huginn/config.json`:

```json
{
  "web": {
    "port": 9000
  }
}
```

**Startup blocks on onboarding prompt**

On servers and in Docker, Huginn waits for a user to complete onboarding before starting. Skip onboarding with:

```bash
huginn tray --server
```

The `--server` flag starts immediately without any interactive prompt. Use it in all non-interactive environments.

---

## Models & backend

**Ollama not running**

Huginn cannot reach the local backend. Start Ollama in a separate terminal or as a background service:

```bash
ollama serve
```

Then retry. Huginn will reconnect automatically once the backend is reachable.

**API key errors**

Check that the environment variable is exported in the shell that starts Huginn:

```bash
echo $ANTHROPIC_API_KEY   # should print your key
huginn
```

In `~/.huginn/config.json`, API key values beginning with `$` are read from the environment. A literal `$ANTHROPIC_API_KEY` string in the config file means Huginn is reading the variable, not the value — verify the variable is set.

**Wrong model or fallback model in use**

If a configured model is not available, Huginn falls back to whichever model Ollama has installed. To see what is installed:

```bash
ollama list
```

Pull missing models:

```bash
ollama pull qwen3-coder:30b    # Chris — planner
ollama pull qwen2.5-coder:14b  # Steve — coder
ollama pull deepseek-r1:14b    # Mark  — reasoner
```

To set a specific model for a slot, update `~/.huginn/config.json`:

```json
{
  "planner_model": "qwen3-coder:30b",
  "coder_model": "qwen2.5-coder:14b",
  "reasoner_model": "deepseek-r1:14b"
}
```

**Slow responses**

Large models on CPU-only hardware are slow by nature. Try a smaller model variant (e.g., `qwen2.5-coder:7b` instead of `:14b`) for interactive sessions. Use larger models for offline Routines where speed is less critical.

**Context window exceeded**

The agent's conversation history has grown past what the model can hold. In the TUI, press `Ctrl+C` to stop the current generation, then use `/compact` to summarize the session and free context space. Alternatively, start a new session for a fresh context window.

---

## Permissions & tool use

**Agent pauses and appears to be doing nothing**

The agent is waiting at a permission approval prompt. Look for the prompt in the TUI or as an inline card in the web UI. The prompt shows the tool name, arguments, and a description of what is about to happen.

Press `a` or `y` to allow the call once. Press `A` to allow all future calls to that tool for this session. Press `d` to deny.

If the TUI is not showing a prompt, try scrolling up in the chat view — the prompt may be above the current scroll position.

**Stale file error**

The agent attempted to write a file that was modified externally between when it read the file and when it tried to write it. Huginn captures a SHA-256 hash of the file at read time and rejects writes when the hash no longer matches.

The agent handles this automatically by re-reading the file and re-planning. If stale file errors repeat in a tight loop, check whether another process (another agent, your editor's auto-save, or a background file watcher) is continuously modifying the same file.

**Routine never completes — permission blocked**

Routines run in headless mode with no user present to respond to approval prompts. If the Routine's prompt leads the agent to attempt a write or exec tool (`write_file`, `bash`, etc.), the call is denied immediately and the agent may stall or fail before `timeout_secs` is reached.

Design Routines to use only read-oriented tools: file reads, code search, git log, web search. If your Routine genuinely needs write access and you have accepted the risk, run Huginn with:

```bash
huginn tray --dangerously-skip-permissions
```

See [features/permissions.md](features/permissions.md) for the full picture.

**`--dangerously-skip-permissions` risks**

This flag bypasses all approval prompts. The agent can write any file, run any shell command, and create commits without any user checkpoint. Use it only in isolated environments (Docker containers, CI runners) where the environment itself provides the safety boundary. Do not use it on a development machine with access to production credentials.

---

## Sessions & memory

**Session history missing after reinstall or path change**

Sessions are stored as JSONL files in `~/.huginn/sessions/`. If you moved or deleted that directory, the history is gone. Back up `~/.huginn/` before reinstalling or migrating machines.

**Agent is confused or losing track of earlier context**

The conversation history has grown long and the model is struggling to hold all of it in its context window. Use `/compact` to summarize the session:

```
/compact
```

This replaces the full message history with a summary and frees up context space. Alternatively, start a new session for a clean slate — long-lived sessions naturally accumulate noise.

**Cross-session memory not working**

Huginn does not persist memory between sessions by default. Agents start each session without knowledge of previous ones unless MuninnDB is configured.

Check that MuninnDB is running and configured:

```bash
huginn memory status
```

If it shows `disconnected`, verify that the MuninnDB process is running and that `~/.huginn/huginn.workspace.json` (or your project's `huginn.workspace.json`) specifies the correct vault:

```json
{
  "memory_vault": "my-project"
}
```

**Agent doesn't remember something from last week**

This is expected without MuninnDB. Each session is independent by default. Configure MuninnDB and teach the agent to store decisions and findings using `memory_write`. On the next session, relevant memories are recalled automatically.

---

## Code intelligence

**Agent references deleted functions or stale code**

The code intelligence index is out of date. The index is stored in `~/.huginn/huginn.pebble`. Delete it to force a full rebuild on the next startup:

```bash
rm -rf ~/.huginn/huginn.pebble
huginn
```

Indexing runs in the background. The first startup after deletion will be slower than usual while the index rebuilds.

**Indexing is slow on first run**

This is a one-time cost. Huginn reads every source file, builds a BM25 term index, and optionally generates embeddings for semantic search. On a large codebase, this can take several minutes. Subsequent runs reuse the index and start quickly.

**Semantic search not working (always gets BM25 results)**

Semantic search requires a backend capable of generating embeddings. If the embedding backend is unavailable (Ollama not running, no embedding model configured), Huginn falls back to BM25 keyword search automatically. The results are still useful — BM25 is the primary ranking signal anyway.

To verify the embedding backend is available, check that Ollama is running and that an embedding model is configured in `~/.huginn/config.json`. Consult the backend documentation for your specific setup.

**Impact analysis output is truncated**

The impact analysis (tracing which files use a changed symbol) caps the result set on large codebases to avoid overwhelming the context window. If you need the full list, run the analysis on a narrower scope (a specific directory or package) rather than the entire repository.

---

## Skills

**Skill is not being applied**

Check that the skill is listed in `~/.huginn/skills/installed.json`. Skills must be registered in the installed manifest to be loaded. If you added a skill directory manually, add an entry to `installed.json` or install it through the Skills page in the web UI.

**Skill loads but the agent ignores it**

The skill's `SKILL.md` file may have a YAML frontmatter parse error. Huginn silently skips skills whose frontmatter cannot be parsed. Verify that the file starts with exactly `---` on the first line and has a valid `name` field:

```
---
name: my-skill
description: "Does something useful"
---

The skill instructions follow here...
```

Open the Skills page in the web UI to see the list of loaded skills and any parse errors.

**Auto-discovered rule file not loading**

Huginn auto-discovers project instructions from these files in the workspace root, checked in this order:

1. `.huginn/rules.md`
2. `CLAUDE.md`
3. `.cursorrules`
4. `.github/copilot-instructions.md`

Verify the file exists in the workspace root (the directory where you run `huginn`, not a subdirectory). If the file exists and is still not loading, check that it is readable:

```bash
ls -la .huginn/rules.md
```

---

## Connections & OAuth

**OAuth redirect_uri mismatch**

The callback URL registered with the provider (GitHub, Google, etc.) does not match the callback URL Huginn sends during the OAuth flow. Log into the provider's developer console and update the redirect URI to match what Huginn expects. The correct callback URL is shown in the Connections page in the web UI when you begin the connection flow.

**Connection shows as active but tool calls return auth errors**

The stored token has expired or been revoked. Disconnect the connection and reconnect:

1. Open the Connections page in the web UI.
2. Find the connection and click **Disconnect**.
3. Click **Connect** and complete the OAuth flow again.

Huginn will store the new token.

**CLI tool not detecting authentication**

Some connection providers (GitHub CLI, AWS CLI) are detected by reading credential files or environment variables that the CLI tool manages. If Huginn cannot detect the auth state:

1. Run the provider's own auth status command (e.g., `gh auth status`, `aws sts get-caller-identity`) to confirm the CLI is authenticated.
2. Restart Huginn so it re-reads the credential files.
3. If the connection still shows as unauthenticated in the web UI, check whether the credential file location is in a non-standard path and whether Huginn has read permission.

**Tokens lost after restart in CI or Docker**

In CI runners and Docker containers, the system keychain is typically unavailable. Huginn falls back to storing tokens in the config file when the keychain is not accessible, but in some environments that directory is also ephemeral. For stateless CI environments, pass credentials via environment variables rather than relying on stored connections.

---

## Routines & Workflows

**Routine not running at the scheduled time**

Three common causes:

1. Huginn was not running when the scheduled time arrived. Huginn does not fire missed jobs. Confirm the process is running with a persistent service manager (launchd, systemd, Docker).
2. The cron expression is wrong. Validate it at [crontab.guru](https://crontab.guru). A common mistake is using 24-hour time where 12-hour was intended.
3. The scheduler is disabled. Check `~/.huginn/config.json`:

```json
{
  "scheduler_enabled": true
}
```

**Results not appearing in Inbox**

Verify: `scheduler_enabled: true` in config, `enabled: true` in the Routine YAML, and that Huginn was running at the scheduled time. If the Routine ran but produced no output, `timeout_secs` may have been reached before the agent finished — increase it or simplify the prompt.

Inbox notifications only appear in the web UI. Run `huginn tray` to access the web UI.

**Routine stalls and never completes**

The agent likely attempted a write or exec tool that was blocked by the permission gate (see the Permissions section above). Design unattended Routines to use only read-oriented tools.

**Slug not found in Workflow**

The Workflow step references a Routine by slug (filename stem), not by the `id` field inside the YAML. If the step says `routine: pr-review`, the file must be `~/.huginn/routines/pr-review.yaml`. Check that the filename matches exactly, including case and hyphens versus underscores.

---

## HuginnCloud & relay

**Machine shows as Offline in HuginnCloud**

The WebSocket relay connection from Huginn to HuginnCloud has dropped. Restart Huginn — the relay reconnects automatically on startup. If the machine stays Offline after restart, check that the machine has outbound network access to `api.huginncloud.com` and that no firewall is blocking WebSocket connections.

**Connect flow fails mid-way**

The browser-based OAuth connection flow to HuginnCloud can fail if:

- The callback URL is blocked by a browser extension or popup blocker.
- The local Huginn server is not reachable from the browser (e.g., `web.bind` is `127.0.0.1` and you are on a remote machine).
- The approval timed out.

Click **Connect** again to restart the flow. If the problem persists, check the Huginn logs in `~/.huginn/logs/` for connection errors.

**WebSocket error: bad handshake**

The relay token has expired or is invalid. Click **Connect** again in the web UI to initiate a fresh token exchange with HuginnCloud. The old connection will be replaced.

---

## Crash reporting

If Huginn panics, the panic handler captures the stack trace and writes it to `~/.huginn/logs/`. To generate a bug report that includes the crash file location and system information:

```bash
huginn report-bug
```

This prints the URL for filing a GitHub issue and the path to any crash files on your machine. Attach the crash file when filing the report.

Crash logs are plain text files. Review them before sharing to make sure they do not contain sensitive information such as API keys, file paths, or source code you do not want to share publicly.
