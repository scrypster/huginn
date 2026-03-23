# Storage Architecture: Pebble KV Backend

## Why Pebble

Huginn is a local developer tool. It runs on a single machine, operates against a local Git repository, and needs to survive crashes without corrupting data. The storage requirements are modest in scope but demanding in reliability: key lookups must be fast, writes must be durable, and the data model must map cleanly to file-path-keyed records without a schema migration story.

Given those constraints, the choice was between four candidates:

| Option | Why rejected |
|--------|-------------|
| SQLite | Requires schema design and migrations. Good for relational data; overkill for keyed blobs. CGo dependency adds friction for a pure-Go binary. |
| BoltDB | Read-only transactions block writers. B-tree page layout struggles with prefix-scan-heavy workloads. Unmaintained. |
| BadgerDB | LSM-tree like Pebble, but significantly higher memory overhead and more complex value-log GC story. |
| In-memory only | Dies on process exit. Huginn's whole value proposition is incremental indexing — re-indexing a large codebase on every startup is unacceptable. |

**Pebble** (`github.com/cockroachdb/pebble/v2 v2.1.4`) wins because:

- Pure Go. No CGo, no C libraries to cross-compile or keep in sync.
- LSM-tree with bloom filters: point lookups and bounded prefix scans are both fast. Huginn uses both patterns heavily.
- Write-ahead log provides crash recovery without any application-level effort.
- Batched writes with `Sync: true` give per-operation durability guarantees where needed.
- Zero schema. Huginn uses a string key space partitioned by prefix, which maps directly to how Pebble works.
- CockroachDB uses it in production. The reliability bar is high.

The trade-off is honest: Pebble is single-process, single-machine, no replication. See the Limitations section.

---

## Physical Layout

There is one Pebble database per huginn workspace. It is opened by `internal/storage/store.go`:

```go
dbPath := filepath.Join(dir, "huginn.pebble")
db, err := pebble.Open(dbPath, &pebble.Options{})
```

`dir` is the workspace cache directory passed in at startup (typically `~/.huginn/cache/<workspace-hash>/`). The `huginn.pebble` subdirectory contains Pebble's standard SSTable layout: `MANIFEST`, `OPTIONS`, numbered `.sst` files, and a `WAL` directory.

The database is opened once per process. All subsystems share the single `*pebble.DB` handle. Subsystems that need direct access (the radar package, the agents package) receive the handle via `store.DB()`. Higher-level subsystems use the typed methods on `Store`.

---

## Key Space Layout

Huginn partitions the key space by prefix. All keys are plain UTF-8 strings encoded as `[]byte`. There is no binary framing, no length prefixes, no version bytes — just readable string keys.

```
huginn.pebble key space
│
├── meta:                           (global metadata)
│   ├── meta:git_head               current HEAD SHA (string)
│   └── meta:workspace_id           workspace identity (string)
│
├── file:                           (per-file intelligence, storage package)
│   └── file:<path>:
│       ├── file:<path>:hash        SHA-256 hex of file content
│       ├── file:<path>:parser_version  int, parser schema version
│       ├── file:<path>:symbols     JSON []Symbol
│       ├── file:<path>:chunks      JSON []FileChunk
│       └── file:<path>:indexed_at  RFC3339 timestamp
│
├── edge:                           (file dependency graph, storage package)
│   └── edge:<from>:<to>            JSON Edge (from, to, symbol, confidence, kind)
│
├── ws:                             (workspace-level summaries)
│   └── ws:summary                  JSON WorkspaceSummary
│
├── repo/<repoID>/snap/<sha>/       (radar snapshot data, radar package)
│   ├── imp/<path>                  JSON ImportRecord {imports, importedBy}
│   ├── fanin/<path>                JSON int (fan-in count cache)
│   └── edge/<from>\x00<to>         presence key (no value, key encodes edge)
│
├── repo/<repoID>/baseline/<branch>/  (drift detection, radar package)
│   ├── graph                       JSON BaselineGraph {edges map}
│   └── policy                      JSON BaselinePolicy {forbiddenEdges, layerRules}
│
├── repo/<repoID>/radar/ack/        (finding acknowledgment, radar package)
│   └── repo/<repoID>/radar/ack/<findingID>  JSON int64 unix timestamp
│
├── agent:summary:                  (agent session memory, agents package)
│   └── agent:summary:<machineID>:<agentName>:<sessionID>  JSON SessionSummary
│
├── agent:delegation:               (agent-to-agent consultation log, agents package)
│   └── agent:delegation:<machineID>:<from>:<to>:<unix-nano-20digits>  JSON DelegationEntry
│
└── stats:tokens:<date>             (planned: cost tracking, v3 design)
```

