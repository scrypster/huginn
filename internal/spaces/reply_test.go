package spaces_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func seedRoot(t *testing.T, store *spaces.SQLiteSpaceStore, spaceID, content string) *spaces.SpaceMessage {
	t.Helper()
	msg, err := store.PostSpaceMessage(spaceID, content, "")
	if err != nil {
		t.Fatalf("seed root: %v", err)
	}
	return msg
}

func TestPostSpaceMessage_RootAndReply(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Replies", "atlas", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := seedRoot(t, store, ch.ID, "hello channel")
	if root.ParentID != "" {
		t.Fatalf("root parent_id want empty, got %q", root.ParentID)
	}
	reply, err := store.PostSpaceMessage(ch.ID, "a reply", root.ID)
	if err != nil {
		t.Fatalf("post reply: %v", err)
	}
	if reply.ParentID != root.ID {
		t.Fatalf("reply parent_id=%q want %q", reply.ParentID, root.ID)
	}

	listed, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 {
		t.Fatalf("roots: got %d want 1: %+v", len(listed.Messages), listed.Messages)
	}
	if listed.Messages[0].ID != root.ID {
		t.Fatalf("root id mismatch")
	}
	if listed.Messages[0].ReplyCount != 1 {
		t.Fatalf("reply_count=%d want 1", listed.Messages[0].ReplyCount)
	}

	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].ID != reply.ID {
		t.Fatalf("replies=%+v", replies)
	}
}

func TestPostSpaceMessage_MissingParent(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	_, err := store.PostSpaceMessage(ch.ID, "orphan", "does-not-exist")
	if err == nil {
		t.Fatal("expected error")
	}
	se, ok := err.(*spaces.SpaceError)
	if !ok || se.Code != "parent_not_found" {
		t.Fatalf("got %v want parent_not_found", err)
	}
}

func TestPostSpaceMessage_EmptyContent(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	for _, c := range []string{"", "   ", "\n\t"} {
		_, err := store.PostSpaceMessage(ch.ID, c, root.ID)
		if err == nil {
			t.Fatalf("content %q: expected error", c)
		}
		if se, ok := err.(*spaces.SpaceError); !ok || se.Code != "invalid_content" {
			t.Fatalf("content %q: got %v", c, err)
		}
	}
}

func TestPostSpaceMessage_ReplyToReplyFlattens(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	child, err := store.PostSpaceMessage(ch.ID, "child", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	nested, err := store.PostSpaceMessage(ch.ID, "nested", child.ID)
	if err != nil {
		t.Fatalf("nested reply should flatten, not 500: %v", err)
	}
	if nested.ParentID != root.ID {
		t.Fatalf("flattened parent=%q want root %q", nested.ParentID, root.ID)
	}
	listed, _ := store.ListSpaceMessages(ch.ID, nil, 50)
	if listed.Messages[0].ReplyCount != 2 {
		t.Fatalf("chip count=%d want 2", listed.Messages[0].ReplyCount)
	}
}

func TestPostSpaceMessage_ParentOtherSpace(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	a, _ := store.CreateChannel("A", "atlas", nil, "", "")
	b, _ := store.CreateChannel("B", "atlas", nil, "", "")
	rootA := seedRoot(t, store, a.ID, "in A")
	_, err := store.PostSpaceMessage(b.ID, "cross", rootA.ID)
	if err == nil {
		t.Fatal("expected cross-space error")
	}
	se, ok := err.(*spaces.SpaceError)
	if !ok || se.Code != "parent_wrong_space" {
		t.Fatalf("got %v want parent_wrong_space", err)
	}
}

func TestPostSpaceMessage_SelfParent(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	// Corrupt row: parent_id = self.
	if _, err := db.Write().Exec(`UPDATE messages SET parent_id = id WHERE id = ?`, root.ID); err != nil {
		t.Fatal(err)
	}
	_, err := store.PostSpaceMessage(ch.ID, "to self-loop", root.ID)
	if err == nil {
		t.Fatal("expected invalid_parent")
	}
	se, ok := err.(*spaces.SpaceError)
	if !ok || se.Code != "invalid_parent" {
		t.Fatalf("got %v want invalid_parent", err)
	}
}

func TestPostSpaceMessage_ConcurrentReplies(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := store.PostSpaceMessage(ch.ID, "r1", root.ID)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := store.PostSpaceMessage(ch.ID, "r2", root.ID)
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent reply: %v", err)
		}
	}
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Fatalf("lost write: got %d replies", len(replies))
	}
	listed, _ := store.ListSpaceMessages(ch.ID, nil, 50)
	if listed.Messages[0].ReplyCount != 2 {
		t.Fatalf("chip=%d want 2", listed.Messages[0].ReplyCount)
	}
}

func TestPostSpaceMessage_20kChars(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	big := strings.Repeat("a", 20_000)
	got, err := store.PostSpaceMessage(ch.ID, big, root.ID)
	if err != nil {
		t.Fatalf("20k reply: %v", err)
	}
	if len(got.Content) != 20_000 {
		t.Fatalf("len=%d", len(got.Content))
	}
}

func TestListSpaceMessages_ExcludesReplies_ChipMatches(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	_, _ = store.PostSpaceMessage(ch.ID, "r1", root.ID)
	_, _ = store.PostSpaceMessage(ch.ID, "r2", root.ID)
	listed, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range listed.Messages {
		if m.ParentID != "" {
			t.Fatalf("root list included reply %s", m.ID)
		}
	}
	if len(listed.Messages) != 1 || listed.Messages[0].ReplyCount != 2 {
		t.Fatalf("list=%+v", listed.Messages)
	}
	replies, _ := store.ListSpaceReplies(ch.ID, root.ID)
	if len(replies) != listed.Messages[0].ReplyCount {
		t.Fatalf("chip %d != replies %d", listed.Messages[0].ReplyCount, len(replies))
	}
}

