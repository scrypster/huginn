package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/scrypster/huginn/internal/models"
	"github.com/scrypster/huginn/internal/runtime"
)

// writeSSEEvent marshals data as JSON and writes a single Server-Sent Events
// "data:" line to w, then calls flusher.Flush() to push it to the client.
// Returns an error only if the JSON marshal fails (Fprintf and Flush errors
// are intentionally ignored because the connection may already be closed).
func writeSSEEvent(w io.Writer, flusher http.Flusher, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	flusher.Flush()
	return nil
}

type builtinStatusResponse struct {
	Installed   bool   `json:"installed"`
	Version     string `json:"version"`
	BinaryPath  string `json:"binary_path"`
	ActiveModel string `json:"active_model"`
	BackendType string `json:"backend_type"`
}

type builtinCatalogEntry struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Provider         string   `json:"provider"`
	ProviderURL      string   `json:"provider_url"`
	Host             string   `json:"host"`
	HostURL          string   `json:"host_url"`
	Filename         string   `json:"filename"`
	SizeBytes        int64    `json:"size_bytes"`
	MinRAMGB         int      `json:"min_ram_gb"`
	RecommendedRAMGB int      `json:"recommended_ram_gb"`
	ContextLength    int      `json:"context_length"`
	Tags             []string `json:"tags"`
	Source           string   `json:"source"`
	Installed        bool     `json:"installed"`
}

type builtinInstalledModel struct {
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	InstalledAt string `json:"installed_at"`
}

func (s *Server) handleBuiltinStatus(w http.ResponseWriter, r *http.Request) {
	if s.runtimeMgr == nil {
		jsonError(w, 503, "runtime manager not configured")
		return
	}
	manifest, err := runtime.LoadManifest()
	if err != nil {
		jsonError(w, 500, "load runtime manifest: "+err.Error())
		return
	}
	jsonOK(w, builtinStatusResponse{
		Installed:   s.runtimeMgr.IsInstalled(),
		Version:     manifest.LlamaServerVersion,
		BinaryPath:  s.runtimeMgr.BinaryPath(),
		ActiveModel: s.cfg.Backend.BuiltinModel,
		BackendType: s.cfg.Backend.Type,
	})
}

