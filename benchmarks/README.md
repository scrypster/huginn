# Benchmarks

## Baseline

`baseline.txt` contains benchmark results collected with `-count=6` across:

- `internal/search` — BM25 index and search at 1K/5K/10K chunks
- `internal/session` — SQLite session store save, load, append, tail
- `internal/swarm` — parallel task execution at 10 and 50 tasks
- `internal/mcp` — MCP client ListTools and CallTool round-trips
- `internal/agents` — agent config loading and memory block construction
- `internal/relay` — outbox enqueue and drain operations

Benchmarks must be run on ubuntu-latest runner class for cross-run comparability. CI uses ubuntu-latest.

## Updating the baseline

```bash
go test -bench=. -benchmem -count=6 -timeout=300s -run='^$' \
    ./internal/search/... \
    ./internal/session/... \
    ./internal/swarm/... \
    ./internal/mcp/... \
    ./internal/agents/... \
    ./internal/relay/... \
    2>&1 | grep -E "^(Benchmark|ok|PASS|FAIL|goos|goarch|pkg|cpu)" > benchmarks/baseline.txt
git add benchmarks/baseline.txt && git commit -m "bench: update baseline"
```

## Comparing against baseline

```bash
go install golang.org/x/perf/cmd/benchstat@latest

go test -bench=. -benchmem -count=6 -timeout=300s -run='^$' \
    ./internal/search/... \
    ./internal/session/... \
    ./internal/swarm/... \
    ./internal/mcp/... \
    ./internal/agents/... \
    ./internal/relay/... \
    2>&1 | grep -E "^(Benchmark|ok|PASS|FAIL|goos|goarch|pkg|cpu)" > benchmarks/current.txt

benchstat benchmarks/baseline.txt benchmarks/current.txt
```

## CI

The `bench` job in `.github/workflows/ci.yml` runs on every pull request, compares
results against `baseline.txt` using benchstat, and fails if any benchmark regresses
by more than 20%.
