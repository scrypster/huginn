// internal/threadmgr/delegate_integration_test.go
package threadmgr_test

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// fakeDelegationStore captures InsertDelegation calls for assertion.
type fakeDelegationStore struct {
	inserted []session.DelegationRecord
}

func (f *fakeDelegationStore) InsertDelegation(d session.DelegationRecord) error {
	f.inserted = append(f.inserted, d)
	return nil
}

func (f *fakeDelegationStore) GetDelegation(id string) (*session.DelegationRecord, error) {
	return nil, nil
}

func (f *fakeDelegationStore) FindDelegationByThread(threadID string) (*session.DelegationRecord, error) {
	return nil, nil
}

func (f *fakeDelegationStore) ListDelegationsBySession(sessionID string, limit, offset int) ([]session.DelegationRecord, error) {
	return nil, nil
}

func (f *fakeDelegationStore) UpdateDelegationStatus(id, status, result string, startedAt *time.Time, completedAt *time.Time) error {
	return nil
}

func (f *fakeDelegationStore) ReconcileOrphanDelegations() error {
	return nil
}

func TestCallingAgentContext_DelegateFnPattern(t *testing.T) {
	store := &fakeDelegationStore{}

	// Simulate the DelegateFn pattern: context carries calling agent name,
	// and a delegation record is inserted after thread creation.
	simulateDelegateFn := func(ctx context.Context, agentName, task, sessionID, threadID string) {
		fromAgent := threadmgr.GetCallingAgent(ctx)
		if store != nil && fromAgent != "" {
			rec := session.DelegationRecord{
				ID:        session.NewID(),
				SessionID: sessionID,
				ThreadID:  threadID,
				FromAgent: fromAgent,
				ToAgent:   agentName,
				Task:      task,
				Status:    "pending",
				CreatedAt: time.Now(),
				StartedAt: time.Now(),
			}
			if err := store.InsertDelegation(rec); err != nil {
				t.Fatalf("InsertDelegation failed: %v", err)
			}
		}
	}

	ctx := threadmgr.SetCallingAgent(context.Background(), "Atlas")
	simulateDelegateFn(ctx, "Coder", "write the handler", "sess-1", "thread-1")

	if len(store.inserted) != 1 {
		t.Fatalf("expected 1 inserted record, got %d", len(store.inserted))
	}

	rec := store.inserted[0]
	if rec.FromAgent != "Atlas" {
		t.Errorf("FromAgent: want Atlas, got %q", rec.FromAgent)
	}
	if rec.ToAgent != "Coder" {
		t.Errorf("ToAgent: want Coder, got %q", rec.ToAgent)
	}
	if rec.SessionID != "sess-1" {
		t.Errorf("SessionID: want sess-1, got %q", rec.SessionID)
	}
	if rec.ThreadID != "thread-1" {
		t.Errorf("ThreadID: want thread-1, got %q", rec.ThreadID)
	}
	if rec.Task != "write the handler" {
		t.Errorf("Task: want 'write the handler', got %q", rec.Task)
	}
	if rec.ID == "" {
		t.Error("ID should be non-empty (ULID)")
	}
}

func TestCallingAgentContext_NoInsertWhenEmpty(t *testing.T) {
	store := &fakeDelegationStore{}

	// When no calling agent is set, no record should be inserted.
	simulateDelegateFn := func(ctx context.Context) {
		fromAgent := threadmgr.GetCallingAgent(ctx)
		if store != nil && fromAgent != "" {
			store.InsertDelegation(session.DelegationRecord{}) //nolint
		}
	}

	simulateDelegateFn(context.Background()) // no SetCallingAgent called

	if len(store.inserted) != 0 {
		t.Errorf("expected 0 inserts when FromAgent is empty, got %d", len(store.inserted))
	}
}
