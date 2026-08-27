package agent

import (
	"strings"
	"testing"
)

func TestBuildDeskFloorContextBlock_NoOthers_Empty(t *testing.T) {
	if got := BuildDeskFloorContextBlock("Steve", []SpaceMember{{Name: "Steve"}}); got != "" {
		t.Fatalf("lone desk DM must stay quiet, got %q", got)
	}
	if got := BuildDeskFloorContextBlock("Steve", nil); got != "" {
		t.Fatalf("empty peers must stay quiet, got %q", got)
	}
}

func TestBuildDeskFloorContextBlock_ListsWinston(t *testing.T) {
	got := BuildDeskFloorContextBlock("Steve", []SpaceMember{
		{Name: "Steve"},
		{Name: "Winston"},
	})
	if got == "" {
		t.Fatal("expected desk floor block")
	}
	if !strings.Contains(got, "[Desk Floor]") {
		t.Errorf("missing heading: %s", got)
	}
	if !strings.Contains(got, "Winston") {
		t.Errorf("missing Winston: %s", got)
	}
	if !strings.Contains(got, "delegate_to_agent") {
		t.Errorf("missing delegate_to_agent: %s", got)
	}
	if strings.Contains(got, "**Steve**") {
		t.Errorf("must not list self as a reachable peer: %s", got)
	}
}
