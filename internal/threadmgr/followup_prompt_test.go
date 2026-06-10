package threadmgr

import (
	"strings"
	"testing"
)

func TestBuildFollowUpPrompt_StatusOutcomes(t *testing.T) {
	cases := []struct {
		status   string
		want     string
		mustNot  string
	}{
		{"completed", "has completed their task", "FAILED"},
		{"", "has completed their task", "FAILED"},
		{"error", "FAILED with an error", "has completed their task"},
		{"completed-with-timeout", "stopped early", "has completed their task"},
		{"blocked", "stopped with status blocked", "has completed their task"},
		{"needs_review", "stopped with status needs_review", "has completed their task"},
	}
	for _, tc := range cases {
		t.Run("status="+tc.status, func(t *testing.T) {
			got := BuildFollowUpPrompt("Sam", &FinishSummary{Summary: "report text", Status: tc.status})
			if !strings.Contains(got, tc.want) {
				t.Errorf("prompt for status %q missing %q:\n%s", tc.status, tc.want, got)
			}
			if strings.Contains(got, tc.mustNot) {
				t.Errorf("prompt for status %q wrongly contains %q:\n%s", tc.status, tc.mustNot, got)
			}
			if !strings.Contains(got, "report text") {
				t.Errorf("prompt missing the delegate's summary text")
			}
			if !strings.Contains(got, "Be honest about failures") {
				t.Errorf("prompt missing honesty instruction")
			}
		})
	}
}

func TestBuildFollowUpPrompt_StructuredFields(t *testing.T) {
	got := BuildFollowUpPrompt("Sam", &FinishSummary{
		Summary:       "did the thing",
		Status:        "completed",
		FilesModified: []string{"a.go", "b.go"},
		KeyDecisions:  []string{"chose X over Y"},
		Artifacts:     []string{"report.md"},
	})
	for _, want := range []string{
		"Files modified: a.go, b.go",
		"Key decisions: chose X over Y",
		"Artifacts: report.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}

	// Fields absent → no empty section headers.
	bare := BuildFollowUpPrompt("Sam", &FinishSummary{Summary: "x", Status: "completed"})
	for _, header := range []string{"Files modified:", "Key decisions:", "Artifacts:"} {
		if strings.Contains(bare, header) {
			t.Errorf("prompt has %q despite empty field:\n%s", header, bare)
		}
	}
}

func TestBuildFollowUpPrompt_TruncatesLongSummary(t *testing.T) {
	long := strings.Repeat("z", maxFollowUpSummaryLen+500)
	got := BuildFollowUpPrompt("Sam", &FinishSummary{Summary: long, Status: "completed"})
	if !strings.Contains(got, "Full report is available in the thread panel") {
		t.Error("long summary must carry a truncation note")
	}
	if strings.Count(got, "z") > maxFollowUpSummaryLen {
		t.Errorf("summary not truncated: %d z's", strings.Count(got, "z"))
	}

	short := BuildFollowUpPrompt("Sam", &FinishSummary{Summary: "brief", Status: "completed"})
	if strings.Contains(short, "Full report is available") {
		t.Error("short summary must not carry a truncation note")
	}
}
