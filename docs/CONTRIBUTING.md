# Contributing to Huginn

This guide answers "how do I build, test, and extend this?" — not "what does it do?" (see the [root README](../README.md) for that).

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Go | 1.25+ | `go version` to verify |
| Ollama | any | Required for integration tests that hit a live backend |
| `gh` CLI | any | Optional; enables GitHub tool registration |
| `git` | any | Required for git tool tests |

No C toolchain or CGO is required. All dependencies are pure Go.

---

## Building

```bash
# Clone
git clone https://github.com/scrypster/huginn
cd huginn

# Build the binary
go build -o huginn .

# Run it
./huginn --print "hello"

# Install to PATH
go install .
```

**Build tags:** none required. The managed backend (llama-server subprocess) is compiled in unconditionally; it is only activated when `backend.type = "managed"` in config.

---

## Running Tests

Always run with `-race`. Huginn makes heavy use of goroutines (swarm, streaming, relay) and the race detector has caught real bugs.

```bash
# All packages
go test -race ./...

# Single package
go test -race ./internal/tools/...

# With verbose output
go test -race -v ./internal/permissions/...

# With coverage
go test -race -coverprofile=cover.out ./...
go tool cover -html=cover.out
```

**Table-driven tests** are the standard pattern. Each test case should be a struct with `name`, inputs, and expected outputs:

```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    {"empty input", "", "", true},
    {"valid path", "/tmp/file.txt", "file.txt", false},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

**Hardening tests** (files named `hardening_roundN_test.go`) cover edge cases and adversarial inputs discovered during fuzzing or production incidents. When you fix a bug, add a hardening test in a new or existing hardening file — do not bury it inside the main test file.

---

## Project Structure

```
huginn/
├── main.go                  # Entry point: flag parsing, subcommand routing, TUI launch
├── go.mod
├── embed/                   # Embedded static assets (model manifest, runtime manifest)
├── docs/
│   ├── CONTRIBUTING.md      # This file
│   └── decisions/           # Architecture Decision Records (ADRs)
└── internal/
    ├── agent/               # Single-agent orchestrator (context builder, agentic loop)
    ├── agents/              # Named persona system (AgentDef, AgentRegistry, consult)
    ├── backend/             # LLM backend interface + Anthropic/OpenAI/OpenRouter/Ollama impls
    ├── compact/             # Context compaction: extractive (safe) + LLM summarization
    ├── config/              # JSON config, forward-only migration chain, atomic save
    ├── diffview/            # Unified diff renderer for the TUI
    ├── headless/            # CI/JSON runner — no TUI required
    ├── logger/              # Rotating file logger + panic handler
    ├── mcp/                 # Model Context Protocol client
    ├── memory/              # MuninnDB client, vault resolver, CLI subcommands
    ├── modelconfig/         # Context window registry, model slot system (planner/coder/reasoner)
    ├── models/              # Model manifest pull + local store
    ├── notepad/             # In-session scratch pad (token-limited)
    ├── patch/               # Unified diff apply (atomic write)
    ├── permissions/         # Tool permission gate + session allow-list
    ├── pricing/             # USD cost estimation from token counts
    ├── radar/               # BFS over reverse import graph (impact analysis)
    ├── relay/               # WebSocket hub for remote agent control
    ├── repo/                # File chunking, git-aware indexer, language detector
    ├── runtime/             # llama-server subprocess manager
    ├── search/              # BM25 + HNSW hybrid semantic search
    ├── session/             # JSONL message store per session
    ├── skills/              # Skill loader (YAML), rule file discovery, registry
    ├── stats/               # Token usage and cost collector
    ├── storage/             # Pebble KV wrapper (schema versioning)
    ├── streaming/           # Goroutine-based streaming runner
    ├── swarm/               # Concurrent agent scheduler with semaphore
    ├── symbol/              # AST/LSP symbol extraction (Go, TypeScript, heuristic)
    ├── tools/               # Tool interface, registry, all built-in tool implementations
    ├── tui/                 # bubbletea TUI (app model, styles, wizard, onboarding)
    ├── vision/              # Image detection and inline image support
    └── workspace/           # huginn.workspace.json discovery
