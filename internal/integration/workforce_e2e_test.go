package integration_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/workforce"
)

// openWorkforceDB opens a fresh SQLite DB with full schema and workstream
// migrations applied. Returns the DB; caller is responsible for cleanup via t.Cleanup.
func openWorkforceDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "workforce.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	// WorkstreamMigrations are already included in the embedded schema via
	// ApplySchema (workstreams + workstream_sessions tables use IF NOT EXISTS),
	// but we run them here for idempotency correctness.
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("Migrate workstreams: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertSession pre-populates the sessions table so foreign-key constraints
// on the artifacts table are satisfied.
func insertSession(t *testing.T, db *sqlitedb.DB, sessionID string) {
	t.Helper()
	if _, err := db.Write().Exec(`INSERT OR IGNORE INTO sessions (id) VALUES (?)`, sessionID); err != nil {
		t.Fatalf("insert session %s: %v", sessionID, err)
	}
}

// TestWorkforceE2E exercises the full artifact + workstream lifecycle.
func TestWorkforceE2E(t *testing.T) {
	ctx := context.Background()
	db := openWorkforceDB(t)
	artifactsDir := t.TempDir()

	artStore := artifact.NewStore(db.Write(), artifactsDir)
	wsStore := spaces.NewWorkstreamStore(db)

	// Step 1: Create a workstream.
	ws, err := wsStore.Create(ctx, "e2e-project", "End-to-end test workstream")
	if err != nil {
		t.Fatalf("Create workstream: %v", err)
	}
	if ws.ID == "" {
		t.Fatal("workstream ID should be non-empty")
	}

	// Step 2: Pre-insert sessions and write 3 artifacts to different sessions.
	sessions := []string{"e2e-sess-A", "e2e-sess-B", "e2e-sess-C"}
	artifactIDs := make([]string, 3)

	for i, sessID := range sessions {
		insertSession(t, db, sessID)
		a := &workforce.Artifact{
			Kind:      workforce.KindDocument,
			Title:     fmt.Sprintf("Artifact for %s", sessID),
			MimeType:  "text/plain",
			AgentName: "e2e-agent",
			SessionID: sessID,
			Content:   []byte(fmt.Sprintf("content for session %s", sessID)),
		}
		if err := artStore.Write(ctx, a); err != nil {
			t.Fatalf("Write artifact for %s: %v", sessID, err)
		}
		artifactIDs[i] = a.ID
	}

	// Step 3: Tag all three sessions to the workstream.
	for _, sessID := range sessions {
		if err := wsStore.TagSession(ctx, ws.ID, sessID); err != nil {
			t.Fatalf("TagSession %s: %v", sessID, err)
		}
	}

	// Step 4: List workstream sessions and verify all 3 are present.
	sessionList, err := wsStore.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessionList) != 3 {
		t.Fatalf("expected 3 sessions in workstream, got %d", len(sessionList))
	}
	sessSet := make(map[string]bool, len(sessionList))
	for _, s := range sessionList {
		sessSet[s] = true
	}
	for _, sessID := range sessions {
		if !sessSet[sessID] {
			t.Errorf("session %s missing from workstream session list", sessID)
		}
	}

	// Step 5: Update artifact status — draft→accepted for one, draft→rejected for another.
	if err := artStore.UpdateStatus(ctx, artifactIDs[0], workforce.StatusAccepted, ""); err != nil {
		t.Fatalf("UpdateStatus accepted: %v", err)
	}
	if err := artStore.UpdateStatus(ctx, artifactIDs[1], workforce.StatusRejected, "not quite right"); err != nil {
		t.Fatalf("UpdateStatus rejected: %v", err)
	}

	// Step 6: List artifacts by session and verify statuses.
	listA, err := artStore.ListBySession(ctx, sessions[0], 0, "")
	if err != nil {
		t.Fatalf("ListBySession A: %v", err)
	}
	if len(listA) != 1 {
		t.Fatalf("expected 1 artifact for session A, got %d", len(listA))
	}
	if listA[0].Status != workforce.StatusAccepted {
		t.Errorf("session A artifact: expected status accepted, got %s", listA[0].Status)
	}

	listB, err := artStore.ListBySession(ctx, sessions[1], 0, "")
	if err != nil {
		t.Fatalf("ListBySession B: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected 1 artifact for session B, got %d", len(listB))
	}
	if listB[0].Status != workforce.StatusRejected {
		t.Errorf("session B artifact: expected status rejected, got %s", listB[0].Status)
	}
	if listB[0].RejectionReason != "not quite right" {
		t.Errorf("session B artifact: expected rejection_reason %q, got %q", "not quite right", listB[0].RejectionReason)
	}

	listC, err := artStore.ListBySession(ctx, sessions[2], 0, "")
	if err != nil {
		t.Fatalf("ListBySession C: %v", err)
	}
	if len(listC) != 1 {
		t.Fatalf("expected 1 artifact for session C, got %d", len(listC))
	}
	if listC[0].Status != workforce.StatusDraft {
		t.Errorf("session C artifact: expected status draft, got %s", listC[0].Status)
	}

	// Step 7: DeleteBySession for session A — verify only session A artifacts are removed.
	if err := artStore.DeleteBySession(ctx, sessions[0]); err != nil {
		t.Fatalf("DeleteBySession A: %v", err)
	}

	afterDeleteA, err := artStore.ListBySession(ctx, sessions[0], 0, "")
	if err != nil {
		t.Fatalf("ListBySession A after delete: %v", err)
	}
	if len(afterDeleteA) != 0 {
		t.Errorf("expected 0 artifacts for deleted session A, got %d", len(afterDeleteA))
	}

	// Sessions B and C should still have their artifacts.
	stillB, err := artStore.ListBySession(ctx, sessions[1], 0, "")
	if err != nil {
		t.Fatalf("ListBySession B after A delete: %v", err)
	}
	if len(stillB) != 1 {
		t.Errorf("expected 1 artifact for session B after deleting A, got %d", len(stillB))
	}

	stillC, err := artStore.ListBySession(ctx, sessions[2], 0, "")
	if err != nil {
		t.Fatalf("ListBySession C after A delete: %v", err)
	}
	if len(stillC) != 1 {
		t.Errorf("expected 1 artifact for session C after deleting A, got %d", len(stillC))
	}

	// Step 8: Workstream cascade delete — delete workstream and verify session tags are removed.
	if err := wsStore.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("Delete workstream: %v", err)
	}

	cascadedSessions, err := wsStore.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions after cascade delete: %v", err)
	}
	if len(cascadedSessions) != 0 {
		t.Errorf("expected 0 session tags after workstream cascade delete, got %d", len(cascadedSessions))
	}
}

