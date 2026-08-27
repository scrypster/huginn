package spaces_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func TestThreadUnseen_SpectatorSilent(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Chat", "Winston", []string{"Steve"}, "", "")
	humanRoot, err := store.PostSpaceMessage(ch.ID, "hallway", "")
	if err != nil {
		t.Fatal(err)
	}
	// Two agent replies: human never commented on THIS thread? They posted a
	// different root. Use a second root that is agent-only by inserting after
	// a human root they did not write... The spectator case is a thread whose
	// only user-role row is absent. Create a root, then we need an agent-only
	// thread: insert agent messages under a root that is also assistant.
	// PostSpaceMessage always creates user roots. So: post a root (human
	// participated) is NOT spectator. Spectator = replies under a root the
	// human did not write and did not reply to.
	// Simulate by inserting assistant root via raw SQL? InsertSpaceThreadMessage
	// requires parent. Instead: post root, then we consider a *different*
	// thread. Easier: create root as user, then check a thread the human did
	// not join by using a second space message posted... wait.
	//
	// Product rule: "if the human already participated (posted/commented)".
	// A user-role root IS participation. Spectator thread = agent-authored
	// root + agent replies. Insert assistant root by writing parent_id=''
	// through Insert with a trick: Post a user root, then we'll test spectator
	// on a thread we never posted by using a second channel's...
	//
	// Simplest spectator: Post root (human), then we test a *reply thread
	// the human didn't comment on* — but they posted the root, so they ARE
	// a participant.
	//
	// True spectator: two agents talking. Create assistant root via store
	// helper that allows empty parent for assistant? I'll insert via SQL.
	if _, err := db.Write().Exec(`
		INSERT INTO messages (id, container_type, container_id, seq, ts, role, content, agent, parent_id)
		SELECT 'agent-root', 'session', container_id, 99, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'assistant', 'hey steve', 'Winston', ''
		FROM messages WHERE id = ?`, humanRoot.ID); err != nil {
		t.Fatal(err)
	}
	_, err = store.InsertSpaceThreadMessage(ch.ID, "working", "agent-root", "assistant", "Steve")
	if err != nil {
		t.Fatalf("agent reply: %v", err)
	}
	_, err = store.InsertSpaceThreadMessage(ch.ID, "done", "agent-root", "assistant", "Winston")
	if err != nil {
		t.Fatal(err)
	}
	joined, err := store.HasThreadParticipation(ch.ID, "agent-root", spaces.LocalViewer)
	if err != nil {
		t.Fatal(err)
	}
	if joined {
		t.Fatal("spectator must not count as participant")
	}
	unseen, err := store.ThreadUnseenForViewer(ch.ID, "agent-root", spaces.LocalViewer)
	if err != nil {
		t.Fatal(err)
	}
	if unseen != 0 {
		t.Fatalf("spectator unseen=%d want 0 (count only, no badge)", unseen)
	}
	if err := store.MarkThreadRead(ch.ID, "agent-root", spaces.LocalViewer); err != nil {
		t.Fatal(err)
	}
	// Mark must stay no-op for spectators.
	unseen, _ = store.ThreadUnseenForViewer(ch.ID, "agent-root", spaces.LocalViewer)
	if unseen != 0 {
		t.Fatalf("spectator after mark-read unseen=%d", unseen)
	}
}

func TestThreadUnseen_ParticipantNewSinceLast(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Chat", "Winston", []string{"Steve"}, "", "")
	root, err := store.PostSpaceMessage(ch.ID, "can you look", "")
	if err != nil {
		t.Fatal(err)
	}
	joined, _ := store.HasThreadParticipation(ch.ID, root.ID, spaces.LocalViewer)
	if !joined {
		t.Fatal("posting the root is participation")
	}
	if err := store.MarkThreadRead(ch.ID, root.ID, spaces.LocalViewer); err != nil {
		t.Fatal(err)
	}
	unseen, _ := store.ThreadUnseenForViewer(ch.ID, root.ID, spaces.LocalViewer)
	if unseen != 0 {
		t.Fatalf("just marked seen, unseen=%d", unseen)
	}
	_, err = store.InsertSpaceThreadMessage(ch.ID, "looking", root.ID, "assistant", "Steve")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.InsertSpaceThreadMessage(ch.ID, "found it", root.ID, "assistant", "Steve")
	if err != nil {
		t.Fatal(err)
	}
	unseen, err = store.ThreadUnseenForViewer(ch.ID, root.ID, spaces.LocalViewer)
	if err != nil {
		t.Fatal(err)
	}
	if unseen != 2 {
		t.Fatalf("participant unseen=%d want 2", unseen)
	}
	if err := store.MarkThreadRead(ch.ID, root.ID, spaces.LocalViewer); err != nil {
		t.Fatal(err)
	}
	unseen, _ = store.ThreadUnseenForViewer(ch.ID, root.ID, spaces.LocalViewer)
	if unseen != 0 {
		t.Fatalf("after open drawer unseen=%d want 0", unseen)
	}
}

func TestThreadUnseen_NoAtNeverCommentedSilent(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Chat", "Winston", nil, "", "")
	root, _ := store.PostSpaceMessage(ch.ID, "root by someone else conceptually", "")
	// Human posted the root — they participated. The silent case is covered
	// by TestThreadUnseen_SpectatorSilent. This asserts hallway list still
	// excludes the two replies and chip is count-only (no new_since on list
	// until we attach it).
	_, _ = store.PostSpaceMessage(ch.ID, "r1", root.ID)
	_, _ = store.PostSpaceMessage(ch.ID, "r2", root.ID)
	listed, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway must stay roots: %+v", listed.Messages)
	}
	if listed.Messages[0].ReplyCount != 2 {
		t.Fatalf("chip count=%d want 2", listed.Messages[0].ReplyCount)
	}
}
