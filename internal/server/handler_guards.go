package server

import (
	"encoding/json"
	"net/http"

	"github.com/scrypster/huginn/internal/scheduler"
	"github.com/scrypster/huginn/internal/threadmgr"
)

func jsonCapabilityError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
		"code":  code,
	})
}

func (s *Server) requireThreadManager(w http.ResponseWriter, status int) *threadmgr.ThreadManager {
	s.mu.Lock()
	tm := s.tm
	s.mu.Unlock()
	if tm == nil {
		jsonCapabilityError(w, status, "thread_manager_not_configured", "thread manager not configured")
		return nil
	}
	return tm
}

func (s *Server) requireScheduler(w http.ResponseWriter) *scheduler.Scheduler {
	s.mu.Lock()
	sched := s.sched
	s.mu.Unlock()
	if sched == nil {
		jsonCapabilityError(w, http.StatusServiceUnavailable, "scheduler_not_configured", "scheduler not configured")
		return nil
	}
	return sched
}

func (s *Server) requireWorkflowRunStore(w http.ResponseWriter, status int) scheduler.WorkflowRunStoreInterface {
	s.mu.Lock()
	store := s.workflowRunStore
	s.mu.Unlock()
	if store == nil {
		jsonCapabilityError(w, status, "workflow_run_store_not_configured", "workflow run store not configured")
		return nil
	}
	return store
}

func (s *Server) requireArtifactStore(w http.ResponseWriter, status int) artifactStore {
	s.mu.Lock()
	store := s.artifactStore
	s.mu.Unlock()
	if store == nil {
		jsonCapabilityError(w, status, "artifact_store_not_configured", "artifact store not available")
		return nil
	}
	return store
}