// TestWorkforceE2E_ConcurrentArtifactWrites launches 10 goroutines that each
// write one artifact to the same session. All writes must succeed and
// ListBySession must return all 10 artifacts.
func TestWorkforceE2E_ConcurrentArtifactWrites(t *testing.T) {
	ctx := context.Background()
	db := openWorkforceDB(t)
	artifactsDir := t.TempDir()

	artStore := artifact.NewStore(db.Write(), artifactsDir)

	const sessID = "concurrent-sess"
	insertSession(t, db, sessID)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			a := &workforce.Artifact{
				Kind:      workforce.KindStructuredData,
				Title:     fmt.Sprintf("concurrent artifact %d", idx),
				MimeType:  "application/json",
				AgentName: "concurrent-agent",
				SessionID: sessID,
				Content:   []byte(fmt.Sprintf(`{"n":%d}`, idx)),
			}
			errs[idx] = artStore.Write(ctx, a)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d Write error: %v", i, err)
		}
	}

	list, err := artStore.ListBySession(ctx, sessID, 200, "")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != n {
		t.Errorf("expected %d artifacts, got %d", n, len(list))
	}
}

// TestWorkforceE2E_ArtifactArchive writes 5 artifacts in terminal states,
// back-dates them past the cutoff, then verifies Archive removes exactly those 5.
// Active (draft/accepted) artifacts must remain untouched.
func TestWorkforceE2E_ArtifactArchive(t *testing.T) {
	ctx := context.Background()
	db := openWorkforceDB(t)
	artifactsDir := t.TempDir()

	artStore := artifact.NewStore(db.Write(), artifactsDir)

	const sessID = "archive-sess"
	insertSession(t, db, sessID)

	// Write 5 terminal-state artifacts.
	terminalStatuses := []workforce.ArtifactStatus{
		workforce.StatusRejected,
		workforce.StatusFailed,
		workforce.StatusSuperseded,
		workforce.StatusRejected,
		workforce.StatusFailed,
	}
	terminalIDs := make([]string, len(terminalStatuses))

	for i, status := range terminalStatuses {
		a := &workforce.Artifact{
			Kind:      workforce.KindDocument,
			Title:     fmt.Sprintf("terminal artifact %d", i),
			AgentName: "archive-agent",
			SessionID: sessID,
			Content:   []byte("terminal content"),
		}
		if err := artStore.Write(ctx, a); err != nil {
			t.Fatalf("Write terminal artifact %d: %v", i, err)
		}
		if err := artStore.UpdateStatus(ctx, a.ID, status, "archive test"); err != nil {
			t.Fatalf("UpdateStatus terminal artifact %d: %v", i, err)
		}
		terminalIDs[i] = a.ID
	}

	// Write 2 active artifacts (draft and accepted) that must NOT be archived.
	activeDraft := &workforce.Artifact{
		Kind:      workforce.KindDocument,
		Title:     "active draft",
		AgentName: "archive-agent",
		SessionID: sessID,
		Content:   []byte("draft content"),
	}
	if err := artStore.Write(ctx, activeDraft); err != nil {
		t.Fatalf("Write active draft: %v", err)
	}

	activeAccepted := &workforce.Artifact{
		Kind:      workforce.KindDocument,
		Title:     "active accepted",
		AgentName: "archive-agent",
		SessionID: sessID,
		Content:   []byte("accepted content"),
	}
	if err := artStore.Write(ctx, activeAccepted); err != nil {
		t.Fatalf("Write active accepted: %v", err)
	}
	if err := artStore.UpdateStatus(ctx, activeAccepted.ID, workforce.StatusAccepted, ""); err != nil {
		t.Fatalf("UpdateStatus accepted: %v", err)
	}

	// Back-date the 5 terminal artifacts so they are older than 2 hours.
	oldTime := time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339Nano)
	for _, id := range terminalIDs {
		if _, err := db.Write().ExecContext(ctx,
			`UPDATE artifacts SET updated_at = ? WHERE id = ?`, oldTime, id,
		); err != nil {
			t.Fatalf("back-date artifact %s: %v", id, err)
		}
	}

	// Archive with a 2-hour cutoff — should remove exactly the 5 terminal artifacts.
	n, err := artStore.Archive(ctx, 2*time.Hour)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 artifacts archived, got %d", n)
	}

	// Verify terminal artifacts are gone.
	for _, id := range terminalIDs {
		_, err := artStore.Read(ctx, id)
		if err == nil {
			t.Errorf("terminal artifact %s should be archived (deleted) but still exists", id)
		}
	}

	// Verify active artifacts are untouched.
	if _, err := artStore.Read(ctx, activeDraft.ID); err != nil {
		t.Errorf("active draft should still exist after archive, got: %v", err)
	}
	if _, err := artStore.Read(ctx, activeAccepted.ID); err != nil {
		t.Errorf("active accepted artifact should still exist after archive, got: %v", err)
	}
}
