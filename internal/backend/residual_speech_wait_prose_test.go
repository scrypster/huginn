package backend

import (
	"strings"
	"testing"
)

// Live repro 2026-08-28: a Lab thread drawer reply streamed the engine status
// "Waiting for the recall task to complete." as Winston's speech. Prose-form
// wait glue must be stripped like the token form, on the after-tools path the
// thread runner uses.
func TestWaitProseGlueStrippedAfterTools(t *testing.T) {
	cases := []string{
		"Waiting for the recall task to complete.",
		"waiting for Steve to respond.",
		"Waiting for the delegated thread to finish...",
	}
	for _, c := range cases {
		got := VisibleAssistantContentAfterTools(c + " The company is Lab.")
		if strings.Contains(got, "Waiting for") || strings.Contains(got, "waiting for") {
			t.Errorf("wait prose survived after-tools strip: %q -> %q", c, got)
		}
		if !strings.Contains(got, "The company is Lab.") {
			t.Errorf("real answer was eaten: %q -> %q", c, got)
		}
	}
	// Legitimate sentence that merely starts with Waiting must survive.
	keep := "Waiting for your sign-off before I proceed with the merge, since it deletes data."
	if got := VisibleAssistantContentAfterTools(keep); !strings.Contains(got, "sign-off") {
		t.Errorf("legitimate sentence was eaten: %q", got)
	}
}
