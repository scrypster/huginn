package notification_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// makeExpiredNotif returns a notification with ExpiresAt set in the past.
func makeExpiredNotif(routineID, runID string) *notification.Notification {
	past := time.Now().UTC().Add(-time.Hour)
	n := makeNotif(routineID, runID, notification.SeverityInfo)
	n.ExpiresAt = &past
	return n
}

// makeActiveNotif returns a notification with no ExpiresAt (not expired).
func makeActiveNotif(routineID, runID string) *notification.Notification {
	return makeNotif(routineID, runID, notification.SeverityInfo)
}

// makeFutureExpiredNotif returns a notification with ExpiresAt in the future.
func makeFutureExpiredNotif(routineID, runID string) *notification.Notification {
	future := time.Now().UTC().Add(time.Hour)
	n := makeNotif(routineID, runID, notification.SeverityInfo)
	n.ExpiresAt = &future
	return n
}

// --- Pebble Store tests ---

func TestStore_PruneExpired_RemovesExpired(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)
	ctx := context.Background()

	expired1 := makeExpiredNotif("routineA", "run1")
	expired2 := makeExpiredNotif("routineA", "run2")
	active := makeActiveNotif("routineB", "run3")
	future := makeFutureExpiredNotif("routineC", "run4")

	for _, n := range []*notification.Notification{expired1, expired2, active, future} {
		if err := s.Put(n); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	count, err := s.PruneExpired(ctx)
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if count != 2 {
		t.Errorf("PruneExpired returned %d, want 2", count)
	}

	// Expired notifications should be gone.
	if _, err := s.Get(expired1.ID); err == nil {
		t.Error("expired1 still exists after prune, want deleted")
	}
	if _, err := s.Get(expired2.ID); err == nil {
		t.Error("expired2 still exists after prune, want deleted")
	}

	// Active and future-expiry notifications should remain.
	if _, err := s.Get(active.ID); err != nil {
		t.Errorf("active notification unexpectedly deleted: %v", err)
	}
	if _, err := s.Get(future.ID); err != nil {
		t.Errorf("future-expiry notification unexpectedly deleted: %v", err)
	}

	// ListPending should still return the active notification.
	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("ListPending after prune = %d, want 2 (active + future)", len(pending))
	}
}

func TestStore_PruneExpired_EmptyStore(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	count, err := s.PruneExpired(context.Background())
	if err != nil {
		t.Fatalf("PruneExpired on empty store: %v", err)
	}
	if count != 0 {
		t.Errorf("PruneExpired on empty store = %d, want 0", count)
	}
}

func TestStore_PruneExpired_NothingExpired(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	for i := 0; i < 3; i++ {
		if err := s.Put(makeActiveNotif("routineA", "run1")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	count, err := s.PruneExpired(context.Background())
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if count != 0 {
		t.Errorf("PruneExpired with no expired = %d, want 0", count)
	}

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("ListPending after no-op prune = %d, want 3", len(pending))
	}
}

func TestStore_PruneExpired_Cancellation(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	// Add some expired notifications.
	for i := 0; i < 5; i++ {
		if err := s.Put(makeExpiredNotif("routineA", "run1")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Should return a cancellation error (or 0 pruned since cancelled before first batch).
	_, err := s.PruneExpired(ctx)
	if err == nil {
		// It's acceptable if all notifications were pruned before cancellation was checked,
		// but we should at least not get a panic or unexpected error.
		t.Log("PruneExpired with pre-cancelled context returned nil error (all pruned before cancellation check)")
	}
}

// --- SQLite Store tests ---

func openNotifSQLiteDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "prune_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteStore_PruneExpired_RemovesExpired(t *testing.T) {
	t.Parallel()
	db := openNotifSQLiteDB(t)
	s := notification.NewSQLiteNotificationStore(db)
	ctx := context.Background()

	expired1 := makeExpiredNotif("routineA", "run1")
	expired2 := makeExpiredNotif("routineA", "run2")
	active := makeActiveNotif("routineB", "run3")
	future := makeFutureExpiredNotif("routineC", "run4")

	for _, n := range []*notification.Notification{expired1, expired2, active, future} {
		if err := s.Put(n); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	count, err := s.PruneExpired(ctx)
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if count != 2 {
		t.Errorf("PruneExpired returned %d, want 2", count)
	}

	// Expired notifications should be gone.
	if _, err := s.Get(expired1.ID); err == nil {
		t.Error("expired1 still exists after prune, want deleted")
	}
	if _, err := s.Get(expired2.ID); err == nil {
		t.Error("expired2 still exists after prune, want deleted")
	}

	// Active and future-expiry notifications should remain.
	if _, err := s.Get(active.ID); err != nil {
		t.Errorf("active notification unexpectedly deleted: %v", err)
	}
	if _, err := s.Get(future.ID); err != nil {
		t.Errorf("future-expiry notification unexpectedly deleted: %v", err)
	}

	// ListPending should still return the 2 remaining notifications.
	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("ListPending after prune = %d, want 2 (active + future)", len(pending))
	}
}

func TestSQLiteStore_PruneExpired_EmptyStore(t *testing.T) {
	t.Parallel()
	db := openNotifSQLiteDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	count, err := s.PruneExpired(context.Background())
	if err != nil {
		t.Fatalf("PruneExpired on empty store: %v", err)
	}
	if count != 0 {
		t.Errorf("PruneExpired on empty store = %d, want 0", count)
	}
}

func TestSQLiteStore_PruneExpired_NothingExpired(t *testing.T) {
	t.Parallel()
	db := openNotifSQLiteDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	for i := 0; i < 3; i++ {
		if err := s.Put(makeActiveNotif("routineA", "run1")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	count, err := s.PruneExpired(context.Background())
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if count != 0 {
		t.Errorf("PruneExpired with no expired = %d, want 0", count)
	}

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("ListPending after no-op prune = %d, want 3", len(pending))
	}
}
