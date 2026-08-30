package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func spaceReplyServer(t *testing.T) (*Server, *spaces.SQLiteSpaceStore, *spaces.Space) {
	t.Helper()
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)
	ch, err := store.CreateChannel("Chat", "atlas", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return srv, store, ch
}

func postSpaceJSON(srv *Server, spaceID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/space-messages/"+spaceID, strings.NewReader(body))
	req.SetPathValue("id", spaceID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePostSpaceMessage(w, req)
	return w
}

func getReplies(srv *Server, spaceID, parentID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/space-messages/"+spaceID+"/replies?parent_id="+parentID, nil)
	req.SetPathValue("id", spaceID)
	w := httptest.NewRecorder()
	srv.handleListSpaceReplies(w, req)
	return w
}

func decodeSpaceMsg(t *testing.T, w *httptest.ResponseRecorder) spaces.SpaceMessage {
	t.Helper()
	var m spaces.SpaceMessage
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	return m
}

func TestHandlePostSpaceMessage_MissingParent(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	w := postSpaceJSON(srv, ch.ID, `{"content":"hi","parent_id":"nope"}`)
	if w.Code != 404 {
		t.Fatalf("got %d %s want 404", w.Code, w.Body.String())
	}
}

func TestHandlePostSpaceMessage_EmptyContent(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	w := postSpaceJSON(srv, ch.ID, `{"content":"   ","parent_id":"`+root.ID+`"}`)
	if w.Code != 400 {
		t.Fatalf("got %d %s want 400", w.Code, w.Body.String())
	}
}

func TestHandlePostSpaceMessage_NestedFlattens(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	child, _ := store.PostSpaceMessage(ch.ID, "child", root.ID)
	w := postSpaceJSON(srv, ch.ID, `{"content":"nested","parent_id":"`+child.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("nested must not 500: %d %s", w.Code, w.Body.String())
	}
	got := decodeSpaceMsg(t, w)
	if got.ParentID != root.ID {
		t.Fatalf("parent=%q want root %q", got.ParentID, root.ID)
	}
}

func TestHandlePostSpaceMessage_ParentOtherSpace(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	other, _ := store.CreateChannel("Other", "atlas", nil, "", "")
	root, _ := store.PostSpaceMessage(other.ID, "elsewhere", "")
	w := postSpaceJSON(srv, ch.ID, `{"content":"cross","parent_id":"`+root.ID+`"}`)
	if w.Code != 400 {
		t.Fatalf("got %d %s want 400", w.Code, w.Body.String())
	}
}

func TestHandlePostSpaceMessage_SelfParent(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	w := postSpaceJSON(srv, ch.ID, `{"id":"abc","parent_id":"abc","content":"loop"}`)
	if w.Code != 400 {
		t.Fatalf("got %d %s want 400", w.Code, w.Body.String())
	}
}

func TestHandlePostSpaceMessage_Concurrent(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		body := `{"content":"c` + string(rune('1'+i)) + `","parent_id":"` + root.ID + `"}`
		go func(b string) {
			defer wg.Done()
			w := postSpaceJSON(srv, ch.ID, b)
			codes <- w.Code
		}(body)
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 201 {
			t.Fatalf("concurrent code %d", c)
		}
	}
	w := getReplies(srv, ch.ID, root.ID)
	if w.Code != 200 {
		t.Fatalf("list replies %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Messages []spaces.SpaceMessage `json:"messages"`
	}
	json.NewDecoder(w.Body).Decode(&body)
	if len(body.Messages) != 2 {
		t.Fatalf("lost write: %d replies", len(body.Messages))
	}
}

func TestHandlePostSpaceMessage_20k(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	big := strings.Repeat("b", 20_000)
	w := postSpaceJSON(srv, ch.ID, `{"content":"`+big+`","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("20k: %d %s", w.Code, w.Body.String())
	}
}

func TestHandleListSpaceMessages_RootsOnlyChip(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	_, _ = store.PostSpaceMessage(ch.ID, "r1", root.ID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/space-messages/"+ch.ID, nil)
	req.SetPathValue("id", ch.ID)
	w := httptest.NewRecorder()
	srv.handleListSpaceMessages(w, req)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var res spaces.SpaceMessagesResult
	json.NewDecoder(w.Body).Decode(&res)
	if len(res.Messages) != 1 || res.Messages[0].ReplyCount != 1 || res.Messages[0].ParentID != "" {
		t.Fatalf("list=%+v", res.Messages)
	}
}

func TestHandleListSpaceReplies_ArchivedFailClosed(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatal(err)
	}
	w := getReplies(srv, ch.ID, root.ID)
	if w.Code != 404 {
		t.Fatalf("archived list replies: %d %s (want 404, no panic)", w.Code, w.Body.String())
	}
}

func TestHandlePostSpaceMessage_PersistsHarnessContent(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	w := postSpaceJSON(srv, ch.ID, `{"content":"TOOL_FAIL: boom","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil || len(replies) != 1 {
		t.Fatalf("persist: %v %+v", err, replies)
	}
	if replies[0].Content != "TOOL_FAIL: boom" {
		t.Fatalf("content=%q", replies[0].Content)
	}
}

func TestHandlePostSpaceMessage_ArchivedFailClosed(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatal(err)
	}
	w := postSpaceJSON(srv, ch.ID, `{"content":"late reply","parent_id":"`+root.ID+`"}`)
	if w.Code != 404 {
		t.Fatalf("POST reply while archived: %d %s (want 404, no panic)", w.Code, w.Body.String())
	}
}

func TestHandleListSpaceReplies_MentionSpectatorUnseenZero(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatal(err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)
	ch, err := store.CreateChannel("Chat", "atlas", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	humanRoot, err := store.PostSpaceMessage(ch.ID, "hallway", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write().Exec(`
		INSERT INTO messages (id, container_type, container_id, seq, ts, role, content, agent, parent_id)
		SELECT 'agent-root-b2', 'session', container_id, 99, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'assistant', 'hey steve', 'Winston', ''
		FROM messages WHERE id = ?`, humanRoot.ID); err != nil {
		t.Fatal(err)
	}
	msg, err := store.InsertSpaceThreadMessage(ch.ID, "@you please look", "agent-root-b2", "assistant", "Steve")
	if err != nil {
		t.Fatalf("agent @you: %v", err)
	}
	var types []string
	srv.onSpaceWS = func(m WSMessage) { types = append(types, m.Type) }
	resp := srv.afterSpaceReplyPersisted(ch.ID, msg, "@you please look")
	if !resp.MentionedHuman {
		t.Fatal("agent @you must emit mentioned_human")
	}
	found := false
	for _, ty := range types {
		if ty == "space_reply_mention" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected space_reply_mention, got %v", types)
	}
	w := getReplies(srv, ch.ID, "agent-root-b2")
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Participant bool `json:"participant"`
		Unseen      int  `json:"unseen"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Participant {
		t.Fatal("spectator must not become a participant from @mention")
	}
	if body.Unseen != 0 {
		t.Fatalf("space_reply_mention spectator unseen=%d want 0", body.Unseen)
	}
}

func TestHandleListSpaces_ForYouFromMentionOnTheWire(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	w := postSpaceJSON(srv, ch.ID, `{"content":"@you look at this","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("post: %d %s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	lw := httptest.NewRecorder()
	srv.handleListSpaces(lw, req)
	if lw.Code != 200 {
		t.Fatalf("list: %d %s", lw.Code, lw.Body.String())
	}
	var listed spaces.ListSpacesResult
	if err := json.NewDecoder(lw.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	var found *spaces.Space
	for _, sp := range listed.Spaces {
		if sp != nil && sp.ID == ch.ID {
			found = sp
			break
		}
	}
	if found == nil {
		t.Fatal("listed spaces missing channel")
	}
	if !found.ForYou {
		t.Fatal("spaces list must return for_you after space_reply_mention")
	}

	// Spectator-style activity: unseen_count is a count, rail is for_you only.
	if found.UnseenCount < 0 {
		t.Fatalf("unseen_count=%d", found.UnseenCount)
	}

	mr := httptest.NewRequest(http.MethodPost, "/api/v1/spaces/"+ch.ID+"/read", nil)
	mr.SetPathValue("id", ch.ID)
	mw := httptest.NewRecorder()
	srv.handleMarkSpaceRead(mw, mr)
	if mw.Code != 200 {
		t.Fatalf("mark-read: %d %s", mw.Code, mw.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	lw = httptest.NewRecorder()
	srv.handleListSpaces(lw, req)
	if err := json.NewDecoder(lw.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	found = nil
	for _, sp := range listed.Spaces {
		if sp != nil && sp.ID == ch.ID {
			found = sp
			break
		}
	}
	if found == nil {
		t.Fatal("listed spaces missing channel after mark-read")
	}
	if found.ForYou {
		t.Fatal("mark-read must clear for_you on the wire")
	}
}

func TestHandleListSpaces_SpectatorUnseenNoForYou(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	// Agent speech, no @human — persist + space_reply only.
	if _, err := store.InsertSpaceThreadMessage(ch.ID, "working", root.ID, "assistant", "Steve"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ForYou {
		t.Fatal("agent chatter must not set for_you")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	lw := httptest.NewRecorder()
	srv.handleListSpaces(lw, req)
	if lw.Code != 200 {
		t.Fatalf("list: %d %s", lw.Code, lw.Body.String())
	}
	var listed spaces.ListSpacesResult
	if err := json.NewDecoder(lw.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	for _, sp := range listed.Spaces {
		if sp != nil && sp.ID == ch.ID && sp.ForYou {
			t.Fatal("spaces list for_you must stay false without mention")
		}
	}
}

func deleteSpaceMsg(srv *Server, spaceID, msgID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/space-messages/"+spaceID+"/"+msgID, nil)
	req.SetPathValue("id", spaceID)
	req.SetPathValue("msgID", msgID)
	w := httptest.NewRecorder()
	srv.handleDeleteSpaceMessage(w, req)
	return w
}

func TestHandleDeleteSpaceMessage_RootAndThread(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	keep, _ := store.PostSpaceMessage(ch.ID, "Ask Steve hostname please", "")
	junk, _ := store.PostSpaceMessage(ch.ID, "wringer-A1-root", "")
	_, _ = store.PostSpaceMessage(ch.ID, "wringer reply", junk.ID)

	w := deleteSpaceMsg(srv, ch.ID, junk.ID)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/space-messages/"+ch.ID, nil)
	req.SetPathValue("id", ch.ID)
	list := httptest.NewRecorder()
	srv.handleListSpaceMessages(list, req)
	if list.Code != 200 {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var res spaces.SpaceMessagesResult
	json.NewDecoder(list.Body).Decode(&res)
	if len(res.Messages) != 1 || res.Messages[0].ID != keep.ID {
		t.Fatalf("hallway after delete=%+v", res.Messages)
	}
	rw := getReplies(srv, ch.ID, junk.ID)
	if rw.Code != 404 {
		t.Fatalf("deleted thread still listed: %d %s", rw.Code, rw.Body.String())
	}
}

func TestHandleDeleteSpaceMessage_Unknown_Returns404(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	w := deleteSpaceMsg(srv, ch.ID, "doesnotexist")
	if w.Code != 404 {
		t.Fatalf("got %d %s want 404", w.Code, w.Body.String())
	}
}

func TestHandleDeleteSpaceMessage_NoStore_Returns503(t *testing.T) {
	srv := testServer(t)
	w := deleteSpaceMsg(srv, "any", "any")
	if w.Code != 503 {
		t.Fatalf("got %d %s want 503", w.Code, w.Body.String())
	}
}

func TestHandleDeleteSpaceMessage_InvalidIDs_Returns400(t *testing.T) {
	srv, _, ch := spaceReplyServer(t)
	w := deleteSpaceMsg(srv, ch.ID, "bad/id")
	if w.Code != 400 {
		t.Fatalf("bad msg id: %d %s want 400", w.Code, w.Body.String())
	}
}
