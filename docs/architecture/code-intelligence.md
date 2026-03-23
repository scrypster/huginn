# Code Intelligence Pipeline

How Huginn understands your codebase and surfaces relevant context to the agent.

---

## The Problem

A language model operates inside a fixed token budget. Dumping every file in a repository into the context window is not viable: a medium-sized Go service can easily exceed 500K tokens, and even if it fit, relevance degrades as noise overwhelms signal.

The alternative — asking the agent to fetch files on demand — shifts the problem rather than solving it: the agent must already know what to ask for. Neither approach answers the question "given a query, which code is actually relevant?"

Huginn solves this with a structured pipeline that:

1. Chunks every text file into addressable units.
2. Extracts symbols (functions, types, imports) and the edges between them.
3. Persists everything in a content-addressed KV store.
4. Answers two distinct query shapes at runtime:
   - "What code is semantically close to this query?" (hybrid BM25 + HNSW search)
   - "Which files are affected if this file changes?" (BFS over the import graph)

---

## Pipeline Diagram

```
  Source files (git-tracked or directory walk)
        |
        v
  ┌─────────────┐    chunk content (200 KB slices)
  │  repo pkg   │──► FileChunk{Path, Content, StartLine}
  └─────────────┘    incremental: skip if SHA-256 unchanged
        |
        v
  ┌──────────────┐   Extractor interface per language
  │  symbol pkg  │──► Symbol{Name, Kind, Path, Line, Exported}
  └──────────────┘    Edge{From, To, Symbol, Kind, Confidence}
        |
        v
  ┌──────────────┐   Pebble KV — content-addressed
  │  storage pkg │   key: repo/{id}/snap/{sha}/imp/{path}
  └──────────────┘   also: file records, chunk blobs, fan-in cache
        |
        ├──────────────────────────────────┐
        v                                  v
  ┌──────────────┐                  ┌─────────────┐
  │  radar pkg   │  BFS over        │  search pkg │  BM25 + HNSW
  │  (on-demand) │  reverse import  │  (at query) │  hybrid scoring
  └──────────────┘  graph           └─────────────┘
        |                                  |
        └──────────────┬───────────────────┘
                       v
              agent context window
```

---

## repo package

**Location:** `internal/repo/`

### Why this package exists

File reading, git enumeration, and chunk management are invalidated at a coarse cadence: on every `git pull`, `git checkout`, or project open. Keeping them isolated from symbol extraction means a file change triggers only a re-chunk of that file, not a full re-parse.

### FileChunk

```go
// internal/repo/indexer.go
type FileChunk struct {
    Path      string
    Content   string
    StartLine int
}
```

A `FileChunk` is the smallest unit the pipeline operates on. Files larger than 200 KB are split on line boundaries:

```go
const chunkSize = 200 * 1024      // 200 KB per chunk
const maxFileBytes = 10 * 1024 * 1024  // 10 MB max single file read
```

Splitting on line boundaries (not byte boundaries) ensures chunks are syntactically coherent — a chunk never starts mid-line.

### Index

```go
type Index struct {
    Chunks []FileChunk
    Root   string
}
```

`Index` is the in-memory view of the whole repo. It is rebuilt on startup and refreshed incrementally on file save.

### Git vs. directory mode

`Build()` first attempts to open a git repository with `go-git`. If that succeeds, it reads exactly the tracked files from HEAD via the git tree object — this respects `.gitignore` automatically and avoids indexing build artifacts. If no git repo is found, it falls back to `buildFromDir()`, which walks the filesystem and skips `.git`, `node_modules`, and `vendor`.

### Incremental indexing

`BuildIncrementalWithStats()` computes SHA-256 of each file's content and compares it to the stored hash. If they match, the file is loaded from the KV store rather than re-read from disk:

```go
// internal/repo/indexer.go
hash := sha256hex(data)
if store != nil {
    stored := store.GetFileRecord(relPath)
    if stored.Hash == hash {
        result.FilesSkipped++
        chunks := store.GetChunks(relPath)
        // ... load from store
        continue
    }
}
// File changed — re-chunk and persist
chunks := chunkContent(relPath, data, chunkSize)
```

