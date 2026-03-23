# Streaming and Runtime — Architecture Deep Dive

> **Audiences:** (1) Users who want to understand what Huginn is doing under the hood. (2) Contributors modifying streaming or subprocess management. (3) Curious readers who want to understand the design decisions.

---

## Overview

Huginn is built around two engineering challenges that are easy to underestimate:

1. **LLM responses arrive as a stream of tokens**, not a single JSON payload. The TUI must stay responsive and renderable while tokens arrive — but Bubble Tea's update model is single-threaded and message-driven. Bridging these two worlds without coupling them is the job of `internal/streaming`.

2. **The local inference engine (llama-server) is a separate OS process** that must be downloaded, extracted, started, health-checked, and shut down correctly across every supported platform. The job of `internal/runtime` is to own that subprocess lifecycle so the rest of Huginn never thinks about it.

These two packages are small by line count but critical by function. Every response the user sees went through `streaming`. Every local model inference went through `runtime`.

---

## Part 1: The `streaming` Package

### Why it exists

Bubble Tea (the TUI framework Huginn uses) is a functional reactive UI loop: you give it a `Model`, it calls `Update(msg)` on your model for each event, and renders. It is single-threaded by design.

LLM inference is the opposite: a goroutine blocks on an HTTP response body, and tokens arrive over time.

The naive approach — call the backend directly inside a `tea.Cmd` — works but has two problems:

- **Testability**: `tea.Cmd` functions are opaque closures. You cannot unit-test that tokens arrive, errors propagate, or panics are caught without spinning up a Bubble Tea program.
- **Coupling**: the streaming infrastructure becomes entangled with TUI message types.

`internal/streaming` solves both by providing a pure-Go goroutine-and-channel runner that has **zero Bubble Tea dependencies**. The TUI wires it up, but the runner itself does not know about `tea.Msg`, Bubble Tea programs, or UI state. This makes the streaming layer independently testable and reusable.

### The `Runner` type

```go
// internal/streaming/runner.go

type WorkFn func(emit func(string)) error

type Runner struct {
    tokenCh chan string  // buffered 256
    errCh   chan error   // buffered 1
}
```

