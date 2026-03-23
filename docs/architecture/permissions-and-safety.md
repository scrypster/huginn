# Permissions and Safety

How Huginn controls what agents can do.

- See also: [session-and-memory.md](session-and-memory.md)

---

## The Problem

An agent with unrestricted file system and shell access is a liability. Without a
permission system, a misguided or adversarially prompted agent could:

- Destroy project files (`rm -rf`, overwrite without backup)
- Exfiltrate source code or credentials through network tools
- Execute arbitrary commands that escape the sandbox entirely (`curl | bash`,
  installing packages, spawning persistent processes)
- Silently clobber a file that the human developer edited concurrently, creating
  a lost-work scenario that is hard to detect after the fact

The permission system exists to make every destructive or irreversible action
visible and user-approved before it happens. It does not prevent agents from
being useful — read-only operations are never blocked.

---

## Three-Tier Permission Model

Every tool in the registry declares a `PermissionLevel`. The level encodes the
tool's maximum risk, not the risk of any particular call.

```
internal/tools/types.go

PermRead  = 0   // always allowed — cannot destroy state
PermWrite = 1   // requires approval — modifies files or creates side effects
PermExec  = 2   // requires approval — arbitrary code execution
```

### PermRead — always allowed

Tools: `read_file`, `list_dir`, `grep`, `search_files`, `git_status`,
`git_log`, `git_diff`, `git_blame`, `web_search`, `fetch_url`,
`memory_recall`, `find_definition`, `list_symbols`, `consult_agent`.

Read-only operations cannot destroy state. Requiring approval for every
`grep` or `read_file` call would make the agent unusable, and the information
hazard of reads is low compared to the operational risk of writes and execution.
No prompt is ever shown for PermRead tools.

### PermWrite — requires approval

Tools: `write_file`, `edit_file`, `git_branch`, `git_commit`, `git_stash`,
`gh_pr_create`, `gh_issue_create`, `memory_write`, `memory_decide`,
`memory_evolve`, `memory_write_batch`, `git_worktree_create`,
`git_worktree_remove`.

Write tools modify durable state. A single bad write can corrupt a file or
create an unintended commit. The agent must receive user approval (or a cached
session-level approval) before any PermWrite call executes.

### PermExec — requires approval

Tools: `bash`, `run_tests`.

Execution is the highest-risk tier. A bash command can do anything the OS
permits: delete arbitrary paths, open network connections, spawn background
processes. Approval is always required unless explicitly bypassed.

---

## Gate Type

`internal/permissions/permissions.go`

```go
type Gate struct {
    mu             sync.Mutex
    skipAll        bool
    sessionAllowed map[string]bool
    promptFunc     func(PermissionRequest) Decision
}
```

`skipAll` — set by the `--dangerously-skip-permissions` CLI flag. When true,
all tools at any level are allowed without any prompt. Intended for CI
pipelines and automation environments where the caller has already accepted
the risk.

`sessionAllowed` — a `map[string]bool` keyed by tool name. Entries are added
only when the user responds `AllowAll` to a prompt. The map grows at most one
entry per distinct tool name (naturally bounded by the number of registered
tools). See thread safety below.

`promptFunc` — a caller-supplied function that blocks until the user responds.
The TUI renders the request and returns one of `Allow`, `AllowOnce`, `AllowAll`,
or `Deny`. If `promptFunc` is nil the gate denies all non-read requests by
default — appropriate for headless contexts.

---

## Decision Flow

```
Gate.Check(req)
        |
        v
  req.Level == PermRead?
        |
       YES --> allow immediately (no lock, no prompt, zero overhead)
        |
       NO
        |
        v
  g.skipAll == true?
        |
       YES --> allow immediately
        |
       NO
        |
        v
  g.mu.Lock()
  g.sessionAllowed[req.ToolName]?
  g.mu.Unlock()
        |
       YES --> allow (cached session approval)
        |
       NO
        |
        v
  g.promptFunc == nil?
        |
       YES --> deny (headless / no prompt registered)
        |
       NO
        |
        v
  decision := g.promptFunc(req)   <-- blocks until user responds
        |
   +---------+-----------+--------+
   |         |           |        |
 Allow   AllowOnce    AllowAll   Deny
   |         |           |        |
 true      true     g.mu.Lock()  false
                    sessionAllowed[tool] = true
                    g.mu.Unlock()
                    true
```