The `IncrementalResult` includes `FilesScanned` and `FilesSkipped` counts for observability.

### Binary detection

Two-layer filter:
1. Extension allowlist (`.png`, `.zip`, `.exe`, `.wasm`, etc.) — fast path, no I/O.
2. Content sniff: reads the first 512 bytes and rejects any file containing a null byte.

### Workspace detection

`Detect()` in `detector.go` runs a 5-step algorithm on startup:

1. Explicit `--workspace` flag path
2. `huginn.workspace.json` in CWD
3. Walk up from CWD to `$HOME` looking for `huginn.workspace.json`
4. CWD is inside a git repo → single-repo mode
5. Plain directory mode

---

## symbol package

**Location:** `internal/symbol/`

### Extractor interface

```go
// internal/symbol/registry.go
type Extractor interface {
    Language() string
    Extract(path string, content []byte) ([]Symbol, []Edge, error)
}
```

The `Registry` maps file extensions to `Extractor` implementations. A fallback extractor is used when no specific extractor matches.

```go
type Registry struct {
    extractors map[string]Extractor // key: lowercase extension e.g. ".go"
    fallback   Extractor
}
```

### Language support

| Extractor | Package | Extensions | Method | Confidence |
|-----------|---------|------------|--------|------------|
| Go | `symbol/goext` | `.go` | `go/ast` static parse | HIGH (imports), MEDIUM (calls) |
| TypeScript | `symbol/tsext` | `.ts`, `.tsx` | regex-based | MEDIUM |
| LSP | `symbol/lsp` | configurable | Language Server Protocol | HIGH |
| Heuristic | `symbol/heuristic` | fallback | regex patterns | LOW |

The Go extractor uses the standard library `go/ast` and `go/parser` package for full AST-level analysis. Parse failures degrade gracefully — the extractor returns empty slices rather than an error, so a syntax error in one file does not block indexing of the rest.

### Symbol and Edge types

```go
// internal/symbol/types.go
type Symbol struct {
    Name     string     `json:"name"`
    Kind     SymbolKind `json:"kind"`   // function, class, interface, type, variable, import, export
    Path     string     `json:"path"`
    Line     int        `json:"line"`
    Exported bool       `json:"exported"`
}

type Edge struct {
    From       string     `json:"from"`
    To         string     `json:"to"`
    Symbol     string     `json:"symbol"`
    Confidence Confidence `json:"confidence"` // HIGH, MEDIUM, LOW
    Kind       EdgeKind   `json:"kind"`       // Import, Call, Instantiation, Extends, Implements
}
```

`Edge.From` and `Edge.To` are file paths. The import graph is stored inverted (importers keyed by importee) to support efficient BFS over the reverse graph.

---

## radar package

**Location:** `internal/radar/`

### Purpose

When a file changes, radar answers: "which other files might now break?" This is the impact radius — the set of files that transitively depend on the changed file.

### BFS traversal

`ComputeImpact()` performs breadth-first search over the reverse import graph stored in Pebble:

```go
// internal/radar/bfs.go
const (
    BFSMaxDepth   = 4
    BFSMaxVisited = 2000
)
```

**Why depth 4?** Beyond four hops, the signal-to-noise ratio collapses. A depth-4 ripple already surfaces indirect callers of callers of callers of callers — that is already a very broad blast radius. Deeper traversal tends to surface the entire codebase via shared utility packages.

**Why 2000 visited nodes?** BFS in a large monorepo can explode combinatorially. 2000 is large enough to capture real impact in any reasonably sized module while bounding worst-case latency.

### ImpactNode and ImpactResult

```go
// internal/radar/bfs.go
type ImpactNode struct {
    Path     string `json:"path"`
    Distance int    `json:"distance"`  // 0 = seed file, 1 = direct importer, etc.
    FanIn    int    `json:"fanIn"`     // how many files import this file
}

type ImpactResult struct {
    Impacted     []ImpactNode `json:"impacted"`
    Truncated    bool         `json:"truncated"`   // true if BFS hit a limit
    NodesVisited int          `json:"nodesVisited"`
}
```