### Separator choice in snap edge keys

The `repo/.../snap/.../edge/` keys use a null byte (`\x00`) to separate the `from` and `to` path segments:

```
repo/{repoID}/snap/{sha}/edge/{from}\x00{to}
```

File paths can contain forward slashes but cannot contain null bytes. Using `/` as a separator caused ambiguity when `from` itself contained slashes (e.g., `cmd/main.go`). The null byte separator eliminates that ambiguity without any escaping. This is documented inline in `drift.go`:

```go
const edgeSep = "\x00"
```

### Prefix scan pattern

Every subsystem that scans a collection uses the same pattern:

```go
prefix := []byte("some:prefix:")
iter, _ := db.NewIter(&pebble.IterOptions{
    LowerBound: prefix,
    UpperBound: incrementLastByte(prefix),
})
```

`incrementLastByte` adds 1 to the last byte of the prefix (with carry) to produce an exclusive upper bound. This is the standard Pebble prefix-scan idiom. All three packages that use Pebble directly implement this helper independently — a minor redundancy worth consolidating in a future refactor.

---

## Subsystem Deep-Dives

### 1. File Intelligence Store (`internal/storage`)

This is the core indexed representation of the codebase. It answers the question: "what do we know about this file?"

**FileRecord** — the fundamental unit. Stored as three separate keys (not one JSON blob) so that hash invalidation does not touch symbols or chunks:

```go
type FileRecord struct {
    Path          string
    Hash          string    // SHA-256 hex — primary cache key
    ParserVersion int       // bumped when parser logic changes, forces re-index
    IndexedAt     time.Time
}
```

The hash is the invalidation mechanism. Before re-indexing a file, huginn reads `file:<path>:hash` and compares it to the current file content hash. If they match and the parser version matches, the existing symbols and chunks are used as-is. This makes incremental indexing fast even for large repositories.

**Why separate keys for hash, symbols, chunks?** Invalidation only needs to delete the hash key. The patch layer calls `store.Invalidate(paths)` after applying diffs — it only deletes `file:<path>:hash` for each changed path. On the next context build, the hash mismatch triggers a re-index, which overwrites all keys atomically via a batch. Deleting only the hash is a single `batch.Delete` per path rather than five.

**Symbols** — extracted code symbols (functions, classes, imports, exports) stored as a JSON array:

```go
type Symbol struct {
    Name     string // e.g., "HandleRequest"
    Kind     string // "function" | "class" | "interface" | "type" | "variable" | "import" | "export"
    Path     string // file path
    Line     int
    Exported bool
}
```

**Chunks** — file content split into RAG-friendly slices:

```go
type FileChunk struct {
    Path      string
    Content   string
    StartLine int
}
```

**Edges** — the dependency graph at the storage layer (distinct from the radar snapshot edges):

```go
type Edge struct {
    From       string
    To         string
    Symbol     string
    Confidence string // "HIGH" | "MEDIUM" | "LOW"
    Kind       string // "Import" | "Call" | "Instantiation" | "Extends" | "Implements"
}
```

Edge keys follow `edge:<from>:<to>`. Forward scan (edges from a file) uses a prefix iterator on `edge:<from>:`. Reverse scan (edges to a file) is a full scan of `edge:` filtered by `edge.To == path`. This O(edges) reverse scan is explicitly noted in the code as acceptable for small codebases. At scale, a secondary index `redge:<to>:<from>` would be added.

**SetFileRecord** uses a batch to write hash, parser version, and indexed_at atomically:

```go
batch := s.db.NewBatch()
batch.Set(keyFileHash(rec.Path), ...)
batch.Set(keyFileParserVersion(rec.Path), ...)
batch.Set(keyFileIndexedAt(rec.Path), ...)
batch.Commit(&pebble.WriteOptions{Sync: true})
```

The `Sync: true` option flushes the WAL before returning. This is correct — the last thing you want is to report a file as indexed and then have the process crash before the write hits disk.

---

### 2. Radar BFS Import Graph (`internal/radar`, `bfs.go`)

Radar's job is impact analysis: given a set of changed files, which other files are transitively affected? This drives the risk scoring pipeline. The answer lives in Pebble.

**ImportRecord** — the core data structure:

