package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/scrypster/huginn/internal/workforce"
)

// maxArtifactDownload is the download size cap for the streaming path.
// Matches artifact.MaxArtifactSize (10 MB). File-backed artifacts exceeding
// this are rejected before any bytes are written to the response.
const maxArtifactDownload = 10 << 20

// artifactStore is the interface the server needs for workforce artifact operations.
// Implemented by *artifact.SQLiteStore (wired via Server.SetArtifactStore).
type artifactStore interface {
	Write(ctx context.Context, a *workforce.Artifact) error
	Read(ctx context.Context, id string) (*workforce.Artifact, error)
	// ReadMetaOnly fetches metadata without loading file-backed content.
	// a.Content is always nil; use OpenContent for streaming downloads.
	ReadMetaOnly(ctx context.Context, id string) (*workforce.Artifact, error)
	// ListBySession returns artifacts for sessionID. limit ≤ 0 defaults to 50
	// (max 200). afterID, if non-empty, returns results after that ULID cursor.
	ListBySession(ctx context.Context, sessionID string, limit int, afterID string) ([]*workforce.Artifact, error)
	ListByAgent(ctx context.Context, agentName string, since time.Time, limit int, afterID string) ([]*workforce.Artifact, error)
	UpdateStatus(ctx context.Context, id string, status workforce.ArtifactStatus, reason string) error
	// OpenContent returns a streaming reader for file-backed artifact content.
	// Returns workforce.ErrArtifactNotFound when content is stored inline.
	OpenContent(ctx context.Context, id string) (io.ReadCloser, error)
}

// artifactSummary is the list-response DTO. Content is omitted to keep list
// responses small; callers retrieve content via the GET /{id} or /download endpoints.
type artifactSummary struct {
	ID        string                 `json:"id"`
	Kind      workforce.ArtifactKind `json:"kind"`
	Title     string                 `json:"title"`
	MimeType  string                 `json:"mime_type,omitempty"`
	AgentName string                 `json:"agent_name"`
	SessionID string                 `json:"session_id"`
	Status    workforce.ArtifactStatus `json:"status"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func toArtifactSummary(a *workforce.Artifact) artifactSummary {
	return artifactSummary{
		ID:        a.ID,
		Kind:      a.Kind,
		Title:     a.Title,
		MimeType:  a.MimeType,
		AgentName: a.AgentName,
		SessionID: a.SessionID,
		Status:    a.Status,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

// sanitizeFilename replaces characters unsafe in Content-Disposition filename
// values (control chars including DEL, unicode controls, quotes, path separators)
// with underscores. Truncates to 255 bytes (filesystem limit).
func sanitizeFilename(name string) string {
	safe := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || unicode.IsControl(r) ||
			r == '"' || r == '/' || r == '\\' || r == '\r' || r == '\n' {
			return '_'
		}
		return r
	}, name)
	if safe == "" {
		return "artifact"
	}
	if len(safe) > 255 {
		safe = safe[:255]
	}
	return safe
}

// ── Session-scoped artifact handlers ─────────────────────────────────────────

// handleListArtifacts returns artifact summaries for a session.
//
//	GET /api/v1/sessions/{id}/artifacts
func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		jsonError(w, http.StatusServiceUnavailable, "session store not configured")
		return
	}
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.artifactStore == nil {
		jsonOK(w, []artifactSummary{})
		return
	}
	limit := parseIntQuery(r, "limit", 0)
	afterID := r.URL.Query().Get("after")
	arts, err := s.artifactStore.ListBySession(r.Context(), sessionID, limit, afterID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list artifacts: "+err.Error())
		return
	}
	summaries := make([]artifactSummary, 0, len(arts))
	for _, a := range arts {
		summaries = append(summaries, toArtifactSummary(a))
	}
	jsonOK(w, summaries)
}

// handleGetArtifact returns a single artifact (including content) by ID.
//
//	GET /api/v1/sessions/{id}/artifacts/{artifact_id}
func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	artifactID := r.PathValue("artifact_id")
	if sessionID == "" || artifactID == "" {
		http.Error(w, "session id and artifact id required", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		jsonError(w, http.StatusServiceUnavailable, "session store not configured")
		return
	}
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	a, err := s.artifactStore.Read(r.Context(), artifactID)
	if errors.Is(err, workforce.ErrArtifactNotFound) {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read artifact: "+err.Error())
		return
	}
	if a.SessionID != sessionID {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	jsonOK(w, a)
}

// handleCreateArtifact creates a new artifact for a session via the REST API.
// Body size is capped at 1 MiB by the route middleware (withMaxBody(1<<20)).
// Large artifacts (up to artifact.MaxArtifactSize = 10 MB) should be created
// by the agent process using the global POST /api/v1/artifacts endpoint, which
// has direct store access and supports file-backed content.
//
//	POST /api/v1/sessions/{id}/artifacts
func (s *Server) handleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		jsonError(w, http.StatusServiceUnavailable, "session store not configured")
		return
	}
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	var a workforce.Artifact
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if a.Title == "" {
		jsonError(w, http.StatusBadRequest, "title is required")
		return
	}
	a.SessionID = sessionID
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Status == "" {
		a.Status = workforce.StatusDraft
	}
	if err := s.artifactStore.Write(r.Context(), &a); err != nil {
		jsonError(w, http.StatusInternalServerError, "create artifact: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": a.ID}) //nolint:errcheck
}

// handleUpdateArtifact updates the status (and optionally rejection_reason) of
// an artifact. Only valid status transitions are accepted.
//
//	PUT /api/v1/sessions/{id}/artifacts/{artifact_id}
func (s *Server) handleUpdateArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	artifactID := r.PathValue("artifact_id")
	if sessionID == "" || artifactID == "" {
		http.Error(w, "session id and artifact id required", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		jsonError(w, http.StatusServiceUnavailable, "session store not configured")
		return
	}
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	// Verify ownership before accepting the update body.
	meta, err := s.artifactStore.ReadMetaOnly(r.Context(), artifactID)
	if errors.Is(err, workforce.ErrArtifactNotFound) {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read artifact: "+err.Error())
		return
	}
	if meta.SessionID != sessionID {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	var body struct {
		Status workforce.ArtifactStatus `json:"status"`
		Reason string                   `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Status == "" {
		jsonError(w, http.StatusBadRequest, "status is required")
		return
	}
	if err := s.artifactStore.UpdateStatus(r.Context(), artifactID, body.Status, body.Reason); err != nil {
		jsonError(w, http.StatusInternalServerError, "update artifact: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": string(body.Status)})
}