Results are sorted by `Distance` ascending, so the most directly affected files appear first.

### Truncated flag

The `Truncated` field is set to `true` in two cases:

1. BFS hit `BFSMaxVisited` — results are partial but still valid up to that point.
2. A corrupt or undecodable import record was encountered.

In the second case, BFS continues rather than aborting:

```go
// internal/radar/bfs.go
var syntaxErr *json.SyntaxError
if errors.As(err, &syntaxErr) || isDecodeError(err) {
    result.Truncated = true
    continue  // skip corrupt record, keep traversing
}
// Real I/O error — return it.
return nil, fmt.Errorf("BFS getImportedBy %s: %w", entry.path, err)
```

This means callers always receive a result; they just need to check `Truncated` to know if it is complete. A hard failure is only returned for genuine I/O errors (disk failure, Pebble internal error).

### Pebble key format

```
repo/{repoID}/snap/{sha}/imp/{filePath}
repo/{repoID}/snap/{sha}/fanin/{filePath}
```

The `snap/{sha}` component ties import records to a specific git HEAD commit. This prevents stale import graphs from leaking across repository states after a `git pull` or branch switch.

---

## search package

**Location:** `internal/search/`

### Why hybrid search?

BM25 and vector search have complementary blind spots:

- **BM25** excels at exact identifier matches: `MyHandler`, `parseSSE`, `BFSMaxDepth`. These are low-frequency tokens that embeddings tend to normalize away.
- **HNSW** (vector/semantic search) excels at concept-level queries: "error handling for network timeout", "database retry logic". These queries often do not share tokens with the target code.

A query like "fix the parseSSE function" needs both: BM25 finds `parseSSE` exactly; HNSW finds semantically related streaming code even if the exact name is absent.

### Searcher and Embedder interfaces

```go
// internal/search/searcher.go
type Searcher interface {
    Index(ctx context.Context, chunks []Chunk) error
    Search(ctx context.Context, query string, n int) ([]Chunk, error)
    Close() error
}

type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    Dimensions() int
}
```

`Chunk` mirrors `repo.FileChunk` with an added `uint64` ID for HNSW indexing:

```go
type Chunk struct {
    ID        uint64
    Path      string
    Content   string
    StartLine int
}
```

### BM25 (keyword search)

`BM25Searcher` implements the Robertson-Sparck Jones BM25 variant with constants `k1=1.5`, `b=0.75`. It tokenizes on whitespace and applies IDF weighting across the chunk corpus. The path is included in the token bag, giving a path-match bonus without a separate scoring pass.

### HNSW (semantic search)

`hnsw.Index` is an in-process, in-memory Hierarchical Navigable Small World graph. It uses cosine distance. Key parameters:

- `M = 16` — outgoing edges per node (graph connectivity)
- `Mmax0 = 32` — maximum edges at layer 0 (denser at base layer)
- `EfConstruct = 200` — search width during construction (quality/speed tradeoff)

The index is protected by a `sync.RWMutex`: `Insert` takes a write lock; `Search` takes a read lock.

### Hybrid scoring with RRF

`HybridSearcher` fuses BM25 and HNSW results using Reciprocal Rank Fusion:

```go
// internal/search/hybrid.go
const rrfK = 60

// For each result list, score[id] += 1.0 / (rrfK + rank + 1)
for rank, chunk := range bm25Results {
    scores[chunk.ID] += 1.0 / float64(rrfK+rank+1)
}
// ...same for HNSW IDs
```

RRF is rank-based rather than score-based, which means it does not require normalizing BM25 scores (which are unbounded) and HNSW distances (which are 0–2). The `rrfK=60` dampening constant prevents a single first-place result from dominating.

### Partial embedding failure

If `Embedder.Embed()` fails during indexing (e.g., the embedding server is unreachable), that chunk is skipped silently:

```go
// internal/search/hybrid.go
vec, err := h.embedder.Embed(ctx, c.Content)
if err != nil {
    // Skip on embedding error for graceful degradation
    continue
}
```

At query time, if embedding the query itself fails, HNSW is skipped entirely and only BM25 results are returned. The caller receives valid results — the search degrades to BM25-only rather than failing.