---

## Session Allow-List

The `sessionAllowed` map caches `AllowAll` decisions for the duration of a
session. Key design properties:

- Keyed by **tool name** (e.g., `"bash"`, `"write_file"`), not by path or
  argument. Approving a tool approves all future calls to that tool.
- **Bounded** — the map can have at most one entry per registered tool name.
  Because the key space is finite and small (fewer than 30 built-in tools),
  the map never grows unboundedly.
- **Idempotent** — setting `sessionAllowed["bash"] = true` a second time is a
  no-op. No counter, no list, just a boolean.
- **Not persisted** — the allow-list is in-memory only. Each new Huginn
  process starts with an empty map. There is no way to carry session approvals
  across restarts; this is intentional.

---

## Patch Application

`internal/patch/patch.go`

The `patch` package handles the mechanics of applying a unified diff to a file
on disk. It is deliberately separate from the permissions package:

- **Permissions answer the question "should this happen?"**
- **Patch answers the question "how does it happen correctly?"**

### Diff Parsing

`Parse()` converts a unified diff string into a `[]Diff`, one per file. Each
`Diff` holds a file path, a slice of `Hunk`s, and an optional `ExpectedHash`.
The parser handles all standard unified diff hunk header variants:

```
@@ -a,b +c,d @@    (standard)
@@ -a,b +c @@      (new side = 1 line, count omitted)
@@ -a +c,d @@      (old side = 1 line, count omitted)
@@ -a +c @@        (both sides = 1 line)
```

### Hunk Verification

`applyHunks()` enforces two invariants before applying any hunk:

1. Hunks must be in ascending order of `OldStart`. Out-of-order or overlapping
   hunks are rejected with an error.
2. Context lines and delete lines are checked against the actual file position.
   If a context line is referenced past the end of the file the operation is
   rejected, preventing silent data corruption.

### ErrStaleFile

`Apply()` reads the file content once, computes its SHA-256, and compares
against `Diff.ExpectedHash` (if set) before writing anything. If the hash does
not match, `ErrStaleFile` is returned:

```go
type ErrStaleFile struct {
    Path     string
    Expected string
    Actual   string
}
```

**Why this exists:** When the agent plans a diff it captures the file's hash at
plan time. If the human developer edits the same file before the agent applies
the patch, the hashes diverge. Without this check, the agent would silently
clobber the developer's changes. `ErrStaleFile` surfaces the conflict so the
caller can re-plan with the current file state.

The read-then-hash-then-apply sequence is done on a single `os.ReadFile` call
to avoid a TOCTOU race between a separate validation read and the apply write.

After a successful apply, `Store.Invalidate()` is called to delete the file's
hash key from the Pebble store. This forces the next code-intelligence index
pass to re-read the file. If the store is nil (e.g., in tests), this step is
skipped non-fatally.

---

## FileLock

`internal/tools/filelock.go`

When swarm agents run in parallel they can target the same file concurrently.
`WriteFileTool` and `EditFileTool` share a single `FileLockManager` instance
(created in `RegisterBuiltins`) that provides per-path mutual exclusion.

```go
type FileLockManager struct {
    mu    sync.Mutex          // guards the locks map
    locks map[string]*pathLock
}

type pathLock struct {
    mu       sync.Mutex   // the per-path lock
    refcount int          // how many goroutines hold or are waiting for this lock
}
```

### Refcount Model

- `Lock(path)`: acquires `f.mu`, increments `pl.refcount`, releases `f.mu`,
  then blocks on `pl.mu`. The refcount increment inside `f.mu` ensures no
  concurrent `Unlock` can delete the map entry before the waiter acquires it.

- `Unlock(path)`: acquires `f.mu`, decrements `pl.refcount`, deletes the map
  entry if refcount reaches zero, **then calls `pl.mu.Unlock()` while still
  holding `f.mu`**.

### Correct Unlock Ordering

The inner `pl.mu.Unlock()` is called inside the `f.mu` critical section. This
is intentional and critical for correctness. The old ordering (unlock `f.mu`
first, then unlock `pl.mu`) had a window where a concurrent `Unlock` goroutine
could:

1. Acquire `f.mu`
2. Decrement `refcount` a second time (potentially to 0 or below)
3. Call `pl.mu.Unlock()` again — a double-unlock that panics at runtime

By holding `f.mu` across `pl.mu.Unlock()`, map state and mutex state are
always updated atomically.

An underflow guard (`if pl.refcount <= 0 { return }`) is a defensive backstop:
if `Unlock` is called more times than `Lock` the guard prevents the cleanup
condition from never firing and leaking the map entry.

---

## Why This Design?

### Why per-tool allow-list instead of per-path or per-operation?

Tools are the natural unit of capability. A developer who approves `bash` once
and selects "Always allow for this session" understands they are authorizing
shell execution broadly — not just for one command. If the allow-list were
keyed by path or by the exact argument, every `bash` call with a slightly
different command would re-prompt, which would be intolerable noise in practice.

The trade-off is that a session-level `bash` approval does not prevent the
agent from running different commands than the one originally approved. See
**Limitations**.

### Why `--dangerously-skip-permissions` rather than simply no permission system?

The flag makes the bypass explicit and visible. In CI pipelines, automated
test runners, or headless scripting environments, blocking for user input is
impossible. The flag allows full bypass while the word "dangerously" in the
flag name signals to operators that they are accepting risk. A system with no
permission concept at all would offer no protection in normal interactive use.

### Why ErrStaleFile on hash mismatch?

Silently clobbering concurrent edits is the worst possible outcome for a
coding tool. A developer can miss that their work was overwritten until much
later, when the damage may be compounded by additional edits or a commit. By
failing loudly at apply time, Huginn forces a re-plan cycle using the current
file state. The cost is one extra round-trip; the benefit is never silently
losing work.

---

## Thread Safety

| Shared state | Protected by | Notes |
|---|---|---|
| `Gate.sessionAllowed` | `Gate.mu` | Locked for read and write; `PermRead` path takes no lock |
| `FileLockManager.locks` | `FileLockManager.mu` | Held across `pl.mu.Unlock()` to prevent double-unlock |
| `pathLock.refcount` | `FileLockManager.mu` | Modified only while outer lock is held |

`Gate.skipAll` and `Gate.promptFunc` are set at construction time and never
mutated, so they require no lock.

---

## Limitations

**No path-based allowlisting.** When a user approves `write_file` for all
session calls, that approval covers any path within the sandbox, not just the
path in the original request. There is no way to say "always allow writes to
`src/` but prompt for writes to `config/`."

**No time-limited permissions.** Session approvals last for the entire process
lifetime. There is no expiry, no idle timeout, and no revocation mechanism
short of restarting Huginn.

**No argument-level inspection for cached approvals.** Once `bash` is cached
as allowed, the gate does not re-inspect the command. The promptFunc receives
the full `PermissionRequest` (including args and a human-readable summary) at
the time of the first prompt, but subsequent calls skip the prompt entirely.

**ErrStaleFile does not retry automatically.** The caller receives the error
and must re-plan. There is no automatic re-diff-and-apply loop.

---

## Agent Toolbelt

The three-tier permission model described above controls what any tool can do
in terms of read, write, and exec risk. The agent toolbelt adds a fourth layer
that controls **which providers each agent is allowed to use at all**.

Each agent carries a `Toolbelt []ToolbeltEntry` that lists the connections it
may access. At session start, the tool registry filters the schemas sent to the
LLM to include only tools from providers in that list — the model never learns
that other providers exist. At call time, a provider tag check enforces the
same constraint at the execution layer. Each toolbelt entry can also set
`approval_gate: true`, which forces a user prompt before any write or exec
operation against that provider, even when `--dangerously-skip-permissions` is
active.

An empty toolbelt imposes no restriction and is the backward-compatible default.

See [agent-toolbelt.md](agent-toolbelt.md) for the full data model, enforcement
flow, approval gate behavior, and threat model.

---

## See Also

- [session-and-memory.md](session-and-memory.md) — session state, JSONL
  message store, and MuninnDB memory integration
- [agent-toolbelt.md](agent-toolbelt.md) — per-agent connection scoping and
  approval gate enforcement