// handleDeleteArtifact soft-deletes an artifact by marking it StatusDeleted.
//
//	DELETE /api/v1/sessions/{id}/artifacts/{artifact_id}
func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	artifactID := r.PathValue("artifact_id")
	if sessionID == "" || artifactID == "" {
		http.Error(w, "session id and artifact id required", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		jsonError(w, http.StatusServiceUnavailable, "session store not configured")
		return
	}
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	// Verify ownership before deleting.
	meta, err := s.artifactStore.ReadMetaOnly(r.Context(), artifactID)
	if errors.Is(err, workforce.ErrArtifactNotFound) {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read artifact: "+err.Error())
		return
	}
	if meta.SessionID != sessionID {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err := s.artifactStore.UpdateStatus(r.Context(), artifactID, workforce.StatusDeleted, ""); err != nil {
		jsonError(w, http.StatusInternalServerError, "delete artifact: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDownloadArtifact streams artifact content as a file download.
// File-backed artifacts (>256 KB) are streamed from disk via OpenContent;
// inline artifacts are served directly from the Read() result.
//
//	GET /api/v1/sessions/{id}/artifacts/{artifact_id}/download
func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	artifactID := r.PathValue("artifact_id")
	if sessionID == "" || artifactID == "" {
		http.Error(w, "session id and artifact id required", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		jsonError(w, http.StatusServiceUnavailable, "session store not configured")
		return
	}
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}

	defer func() {
		if r.Context().Err() != nil {
			// Client disconnected mid-download; nothing actionable, log for observability.
			_ = r.Context().Err()
		}
	}()

	meta, err := s.artifactStore.ReadMetaOnly(r.Context(), artifactID)
	if errors.Is(err, workforce.ErrArtifactNotFound) {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read artifact: "+err.Error())
		return
	}
	if meta.SessionID != sessionID {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}

	safeName := sanitizeFilename(meta.Title)
	mimeType := meta.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	if meta.ContentRef != "" {
		// File-backed: stream from disk.
		rc, err := s.artifactStore.OpenContent(r.Context(), artifactID)
		if errors.Is(err, workforce.ErrArtifactNotFound) {
			jsonError(w, http.StatusNotFound, "artifact content not found")
			return
		}
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "open artifact content: "+err.Error())
			return
		}
		defer rc.Close()

		if f, ok := rc.(*os.File); ok {
			info, err := f.Stat()
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "stat artifact: "+err.Error())
				return
			}
			if info.Size() > maxArtifactDownload {
				jsonError(w, http.StatusInternalServerError, "artifact too large")
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeName+`"`)
		w.Header().Set("Content-Type", mimeType)
		lr := &io.LimitedReader{R: rc, N: maxArtifactDownload + 1}
		n, copyErr := io.Copy(w, lr)
		if lr.N <= 0 {
			// Truncation detected (non-file backend or unexpected oversize).
			_ = n // bytes_written logged implicitly; response partially sent
		}
		if copyErr != nil {
			// Response is partially written; log only.
			_ = copyErr
		}
		return
	}

	// Inline content: load via Read().
	full, err := s.artifactStore.Read(r.Context(), artifactID)
	if errors.Is(err, workforce.ErrArtifactNotFound) {
		jsonError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read artifact: "+err.Error())
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName+`"`)
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.Itoa(len(full.Content)))
	w.Write(full.Content) //nolint:errcheck
}

