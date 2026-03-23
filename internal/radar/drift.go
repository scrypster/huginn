package radar

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cockroachdb/pebble/v2"
)

// DriftResult holds all detected violations.
type DriftResult struct {
	ForbiddenEdges       []DriftViolation `json:"forbiddenEdges"`
	NewCycles            []Cycle          `json:"newCycles"`
	CrossLayerViolations []DriftViolation `json:"crossLayerViolations"`
	NewEdges             []NewEdge        `json:"newEdges"`
}

// DriftViolation is a single forbidden or cross-layer edge.
type DriftViolation struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rule string `json:"rule"`
}

// Cycle is a detected import cycle.
type Cycle struct {
	Nodes []string `json:"nodes"`
}

// NewEdge is an edge present in current snapshot but absent from baseline.
type NewEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Layer represents an architectural layer.
type Layer struct {
	Name string
	Rank int
}

var defaultLayers = map[string]Layer{
	"cmd":               {Name: "cmd", Rank: 40},
	"internal/api":      {Name: "api", Rank: 30},
	"internal/handler":  {Name: "api", Rank: 30},
	"internal/http":     {Name: "api", Rank: 30},
	"internal/grpc":     {Name: "api", Rank: 30},
	"internal/service":  {Name: "application", Rank: 20},
	"internal/app":      {Name: "application", Rank: 20},
	"internal/usecase":  {Name: "application", Rank: 20},
	"internal/domain":   {Name: "domain", Rank: 10},
	"internal/model":    {Name: "domain", Rank: 10},
	"internal/entity":   {Name: "domain", Rank: 10},
	"internal/infra":    {Name: "infra", Rank: 5},
	"internal/platform": {Name: "infra", Rank: 5},
	"internal/repo":     {Name: "infra", Rank: 5},
	"pkg":               {Name: "pkg", Rank: 15},
	"src/api":           {Name: "api", Rank: 30},
	"src/routes":        {Name: "api", Rank: 30},
	"src/controllers":   {Name: "api", Rank: 30},
	"src/services":      {Name: "application", Rank: 20},
	"src/domain":        {Name: "domain", Rank: 10},
	"src/models":        {Name: "domain", Rank: 10},
	"src/infra":         {Name: "infra", Rank: 5},
	"src/lib":           {Name: "infra", Rank: 5},
	"src/utils":         {Name: "infra", Rank: 5},
	"app/api":           {Name: "api", Rank: 30},
	"app/routers":       {Name: "api", Rank: 30},
	"app/services":      {Name: "application", Rank: 20},
	"app/domain":        {Name: "domain", Rank: 10},
	"app/models":        {Name: "domain", Rank: 10},
	"app/infra":         {Name: "infra", Rank: 5},
}

func inferLayer(path string) *Layer {
	normalized := filepath.ToSlash(path)
	parts := strings.SplitN(normalized, "/", 3)
	if len(parts) >= 2 {
		twoSeg := parts[0] + "/" + parts[1]
		if layer, ok := defaultLayers[twoSeg]; ok {
			return &layer
		}
	}
	if len(parts) >= 1 {
		if layer, ok := defaultLayers[parts[0]]; ok {
			return &layer
		}
	}
	return nil
}

func isLayerViolation(fromPath, toPath string) *DriftViolation {
	fromLayer := inferLayer(fromPath)
	toLayer := inferLayer(toPath)
	if fromLayer == nil || toLayer == nil {
		return nil
	}
	if toLayer.Rank == 5 {
		return nil
	}
	if fromLayer.Rank < toLayer.Rank {
		return &DriftViolation{
			From: fromPath,
			To:   toPath,
			Rule: fmt.Sprintf("layer violation: %s (rank %d) imports %s (rank %d)",
				fromLayer.Name, fromLayer.Rank, toLayer.Name, toLayer.Rank),
		}
	}
	return nil
}

type color int

const (
	white color = iota
	gray
	black
)

const maxCycleLen = 8