```go
type ImportRecord struct {
    Imports    []string `json:"imports"`    // files this file imports
    ImportedBy []string `json:"importedBy"` // files that import this file
}
```

Stored at key `repo/{repoID}/snap/{sha}/imp/{path}`. The `repoID` and `sha` identify which snapshot this belongs to. Multiple snapshots can coexist in the same database, each rooted at a different SHA.

**BFS traversal** reads `ImportedBy` chains from Pebble at each hop:

```
ComputeImpact(db, repoID, sha, changedFiles)
    │
    ├── for each changed file: seed the BFS queue
    │
    └── while queue not empty:
        ├── read repo/{repoID}/snap/{sha}/imp/{path}  ← Pebble read
        ├── for each importer: enqueue if not visited
        └── stop at BFSMaxDepth=4 or BFSMaxVisited=2000
```

The caps (depth 4, visited 2000) are intentional: they bound the query time and prevent pathological traversals in highly connected codebases. When either cap is hit, `result.Truncated = true` is set so callers know the result is partial.

**Error handling in BFS** is noteworthy. Rather than aborting on a corrupt record, BFS treats JSON decode errors as missing records and sets `Truncated = true`, then continues:

```go
var syntaxErr *json.SyntaxError
if errors.As(err, &syntaxErr) || isDecodeError(err) {
    result.Truncated = true
    continue  // keep going with what we have
}
// Real I/O error — return it.
return nil, fmt.Errorf("BFS getImportedBy %s: %w", entry.path, err)
```

The rationale: a corrupt record in one file's import data should not abort the entire impact analysis. The caller gets partial (but correct) results plus a signal that results are incomplete.

---

### 3. Fan-In Cache (`internal/radar`, `bfs.go`)

Fan-in (how many files import a given file) is a key input to the centrality score. Rather than recomputing it during every BFS traversal by counting `ImportedBy` lengths, huginn pre-computes and caches fan-in counts when a snapshot is written:

```go
func WriteFanInCache(batch *pebble.Batch, repoID, sha string, imports map[string]ImportRecord) error {
    for path, rec := range imports {
        key := fanInKey(repoID, sha, path)        // repo/{repoID}/snap/{sha}/fanin/{path}
        val, _ := jsonMarshal(len(rec.ImportedBy))
        batch.Set(key, val, pebble.Sync)
    }
    return nil
}
```

This is written in the same batch as the ImportRecords, so the cache is always consistent with the source data. The fan-in value is a single JSON integer, not a struct — small and fast to decode.

---

### 4. Snap Edge Store (`internal/radar`, `drift.go`)

Drift detection needs to compare the current snapshot's import graph against a stored baseline. The current snapshot's edges are stored at:

```
repo/{repoID}/snap/{sha}/edge/{from}\x00{to}
```

The value is empty — the key itself encodes the edge. This is intentional: drift detection only needs to ask "does edge A→B exist?" not "what metadata does this edge have?" Storing edges as keys rather than values means the prefix scan for all edges in a snapshot requires no value deserialization at all.

Scanning current edges:

```go
prefix := []byte(fmt.Sprintf("repo/%s/snap/%s/edge/", repoID, sha))
iter, _ := db.NewIter(&pebble.IterOptions{
    LowerBound: prefix,
    UpperBound: incrementLastByte(prefix),
})
// parse from/to by splitting on \x00 after stripping prefix
```

---

### 5. Drift Baseline (`internal/radar`, `drift.go`)

The baseline is what huginn compares the current snapshot against. It has two components:

**BaselineGraph** — the set of allowed edges, keyed by branch:

```
repo/{repoID}/baseline/{branch}/graph
```

```go
type BaselineGraph struct {
    Edges map[string][]string `json:"edges"` // from → []to
}
```

**BaselinePolicy** — explicit rules for what is forbidden:

```
repo/{repoID}/baseline/{branch}/policy
```

```go
type BaselinePolicy struct {
    ForbiddenEdges []ForbiddenEdge `json:"forbiddenEdges"`
    LayerRules     []LayerRule     `json:"layerRules"`
}
```

Both are loaded at the start of `DetectDrift`. If either is missing (e.g., first run on this branch), the error is silently swallowed and an empty baseline is used — meaning all current edges are treated as "new" but none are forbidden. This is the correct default: alert on new things, don't block on missing history.

---

### 6. Radar Acknowledgment Store (`internal/radar`, `notify.go`)

When a user acknowledges a radar finding, huginn suppresses re-notification for a window that varies by severity (INFO: 24h, MEDIUM: 4h, HIGH/CRITICAL: 1h). The acknowledgment is persisted to Pebble:

```
repo/{repoID}/radar/ack/{findingID}
```

The value is a JSON `int64` Unix timestamp. `findingID` is a 16-hex-character SHA-256 prefix of the finding type and sorted file list — deterministic across runs.

The ack store is accessed through an interface (`AckStore`) so it can be swapped in tests:

```go
type AckStore interface {
    GetAck(findingID string) (ackedAt int64, found bool)
    SetAck(findingID string, ackedAt int64) error
}
```

`PebbleAckStore` is the production implementation. This is one of the cleaner abstractions in the codebase — the radar scoring logic never touches Pebble directly.

---

### 7. Agent Memory Store (`internal/agents`, `memory.go`)

Named agents accumulate two kinds of memory across sessions: session summaries and delegation logs.

**Session summaries** record what an agent did in a session — files touched, decisions made, open questions:

```go
type SessionSummary struct {
    SessionID     string
    MachineID     string
    AgentName     string
    Timestamp     time.Time
    Summary       string
    FilesTouched  []string
    Decisions     []string
    OpenQuestions []string
}
```

Key: `agent:summary:{machineID}:{agentName}:{sessionID}`

The `machineID` segment ensures that multiple machines sharing the same Pebble database (e.g., via a shared filesystem) do not bleed agent memory into each other. The `agentName` segment isolates memory between named agents. The `sessionID` is the terminal discriminator.

**Delegation entries** record agent-to-agent consultations — what was asked and what the answer was:

```go
type DelegationEntry struct {
    From      string
    To        string
    Question  string
    Answer    string
    Timestamp time.Time
}
```

Key: `agent:delegation:{machineID}:{from}:{to}:{unix-nano-zero-padded-20-digits}`

The zero-padded 20-digit nanosecond timestamp is the key design choice here: because Pebble sorts keys lexicographically, chronological order and lexicographic order are identical for zero-padded numeric timestamps. This means "load most recent N delegations" is just a prefix scan followed by a sort — no secondary index needed. Trimming to the most recent 10 entries is a straightforward batch delete of the prefix scan head:

```go
if len(keys) <= max {
    return nil
}
toDelete := keys[:len(keys)-max] // oldest first in lexicographic order
batch.Delete(toDelete...)
```

---

## HNSW Vector Index: No Pebble Integration

The HNSW index (`internal/search/hnsw/index.go`) is a pure in-memory data structure. It does not use Pebble. Vectors are inserted at runtime and are not persisted across process restarts.

This is a current limitation. The index is rebuilt from stored chunks on each session start. For small codebases this is fast enough. For larger codebases, or for workloads where embedding generation is expensive (Ollama-backed embeddings at local inference speed), this is a meaningful startup cost.

The v3 design does not yet specify a persistence strategy for the vector index. The likely path is serializing the index to a Pebble key (`hnsw:index:v1` or similar) as a gob- or msgpack-encoded blob after each update, and loading it on startup. The tradeoff is index staleness vs. rebuild cost.

---

## Error Handling

### ErrNotFound

`pebble.ErrNotFound` is not an error — it is the normal "this key does not exist" signal. Every `db.Get` call in huginn checks for it explicitly and treats it as a zero-value result:

```go
val, closer, err := s.db.Get(keyFileHash(path))
if err != nil {
    if err == pebble.ErrNotFound {
        return ""  // not indexed yet
    }
    log.Printf("error reading file hash for %s: %v", path, err)
    return ""
}
defer closer.Close()
```

### Corruption Recovery

The BFS and scoring code treat JSON decode failures as soft errors. A corrupt record in one file's import data causes `result.Truncated = true` and BFS continues. This is a deliberate design choice: partial analysis is more useful than a hard failure.

The storage package does not implement recovery beyond what Pebble's WAL provides. If the Pebble database itself is corrupt (hardware failure, killed mid-compaction), `pebble.Open` will return an error and huginn will surface it to the user. Recovery at that point requires deleting the `huginn.pebble` directory and re-indexing from scratch.

### Nil DB guard

Every method on `Store` checks `s.db == nil` and returns a zero-value result or a descriptive error. This handles the case where `Open` failed but the caller continued:

```go
func (s *Store) GetFileHash(path string) string {
    if s.db == nil {
        return ""
    }
    ...
}
```

---

## Write Durability

All writes that must survive a crash use `&pebble.WriteOptions{Sync: true}` or the `pebble.Sync` shorthand. This flushes the WAL entry to disk before returning.

