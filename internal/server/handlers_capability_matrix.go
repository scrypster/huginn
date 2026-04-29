package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/security"
)

var errCapabilityMatrixUnavailable = errors.New("capability matrix unavailable")

type validateToolbeltRequest struct {
	Toolbelt []agents.ToolbeltEntry `json:"toolbelt"`
}

func (s *Server) buildCapabilityMatrix() (security.CapabilityMatrix, error) {
	if s.connStore == nil {
		return security.CapabilityMatrix{}, fmt.Errorf("%w: connections not configured", errCapabilityMatrixUnavailable)
	}
	conns, err := s.connStore.List()
	if err != nil {
		return security.CapabilityMatrix{}, fmt.Errorf("%w: list connections: %v", errCapabilityMatrixUnavailable, err)
	}
	return security.NewCapabilityMatrix(conns), nil
}

func (s *Server) evaluateToolbelt(tb []agents.ToolbeltEntry) (security.ValidationResult, error) {
	// Keep backward compatibility for tests/modes that run without connections.
	if s.connStore == nil {
		return security.ValidationResult{Valid: true, Decisions: []security.ToolbeltDecision{}}, nil
	}
	matrix, err := s.buildCapabilityMatrix()
	if err != nil {
		return security.ValidationResult{}, err
	}
	return matrix.ValidateToolbelt(tb), nil
}

func (s *Server) handleGetCapabilityMatrix(w http.ResponseWriter, r *http.Request) {
	matrix, err := s.buildCapabilityMatrix()
	if err != nil {
		if errors.Is(err, errCapabilityMatrixUnavailable) {
			jsonError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, matrix)
}

func (s *Server) handleValidateCapabilityMatrix(w http.ResponseWriter, r *http.Request) {
	var req validateToolbeltRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	result, err := s.evaluateToolbelt(req.Toolbelt)
	if err != nil {
		if errors.Is(err, errCapabilityMatrixUnavailable) {
			jsonError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, result)
}
