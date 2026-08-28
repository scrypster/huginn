package backend

import "testing"

// liveWinstonEchoAsk is the 2026-08-27 huginn-dev161 reproduction: a
// statement-shaped ask persisted back verbatim as if it were Winston's own
// reply, with only the @mention stripped and the first letter re-cased.
const liveWinstonEchoAsk = "@Winston for the record: our staging server is called valkyrie and deploys happen on Fridays."
const liveWinstonEcho = "For the record: our staging server is called valkyrie and deploys happen on Fridays."

func TestEchoAckRewrite_NearVerbatimEchoRewritten(t *testing.T) {
	got := EchoAckRewrite(liveWinstonEcho, liveWinstonEchoAsk)
	if got != "Noted." {
		t.Fatalf("got %q, want a short ack", got)
	}
}

func TestPersistVisibleAssistantContent_EchoAskRewrittenToAck(t *testing.T) {
	got := PersistVisibleAssistantContent(liveWinstonEcho, liveWinstonEchoAsk)
	if got != "Noted." {
		t.Fatalf("got %q, want a short ack", got)
	}
}

func TestEchoAckRewrite_ExactEchoWithoutMention(t *testing.T) {
	ask := "our deploy window is Fridays at 5pm."
	got := EchoAckRewrite(ask, ask)
	if got != "Noted." {
		t.Fatalf("got %q, want a short ack", got)
	}
}

func TestEchoAckRewrite_RealTeammateReplyUnchanged(t *testing.T) {
	ask := "@Winston what's the staging server called?"
	reply := "It's called valkyrie."
	got := EchoAckRewrite(reply, ask)
	if got != reply {
		t.Fatalf("real reply rewritten: got %q, want %q", got, reply)
	}
}

func TestEchoAckRewrite_ShortRepliesNeverRewritten(t *testing.T) {
	for _, tc := range []struct{ ask, reply string }{
		{"ping", "Pong."},
		{"thanks", "You're welcome."},
		{"yes", "yes"},
	} {
		if got := EchoAckRewrite(tc.reply, tc.ask); got != tc.reply {
			t.Errorf("ask=%q reply=%q rewritten to %q", tc.ask, tc.reply, got)
		}
	}
}

func TestEchoAckRewrite_UnrelatedLongReplyUnchanged(t *testing.T) {
	ask := "@Winston for the record: our staging server is called valkyrie and deploys happen on Fridays."
	reply := "Got it — I'll keep that in mind for future deploy questions."
	got := EchoAckRewrite(reply, ask)
	if got != reply {
		t.Fatalf("unrelated reply rewritten: got %q, want %q", got, reply)
	}
}

func TestEchoAckRewrite_NearVerbatimWithMinorRewordingRewritten(t *testing.T) {
	ask := "@Winston our staging server is called valkyrie and deploys happen every Friday."
	// Model reorders/paraphrases slightly but is still substantially the
	// same sentence echoed back, not an acknowledgment.
	reply := "Our staging server is called valkyrie, and deploys happen every Friday."
	got := EchoAckRewrite(reply, ask)
	if got != "Noted." {
		t.Fatalf("got %q, want a short ack", got)
	}
}
