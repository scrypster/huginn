# Code Intelligence

## What it is

Huginn indexes your codebase on first run using a hybrid of BM25 keyword search and HNSW vector search. Agents query the index automatically on every request — you never need to paste file contents or explain the project structure.

The index also powers **impact analysis**: a breadth-first traversal of the reverse import graph that shows which files are affected when a given file changes. Agents use this to reason about the blast radius of edits before making them.

---

## How to use it

Run `huginn` in your project directory. Indexing happens automatically on first launch. You will see a brief indexing status message; once it finishes, the index is ready.

From that point, agents use the index on every prompt:

- "What does the auth middleware do?" — agents search the index for auth-related code
- "Refactor the payment module" — agents load relevant files from the index automatically
- "Which files will break if I change the database schema?" — agents run impact analysis over the import graph

The index updates incrementally when files change. Only files whose content has changed (detected by SHA-256 hash) are re-indexed; unchanged files are loaded from the persistent store. No manual rebuild is needed during normal use.

---

## Configuration

| Setting | Description |
|---------|-------------|
| `--workspace <path>` | Set the working directory to index. For large monorepos, point at a specific package or service subdirectory to keep the index small and queries fast. |
| `huginn.workspace.json` | Drop a `huginn.workspace.json` file in a directory to mark it as the workspace root. Huginn detects this automatically on startup. |

Huginn detects the workspace in this order:

1. `--workspace` flag
2. `huginn.workspace.json` in the current directory
3. Walk up from the current directory looking for `huginn.workspace.json`
4. Current directory is inside a git repo — use the repo root
5. Plain directory mode

In git mode, Huginn indexes only git-tracked files (respecting `.gitignore`). In plain directory mode, it skips `.git`, `node_modules`, and `vendor` directories.

---

## How the index works

### Hybrid search

Huginn uses two search methods that complement each other:

- **BM25 keyword search** — finds exact identifier matches. Best for queries like "where is `parseSSE` called?" or searching for a specific function name.
- **HNSW vector search** — finds semantically similar code. Best for concept-level queries like "error handling for network timeouts" or "database retry logic".

Results from both methods are fused using Reciprocal Rank Fusion (RRF), which combines rank positions without requiring score normalization. If the embedding backend is unavailable, Huginn falls back to BM25 search only — queries still work, just without semantic matching.

### Impact analysis

When reasoning about the effect of a change, agents can request an impact analysis for any file. Huginn performs a breadth-first search over the reverse import graph (files that import the target file, then their importers, and so on) up to 4 hops deep. Results are sorted by distance:

- Distance 0: the changed file itself
- Distance 1: direct importers
- Distance 2: files that import those importers
- And so on

The BFS is capped at 2,000 visited nodes to bound latency on large codebases. If the cap is reached, the result includes a `truncated` flag indicating the analysis is partial.

### Language support

| Language | Method | Confidence |
|----------|--------|------------|
| Go | AST-level parse via `go/ast` | HIGH (imports), MEDIUM (calls) |
| TypeScript / TSX | Regex-based extraction | MEDIUM |
| LSP-enabled languages | Language Server Protocol | HIGH |
| All others | Heuristic regex patterns | LOW |

Go and TypeScript have dedicated extractors. Other languages use the heuristic fallback, which still produces usable results for search but lower-confidence import edges for impact analysis.

### Persistent storage

The index is stored in a Pebble (LSM-tree) database at `{huginn_data_dir}/huginn.pebble`. This includes file records, chunk data, and the import graph. The HNSW vector index is held in memory and rebuilt from stored chunks at startup — it is not written to disk.

---

## Tips & common patterns

- **Point at a subdirectory for large monorepos** — run `huginn --workspace packages/my-service` to index only the relevant package. This keeps the index small and queries fast.
- **Impact analysis is on-demand** — agents run it when the task calls for it (for example, before refactoring a widely-used module). You can also ask for it directly: "which files depend on internal/auth/tokens.go?"
- **The index is git-aware** — in git repos, the import graph is snapshotted per commit SHA. Switching branches or pulling changes causes only the changed files to be re-indexed; the old snapshot remains valid until pruned.
- **Force a full rebuild if the index is stale** — if the agent references deleted code or misses recent files, delete the Pebble store and restart:
  ```sh
  rm -rf ~/.huginn/huginn.pebble
  huginn
  ```
- **Existing rule files are picked up automatically** — Huginn scans for `.cursorrules`, `CLAUDE.md`, `.claude/CLAUDE.md`, `.huginn/rules.md`, and `.github/copilot-instructions.md` at startup. These are injected into the agent's system prompt automatically.

---

## Troubleshooting

**Agent references deleted code or misses recent files**

The index may be stale. Force a full rebuild by removing the Pebble store:

```sh
rm -rf ~/.huginn/huginn.pebble
huginn
```

**Indexing is slow on first run**

Large repositories take longer to index on first launch. Subsequent launches use the stored index and only re-index changed files. No action needed — this is a one-time cost.

**Semantic search not working / results seem keyword-only**

Vector embeddings require a working backend model. If the backend is unavailable (Ollama not running, API key missing, network error), Huginn falls back to BM25 keyword search only. Check your backend configuration and confirm the model is reachable. Once the backend is available again, restart Huginn to rebuild the HNSW index.

**Impact analysis results are marked as truncated**

The breadth-first traversal hit the 2,000-node cap, meaning results are partial. This typically happens with very widely-used utility packages (e.g., a shared `errors` or `config` package imported by most of the codebase). The files closest to the changed file (shortest distance) are still accurate; deeper results may be incomplete.
