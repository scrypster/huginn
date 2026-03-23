package radar

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/cockroachdb/pebble/v2"
)

const (
	// BFSDefaultMaxDepth is the default hop limit for ComputeImpact.
	BFSDefaultMaxDepth = 4
	// BFSMaxDepth is an alias for BFSDefaultMaxDepth kept for backwards compatibility.
	BFSMaxDepth   = BFSDefaultMaxDepth
	BFSMaxVisited = 2_000
)

// BFSConfig allows callers to override BFS traversal limits per call.
// Zero values fall back to the package defaults (BFSDefaultMaxDepth, BFSMaxVisited).
type BFSConfig struct {
	MaxDepth   int // 0 = use BFSDefaultMaxDepth
	MaxVisited int // 0 = use BFSMaxVisited
}

// ErrBFSLimitExceeded is returned (wrapped in ImpactResult.Err) when the BFS
// traversal stops because it reached BFSMaxVisited nodes. The partial result
// is still valid; callers should treat it as incomplete.
var ErrBFSLimitExceeded = errors.New("radar: BFS max-visited limit exceeded; result is partial")

// ImpactNode is a file reached during BFS traversal.
type ImpactNode struct {
	Path     string `json:"path"`
	Distance int    `json:"distance"`
	FanIn    int    `json:"fanIn"`
}

// ImpactResult is the output of BFS impact traversal.
type ImpactResult struct {
	Impacted     []ImpactNode `json:"impacted"`
	Truncated    bool         `json:"truncated"`
	NodesVisited int          `json:"nodesVisited"`
	// Err is non-nil when traversal was stopped early (e.g. ErrBFSLimitExceeded).
	// The partial result in Impacted is still usable.
	Err error `json:"-"`
}

// ComputeImpact performs bounded BFS over the reverse import graph.
func ComputeImpact(db *pebble.DB, repoID, sha string, changedFiles []string) (*ImpactResult, error) {
	seeds := make([]string, len(changedFiles))
	copy(seeds, changedFiles)
	sort.Strings(seeds)

	type queueEntry struct {
		path     string
		distance int
	}

	visited := make(map[string]int, BFSMaxVisited)
	queue := make([]queueEntry, 0, len(seeds)*4)
	result := &ImpactResult{}

	for _, path := range seeds {
		if _, seen := visited[path]; !seen {
			visited[path] = 0
			queue = append(queue, queueEntry{path: path, distance: 0})
		}
	}

	head := 0
	for head < len(queue) {
		if len(visited) >= BFSMaxVisited {
			result.Truncated = true
			result.Err = ErrBFSLimitExceeded
			slog.Warn("radar: BFS max-visited limit reached; stopping traversal with partial result",
				"limit", BFSMaxVisited, "repo", repoID, "sha", sha)
			break
		}
		entry := queue[head]
		head++
		if entry.distance >= BFSMaxDepth {
			continue
		}
		importers, err := getImportedBy(db, repoID, sha, entry.path)
		if err != nil {
			if errors.Is(err, pebble.ErrNotFound) {
				continue // no import record for this file — expected
			}
			// Corrupt/undecodable record: treat like a missing record.
			// Set Truncated=true so callers know results are partial, then continue
			// BFS rather than aborting and losing all already-computed results.
			var syntaxErr *json.SyntaxError
			if errors.As(err, &syntaxErr) || isDecodeError(err) {
				result.Truncated = true
				continue
			}
			// Real I/O error — return it.
			return nil, fmt.Errorf("BFS getImportedBy %s: %w", entry.path, err)
		}
		sort.Strings(importers)
		for _, imp := range importers {
			if _, seen := visited[imp]; seen {
				continue
			}
			if len(visited) >= BFSMaxVisited {
				result.Truncated = true
				result.Err = ErrBFSLimitExceeded
				break
			}
			newDist := entry.distance + 1
			visited[imp] = newDist
			queue = append(queue, queueEntry{path: imp, distance: newDist})
		}
	}

	nodes := make([]ImpactNode, 0, len(visited))
	for path, dist := range visited {
		fanIn := 0
		if rec, err := getImportRecord(db, repoID, sha, path); err == nil {
			fanIn = len(rec.ImportedBy)
		}
		nodes = append(nodes, ImpactNode{Path: path, Distance: dist, FanIn: fanIn})
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Distance != nodes[j].Distance {
			return nodes[i].Distance < nodes[j].Distance
		}
		return nodes[i].Path < nodes[j].Path
	})

	result.Impacted = nodes
	result.NodesVisited = len(visited)
	return result, nil
}

