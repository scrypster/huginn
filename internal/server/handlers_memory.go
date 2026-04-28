// internal/server/handlers_memory.go
package server

import (
	"context"
	"net/http"
)

// handleMemoryReplicationStatus returns replication queue counts from SQLite.
// GET /api/v1/memory/replication-status
// Response: {"pending":N,"failed":N,"dead":N,"connected":bool}
func (s *Server) handleMemoryReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		jsonOK(w, map[string]any{
			"pending":   0,
			"failed":    0,
			"dead":      0,
			"connected": false,
		})
		return
	}

	rows, err := s.db.Read().QueryContext(
		context.Background(),
		`SELECT status, COUNT(*) FROM memory_replication_queue GROUP BY status`,
	)
	if err != nil {
		jsonOK(w, map[string]any{
			"pending":   0,
			"failed":    0,
			"dead":      0,
			"connected": true,
		})
		return
	}
	defer rows.Close()

	result := map[string]int{
		"pending": 0,
		"failed":  0,
		"dead":    0,
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err == nil {
			if _, ok := result[status]; ok {
				result[status] = count
			}
		}
	}

	jsonOK(w, map[string]any{
		"pending":   result["pending"],
		"failed":    result["failed"],
		"dead":      result["dead"],
		"connected": true,
	})
}