func TestListSpaceReplies_ArchivedSpaceFailClosed(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	_, _ = store.PostSpaceMessage(ch.ID, "r1", root.ID)
	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatal(err)
	}
	_, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err == nil {
		t.Fatal("expected fail-closed on archived space")
	}
	se, ok := err.(*spaces.SpaceError)
	if !ok || se.Code != "space_not_found" {
		t.Fatalf("got %v want space_not_found", err)
	}
	_, err = store.PostSpaceMessage(ch.ID, "after archive", root.ID)
	if err == nil {
		t.Fatal("post after archive should fail")
	}
}

func TestListSpaceMessages_LastPreviewStripped(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	_, _ = store.PostSpaceMessage(ch.ID, "looks good to me", root.ID)
	_, _ = store.PostSpaceMessage(ch.ID, "TOOL_FAIL: boom", root.ID)
	listed, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 {
		t.Fatalf("hallway=%+v", listed.Messages)
	}
	if listed.Messages[0].LastPreview != "looks good to me" {
		t.Fatalf("last_preview=%q", listed.Messages[0].LastPreview)
	}
	if listed.Messages[0].ReplyCount != 2 {
		t.Fatalf("count=%d", listed.Messages[0].ReplyCount)
	}
}

func TestSpaceMessageCreatedAtJSON(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Ages", "atlas", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := seedRoot(t, store, ch.ID, "how old am I")
	if root.CreatedAt == "" || root.Ts == "" {
		t.Fatalf("posted message missing timestamps: %+v", root)
	}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["created_at"] != root.Ts && wire["created_at"] != root.CreatedAt {
		t.Fatalf("created_at=%v ts=%v created=%v json=%s", wire["created_at"], root.Ts, root.CreatedAt, raw)
	}
	if wire["ts"] != root.Ts {
		t.Fatalf("ts missing from json: %s", raw)
	}

	listed, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 || listed.Messages[0].CreatedAt == "" {
		t.Fatalf("list missing created_at: %+v", listed.Messages)
	}
	reply, err := store.PostSpaceMessage(ch.ID, "a reply", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reply.CreatedAt == "" {
		t.Fatal("reply missing created_at")
	}
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].CreatedAt == "" {
		t.Fatalf("replies missing created_at: %+v", replies)
	}
}

func TestDeleteSpaceMessage_RootCascadesThread(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Replies", "atlas", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	keep := seedRoot(t, store, ch.ID, "keep this root")
	junk := seedRoot(t, store, ch.ID, "wringer-A1-root")
	_, err = store.PostSpaceMessage(ch.ID, "junk reply", junk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkThreadRead(ch.ID, junk.ID, spaces.LocalViewer); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSpaceMessage(ch.ID, junk.ID); err != nil {
		t.Fatalf("delete root: %v", err)
	}
	if _, err := store.GetSpaceMessage(ch.ID, junk.ID); err == nil {
		t.Fatal("deleted root still loadable")
	}
	replies, err := store.ListSpaceReplies(ch.ID, junk.ID)
	if err == nil {
		t.Fatalf("deleted root still lists replies: %+v", replies)
	}
	listed, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 || listed.Messages[0].ID != keep.ID {
		t.Fatalf("kept roots=%+v want only %s", listed.Messages, keep.ID)
	}
}

func TestDeleteSpaceMessage_ReplyLeavesRoot(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root stays")
	r1, err := store.PostSpaceMessage(ch.ID, "drop me", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := store.PostSpaceMessage(ch.ID, "keep me", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSpaceMessage(ch.ID, r1.ID); err != nil {
		t.Fatalf("delete reply: %v", err)
	}
	if _, err := store.GetSpaceMessage(ch.ID, root.ID); err != nil {
		t.Fatalf("root gone: %v", err)
	}
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].ID != r2.ID {
		t.Fatalf("replies=%+v want only %s", replies, r2.ID)
	}
}

func TestDeleteSpaceMessage_UnknownAndWrongSpace(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	other, _ := store.CreateChannel("Other", "atlas", nil, "", "")
	root := seedRoot(t, store, other.ID, "elsewhere")

	err := store.DeleteSpaceMessage(ch.ID, "does-not-exist")
	se, ok := err.(*spaces.SpaceError)
	if !ok || se.Code != "parent_not_found" {
		t.Fatalf("unknown: %v want parent_not_found", err)
	}
	err = store.DeleteSpaceMessage(ch.ID, root.ID)
	se, ok = err.(*spaces.SpaceError)
	if !ok || se.Code != "parent_wrong_space" {
		t.Fatalf("wrong space: %v want parent_wrong_space", err)
	}
	if _, err := store.GetSpaceMessage(other.ID, root.ID); err != nil {
		t.Fatalf("cross-space delete must not remove source: %v", err)
	}
}

func TestDeleteSpaceMessage_ArchivedFailClosed(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Replies", "atlas", nil, "", "")
	root := seedRoot(t, store, ch.ID, "root")
	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatal(err)
	}
	err := store.DeleteSpaceMessage(ch.ID, root.ID)
	se, ok := err.(*spaces.SpaceError)
	if !ok || se.Code != "space_not_found" {
		t.Fatalf("archived: %v want space_not_found", err)
	}
}