// ComputeImpactWithConfig performs bounded BFS over the reverse import graph
// using caller-supplied limits. Zero values in cfg fall back to package defaults.
func ComputeImpactWithConfig(db *pebble.DB, repoID, sha string, changedFiles []string, cfg BFSConfig) (*ImpactResult, error) {
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = BFSDefaultMaxDepth
	}
	maxVisited := cfg.MaxVisited
	if maxVisited <= 0 {
		maxVisited = BFSMaxVisited
	}

	seeds := make([]string, len(changedFiles))
	copy(seeds, changedFiles)
	sort.Strings(seeds)

	type queueEntry struct {
		path     string
		distance int
	}

	visited := make(map[string]int, maxVisited)
	queue := make([]queueEntry, 0, len(seeds)*4)
	result := &ImpactResult{}

	for _, path := range seeds {
		if _, seen := visited[path]; !seen {
			visited[path] = 0
			queue = append(queue, queueEntry{path: path, distance: 0})
		}
	}

	head := 0
	for head < len(queue) {
		if len(visited) >= maxVisited {
			result.Truncated = true
			result.Err = ErrBFSLimitExceeded
			slog.Warn("radar: BFS max-visited limit reached; stopping traversal with partial result",
				"limit", maxVisited, "repo", repoID, "sha", sha)
			break
		}
		entry := queue[head]
		head++
		if entry.distance >= maxDepth {
			continue
		}
		importers, err := getImportedBy(db, repoID, sha, entry.path)
		if err != nil {
			if errors.Is(err, pebble.ErrNotFound) {
				continue
			}
			var syntaxErr *json.SyntaxError
			if errors.As(err, &syntaxErr) || isDecodeError(err) {
				result.Truncated = true
				continue
			}
			return nil, fmt.Errorf("BFS getImportedBy %s: %w", entry.path, err)
		}
		sort.Strings(importers)
		for _, imp := range importers {
			if _, seen := visited[imp]; seen {
				continue
			}
			if len(visited) >= maxVisited {
				result.Truncated = true
				result.Err = ErrBFSLimitExceeded
				break
			}
			newDist := entry.distance + 1
			visited[imp] = newDist
			queue = append(queue, queueEntry{path: imp, distance: newDist})
		}
	}

	nodes := make([]ImpactNode, 0, len(visited))
	for path, dist := range visited {
		fanIn := 0
		if rec, err := getImportRecord(db, repoID, sha, path); err == nil {
			fanIn = len(rec.ImportedBy)
		}
		nodes = append(nodes, ImpactNode{Path: path, Distance: dist, FanIn: fanIn})
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Distance != nodes[j].Distance {
			return nodes[i].Distance < nodes[j].Distance
		}
		return nodes[i].Path < nodes[j].Path
	})

	result.Impacted = nodes
	result.NodesVisited = len(visited)
	return result, nil
}

func impKey(repoID, sha, path string) []byte {
	return []byte(fmt.Sprintf("repo/%s/snap/%s/imp/%s", repoID, sha, path))
}

func getImportRecord(db *pebble.DB, repoID, sha, path string) (*ImportRecord, error) {
	val, closer, err := db.Get(impKey(repoID, sha, path))
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	var rec ImportRecord
	if err := jsonUnmarshal(val, &rec); err != nil {
		return nil, fmt.Errorf("decode import record for %s: %w", path, err)
	}
	return &rec, nil
}

func getImportedBy(db *pebble.DB, repoID, sha, path string) ([]string, error) {
	rec, err := getImportRecord(db, repoID, sha, path)
	if err != nil {
		return nil, err
	}
	return rec.ImportedBy, nil
}

// isDecodeError reports whether err originates from a JSON decode failure
// (syntax error, type mismatch, etc.) rather than an I/O error.
func isDecodeError(err error) bool {
	if err == nil {
		return false
	}
	var syntax *json.SyntaxError
	var unmarshal *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &unmarshal)
}

func fanInKey(repoID, sha, path string) []byte {
	return []byte(fmt.Sprintf("repo/%s/snap/%s/fanin/%s", repoID, sha, path))
}

// WriteFanInCache writes fan-in counts for all files in a snapshot.
func WriteFanInCache(batch *pebble.Batch, repoID, sha string, imports map[string]ImportRecord) error {
	for path, rec := range imports {
		key := fanInKey(repoID, sha, path)
		val, err := jsonMarshal(len(rec.ImportedBy))
		if err != nil {
			return err
		}
		if err := batch.Set(key, val, pebble.Sync); err != nil {
			return err
		}
	}
	return nil
}