func detectCycles(adj map[string][]string, maxLen int) []Cycle {
	colors := make(map[string]color, len(adj))
	var cycles []Cycle

	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	stack := make([]string, 0, 64)

	var dfs func(node string)
	dfs = func(node string) {
		colors[node] = gray
		stack = append(stack, node)

		neighbors := make([]string, len(adj[node]))
		copy(neighbors, adj[node])
		sort.Strings(neighbors)

		for _, next := range neighbors {
			switch colors[next] {
			case white:
				dfs(next)
			case gray:
				cycle := extractCycle(stack, next)
				if len(cycle) > 0 && len(cycle) <= maxLen {
					cycles = append(cycles, Cycle{Nodes: cycle})
				}
			}
		}

		stack = stack[:len(stack)-1]
		colors[node] = black
	}

	for _, n := range nodes {
		if colors[n] == white {
			dfs(n)
		}
	}

	return deduplicateCycles(cycles)
}

func extractCycle(stack []string, target string) []string {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == target {
			cycle := make([]string, len(stack)-i)
			copy(cycle, stack[i:])
			return cycle
		}
	}
	return nil
}

func deduplicateCycles(cycles []Cycle) []Cycle {
	seen := make(map[string]bool, len(cycles))
	var unique []Cycle
	for _, c := range cycles {
		key := canonicalCycleKey(c.Nodes)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, c)
		}
	}
	return unique
}

func canonicalCycleKey(nodes []string) string {
	if len(nodes) == 0 {
		return ""
	}
	minIdx := 0
	for i, n := range nodes {
		if n < nodes[minIdx] {
			minIdx = i
		}
	}
	rotated := make([]string, len(nodes))
	for i := range nodes {
		rotated[i] = nodes[(minIdx+i)%len(nodes)]
	}
	return strings.Join(rotated, "→")
}

// DetectDrift compares current snapshot against baseline for violations.
func DetectDrift(db *pebble.DB, repoID, sha, branch string, changedFiles []string) (*DriftResult, error) {
	result := &DriftResult{}

	baseline, err := loadBaseline(db, repoID, branch)
	if err != nil {
		baseline = &BaselineGraph{Edges: make(map[string][]string)}
	}
	baselineSet := buildEdgeSet(baseline)

	policy, err := loadPolicy(db, repoID, branch)
	if err != nil {
		policy = &BaselinePolicy{}
	}

	currentEdges, err := scanCurrentEdges(db, repoID, sha)
	if err != nil {
		return nil, fmt.Errorf("scan edges: %w", err)
	}

	for _, edge := range currentEdges {
		edgeKey := edge.From + "→" + edge.To
		if !baselineSet[edgeKey] {
			result.NewEdges = append(result.NewEdges, NewEdge{From: edge.From, To: edge.To})
		}
		if violation := checkForbidden(edge.From, edge.To, policy); violation != nil {
			result.ForbiddenEdges = append(result.ForbiddenEdges, *violation)
		}
		if violation := isLayerViolation(edge.From, edge.To); violation != nil {
			result.CrossLayerViolations = append(result.CrossLayerViolations, *violation)
		}
	}

	adj := buildAdjacencyFromEdges(currentEdges)
	reachable := reachableSubgraph(adj, changedFiles, BFSMaxDepth)
	result.NewCycles = detectCycles(reachable, maxCycleLen)

	return result, nil
}

type edgePair struct {
	From string
	To   string
}

func buildEdgeSet(baseline *BaselineGraph) map[string]bool {
	set := make(map[string]bool, len(baseline.Edges)*4)
	for from, tos := range baseline.Edges {
		for _, to := range tos {
			set[from+"→"+to] = true
		}
	}
	return set
}

func buildAdjacencyFromEdges(edges []edgePair) map[string][]string {
	adj := make(map[string][]string, len(edges))
	for _, e := range edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	return adj
}