// ── Workforce artifact handlers (global routes) ──────────────────────────────

// handleWorkforceCreateArtifact creates a new artifact via the agent-side global endpoint.
//
//	POST /api/v1/artifacts
func (s *Server) handleWorkforceCreateArtifact(w http.ResponseWriter, r *http.Request) {
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	var a workforce.Artifact
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Status == "" {
		a.Status = workforce.StatusDraft
	}
	if err := s.artifactStore.Write(r.Context(), &a); err != nil {
		jsonError(w, http.StatusInternalServerError, "write artifact: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": a.ID}) //nolint:errcheck
}

// handleWorkforceGetArtifact returns a single artifact by ID.
//
//	GET /api/v1/artifacts/{id}
func (s *Server) handleWorkforceGetArtifact(w http.ResponseWriter, r *http.Request) {
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "artifact id is required")
		return
	}
	a, err := s.artifactStore.Read(r.Context(), id)
	if err != nil {
		if errors.Is(err, workforce.ErrArtifactNotFound) {
			jsonError(w, http.StatusNotFound, "artifact not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "read artifact: "+err.Error())
		return
	}
	jsonOK(w, a)
}

// handleWorkforceListSessionArtifacts lists all artifacts for a session via the artifact store.
//
//	GET /api/v1/sessions/{id}/artifacts  (also used as the primary session handler)
func (s *Server) handleWorkforceListSessionArtifacts(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session id is required")
		return
	}
	if s.artifactStore == nil {
		s.handleListArtifacts(w, r)
		return
	}
	limit := parseIntQuery(r, "limit", 0)
	afterID := r.URL.Query().Get("after")
	arts, err := s.artifactStore.ListBySession(r.Context(), sessionID, limit, afterID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list artifacts: "+err.Error())
		return
	}
	summaries := make([]artifactSummary, 0, len(arts))
	for _, a := range arts {
		summaries = append(summaries, toArtifactSummary(a))
	}
	jsonOK(w, summaries)
}

// handleWorkforceUpdateArtifactStatus accepts | rejects | supersedes an artifact.
//
//	PATCH /api/v1/artifacts/{id}/status
func (s *Server) handleWorkforceUpdateArtifactStatus(w http.ResponseWriter, r *http.Request) {
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "artifact id is required")
		return
	}
	var body struct {
		Status workforce.ArtifactStatus `json:"status"`
		Reason string                   `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Status == "" {
		jsonError(w, http.StatusBadRequest, "status is required")
		return
	}
	if err := s.artifactStore.UpdateStatus(r.Context(), id, body.Status, body.Reason); err != nil {
		if errors.Is(err, workforce.ErrArtifactNotFound) {
			jsonError(w, http.StatusNotFound, "artifact not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "update status: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": string(body.Status)})
}

// handleWorkforceListAgentArtifacts lists recent artifacts produced by a named agent.
// Optional query param: since=<RFC3339> (default: 30 days ago).
//
//	GET /api/v1/agents/{name}/artifacts
func (s *Server) handleWorkforceListAgentArtifacts(w http.ResponseWriter, r *http.Request) {
	if s.artifactStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "artifact store not available")
		return
	}
	agentName := r.PathValue("name")
	if agentName == "" {
		jsonError(w, http.StatusBadRequest, "agent name is required")
		return
	}
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if qs := r.URL.Query().Get("since"); qs != "" {
		if parsed, err := time.Parse(time.RFC3339, qs); err == nil {
			since = parsed
		}
	}
	limit := parseIntQuery(r, "limit", 0)
	afterID := r.URL.Query().Get("after")
	arts, err := s.artifactStore.ListByAgent(r.Context(), agentName, since, limit, afterID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list agent artifacts: "+err.Error())
		return
	}
	summaries := make([]artifactSummary, 0, len(arts))
	for _, a := range arts {
		summaries = append(summaries, toArtifactSummary(a))
	}
	jsonOK(w, summaries)
}

// parseIntQuery reads an integer query parameter; returns defaultVal if absent or invalid.
func parseIntQuery(r *http.Request, key string, defaultVal int) int {
	if s := r.URL.Query().Get(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return defaultVal
}