`NewRunner()` allocates a `Runner` with a 256-token buffer on `tokenCh` and a 1-slot buffer on `errCh`. The buffer size matters: it lets the goroutine run ahead of the consumer (the TUI's `Update` loop) without blocking. If the TUI is slow to process tokens, the producer backs up gracefully.

`Start(ctx, fn)` launches `fn` in a goroutine. `fn` receives an `emit` callback — every call to `emit(token)` sends a token to `tokenCh` (or drops it if the context is cancelled):

```go
func (r *Runner) Start(ctx context.Context, fn WorkFn) {
    go func() {
        defer func() {
            if rec := recover(); rec != nil {
                close(r.tokenCh)
                r.errCh <- fmt.Errorf("internal panic: %v", rec)
                return
            }
        }()
        err := fn(func(token string) {
            select {
            case r.tokenCh <- token:
            case <-ctx.Done():
            }
        })
        close(r.tokenCh)
        r.errCh <- err
    }()
}
```

**Key guarantees:**
- `tokenCh` is **always closed** before `errCh` receives — even on panic. The consumer can `range r.TokenCh()` safely.
- Panics inside `fn` are caught and converted to errors. The TUI never sees a crash from a bad backend implementation.
- Context cancellation (e.g. Ctrl+C) stops token delivery without killing the goroutine — `fn` itself must check `ctx.Done()` if it wants to terminate early. The `emit` callback silently drops tokens after cancellation.

### How the TUI uses it

The TUI (`internal/tui/app.go`) holds a `*streaming.Runner` field called `runner`. When the user sends a message, the TUI launches:

```go
r := streaming.NewRunner()
a.runner = r
r.Start(ctx, func(emit func(string)) error {
    return a.orch.Chat(ctx, userMsg, emit, nil)
})
```

It then returns a `tea.Cmd` that reads from `r.TokenCh()`. Each token becomes a `tokenMsg` dispatched through `Update()`. When `tokenCh` is closed, the command reads from `r.ErrCh()` and dispatches `streamDoneMsg{err}`.

This is the critical boundary: the goroutine (LLM call) and the event loop (TUI) are decoupled by channels. The TUI never blocks on the LLM.

### Streaming pipeline: end to end

```
User presses Enter
        |
        v
  App.Update() (tea.KeyMsg)
        |
        +---> streaming.NewRunner()
        |         |
        |         v
        |     goroutine: orch.Chat(ctx, msg, emit, onEvent)
        |         |
        |         v
        |     backend.ChatCompletion(ctx, req)
        |         |
        |         v
        |     HTTP SSE stream from LLM endpoint
        |         |
        |    [token arrives in SSE chunk]
        |         |
        |         v
        |     req.OnToken("token text")  <-- emit callback
        |         |
        |         v
        |     tokenCh <- "token text"   (buffered, cap 256)
        |
        +---> tea.Cmd reads tokenCh
                  |
                  v
            tokenMsg("token text")
                  |
                  v
          App.Update() dispatches tokenMsg
                  |
                  v
         a.streaming.WriteString(token)
                  |
                  v
         App.View() renders live content
                  |
                  v
           Terminal renders frame
```

When the SSE stream ends:
```
LLM sends [DONE]
        |
        v
OnEvent called with StreamDone
        |
        v
fn returns nil (or error)
        |
        v
close(tokenCh)        <-- signals consumer to stop ranging
errCh <- nil          <-- or the error
        |
        v
tea.Cmd reads ErrCh, dispatches streamDoneMsg
        |
        v
App.Update() finalizes the message in history
```

### The richer event model (`OnEvent`)

The original design used only `OnToken func(string)`. Version 3 added `OnEvent func(backend.StreamEvent)` to the `ChatRequest` type:

```go
type StreamEventType string

const (
    StreamText    StreamEventType = "text"
    StreamThought StreamEventType = "thought" // Anthropic extended thinking
    StreamDone    StreamEventType = "done"
)

type StreamEvent struct {
    Type    StreamEventType
    Content string
}
```

The TUI converts events to typed messages:

```go
func streamEventToMsg(e backend.StreamEvent) tea.Msg {
    switch e.Type {
    case backend.StreamThought:
        return thinkingTokenMsg(e.Content)
    case backend.StreamDone:
        return streamDoneMsg{}
    default:
        return tokenMsg(e.Content)
    }
}
```

`thinkingTokenMsg` is a distinct Go type from `tokenMsg`. The TUI's `Update` switch dispatches them to different rendering paths: regular tokens go to `a.streaming` and are committed to history; thinking tokens go to `a.thoughtStreaming` and are displayed in muted gray but discarded when the stream ends. Anthropic's "extended thinking" is surfaced this way.

**Backward compatibility:** `OnToken` is always called alongside `OnEvent` if both are set. Existing code that only provides `OnToken` continues to work.

### Two streaming patterns in the TUI

The TUI actually uses two different patterns for streaming, depending on the call path:

**Pattern 1: Runner-based (most operations)**

Used by `streamChat`, `streamPlan`, `streamIterate`. The `Runner` manages the goroutine; a `tea.Cmd` ranges `TokenCh()`.

**Pattern 2: Raw-channel streaming (agent loop)**

Used by `streamAgentChat` and `streamDispatch`. The agent loop (tool-calling) is more complex: it runs multiple LLM turns, executes tools between turns, and fires callbacks for tool start/done. The TUI creates a raw `chan string` (aliased as `tokenCh`) and an `errCh`, and launches the orchestrator in a separate goroutine. Events (tokens, tool-call, tool-done) are sent as `tea.Msg` values directly to an `eventCh chan tea.Msg`.

The distinction reflects a real difference: the `Runner` is clean for single-pass streaming; the raw channel pattern accommodates the multi-turn, multi-event agent loop where the simple `range TokenCh()` idiom is too narrow.

### Test coverage

`internal/streaming/runner_test.go` covers:

| Test | What it verifies |
|---|---|
| `TestRunner_TokenAccumulation` | All tokens arrive and can be joined |
| `TestRunner_ErrorPropagation` | Error from `fn` reaches `ErrCh` |
| `TestRunner_Cancellation` | Context cancel stops delivery cleanly |
| `TestRunner_PanicRecovery` | Panic inside `fn` becomes error, never crashes |
| `TestRunner_EmptyStream` | Zero-token stream closes cleanly |

These tests run without Bubble Tea, without a network, without any mock infrastructure. The `WorkFn` interface is narrow enough that tests are trivial to write.

### Known limitations

- **No backpressure signal**: if the TUI falls catastrophically behind (e.g. a blocking render), the token buffer fills (256 slots) and the goroutine blocks inside `emit`. This is unlikely in practice but worth noting.
- **One Runner per stream**: `Runner` is not reusable. Create a new one for each streaming operation.
- **No token counting in the Runner**: token accumulation for stats happens in the orchestrator layer, not here. The `Runner` is a pure conduit.
- **`OnToken` vs `OnEvent` are additive not exclusive**: if the backend fires `OnEvent`, it also fires `OnToken` for `StreamText` events. Callers that provide both get double delivery. This is a backward-compat trade-off.

---

## Part 2: The `runtime` Package

### Why it exists

Huginn's default mode manages a `llama-server` subprocess (the llama.cpp HTTP inference server). This is a deliberate architectural choice — see the design rationale first.

### The fundamental choice: subprocess over CGo

The alternatives were:

1. **CGo embedding of llama.cpp**: link llama.cpp into the Go binary directly.
2. **Ollama dependency**: use the Ollama daemon as the inference engine.
3. **Managed subprocess**: download and manage `llama-server` as a separate process.

CGo was rejected because it requires platform-specific build toolchains (CUDA toolkit on Linux, MSVC on Windows), breaks `go install`, increases compile time dramatically, and creates a maintenance burden tracking llama.cpp's C API. The Go binary would no longer be a self-contained artifact.

The Ollama dependency was removed (it was the original v1 design) because it requires users to install and run a daemon separately — defeating the "single binary, zero external tools" goal. It also imposed the Ollama API contract on all of Huginn's internals.

The managed subprocess approach keeps the Go binary pure Go. Hardware optimization (Metal on macOS, CUDA on Linux) is delegated to llama.cpp's already-optimized `llama-server`. `go install` works. The binary is small. New llama.cpp versions are picked up by updating `runtime.json` — no Go rebuild needed.

### Component relationships

```
internal/runtime/
    platform.go    -- Detect() OS/arch/GPU, produce platform key
    manifest.go    -- Load embedded runtime.json, look up binary by platform key
    server.go      -- Manager: download, extract, Start, WaitForReady, Shutdown
```

```
embed/
    runtime.json   -- version-pinned per-platform download URLs + SHA256 + archive metadata
```

```
~/.huginn/bin/
    llama-server-<version>/
        llama-server  -- extracted binary, chmod 0755
```

### Platform detection

```go
// internal/runtime/platform.go

type Platform struct {
    OS   string // "darwin", "linux", "windows"
    Arch string // "arm64", "amd64"
    CUDA bool   // true if nvidia-smi found on linux
}

func Detect() Platform {
    p := Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
    if p.OS == "linux" {
        if _, err := exec.LookPath("nvidia-smi"); err == nil {
            p.CUDA = true
        }
    }
    return p
}

func (p Platform) Key() string {
    key := p.OS + "-" + p.Arch
    if p.CUDA { key += "-cuda" }
    return key
}
```

CUDA detection uses `nvidia-smi` presence rather than the CUDA SDK because `nvidia-smi` ships with the driver (not the toolkit), is present on any GPU-enabled Linux system, and is a reliable proxy. macOS Metal is implicit — the `darwin-arm64` binary from llama.cpp has Metal acceleration built in. Windows has no GPU variant in the current manifest (CUDA on Windows requires the `llama-b*-bin-win-cuda*` builds; this is a known gap, tracked as a limitation).

Platform keys produced by `Detect().Key()`:

| Platform | Key |
|---|---|
| macOS Apple Silicon | `darwin-arm64` |
| macOS Intel | `darwin-amd64` |
| Linux x86, no GPU | `linux-amd64` |
| Linux x86, NVIDIA GPU | `linux-amd64-cuda` |
| Windows x86 | `windows-amd64` |

### The runtime manifest

`embed/runtime.json` is embedded into the binary at compile time via `//go:embed runtime.json`. It is parsed once at startup:

```go
//go:embed runtime.json
var runtimeJSON []byte

type BinaryEntry struct {
    URL         string `json:"url"`
    SHA256      string `json:"sha256"`
    ExtractPath string `json:"extract_path"`
    ArchiveType string `json:"archive_type"` // "zip" or "tar.gz"
}

type RuntimeManifest struct {
    Version            int                    `json:"huginn_runtime_version"`
    LlamaServerVersion string                 `json:"llama_server_version"`
    Binaries           map[string]BinaryEntry `json:"binaries"`
}
```

The version pin means all users on the same Huginn release run the identical llama-server binary. Breaking llama.cpp API changes are absorbed by updating `runtime.json` with the new version and testing before shipping.

`ExtractPath` is the path of the target binary inside the archive (e.g. `llama-b8192/llama-server` inside a tar.gz). Archive formats vary by platform: macOS and Windows ship zip; Linux ships tar.gz.

### The Manager lifecycle

```
NewManager(huginnDir)
    ├── LoadManifest()       -- parse runtime.json
    ├── Detect()             -- get platform key
    └── returns *Manager

IsInstalled()                -- os.Stat(BinaryPath())
    └── false → call Download(ctx, onProgress)
                    ├── BinaryForPlatform(key)
                    ├── downloadFile(ctx, url, archivePath, onProgress)
                    ├── extractZip / extractTarGz → BinaryPath()
                    └── os.Chmod(BinaryPath(), 0755)

Start(modelPath, port)       -- exec.Command(BinaryPath(), --model, --port, --ctx-size, --parallel)
    └── cmd.Start()          -- non-blocking; process runs in background

WaitForReady(ctx)            -- polls http://localhost:{port}/health every 300ms
    ├── 30s global timeout
    ├── fast-fail if cmd.ProcessState != nil (process already exited)
    └── returns nil on first HTTP 200

Shutdown()                   -- SIGINT → 5s wait → SIGKILL fallback
```

**Binary path convention**: `~/.huginn/bin/llama-server-{version}/llama-server`. The version is in the path so multiple Huginn versions can coexist without collisions.

**Port allocation**: `FindFreePort()` binds `127.0.0.1:0`, reads the assigned port from `l.Addr().(*net.TCPAddr).Port`, then closes the listener. This is always loopback — binding to `0.0.0.0:0` would expose the ephemeral bind to all interfaces. There is a small TOCTOU window between `l.Close()` and `llama-server` binding the port, but this is unavoidable without `SO_REUSEPORT` mechanics. In practice it is not a problem.

### Health check and fast-fail

`WaitForReady` uses two mechanisms to avoid the full 30-second wait:

1. **`cmd.ProcessState != nil`** — set by the OS when a process exits. If the server crashes at startup (bad model file, missing CUDA library), this is detected within 300ms rather than waiting 30 seconds.
2. **HTTP 200 from `/health`** — when the server is up and model is loaded.

```
Timeline for a normal startup:
  t=0ms   cmd.Start()
  t=300ms first health poll → HTTP connection refused (server not up yet)
  t=600ms second poll → HTTP connection refused
  t=2000ms server binds port, model loaded
  t=2300ms third poll → HTTP 200 → WaitForReady returns nil

Timeline for a crash (bad model file):
  t=0ms   cmd.Start()
  t=50ms  llama-server exits with error
  t=300ms first poll → cmd.ProcessState != nil → return error immediately
```

### Shutdown sequence

```go
func (m *Manager) Shutdown() error {
    if m.cmd == nil || m.cmd.Process == nil {
        return nil
    }
    _ = m.cmd.Process.Signal(os.Interrupt)   // SIGINT → graceful exit
    done := make(chan error, 1)
    go func() { done <- m.cmd.Wait() }()
    select {
    case <-done:
        return nil
    case <-time.After(5 * time.Second):
        return m.cmd.Process.Kill()           // SIGKILL if graceful fails
    }
}
```

SIGINT is preferred because llama-server handles it gracefully (flushes in-flight requests). SIGKILL is the fallback for hung processes. The 5-second window is long enough for a typical inference call to complete but short enough that Huginn's shutdown does not feel frozen.

### Archive extraction

Both `extractZip` and `extractTarGz` use an exact-match on `ExtractPath` — they do not extract the entire archive. This keeps the extraction fast (no wasted I/O for large archives) and avoids path traversal issues (only the declared path is written to disk).

```
extractZip(archivePath, extractPath, finalPath)
    ├── zip.OpenReader(archivePath)
    ├── range r.File — find f.Name == extractPath
    └── io.Copy to finalPath

extractTarGz(archivePath, extractPath, finalPath)
    ├── gzip.NewReader
    ├── tar.NewReader
    ├── tr.Next() loop — find hdr.Name == extractPath
    └── io.Copy to finalPath
```

If `extractPath` is not found in the archive, both functions return an error with "not found in archive". This guards against manifest mismatches (e.g. a llama.cpp release that reorganized its archive structure).

### Test coverage

`internal/runtime/` has three test files covering distinct areas:

**`manifest_test.go`** — pure unit tests, no network, no subprocess:
- `TestLoadManifest` — manifest parses without error, version == 1
- `TestBinaryForPlatform_Found` / `_NotFound` / `_EmptyKey` — lookup logic
- `TestLoadManifest_AllPlatformsHaveNonEmptyURLs` — structural validation of the manifest content
- `TestLoadManifest_ArchiveTypesAreKnown` — only "zip" and "tar.gz" appear
- Platform key formatting tests for all supported OS/arch/CUDA combinations

**`server_test.go`** — integration-adjacent tests using temp dirs and `exec.Command`:
- `TestManager_IsInstalled_false` / `_true` — filesystem checks
- `TestWaitForReady_ProcessDies` — runs a real `sh -c 'exit 1'` and verifies fast-fail
- `TestManager_Endpoint` — URL construction

**`server_extra_test.go`** — hardening tests using `httptest.Server`:
- `TestDownloadFile_Success` / `_CancelledContext` / `_InvalidURL` — download path coverage
- `TestExtractZip_Success` / `_MissingPath` / `_CorruptedZip` — archive extraction edge cases
- `TestExtractTarGz_Success` / `_MissingPath` / `_CorruptedInput` / `_CorruptedGzipValidHeader` — additional tar.gz edge cases
- `TestShutdown_NilManager_NoOp` / `_AlreadyExitedProcess` — shutdown idempotency

**`port_test.go`**:
- `TestFindFreePort_ReturnsValidPort` — port in range 1024–65535
- `TestFindFreePort_PortIsActuallyFree` — can bind immediately after

### Known limitations

- **No SHA256 verification**: the manifest has a `SHA256` field but `Download()` does not verify it after extraction. This is a documented gap — the manifest contains the hash, the verification code is not yet implemented.
- **Windows GPU binaries**: no `windows-amd64-cuda` entry in `runtime.json`. CUDA on Windows requires different llama.cpp builds; support is deferred.
- **Single model slot**: `Start()` passes `--ctx-size 4096 --parallel 1`. The context size is not configurable per-model. A future version should pass `--ctx-size` from model metadata.
- **TOCTOU on port**: `FindFreePort()` has a small race between `l.Close()` and the subprocess binding. In practice this is harmless but not zero-risk.
- **No `nvidia-smi` on Linux with AMD GPU**: `Detect()` only checks for `nvidia-smi`. ROCm/AMD GPU support is not present.
- **Single backend per Manager**: each `Manager` manages exactly one `llama-server`. Running two models simultaneously (e.g. planner and coder on different models) would require two `Manager` instances. This is not currently wired in `main.go`.

---

## Part 3: Context Compaction (`internal/compact`)

Context compaction is not in `streaming` or `runtime`, but it is a direct consequence of streaming: as conversations grow across many agent turns, the message history accumulates. Left unchecked, it overflows the LLM's context window, causing `HTTP 400` errors or silently truncated responses. This section covers how Huginn handles this.

### Why compaction exists

LLMs have fixed context windows (e.g. 32,768 tokens for a 7B local model, 200,000 for Claude). Long agentic sessions with many tool calls (read_file, write_file, bash) accumulate rapidly. The old approach — `trimHistory()` dropping the oldest messages — was blunt: it discarded potentially critical context (file paths written, decisions made, errors resolved).

Compaction replaces trimming with summarization: compress the history while preserving high-value information.

### Token estimation

```go
// EstimateTokens counts using cl100k_base tiktoken encoding (embedded BPE data).
// Falls back to len/4 if the tokenizer is unavailable.
func EstimateTokens(messages []backend.Message) int
```

The tokenizer is initialized once via `sync.Once` from an embedded `cl100k_base.tiktoken` BPE file. This avoids a CDN fetch on first use (the original tiktoken-go behavior). The `cl100k_base` encoding is used for all models — it is exact for GPT-4/OpenAI models, ~10-15% off for Claude (which uses a different tokenizer), and an approximation for local models. The error is documented and acceptable for the use case (trigger threshold at 70% provides headroom).

### Compaction trigger

`ShouldCompact(messages, budgetTokens)` returns true when `EstimateTokens(messages) / budgetTokens >= trigger`. Default trigger: 0.70. The budget comes from `backend.ContextWindow()` — the actual context window of the current model. This ensures compaction triggers relative to the actual model limit, not a global constant.

### Two compaction strategies

**`ExtractiveStrategy`** (safe fallback, no LLM call):

```
Compact(messages, budget, nil, "")
    ├── extractFilePaths(messages)   -- all unique file_path/path args from tool calls
    ├── lastNExchanges(messages, 2)  -- last 2 user/assistant exchanges verbatim
    └── return [summary_message] + tail
```

The summary message starts with `## Summary` and lists all file paths touched. The tail preserves the 2 most recent exchanges so the model has immediate context. This strategy never fails and never makes an LLM call.

**`LLMStrategy`** (semantic summary with fallback):

```
Compact(messages, budget, backend, model)
    ├── buildConvText(messages)
    ├── callLLM(ctx_with_30s_timeout, backend, model, summaryPrompt + conv)
    │   ├── success + "## Summary" in response → use it
    │   ├── malformed response → retry once
    │   └── retry fails → fall back to ExtractiveStrategy
    └── budget check: if compacted > budget → fall back to ExtractiveStrategy
```

The 30-second timeout on the LLM call (`llmCompactionTimeout`) prevents a hung backend from blocking the agent loop. On timeout, the fallback fires silently. The user sees slightly less context quality but the session continues.

### Compaction flow in the Orchestrator

After every LLM call that appends to history (Chat, Plan, Implement, AgentChat, CodeWithAgent), the Orchestrator calls `compactHistory(ctx)`:

```go
func (o *Orchestrator) compactHistory(ctx context.Context) {
    o.mu.Lock()
    snapshot := make([]backend.Message, len(o.history))
    copy(snapshot, o.history)
    modelName := o.models.Get(modelconfig.SlotCoder)
    comp := o.compactor
    o.mu.Unlock()

    // MaybeCompact is called WITHOUT the lock held.
    // This prevents the mutex from being held during a potentially long LLM call.
    newHistory, wasCompacted, _ := comp.MaybeCompact(ctx, snapshot, o.backend, modelName)
    if wasCompacted {
        o.mu.Lock()
        o.history = newHistory
        o.mu.Unlock()
        o.sc.Record("agent.compaction_triggered", 1)
    }
}
```

The lock release before `MaybeCompact` is intentional and critical: `LLMStrategy.Compact` may make an LLM call (30s timeout). Holding the mutex during that call would block all other Orchestrator methods (state queries, tool execution callbacks) for the duration. The snapshot-copy pattern is the safe alternative.

### Compaction modes

| Mode | Behavior |
|---|---|
| `auto` | Compact when `EstimateTokens / budget >= trigger` |
| `never` | Never compact; history grows unbounded |
| `always` | Compact after every turn (for testing; use `ExtractiveStrategy`) |

Default from config: `auto`, trigger 0.70.

---

## Part 4: How Runtime Connects to Streaming

The `runtime` and `streaming` packages do not directly reference each other. They connect through the `Backend` interface:

```
runtime.Manager.Start()
        |
        v
llama-server process (localhost:{port})
        |
        v
backend.ManagedBackend (embeds ExternalBackend, points at localhost:{port})
        |
        v
backend.Backend interface
        |
        v
agent.Orchestrator (calls ChatCompletion with OnToken/OnEvent callbacks)
        |
        v
streaming.Runner (goroutine + channels)
        |
        v
TUI App (tokenMsg / thinkingTokenMsg / streamDoneMsg)
```

`ManagedBackend` wraps `ExternalBackend` with a custom `Shutdown` hook that calls `Manager.Shutdown()`. For the rest of Huginn, `ManagedBackend` and `ExternalBackend` (pointing at Ollama/OpenAI/etc.) are identical.

---

## Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Zero Bubble Tea imports in `streaming` | Yes | Enables unit testing without TUI; keeps the layer reusable |
| 256-token buffer on tokenCh | Yes | Allows producer to run ahead; prevents blocking on slow renders |
| Panic recovery in Runner | Yes | Malformed backend responses that panic do not crash the TUI |
| Subprocess over CGo | Subprocess | Pure Go binary; no build toolchain requirements; `go install` works |
| Embedded runtime manifest | `//go:embed` | Version pinning at compile time; offline operation after install |
| Exact-path archive extraction | Yes | Fast; avoids path traversal; explicit about what gets written |
| Release-lock before LLM compaction | Yes | Prevents mutex starvation during 30s LLM timeout |
| Tiktoken cl100k_base for all models | Yes | Embedded BPE avoids CDN fetch; known ~10-15% error for Claude documented |
| Extractive fallback in LLMStrategy | Yes | Compaction never fails; session continues even if LLM summarization is broken |
| `OnToken` + `OnEvent` coexist | Yes | Backward compat; existing code continues working; richer streaming opt-in |