Multi-key writes (e.g., `SetFileRecord` writing hash + parser version + indexed_at) use a `*pebble.Batch` to make the write atomic from Pebble's perspective:

```go
batch := s.db.NewBatch()
defer batch.Close()
batch.Set(keyFileHash(rec.Path), ...)
batch.Set(keyFileParserVersion(rec.Path), ...)
batch.Set(keyFileIndexedAt(rec.Path), ...)
return batch.Commit(&pebble.WriteOptions{Sync: true})
```

Without the batch, a crash between the second and third Set calls would leave the store in a partially-written state — hash written but indexed_at missing.

---

## Data Flow: Indexing a File

```
file on disk
    │
    ▼
hash(content)  ──── matches stored hash? ──── yes ──── skip (cache hit)
    │                                                       │
    no                                                      ▼
    │                                               symbols, chunks
    ▼                                               returned from store
parse(content)
    │
    ├── symbols []Symbol  ──────────────────────────────────────────────┐
    ├── chunks  []FileChunk ──────────────────────────────────────────┐ │
    └── edges   []Edge ────────────────────────────────────────────┐ │ │
                                                                   │ │ │
                                                                   ▼ ▼ ▼
                                                            pebble.Batch
                                                                   │
                                                        batch.Commit(Sync:true)
                                                                   │
                                                                   ▼
                                                            huginn.pebble
```

---

## Data Flow: Radar Impact Analysis

```
git diff / changed files
    │
    ▼
ComputeImpact(db, repoID, sha, changedFiles)
    │
    ├── BFS queue: seed with changed files
    │       │
    │       └── each hop: db.Get("repo/{repoID}/snap/{sha}/imp/{path}")
    │               → ImportRecord.ImportedBy → enqueue unvisited importers
    │               (max depth 4, max visited 2000)
    │
    ├── for each visited node: db.Get("repo/{repoID}/snap/{sha}/imp/{path}")
    │       → ImportRecord.ImportedBy.len → fan-in count
    │
    └── ImpactResult{Impacted, Truncated, NodesVisited}

        │
        ▼
DetectDrift(db, repoID, sha, branch, changedFiles)
    │
    ├── db.Get("repo/{repoID}/baseline/{branch}/graph") → BaselineGraph
    ├── db.Get("repo/{repoID}/baseline/{branch}/policy") → BaselinePolicy
    ├── scan "repo/{repoID}/snap/{sha}/edge/" → []edgePair
    │
    └── DriftResult{ForbiddenEdges, NewCycles, CrossLayerViolations, NewEdges}

        │
        ▼
ComputeScore(ScoreInput) → RadarScore

        │
        ▼
ClassifyFinding(score, type, files, ackStore)
    │
    ├── db.Get("repo/{repoID}/radar/ack/{findingID}") → suppression check
    │
    └── (Severity, NotifyAction)
```

---

## Known Limitations

**Single machine, single process.** Pebble does not support concurrent opens from multiple processes. If two huginn instances try to open the same database, the second will fail. This is acceptable for a developer CLI — a single developer does not typically run two huginn instances against the same workspace.

**No replication.** There is no built-in mechanism to share the Pebble store across machines. Agent memory keyed by `machineID` is machine-local by design. The v3 design routes cross-machine memory sharing through MuninnDB instead, leaving Pebble as local-only storage.

**Reverse edge scan is O(edges).** `GetEdgesTo` performs a full scan of all `edge:` keys and filters by `To` field. This is acceptable for small-to-medium codebases (thousands of edges). For very large repositories, a secondary index `redge:<to>:<from>` would bring this to O(fan-in).

**HNSW index is ephemeral.** The vector index is rebuilt on every process start. For large codebases with expensive embedding generation, this startup cost will be noticeable. Persistence is not yet designed.

**No compaction tuning.** Pebble is opened with `&pebble.Options{}` — all defaults. For a developer tool this is fine. If the key space grows significantly (many snapshots, large symbol sets), explicit compaction configuration may become necessary.

**No snapshot GC.** Radar snapshot data (import records, fan-in cache, edges) accumulates for every `(repoID, sha)` pair. There is no code to prune old snapshots. For active repositories with frequent commits, this will grow unbounded. A GC pass keyed on "keep only the N most recent SHAs per repo" is a natural future addition.

**Workspace ID not yet used.** `keyMetaWorkspaceID` is defined in `schema.go` but nothing writes to it in the current codebase. It is reserved for future multi-workspace disambiguation.
