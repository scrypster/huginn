package agents

import (
	"testing"
)

// TestIncrementBytes_AllFF tests the overflow path where all bytes are 0xFF.
// This exercises the final `return append(b, 0x00)` branch.
func TestIncrementBytes_AllFF(t *testing.T) {
	b := []byte{0xFF, 0xFF, 0xFF}
	result := incrementBytes(b)
	// When all bytes are 0xFF, the function appends a 0x00 byte.
	// The length should be one more than the original.
	if len(result) != len(b)+1 {
		t.Errorf("expected len %d for all-0xFF input, got %d", len(b)+1, len(result))
	}
	if result[len(result)-1] != 0x00 {
		t.Errorf("expected trailing 0x00, got 0x%02x", result[len(result)-1])
	}
}

// TestIncrementBytes_SingleFF exercises the single 0xFF byte overflow.
func TestIncrementBytes_SingleFF(t *testing.T) {
	b := []byte{0xFF}
	result := incrementBytes(b)
	if len(result) != 2 {
		t.Errorf("expected len 2, got %d", len(result))
	}
}

// TestIncrementBytes_LastByteNotFF exercises the normal (non-overflow) path.
func TestIncrementBytes_LastByteNotFF(t *testing.T) {
	b := []byte{0x61, 0x62} // "ab"
	result := incrementBytes(b)
	if result[len(result)-1] != 0x63 {
		t.Errorf("expected last byte 0x63 ('c'), got 0x%02x", result[len(result)-1])
	}
}

// TestIncrementBytes_MixedFF exercises partial overflow (last byte is 0xFF, rolls over to next).
func TestIncrementBytes_MixedFF(t *testing.T) {
	b := []byte{0x61, 0xFF} // "a\xFF"
	result := incrementBytes(b)
	// last byte 0xFF overflows to 0x00, penultimate byte increments from 0x61 to 0x62
	if result[len(result)-1] != 0x00 {
		t.Errorf("expected last byte 0x00 after overflow, got 0x%02x", result[len(result)-1])
	}
	if result[len(result)-2] != 0x62 {
		t.Errorf("expected penultimate byte 0x62, got 0x%02x", result[len(result)-2])
	}
}