```

---

## Adding a New Tool

A tool is anything an agent can call: file operations, shell commands, web search, git operations, etc.

### Step 1: Implement the `Tool` interface

The interface is defined in `internal/tools/types.go`:

```go
type Tool interface {
    Name()        string
    Description() string
    Permission()  PermissionLevel   // PermRead, PermWrite, or PermExec
    Schema()      backend.Tool      // JSON schema sent to the model
    Execute(ctx context.Context, args map[string]any) ToolResult
}
```

Create a new file in `internal/tools/`. Use an existing tool as a reference — `read_file.go` is a clean example of `PermRead`, `write_file.go` demonstrates `PermWrite` with the `FileLockManager`.

```go
// internal/tools/my_tool.go
package tools

import (
    "context"
    "github.com/scrypster/huginn/internal/backend"
)

type MyTool struct {
    SandboxRoot string
}

func (t *MyTool) Name() string        { return "my_tool" }
func (t *MyTool) Description() string { return "Does the thing." }
func (t *MyTool) Permission() PermissionLevel { return PermRead }

func (t *MyTool) Schema() backend.Tool {
    return backend.Tool{
        Type: "function",
        Function: backend.ToolFunction{
            Name:        t.Name(),
            Description: t.Description(),
            Parameters: backend.ToolParameters{
                Type: "object",
                Properties: map[string]backend.ToolProperty{
                    "path": {Type: "string", Description: "File path relative to workspace root."},
                },
                Required: []string{"path"},
            },
        },
    }
}

func (t *MyTool) Execute(ctx context.Context, args map[string]any) ToolResult {
    path, _ := args["path"].(string)
    // Validate path stays within sandbox
    if err := sandboxCheck(t.SandboxRoot, path); err != nil {
        return ToolResult{IsError: true, Error: err.Error()}
    }
    // Do work...
    return ToolResult{Output: "done"}
}
```

### Step 2: Register in `builtin.go`

Add your tool to the appropriate registration function in `internal/tools/builtin.go`:

```go
func RegisterBuiltins(reg *Registry, sandboxRoot string, bashTimeout time.Duration) {
    // ...existing registrations...
    reg.Register(&MyTool{SandboxRoot: sandboxRoot})
}
```

If your tool belongs to a different group (git, web, GitHub), add it to the matching `Register*` function or create a new one following the same pattern.

### Step 3: Write a test

Create `internal/tools/my_tool_test.go`. Use a temporary directory as the sandbox root. Always test the sandbox escape case:

```go
func TestMyTool_SandboxEscape(t *testing.T) {
    dir := t.TempDir()
    tool := &MyTool{SandboxRoot: dir}
    result := tool.Execute(context.Background(), map[string]any{
        "path": "../../etc/passwd",
    })
    if !result.IsError {
        t.Fatal("expected sandbox violation to be rejected")
    }
}
```

---

## Adding a New Backend

A backend is any LLM endpoint Huginn can call. The interface is in `internal/backend/backend.go`:

```go
type Backend interface {
    ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Health(ctx context.Context) error
    Shutdown(ctx context.Context) error  // no-op for stateless backends
    ContextWindow() int                   // max tokens for the configured model
}
```

### Step 1: Implement the interface

Create `internal/backend/myprovider.go`. Follow `anthropic.go` or `openrouter.go` as reference. Key contract points:

- `ChatCompletion` must handle streaming via `req.OnEvent` if non-nil, falling back to `req.OnToken`.
- `ContextWindow` should consult `modelconfig.DefaultRegistry()` and fall back to a safe default (e.g. 8192).
- `Shutdown` returns `nil` for stateless HTTP backends.

### Step 2: Wire into the factory

Add your provider to the switch in `internal/backend/factory.go`:

```go
case "myprovider":
    return NewMyProviderBackend(resolvedAPIKey, model), nil
```

### Step 3: Add a config migration (if needed)

If the new provider requires a new config field, increment `currentConfigVersion` in `internal/config/config.go`, add the migration function to `configMigrations`, and implement `migrateVNtoVN+1`.

### Step 4: Write tests

Follow the pattern in `backend_test.go` — use `httptest.NewServer` to avoid hitting real APIs in unit tests.

---

## Adding a Skill

Skills live in `~/.huginn/skills/<skill-name>/skill.yaml`. You can also ship example skills in `embed/skills/` to be copied on first run.

### Skill YAML structure

```yaml
name: my-skill
description: "One sentence: what does this skill teach the agent?"
mode: prompt          # prompt | template | shell
system_prompt_fragment: |
  Always check for nil pointers before dereferencing. If you find one,
  add a guard clause rather than a nil check in a conditional.