func (s *Server) handleBuiltinDownload(w http.ResponseWriter, r *http.Request) {
	if s.runtimeMgr == nil {
		jsonError(w, 503, "runtime manager not configured")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	err := s.runtimeMgr.Download(ctx, func(downloaded, total int64) {
		writeSSEEvent(w, flusher, map[string]any{ //nolint:errcheck
			"type":       "progress",
			"downloaded": downloaded,
			"total":      total,
		})
	})
	if err != nil {
		writeSSEEvent(w, flusher, map[string]string{"type": "error", "content": err.Error()}) //nolint:errcheck
		return
	}
	writeSSEEvent(w, flusher, map[string]string{"type": "done"}) //nolint:errcheck
}

func (s *Server) handleBuiltinListModels(w http.ResponseWriter, r *http.Request) {
	if s.modelStore == nil {
		jsonOK(w, []builtinInstalledModel{})
		return
	}
	entries, err := s.modelStore.Installed()
	if err != nil {
		jsonError(w, 500, "load model store: "+err.Error())
		return
	}
	result := make([]builtinInstalledModel, 0, len(entries))
	for _, e := range entries {
		result = append(result, builtinInstalledModel{
			Name:        e.Name,
			Filename:    e.Filename,
			Path:        e.Path,
			SizeBytes:   e.SizeBytes,
			InstalledAt: e.InstalledAt.Format(time.RFC3339),
		})
	}
	jsonOK(w, result)
}

func (s *Server) handleBuiltinCatalog(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	cachePath := models.DefaultCatalogCachePath()
	var catalog map[string]models.ModelEntry
	var err error
	if refresh {
		catalog, err = models.RefreshCatalog(cachePath)
	} else {
		catalog, err = models.LoadCatalog(cachePath)
	}
	if err != nil {
		jsonError(w, 500, "load model catalog: "+err.Error())
		return
	}
	var installed map[string]models.LockEntry
	if s.modelStore != nil {
		installed, _ = s.modelStore.Installed()
	}
	result := make([]builtinCatalogEntry, 0, len(catalog))
	for name, entry := range catalog {
		_, isInstalled := installed[name]
		result = append(result, builtinCatalogEntry{
			Name:             name,
			Description:      entry.Description,
			Provider:         entry.Provider,
			ProviderURL:      entry.ProviderURL,
			Host:             entry.Host,
			HostURL:          entry.HostURL,
			Filename:         entry.Filename,
			SizeBytes:        entry.SizeBytes,
			MinRAMGB:         entry.MinRAMGB,
			RecommendedRAMGB: entry.RecommendedRAMGB,
			ContextLength:    entry.ContextLength,
			Tags:             entry.Tags,
			Source:           entry.Source,
			Installed:        isInstalled,
		})
	}
	jsonOK(w, result)
}

func (s *Server) handleBuiltinPullModel(w http.ResponseWriter, r *http.Request) {
	if s.modelStore == nil {
		jsonError(w, 503, "model store not configured")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, 400, "name is required")
		return
	}
	catalog, err := models.LoadCatalog(models.DefaultCatalogCachePath())
	if err != nil {
		jsonError(w, 500, "load model catalog: "+err.Error())
		return
	}
	entry, ok := catalog[body.Name]
	if !ok {
		jsonError(w, 404, "model not found in catalog: "+body.Name)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok2 := w.(http.Flusher)
	if !ok2 {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	destPath := s.modelStore.ModelPath(entry.Filename)
	ctx := r.Context()
	err = models.Pull(ctx, entry.URL, destPath, entry.SHA256, func(p models.PullProgress) {
		writeSSEEvent(w, flusher, map[string]any{ //nolint:errcheck
			"type":       "progress",
			"downloaded": p.Downloaded,
			"total":      p.Total,
			"speed":      p.Speed,
		})
	})
	if err != nil {
		writeSSEEvent(w, flusher, map[string]string{"type": "error", "content": err.Error()}) //nolint:errcheck
		return
	}
	_ = s.modelStore.Record(body.Name, models.LockEntry{
		Name:        body.Name,
		Filename:    entry.Filename,
		Path:        destPath,
		SHA256:      entry.SHA256,
		SizeBytes:   entry.SizeBytes,
		InstalledAt: time.Now(),
	})
	writeSSEEvent(w, flusher, map[string]string{"type": "done", "name": body.Name}) //nolint:errcheck
}

func (s *Server) handleBuiltinActivate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
		jsonError(w, 400, "model is required")
		return
	}
	if s.modelStore != nil {
		installed, err := s.modelStore.Installed()
		if err != nil {
			jsonError(w, 500, "load model store: "+err.Error())
			return
		}
		if _, ok := installed[body.Model]; !ok {
			jsonError(w, 400, "model not installed: "+body.Model)
			return
		}
	}
	s.cfg.Backend.Type = "managed"
	s.cfg.Backend.BuiltinModel = body.Model
	if err := s.saveConfig(&s.cfg); err != nil {
		jsonError(w, 500, "save config: "+err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"activated":        true,
		"model":            body.Model,
		"requires_restart": true,
	})
}

// handleBuiltinDeleteModel removes a built-in model from the lock file and deletes
// its file from disk (best-effort).
//
//	DELETE /api/v1/builtin/models/{name}
func (s *Server) handleBuiltinDeleteModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || !validModelName.MatchString(name) {
		jsonError(w, http.StatusBadRequest, "invalid model name")
		return
	}
	if s.modelStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "model store not available")
		return
	}
	installed, err := s.modelStore.Installed()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "load model store: "+err.Error())
		return
	}
	entry, ok := installed[name]
	if !ok {
		jsonError(w, http.StatusNotFound, "model not installed: "+name)
		return
	}
	if err := s.modelStore.Remove(name); err != nil {
		jsonError(w, http.StatusInternalServerError, "remove lock entry: "+err.Error())
		return
	}
	// entry.Path is set at install time from the lock file, never from user input.
	if entry.Path != "" {
		if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
			slog.Warn("builtin model: file delete failed", "path", entry.Path, "err", err)
		}
	}
	jsonOK(w, map[string]bool{"deleted": true})
}