func reachableSubgraph(adj map[string][]string, seeds []string, maxDepth int) map[string][]string {
	visited := make(map[string]bool, 256)
	type entry struct {
		node  string
		depth int
	}
	queue := make([]entry, 0, len(seeds))
	for _, s := range seeds {
		if !visited[s] {
			visited[s] = true
			queue = append(queue, entry{node: s, depth: 0})
		}
	}
	head := 0
	for head < len(queue) {
		e := queue[head]
		head++
		if e.depth >= maxDepth {
			continue
		}
		for _, neighbor := range adj[e.node] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, entry{node: neighbor, depth: e.depth + 1})
			}
		}
	}
	sub := make(map[string][]string, len(visited))
	for node := range visited {
		if neighbors, ok := adj[node]; ok {
			filtered := make([]string, 0, len(neighbors))
			for _, n := range neighbors {
				if visited[n] {
					filtered = append(filtered, n)
				}
			}
			if len(filtered) > 0 {
				sub[node] = filtered
			}
		}
	}
	return sub
}

func checkForbidden(from, to string, policy *BaselinePolicy) *DriftViolation {
	for _, rule := range policy.ForbiddenEdges {
		if matchGlobOrPrefix(from, rule.From) && matchGlobOrPrefix(to, rule.To) {
			return &DriftViolation{
				From: from,
				To:   to,
				Rule: fmt.Sprintf("forbidden: %s -> %s", rule.From, rule.To),
			}
		}
	}
	return nil
}

func matchGlobOrPrefix(path, pattern string) bool {
	matched, err := filepath.Match(pattern, path)
	if err == nil && matched {
		return true
	}
	// For glob patterns ending in *, strip the * and do prefix match
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	// For non-glob patterns, require exact match or directory prefix (pattern + "/")
	return path == pattern || strings.HasPrefix(path, pattern+"/")
}

func baselineGraphKey(repoID, branch string) []byte {
	return []byte(fmt.Sprintf("repo/%s/baseline/%s/graph", repoID, branch))
}

func baselinePolicyKey(repoID, branch string) []byte {
	return []byte(fmt.Sprintf("repo/%s/baseline/%s/policy", repoID, branch))
}

func loadBaseline(db *pebble.DB, repoID, branch string) (*BaselineGraph, error) {
	val, closer, err := db.Get(baselineGraphKey(repoID, branch))
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	var bg BaselineGraph
	if err := jsonUnmarshal(val, &bg); err != nil {
		return nil, err
	}
	return &bg, nil
}

func loadPolicy(db *pebble.DB, repoID, branch string) (*BaselinePolicy, error) {
	val, closer, err := db.Get(baselinePolicyKey(repoID, branch))
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	var bp BaselinePolicy
	if err := jsonUnmarshal(val, &bp); err != nil {
		return nil, err
	}
	return &bp, nil
}

// edgeSep is a null byte used to separate the "from" and "to" segments in
// snap edge keys. File paths cannot contain \x00, so this avoids the
// ambiguity that "/" caused when "from" contained slashes (e.g. "cmd/main.go").
const edgeSep = "\x00"

// snapEdgeKey builds the Pebble key for a snap edge.
// Format: repo/{repoID}/snap/{sha}/edge/{from}\x00{to}
func snapEdgeKey(repoID, sha, from, to string) []byte {
	return []byte(fmt.Sprintf("repo/%s/snap/%s/edge/%s%s%s", repoID, sha, from, edgeSep, to))
}

func scanCurrentEdges(db *pebble.DB, repoID, sha string) ([]edgePair, error) {
	prefix := []byte(fmt.Sprintf("repo/%s/snap/%s/edge/", repoID, sha))
	iter, err := db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var edges []edgePair
	prefixStr := string(prefix)
	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		rest := strings.TrimPrefix(key, prefixStr)
		parts := strings.SplitN(rest, edgeSep, 2)
		if len(parts) == 2 {
			edges = append(edges, edgePair{From: parts[0], To: parts[1]})
		}
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}
	return edges, nil
}

func incrementLastByte(b []byte) []byte {
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end
		}
	}
	// All bytes were 0xFF — overflow: append a 0x00 byte to produce a valid upper bound.
	return append(b, 0x00)
}