```

**Mode: `prompt`** — `system_prompt_fragment` is appended verbatim to the system message.

**Mode: `template`** — same as `prompt` but the fragment is rendered as a Go `text/template` before injection. Use `{{ .WorkspaceRoot }}` or other variables provided by the loader.

**Mode: `shell`** — `command` field (instead of `system_prompt_fragment`) is executed; stdout is injected into context. Use this for dynamic context (e.g. running `go vet` and injecting the output).

### Loader behavior

`skills.Loader.LoadAll()` scans subdirectories of `skillsDir`. A missing `skillsDir` is not an error (returns empty slice). Invalid skill directories are skipped with a log warning so a bad skill never prevents startup.

---

## Code Conventions

### Error wrapping

Always wrap errors with context using `fmt.Errorf("package: operation: %w", err)`. The wrapping chain should read like a call stack:

```go
// Good
return fmt.Errorf("storage: get key %q: %w", key, err)

// Bad — no context, impossible to trace
return err
```

### Atomic file writes

Never write directly to the target path. Write to a `.tmp` sibling and `os.Rename` into place. A partial write on crash should never corrupt the original:

```go
tmp := path + ".tmp"
if err := os.WriteFile(tmp, data, 0o600); err != nil {
    return err
}
return os.Rename(tmp, path)
```

This pattern is used in `config.SaveTo`, `agents.SaveAgentsTo`, and `patch.Apply`. Follow it for any new file-writing code.

### Context propagation

Every function that performs I/O or calls an LLM must accept `context.Context` as its first argument and respect cancellation:

```go
func (b *MyBackend) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    // Pass ctx to all downstream calls
    resp, err := b.client.Do(req.WithContext(ctx))
    // ...
}
```

Do not store contexts in structs.

### Mutex naming

Each mutex should protect exactly one data structure and be named to make that obvious:

```go
type SkillRegistry struct {
    mu     sync.RWMutex  // protects skills
    skills []Skill
}
```

Use `sync.RWMutex` when reads dominate. Use `sync.Mutex` when writes are as frequent as reads. Document what each mutex protects with a comment on the field if it is not obvious from the field name.

### Sandbox checks

Every tool that accepts a file path must validate it against `SandboxRoot` using `sandboxCheck` (defined in `internal/tools/sandbox.go`) before any filesystem operation. This prevents path traversal attacks (`../../etc/passwd`).

### Permissions

Assign the most restrictive `PermissionLevel` that is correct:
- Use `PermRead` for any tool that only reads state and has no side effects.
- Use `PermWrite` for tools that modify files.
- Use `PermExec` for tools that run arbitrary processes (bash, run_tests).

Never downgrade a tool's permission level to avoid user prompts — that is a security regression.

---

## Commit Message Format

```
<type>: <short imperative summary under 72 chars>

<optional body: why, not what; wrap at 80 chars>

<optional footer: Closes #123, Breaking-Change: ...>
```

**Types:** `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `perf`

**Examples:**

```
feat: add shell mode to skills loader

Shell-mode skills execute a command at context-build time and inject
stdout into the system message. This enables dynamic context (e.g.
live linting output) without hardcoding it into the prompt.

test: add hardening tests for sandbox path traversal in grep tool

fix: prevent double-compact when context window is exactly at trigger

docs: add root README and CONTRIBUTING guide
```

Do not use past tense ("added", "fixed"). Use imperative ("add", "fix").

---

## Submitting a Pull Request

1. **Fork** the repository and create a feature branch from `main`.

2. **Write tests first** when fixing a bug. The test should fail before your fix and pass after.

3. **Run the full suite with the race detector** before opening a PR:
   ```bash
   go test -race ./...
   ```

4. **Check for lint issues:**
   ```bash
   go vet ./...
   ```

5. **Keep PRs focused.** One logical change per PR. If you are adding a backend and fixing a bug in the session store, open two PRs.

6. **Reference the relevant ADR** in your PR description if your change involves an architectural decision. If there is no existing ADR, consider writing one in `docs/decisions/`.

7. **Describe the "why"** in your PR body, not just the "what". The diff shows what changed; the description should explain why the change is correct.

8. The maintainers may request changes. Address feedback by pushing new commits — do not force-push a rewritten history to an open PR.

---

See the [root README](../README.md) for an overview of the full system, backend configuration, and CLI reference.
