package threadmgr

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// threadSnapshot is the on-disk representation of a thread's persistent metadata.
// It captures all fields needed to reconstruct dependency relationships after a restart.
type threadSnapshot struct {
	ID            string       `json:"id"`
	SessionID     string       `json:"session_id"`
	AgentID       string       `json:"agent_id"`
	Task          string       `json:"task"`
	Status        ThreadStatus `json:"status"`
	DependsOn     []string     `json:"depends_on,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	CompletedAt   time.Time    `json:"completed_at,omitempty"`
	CreatedByUser string       `json:"created_by_user,omitempty"`
	CreatedReason string       `json:"created_reason,omitempty"`
	TokenBudget   int          `json:"token_budget,omitempty"`
	TokensUsed    int          `json:"tokens_used,omitempty"`
}

// sessionGraph is the on-disk JSON file written per session.
type sessionGraph struct {
	SessionID string           `json:"session_id"`
	SnapshotAt time.Time       `json:"snapshot_at"`
	Threads   []threadSnapshot `json:"threads"`
}

// graphPath returns the path for a session's dependency graph JSON file.
func graphPath(dir, sessionID string) string {
	return filepath.Join(dir, "graph-"+sessionID+".json")
}

// snapshotGraph serialises all threads for sessionID to a JSON file in dir.
// The write is atomic (write-to-tmp then rename).
// Caller must NOT hold tm.mu when calling this function.
func (tm *ThreadManager) snapshotGraph(sessionID, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("threadmgr: snapshotGraph mkdir %s: %w", dir, err)
	}

	// Collect snapshots under read lock.
	tm.mu.RLock()
	var snaps []threadSnapshot
	for _, t := range tm.threads {
		if t.SessionID != sessionID {
			continue
		}
		deps := make([]string, len(t.DependsOn))
		copy(deps, t.DependsOn)
		snaps = append(snaps, threadSnapshot{
			ID:            t.ID,
			SessionID:     t.SessionID,
			AgentID:       t.AgentID,
			Task:          t.Task,
			Status:        t.Status,
			DependsOn:     deps,
			CreatedAt:     t.CreatedAt,
			CompletedAt:   t.CompletedAt,
			CreatedByUser: t.CreatedByUser,
			CreatedReason: t.CreatedReason,
			TokenBudget:   t.TokenBudget,
			TokensUsed:    t.TokensUsed,
		})
	}
	tm.mu.RUnlock()

	g := sessionGraph{
		SessionID:  sessionID,
		SnapshotAt: time.Now().UTC(),
		Threads:    snaps,
	}

	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("threadmgr: snapshotGraph marshal: %w", err)
	}

	path := graphPath(dir, sessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("threadmgr: snapshotGraph write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("threadmgr: snapshotGraph rename: %w", err)
	}
	return nil
}

// loadGraph reads the persisted dependency graph for sessionID from dir and
// restores threads into the manager. Existing threads with the same ID are
// skipped (idempotent). Only terminal threads are restored — non-terminal
// threads that were in-flight when the process died are marked StatusError to
// prevent them from blocking downstream threads indefinitely.
// Returns the number of threads restored and any error.
func (tm *ThreadManager) loadGraph(sessionID, dir string) (int, error) {
	path := graphPath(dir, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // no graph on disk — normal for new sessions
		}
		return 0, fmt.Errorf("threadmgr: loadGraph read %s: %w", path, err)
	}

	var g sessionGraph
	if err := json.Unmarshal(data, &g); err != nil {
		return 0, fmt.Errorf("threadmgr: loadGraph parse %s: %w", path, err)
	}

	restored := 0
	tm.mu.Lock()
	for _, s := range g.Threads {
		if _, exists := tm.threads[s.ID]; exists {
			continue // already live — skip
		}
		deps := make([]string, len(s.DependsOn))
		copy(deps, s.DependsOn)

		status := s.Status
		// Threads that were non-terminal when snapshotted cannot be resumed;
		// mark them as error so the DAG can make progress.
		switch status {
		case StatusDone, StatusCancelled, StatusError:
			// terminal — keep as-is
		default:
			slog.Warn("threadmgr: non-terminal thread on restore, marking error",
				"thread_id", s.ID, "was_status", status)
			status = StatusError
		}

		t := &Thread{
			ID:            s.ID,
			SessionID:     s.SessionID,
			AgentID:       s.AgentID,
			Task:          s.Task,
			Status:        status,
			DependsOn:     deps,
			CreatedAt:     s.CreatedAt,
			CompletedAt:   s.CompletedAt,
			CreatedByUser: s.CreatedByUser,
			CreatedReason: s.CreatedReason,
			StartedAt:     s.CreatedAt,
			TokenBudget:   s.TokenBudget,
			TokensUsed:    s.TokensUsed,
			InputCh:       make(chan string, 1),
		}
		tm.threads[t.ID] = t
		restored++
	}
	tm.mu.Unlock()
	return restored, nil
}

// RestoreSession loads the persisted dependency graph for sessionID from the
// configured graphDir. It is a no-op if graphDir is empty or the file does not
// exist. Returns the number of threads restored.
func (tm *ThreadManager) RestoreSession(sessionID string) (int, error) {
	tm.mu.RLock()
	dir := tm.graphDir
	tm.mu.RUnlock()
	if dir == "" {
		return 0, nil
	}
	n, err := tm.loadGraph(sessionID, dir)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		slog.Info("threadmgr: restored thread dependency graph",
			"session", sessionID, "threads", n)
	}
	return n, nil
}

// canReach reports whether node `from` can reach node `to` by following edges
// in the adjacency map (map[threadID][]dependencyIDs). The search is a targeted
// DFS from `from`; it terminates early on cycle detection and does not traverse
// the entire graph. The visited map must be non-nil on entry and is mutated in
// place (callers should pass a fresh map each time).
//
// Complexity: O(depth of `from`'s ancestry), not O(V+E) of the entire graph.
func canReach(adj map[string][]string, from, to string, visited map[string]bool) bool {
	if from == to {
		return true
	}
	if visited[from] {
		return false // already explored this path
	}
	visited[from] = true
	for _, next := range adj[from] {
		if canReach(adj, next, to, visited) {
			return true
		}
	}
	return false
}

// HasCycle reports whether adding an edge from `parentID` to `childID` would
// create a cycle in the current dependency graph. It uses the targeted canReach
// DFS (O(depth)) rather than a full-graph DFS (O(V+E)).
// Caller must NOT hold tm.mu.
func (tm *ThreadManager) HasCycle(parentID, childID string) bool {
	// Build adjacency list (child → dependencies) under read lock.
	tm.mu.RLock()
	adj := make(map[string][]string, len(tm.threads))
	for id, t := range tm.threads {
		if len(t.DependsOn) > 0 {
			deps := make([]string, len(t.DependsOn))
			copy(deps, t.DependsOn)
			adj[id] = deps
		}
	}
	tm.mu.RUnlock()

	// A cycle would exist if childID can already reach parentID.
	// (Adding parentID → childID would then create parentID → ... → parentID.)
	return canReach(adj, childID, parentID, make(map[string]bool))
}
