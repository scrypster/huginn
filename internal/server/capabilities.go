package server

import (
	"fmt"
	"sort"
	"strings"
)

// Capabilities returns a normalized map of runtime-wired server features.
func (s *Server) Capabilities() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]bool{
		"orchestrator":       s.orch != nil,
		"session_store":      s.store != nil,
		"thread_manager":     s.tm != nil,
		"scheduler":          s.sched != nil,
		"workflow_run_store": s.workflowRunStore != nil,
		"notification_store": s.notifStore != nil,
		"artifact_store":     s.artifactStore != nil,
		"space_store":        s.spaceStore != nil,
		"database":           s.db != nil,
		"runtime_manager":    s.runtimeMgr != nil,
		"model_store":        s.modelStore != nil,
	}
}

// ValidateWiring verifies required dependencies were wired before Start().
func (s *Server) ValidateWiring() error {
	caps := s.Capabilities()
	required := []string{"orchestrator", "session_store"}
	var missing []string
	for _, key := range required {
		if !caps[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("server wiring invalid: missing required capabilities: %s", strings.Join(missing, ", "))
}
