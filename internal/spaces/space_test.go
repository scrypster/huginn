package spaces

import (
	"encoding/json"
	"strings"
	"testing"
)

// The hallway list endpoint must not drop the diff captured by write/edit
// tools — the DiffCard hydrates from this projection after a reload
// (live gap found on v0.4.0-fable14, 2026-08-28).
func TestSpaceMessageToolCall_DiffRoundTrips(t *testing.T) {
	stored := `[{"id":"t1","name":"edit_file","diff":{"path":"a.go","unified":"-x\n+y","added":1,"removed":1}}]`
	var tcs []SpaceMessageToolCall
	if err := json.Unmarshal([]byte(stored), &tcs); err != nil {
		t.Fatal(err)
	}
	if len(tcs) != 1 || tcs[0].Diff == nil || tcs[0].Diff["path"] != "a.go" {
		t.Fatalf("diff dropped in projection: %+v", tcs)
	}
	out, _ := json.Marshal(tcs)
	if !strings.Contains(string(out), `"unified":"-x\n+y"`) {
		t.Fatalf("diff not re-serialized: %s", out)
	}
}
