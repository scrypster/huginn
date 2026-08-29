package checkpoint

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// HTTPHandler returns a self-contained http.Handler exposing the run
// checkpoint REST surface. It has no dependency on internal/server's
// Server type — the orchestrator setup path mounts it directly:
//
//	mux.Handle("/api/v1/checkpoints/", http.StripPrefix("/api/v1/checkpoints", checkpoint.HTTPHandler(mgr)))
//
// Routes (all JSON):
//
//	GET  /                     -> list recent runs (?limit=N)
//	GET  /{threadID}           -> one run's ledger record
//	GET  /{threadID}/diff      -> unified diff for the run
//	POST /{threadID}/revert    -> revert the run; body: {"all":bool,"only_paths":[...],"allow_after_push":bool}
//	POST /gc                   -> garbage-collect old runs; body: {"keep_runs":N,"max_age_seconds":N}
func HTTPHandler(m *Manager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		runs, err := m.List(r.Context(), limit)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, runs)
	})
	mux.HandleFunc("POST /gc", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			KeepRuns      int   `json:"keep_runs"`
			MaxAgeSeconds int64 `json:"max_age_seconds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // empty body = defaults
		opts := GCOptions{KeepRuns: body.KeepRuns}
		if body.MaxAgeSeconds > 0 {
			opts.MaxAge = time.Duration(body.MaxAgeSeconds) * time.Second
		}
		result, err := m.GC(r.Context(), opts)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /{threadID}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("threadID")
		rec, err := m.Get(r.Context(), id)
		if err != nil {
			writeJSONError(w, statusForErr(err), err)
			return
		}
		writeJSON(w, http.StatusOK, rec)
	})
	mux.HandleFunc("GET /{threadID}/diff", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("threadID")
		diff, err := m.DiffRun(r.Context(), id)
		if err != nil {
			writeJSONError(w, statusForErr(err), err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(diff))
	})
	mux.HandleFunc("POST /{threadID}/revert", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("threadID")
		var body struct {
			All            bool     `json:"all"`
			OnlyPaths      []string `json:"only_paths"`
			AllowAfterPush bool     `json:"allow_after_push"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		result, err := m.RevertRun(r.Context(), id, RevertOptions{
			All:            body.All,
			OnlyPaths:      body.OnlyPaths,
			AllowAfterPush: body.AllowAfterPush,
		})
		if err != nil {
			writeJSONError(w, statusForErr(err), err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	return mux
}

func statusForErr(err error) int {
	switch {
	case err == ErrRunNotFound:
		return http.StatusNotFound
	case err == ErrAlreadyPushed:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
