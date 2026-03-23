package tui

import (
	"testing"
)

// TestHashFileTrigger_IsFileSyntax verifies that the # prefix is
// recognised as a file attachment trigger (not sent as a raw message).
func TestHashFileTrigger_IsFileSyntax(t *testing.T) {
	cases := []struct {
		input  string
		isFile bool
	}{
		{"#main.go", true},
		{"#internal/tools/write_file.go", true},
		{"@Stacy fix the bug", false}, // delegation — not file
		{"just a message", false},
		{"", false},
	}

	for _, tc := range cases {
		got := isFileAttachmentInput(tc.input)
		if got != tc.isFile {
			t.Errorf("isFileAttachmentInput(%q) = %v, want %v", tc.input, got, tc.isFile)
		}
	}
}
