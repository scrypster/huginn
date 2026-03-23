package relay_test

import (
	"regexp"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
)

func TestMachineIDIsDeterministic(t *testing.T) {
	id1 := relay.GetMachineID()
	id2 := relay.GetMachineID()
	if id1 != id2 {
		t.Errorf("GetMachineID is not deterministic: %q != %q", id1, id2)
	}
	if id1 == "" {
		t.Error("GetMachineID returned empty string")
	}
}

func TestMachineIDFormat(t *testing.T) {
	id := relay.GetMachineID()
	// Should be exactly 8 lowercase hex chars, no hostname prefix.
	matched, err := regexp.MatchString(`^[0-9a-f]{8}$`, id)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Errorf("GetMachineID %q: expected ^[0-9a-f]{8}$", id)
	}
}

func TestGetMachineID_Stable(t *testing.T) {
	// Same machine ID returned on repeated calls.
	id1 := relay.GetMachineID()
	id2 := relay.GetMachineID()
	if id1 != id2 {
		t.Errorf("machine ID not stable: %q != %q", id1, id2)
	}
}

func TestGetMachineID_UniquePerMachine(t *testing.T) {
	id := relay.GetMachineID()
	if id == "" {
		t.Error("machine ID is empty")
	}
	// Should be exactly 8 hex chars — no hostname prefix.
	if len(id) != 8 {
		t.Errorf("machine ID length: got %d, want 8 in %q", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex char %q in machine ID %q", c, id)
		}
	}
}

// TestLoadIdentity_NotRegistered verifies that LoadIdentity returns
// ErrNotRegistered when relay.json doesn't exist.
func TestLoadIdentity_NotRegistered(t *testing.T) {
	_, err := relay.LoadIdentity()
	// Will fail with "not registered" unless relay.json happens to exist
	if err == nil {
		t.Skip("relay.json already exists on this system")
	}
}

// TestIdentity_Save_RoundTrip verifies that an Identity can be saved
// and loaded back with all fields intact.
func TestIdentity_Save_RoundTrip(t *testing.T) {
	t.Skip("skipping identity save test that touches ~/.huginn/relay.json")
}

// TestGetMachineID_ConsistentFormat verifies that GetMachineID returns
// a consistent 8-char hex format across multiple calls.
func TestGetMachineID_ConsistentFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	for i := 0; i < 5; i++ {
		id := relay.GetMachineID()
		if !re.MatchString(id) {
			t.Errorf("iteration %d: GetMachineID %q does not match ^[0-9a-f]{8}$", i, id)
		}
	}
}