---

## storage package

**Location:** `internal/storage/`

### Pebble KV store

All persistent intelligence data is stored in a single Pebble (LSM-tree) database at `{huginn_data_dir}/huginn.pebble`. Pebble provides:

- Atomic batch writes (used when writing import records for a snapshot)
- Range scans (used for snapshot migration)
- Crash-safe durability

### Key schema

```
file:{path}                         → FileRecord (hash, parser version, indexed_at)
chunks:{path}                       → []FileChunk (JSON)
repo/{id}/snap/{sha}/imp/{path}     → ImportRecord (ImportedBy []string)
repo/{id}/snap/{sha}/fanin/{path}   → int (fan-in count cache)
git:head                            → current HEAD SHA
stats:cost:{YYYY-MM}               → monthly cost float64
```

The `snap/{sha}` namespace ties all import data to a specific commit. When HEAD advances, the old snapshot remains readable until explicitly pruned, which prevents the BFS from reading stale data mid-operation.

---

## Why this design?

### Why BFS for impact analysis?

BFS naturally produces "blast radius rings": distance-0 is the changed file itself, distance-1 its direct importers, distance-2 their importers, and so on. This ring structure is directly useful to the agent — it can present the most affected files first and expand as needed.

An alternative like topological sort over the full DAG would produce a single flat list with no depth ordering and no practical bound.

### Why hybrid search over pure vector?

Pure vector search fails on exact identifier queries. If a developer asks "where is `parseSSE` called?", the embedding for that query is close to embeddings about SSE parsing in general — but the exact function name `parseSSE` may appear in only one file. BM25 finds it instantly. The hybrid approach means neither query shape is a blind spot.

### Why separate packages for repo, symbol, and radar?

Each package has a different invalidation cadence:

| Package | Invalidated when |
|---------|-----------------|
| `repo` | File content changes (save or git op) |
| `symbol` | File content changes (triggers re-extraction) |
| `radar` | On-demand (user runs `/impact` or agent needs blast radius) |

Keeping them separate means a file save triggers only `repo` + `symbol` re-indexing — not a BFS traversal. The BFS is expensive (reads from Pebble, may visit thousands of nodes) and should only run when explicitly requested.

---

## Thread Safety

| Component | Concurrency guarantee |
|-----------|----------------------|
| `repo.Build` / `BuildIncremental` | Not safe for concurrent calls; call from a single goroutine |
| `repo.Index.BuildContext` | Read-only after construction; safe to call concurrently |
| `storage.Store` | Pebble is thread-safe; `Store` methods are safe for concurrent use |
| `radar.ComputeImpact` | Read-only Pebble access; safe for concurrent use |
| `hnsw.Index.Insert` | Holds write lock; safe to call concurrently with `Search` |
| `hnsw.Index.Search` | Holds read lock; safe for concurrent searchers |
| `search.HybridSearcher` | `Index` and `Search` are safe for concurrent use |

---

## Limitations

- **BFS depth cap**: The 4-hop limit means truly deep transitive dependencies (common in plugin or framework code) are not surfaced. The `Truncated` flag signals this.
- **HNSW approximate results**: HNSW is an approximate nearest-neighbor algorithm. It can miss exact nearest neighbors. The hybrid design mitigates this for identifier queries, but pure semantic queries may omit relevant chunks.
- **Symbol extraction language gaps**: Go and TypeScript have dedicated AST extractors. Other languages fall back to the heuristic extractor, which uses regex patterns and produces `LOW` confidence edges only.
- **In-memory HNSW**: The HNSW index is not persisted to disk. It is rebuilt from chunks on each startup. For very large repos (millions of chunks), startup time may be noticeable.
- **BM25 tokenization**: The tokenizer splits on non-alphanumeric characters. Identifiers with underscores (`parse_sse`) tokenize correctly, but camelCase identifiers (`parseSSE`) are treated as a single token, which limits partial-identifier matching.

---

## See Also

- [backend.md](./backend.md) — LLM Backend Abstraction
- `session-and-memory.md` — How the agent maintains session state and memory
- `swarm.md` — Multi-agent orchestration
