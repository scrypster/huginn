package threadmgr

import "strings"

// maxFollowUpSummaryLen caps how much of the delegate's narrative is embedded
// in the lead agent's synthesis prompt — the full report lives in the thread
// panel; the lead writes a brief synthesis, not a regurgitation.
const maxFollowUpSummaryLen = 1200

// BuildFollowUpPrompt constructs the synthesis prompt sent to the lead agent
// after a delegated thread finishes. It leads with an honest outcome line —
// an errored or timed-out delegate must not be presented to the user as a
// completed task — and surfaces the structured result fields (files modified,
// key decisions, artifacts) the lead needs to decide next steps.
func BuildFollowUpPrompt(completedAgentID string, summary *FinishSummary) string {
	summaryText := strings.TrimSpace(summary.Summary)
	truncNote := ""
	if len(summaryText) > maxFollowUpSummaryLen {
		summaryText = summaryText[:maxFollowUpSummaryLen]
		truncNote = "\n\n(Full report is available in the thread panel.)"
	}

	var outcome string
	switch summary.Status {
	case "error":
		outcome = completedAgentID + "'s task FAILED with an error. What they reported before failing:"
	case "completed-with-timeout":
		outcome = completedAgentID + " stopped early (timeout or turn limit) and may have left work unfinished. What they got done:"
	case "blocked", "needs_review":
		outcome = completedAgentID + " stopped with status " + summary.Status + ". Their report:"
	default:
		outcome = completedAgentID + " has completed their task. Key findings:"
	}

	var details strings.Builder
	if len(summary.FilesModified) > 0 {
		details.WriteString("\nFiles modified: " + strings.Join(summary.FilesModified, ", "))
	}
	if len(summary.KeyDecisions) > 0 {
		details.WriteString("\nKey decisions: " + strings.Join(summary.KeyDecisions, "; "))
	}
	if len(summary.Artifacts) > 0 {
		details.WriteString("\nArtifacts: " + strings.Join(summary.Artifacts, ", "))
	}

	return outcome + "\n\n" + summaryText + details.String() + truncNote +
		"\n\nPlease give the user a brief synthesis (3-5 sentences max) in your own words. Be honest about failures or unfinished work. Do NOT repeat the full report — just the key takeaways and recommended next steps."
}
