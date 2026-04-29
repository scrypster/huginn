package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWriteDeliveryFailure_CreatesFile verifies that WriteDeliveryFailure
// (the synchronous inner path) writes a JSONL record to disk.
func TestWriteDeliveryFailure_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	rec := DeliveryFailureRecord{
		Ts:         time.Now().UTC().Format(time.RFC3339),
		WorkflowID: "wf-123",
		RunID:      "run-456",
		URL:        "https://hooks.example.com/notify",
		Attempts:   3,
		LastError:  "connection refused",
	}

	if err := appendDeliveryFailure(dir, rec); err != nil {
		t.Fatalf("appendDeliveryFailure: %v", err)
	}

	day := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, "delivery-failures", day+".jsonl")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected dead-letter file at %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dead-letter file: %v", err)
	}
	content := string(data)

	if content == "" {
		t.Fatal("dead-letter file is empty")
	}
	if len(data) < 10 {
		t.Errorf("dead-letter file too small (%d bytes)", len(data))
	}
}

// TestReadDeliveryFailures_Empty verifies that an empty / missing directory
// returns an empty slice rather than an error.
func TestReadDeliveryFailures_Empty(t *testing.T) {
	dir := t.TempDir()

	records, err := ReadDeliveryFailures(dir, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records from empty dir, got %d", len(records))
	}
}

// TestReadDeliveryFailures_ReturnsRecords verifies that records written by
// appendDeliveryFailure are returned by ReadDeliveryFailures.
func TestReadDeliveryFailures_ReturnsRecords(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		rec := DeliveryFailureRecord{
			Ts:         time.Now().UTC().Format(time.RFC3339),
			WorkflowID: "wf-test",
			RunID:      "run-test",
			URL:        "https://hooks.example.com/notify",
			Attempts:   3,
			LastError:  "timeout",
		}
		if err := appendDeliveryFailure(dir, rec); err != nil {
			t.Fatalf("appendDeliveryFailure[%d]: %v", i, err)
		}
	}

	records, err := ReadDeliveryFailures(dir, 100)
	if err != nil {
		t.Fatalf("ReadDeliveryFailures: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
	for _, r := range records {
		if r.WorkflowID != "wf-test" {
			t.Errorf("unexpected workflow_id: %q", r.WorkflowID)
		}
		if r.Attempts != 3 {
			t.Errorf("unexpected attempts: %d", r.Attempts)
		}
	}
}

// TestReadDeliveryFailures_LimitEnforced verifies that the limit parameter
// caps the number of returned records.
func TestReadDeliveryFailures_LimitEnforced(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 10; i++ {
		rec := DeliveryFailureRecord{
			Ts:         time.Now().UTC().Format(time.RFC3339),
			WorkflowID: "wf-limit",
			RunID:      "run-limit",
			URL:        "https://hooks.example.com/notify",
			Attempts:   3,
			LastError:  "timeout",
		}
		if err := appendDeliveryFailure(dir, rec); err != nil {
			t.Fatalf("appendDeliveryFailure: %v", err)
		}
	}

	records, err := ReadDeliveryFailures(dir, 5)
	if err != nil {
		t.Fatalf("ReadDeliveryFailures: %v", err)
	}
	if len(records) > 5 {
		t.Errorf("expected at most 5 records with limit=5, got %d", len(records))
	}
}

// TestReadDeliveryFailures_IgnoresOldFiles verifies that files older than 7
// days are excluded from the results.
func TestReadDeliveryFailures_IgnoresOldFiles(t *testing.T) {
	dir := t.TempDir()
	failuresDir := filepath.Join(dir, "delivery-failures")
	if err := os.MkdirAll(failuresDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write an old file (8 days ago).
	oldDay := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02")
	oldPath := filepath.Join(failuresDir, oldDay+".jsonl")
	oldContent := `{"ts":"2024-01-01T00:00:00Z","workflow_id":"old-wf","run_id":"old-run","url":"https://example.com","attempts":3,"last_error":"old"}` + "\n"
	if err := os.WriteFile(oldPath, []byte(oldContent), 0644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	// Write a recent file (today).
	rec := DeliveryFailureRecord{
		Ts:         time.Now().UTC().Format(time.RFC3339),
		WorkflowID: "new-wf",
		RunID:      "new-run",
		URL:        "https://hooks.example.com/notify",
		Attempts:   3,
		LastError:  "timeout",
	}
	if err := appendDeliveryFailure(dir, rec); err != nil {
		t.Fatalf("appendDeliveryFailure: %v", err)
	}

	records, err := ReadDeliveryFailures(dir, 100)
	if err != nil {
		t.Fatalf("ReadDeliveryFailures: %v", err)
	}

	// The old file's record must not appear.
	for _, r := range records {
		if r.WorkflowID == "old-wf" {
			t.Errorf("old delivery failure (>7 days) should not appear in results")
		}
	}
	// The new record must appear.
	found := false
	for _, r := range records {
		if r.WorkflowID == "new-wf" {
			found = true
		}
	}
	if !found {
		t.Error("new delivery failure not found in results")
	}
}

func TestWriteDeliveryFailure_AppendsViaAsyncWrapper(t *testing.T) {
	dir := t.TempDir()
	WriteDeliveryFailure(dir, "wf-async", "run-async", "https://hooks.example.com/notify", 5, "timeout")

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		records, err := ReadDeliveryFailures(dir, 10)
		if err != nil {
			t.Fatalf("ReadDeliveryFailures: %v", err)
		}
		if len(records) > 0 {
			if records[0].WorkflowID != "wf-async" {
				t.Fatalf("workflow_id = %q, want %q", records[0].WorkflowID, "wf-async")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected async WriteDeliveryFailure record within 8s")
}

func TestMarkDeliveryFailureRetried_FiltersOriginalFailure(t *testing.T) {
	dir := t.TempDir()
	rec := DeliveryFailureRecord{
		Ts:         time.Now().UTC().Format(time.RFC3339),
		WorkflowID: "wf-retry",
		RunID:      "run-retry",
		URL:        "https://hooks.example.com/notify",
		Attempts:   3,
		LastError:  "server error",
	}
	if err := appendDeliveryFailure(dir, rec); err != nil {
		t.Fatalf("appendDeliveryFailure: %v", err)
	}

	MarkDeliveryFailureRetried(dir, rec.WorkflowID, rec.RunID, rec.URL)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		records, err := ReadDeliveryFailures(dir, 10)
		if err != nil {
			t.Fatalf("ReadDeliveryFailures: %v", err)
		}
		if len(records) == 0 {
			return // original failure is filtered once retry marker exists
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected retried failure to be filtered from actionable list")
}
