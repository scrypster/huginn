package agent

import (
	"context"
	"testing"
)

// TestChatWithAgent_TrivialAskRecordsTurnMetrics verifies completeTrivialAsk
// (the tools-free hallway path RunLoop never sees) still produces a
// turn_metrics row — otherwise the prompt-budget win it exists for is
// invisible in telemetry (D2). turn_kind must be "trivial" and PromptChars
// must reflect the actual (small) skeleton-persona prompt, not be zero/absent.
func TestChatWithAgent_TrivialAskRecordsTurnMetrics(t *testing.T) {
	w := newTestTurnMetricsWriter(t)
	o, _, ag := leadWithDelegationTools(t)
	o.SetTurnMetricsWriter(w)

	if err := o.ChatWithAgent(context.Background(), ag, "what time is it", "sess-trivial-metrics", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	row := waitForTurnMetricsRow(t, w)
	if row.TurnKind != "trivial" {
		t.Errorf("turn_kind = %q, want trivial", row.TurnKind)
	}
	if row.SessionID != "sess-trivial-metrics" {
		t.Errorf("session_id = %q, want sess-trivial-metrics", row.SessionID)
	}
	if row.PromptChars <= 0 {
		t.Errorf("prompt_chars = %d, want > 0", row.PromptChars)
	}
	if row.IsError {
		t.Error("expected is_error=false for a successful trivial turn")
	}
}

// TestChatWithAgent_TrivialAskMetrics_HadFirstToken verifies the streamed
// token from the trivial-ask backend call stamps had_first_token=true —
// the completeTrivialAsk path uses backend.ChatCompletion's OnToken callback
// directly, so the hook must be wired through it the same way RunLoop wires
// it through OnToken/OnEvent.
func TestChatWithAgent_TrivialAskMetrics_HadFirstToken(t *testing.T) {
	w := newTestTurnMetricsWriter(t)
	o, _, ag := leadWithDelegationTools(t)
	o.SetTurnMetricsWriter(w)

	if err := o.ChatWithAgent(context.Background(), ag, "what time is it", "sess-trivial-ft", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	row := waitForTurnMetricsRow(t, w)
	if !row.HadFirstToken {
		t.Error("expected had_first_token=true for a streamed trivial turn")
	}
}
