package threadmgr

import (
	"encoding/json"
	"sync"
	"testing"
)

// TestListBySession_NoRaceWithAttachVetResult proves ListBySession returns a
// deep copy of a thread's Summary, matching Get. Before the fix,
// ListBySession did `cp := *t` and left cp.Summary aliasing the live
// *FinishSummary; a concurrent AttachVetResult mutating that same
// FinishSummary's fields (Summary, VetLabel, VetFindings) while this
// goroutine json-marshals the "copy" it got from ListBySession raced under
// -race — reads through the aliased pointer, writes through the live one.
func TestListBySession_NoRaceWithAttachVetResult(t *testing.T) {
	tm := New()
	th, err := tm.Create(CreateParams{SessionID: "s1", AgentID: "coder", Task: "x"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.Start(th.ID, nil, func() {})
	tm.Complete(th.ID, FinishSummary{Summary: "done", Status: "completed"})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		tm.AttachVetResult(th.ID, "3 findings", "- something is wrong")
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			for _, t := range tm.ListBySession("s1") {
				if _, err := json.Marshal(t.Summary); err != nil {
					panic(err)
				}
			}
		}
	}()

	wg.Wait()
}
