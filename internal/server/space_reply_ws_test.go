package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/spaces"
)

func TestHandlePostSpaceMessage_ReplyBroadcastsSpaceReply(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []WSMessage
	srv.onSpaceWS = func(msg WSMessage) {
		mu.Lock()
		events = append(events, msg)
		mu.Unlock()
	}
	w := postSpaceJSON(srv, ch.ID, `{"content":"a reply","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	var found *WSMessage
	for i := range events {
		if events[i].Type == "space_reply" {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("space_reply not fired: %+v", events)
	}
	p := found.Payload
	if p["space_id"] != ch.ID {
		t.Fatalf("space_id=%v", p["space_id"])
	}
	if p["parent_id"] != root.ID {
		t.Fatalf("parent_id=%v want %s", p["parent_id"], root.ID)
	}
	if p["reply_count"] != 1 {
		t.Fatalf("reply_count=%v (%T)", p["reply_count"], p["reply_count"])
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway leaked replies: %+v", listed.Messages)
	}
	if listed.Messages[0].ReplyCount != 1 {
		t.Fatalf("chip count=%d", listed.Messages[0].ReplyCount)
	}
}

func TestEmitSpaceReply_NilHubEmptySpaceID(t *testing.T) {
	srv := testServer(t)
	srv.wsHub = nil
	srv.emitSpaceReply("", "p", nil, 1, "hi")
	srv.emitSpaceReply("space", "p", &spaces.SpaceMessage{ID: "m", Content: "hi"}, 1, "hi")
}

func TestHandlePostSpaceMessage_NoMentionDoesNotRunAgent(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	ran := false
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, _, _ string) (string, error) {
		ran = true
		return "should not run", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"just a comment","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	if ran {
		t.Fatal("no @mention must not run anyone")
	}
	replies, _ := store.ListSpaceReplies(ch.ID, root.ID)
	if len(replies) != 1 || replies[0].Role != "user" {
		t.Fatalf("want persist-only user reply, got %+v", replies)
	}
}

func TestHandlePostSpaceMessage_LastPreviewStripsHarness(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	w := postSpaceJSON(srv, ch.ID, `{"content":"TOOL_FAIL: boom","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var resp postSpaceMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.LastPreview, "TOOL_FAIL") {
		t.Fatalf("last_preview leaked harness: %q", resp.LastPreview)
	}
	listed := getHallway(t, srv, ch.ID)
	if strings.Contains(listed.Messages[0].LastPreview, "TOOL_FAIL") {
		t.Fatalf("list last_preview leaked: %q", listed.Messages[0].LastPreview)
	}
}

func TestHandlePostSpaceMessage_LabCannotWakeReggie(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	lab, err := store.CreateCompany("Lab", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := lab.ID
	members := []string{"Steve"}
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid, Members: &members}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	ran := []string{}
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		ran = append(ran, agent)
		return "hi", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"hey @Reggie","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	if w.Code == 500 {
		t.Fatalf("must not 500: %s", w.Body.String())
	}
	if w.Code != 201 && w.Code != 400 && w.Code != 403 {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var resp postSpaceMessageResponse
	_ = json.NewDecoder(strings.NewReader(w.Body.String())).Decode(&resp)
	if len(ran) != 0 {
		t.Fatalf("Reggie woke: %v", ran)
	}
	if w.Code == 201 {
		found := false
		for _, e := range resp.WakeErrors {
			if strings.EqualFold(e.Agent, "Reggie") && (e.Reason == "not_in_roster" || e.Reason == "not_in_company") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected wake_error for Reggie, got %+v body=%s", resp.WakeErrors, w.Body.String())
		}
	}
}

func TestHandlePostSpaceMessage_HuginnCanWakeSteve(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("Huginn Co", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel("Huginn", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		if agent != "Steve" {
			t.Fatalf("woke %s", agent)
		}
		return "on it", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"can you look @Steve","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Fatalf("want user+steve, got %d %+v", len(replies), replies)
	}
	var steve *spaces.SpaceMessage
	for i := range replies {
		if replies[i].Role == "assistant" && replies[i].Agent == "Steve" {
			steve = &replies[i]
		}
	}
	if steve == nil || steve.ParentID != root.ID {
		t.Fatalf("Steve speech missing or leaked to hallway: %+v", replies)
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 {
		t.Fatalf("hallway leaked: %+v", listed.Messages)
	}
}

func TestHandlePostSpaceMessage_HumanMentionNotifiesNotRun(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var types []string
	srv.onSpaceWS = func(msg WSMessage) { types = append(types, msg.Type) }
	ran := false
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, _, _ string) (string, error) {
		ran = true
		return "nope", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"@you need this","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	if ran {
		t.Fatal("@you must not run an agent")
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
}

func TestNewWiresSpaceThreadRunner(t *testing.T) {
	srv := testServer(t)
	if srv.spaceThreadRunner == nil {
		t.Fatal("constructed server must wire a space thread runner")
	}
}

func TestWiredSpaceThreadRunnerInsertsParentID(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "test-model"})
	srv.orch.SetAgentRegistry(reg)
	members := []string{"Steve"}
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Members: &members}); err != nil {
		t.Fatal(err)
	}
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	// Do not override SetSpaceThreadRunner — this is the New-wired ChatWithAgent path
	// (stubBackend, no live 14b).
	w := postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve look at this","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	var steve *spaces.SpaceMessage
	for i := range replies {
		if replies[i].Role == "assistant" && replies[i].Agent == "Steve" {
			steve = &replies[i]
		}
	}
	if steve == nil || steve.ParentID != root.ID || strings.TrimSpace(steve.Content) == "" {
		t.Fatalf("wired runner must insert parent_id assistant row: %+v", replies)
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 {
		t.Fatalf("hallway leaked: %+v", listed.Messages)
	}
	if listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway root grew a parent_id: %+v", listed.Messages[0])
	}
}

func TestThreadUnseen_ParticipantHTTP(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	partRoot, _ := store.PostSpaceMessage(ch.ID, "I posted this", "")
	_ = store.MarkThreadRead(ch.ID, partRoot.ID, spaces.LocalViewer)
	_, _ = store.InsertSpaceThreadMessage(ch.ID, "one", partRoot.ID, "assistant", "Steve")
	_, _ = store.InsertSpaceThreadMessage(ch.ID, "two", partRoot.ID, "assistant", "Steve")
	req := getReplies(srv, ch.ID, partRoot.ID)
	if req.Code != 200 {
		t.Fatalf("%d %s", req.Code, req.Body.String())
	}
	var body struct {
		Messages    []spaces.SpaceMessage `json:"messages"`
		Participant bool                  `json:"participant"`
		Unseen      int                   `json:"unseen"`
	}
	json.NewDecoder(req.Body).Decode(&body)
	if !body.Participant || body.Unseen != 2 {
		t.Fatalf("participant unseen: %+v", body)
	}
	mr := httptest.NewRequest(http.MethodPost, "/api/v1/space-messages/"+ch.ID+"/thread-read", strings.NewReader(`{"parent_id":"`+partRoot.ID+`"}`))
	mr.SetPathValue("id", ch.ID)
	mr.Header.Set("Content-Type", "application/json")
	mw := httptest.NewRecorder()
	srv.handleMarkSpaceThreadRead(mw, mr)
	if mw.Code != 200 {
		t.Fatalf("mark-read %d %s", mw.Code, mw.Body.String())
	}
	req = getReplies(srv, ch.ID, partRoot.ID)
	body = struct {
		Messages    []spaces.SpaceMessage `json:"messages"`
		Participant bool                  `json:"participant"`
		Unseen      int                   `json:"unseen"`
	}{}
	json.NewDecoder(req.Body).Decode(&body)
	if body.Unseen != 0 {
		t.Fatalf("after open unseen=%d", body.Unseen)
	}
}

func TestSpaceThreadWakePrompt_IncludesParentAndPriorReply(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, err := store.PostSpaceMessage(ch.ID, "We should ship the hallway chip tonight", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertSpaceThreadMessage(ch.ID, "I'll take the chip; Steve can review the wake path.", root.ID, "assistant", "Winston"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertSpaceThreadMessage(ch.ID, "TOOL_FAIL: wait_for_threads exploded", root.ID, "assistant", "Winston"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertSpaceThreadMessage(ch.ID, `{"name":"wait_for_threads","arguments":{}}`, root.ID, "assistant", "Winston"); err != nil {
		t.Fatal(err)
	}
	got := srv.spaceThreadWakePrompt(ch.ID, root.ID, "@Steve can you look at the wake path?")
	if !strings.Contains(got, "We should ship the hallway chip tonight") {
		t.Fatalf("parent missing:\n%s", got)
	}
	if !strings.Contains(got, "Winston") || !strings.Contains(got, "I'll take the chip") {
		t.Fatalf("prior reply name+content missing:\n%s", got)
	}
	if !strings.Contains(got, "@Steve can you look at the wake path?") {
		t.Fatalf("mention missing:\n%s", got)
	}
	if strings.Contains(got, "TOOL_FAIL") || strings.Contains(got, "wait_for_threads") {
		t.Fatalf("harness junk leaked:\n%s", got)
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway leaked thread rows: %+v", listed.Messages)
	}
}

func getHallway(t *testing.T, srv *Server, spaceID string) spaces.SpaceMessagesResult {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/space-messages/"+spaceID, nil)
	req.SetPathValue("id", spaceID)
	w := httptest.NewRecorder()
	srv.handleListSpaceMessages(w, req)
	if w.Code != 200 {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
	var res spaces.SpaceMessagesResult
	json.NewDecoder(w.Body).Decode(&res)
	return res
}

func TestHandlePostSpaceMessage_WakeReturnsBeforeRunner(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("Huginn Co", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel("Huginn", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	started := make(chan struct{})
	release := make(chan struct{})
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		if agent != "Steve" {
			t.Errorf("woke %s", agent)
		}
		close(started)
		<-release
		return "on it", nil
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postSpaceJSON(srv, ch.ID, `{"content":"can you look @Steve","parent_id":"`+root.ID+`"}`)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner never started")
	}
	select {
	case w := <-done:
		if w.Code != 201 {
			t.Fatalf("POST should return before runner finishes: %d %s", w.Code, w.Body.String())
		}
		var resp postSpaceMessageResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.ReplyCount < 1 {
			t.Fatalf("chip should already count the user reply: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST blocked on the runner — drawer would wait on 14b")
	}
	replies, _ := store.ListSpaceReplies(ch.ID, root.ID)
	if len(replies) != 1 || replies[0].Role != "user" {
		t.Fatalf("assistant must not exist until runner finishes: %+v", replies)
	}
	close(release)
	srv.waitSpaceThreadWakes()
	replies, err = store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Fatalf("want user+steve after runner, got %+v", replies)
	}
	if replies[1].Role != "assistant" || replies[1].Agent != "Steve" || replies[1].ParentID != root.ID {
		t.Fatalf("steve speech: %+v", replies[1])
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway leaked: %+v", listed.Messages)
	}
}

func TestHandlePostSpaceMessage_MJNotifiesNotRun(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var types []string
	srv.onSpaceWS = func(msg WSMessage) { types = append(types, msg.Type) }
	ran := false
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, _, _ string) (string, error) {
		ran = true
		return "nope", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"@MJ need this","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	if ran {
		t.Fatal("@MJ must not run an agent")
	}
	found := false
	for _, ty := range types {
		if ty == "space_reply_mention" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected space_reply_mention for @MJ, got %v", types)
	}
}

func TestHandlePostSpaceMessage_TwoAgentsSameParentChip(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertSpaceThreadMessage(ch.ID, "winston take", root.ID, "assistant", "Winston"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertSpaceThreadMessage(ch.ID, "steve follow", root.ID, "assistant", "Steve"); err != nil {
		t.Fatal(err)
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway leaked: %+v", listed.Messages)
	}
	if listed.Messages[0].ReplyCount != 2 {
		t.Fatalf("chip=%d want 2", listed.Messages[0].ReplyCount)
	}
	req := getReplies(srv, ch.ID, root.ID)
	if req.Code != 200 {
		t.Fatalf("%d %s", req.Code, req.Body.String())
	}
	var body struct {
		Messages    []spaces.SpaceMessage `json:"messages"`
		Participant bool                  `json:"participant"`
		Unseen      int                   `json:"unseen"`
	}
	json.NewDecoder(req.Body).Decode(&body)
	if len(body.Messages) != 2 {
		t.Fatalf("replies=%d %+v", len(body.Messages), body.Messages)
	}
	if body.Messages[0].Agent != "Winston" || body.Messages[1].Agent != "Steve" {
		t.Fatalf("order %+v", body.Messages)
	}
	if body.Messages[0].ParentID != root.ID || body.Messages[1].ParentID != root.ID {
		t.Fatalf("parent leak %+v", body.Messages)
	}
}

func TestHandlePostSpaceMessage_LocalUserNotifiesNotRun(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	t.Setenv("USER", "mjbonanno")
	ran := false
	var types []string
	srv.onSpaceWS = func(msg WSMessage) { types = append(types, msg.Type) }
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, _, _ string) (string, error) {
		ran = true
		return "nope", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"@mjbonanno ping","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	if ran {
		t.Fatal("local user mention must not run an agent")
	}
	found := false
	for _, ty := range types {
		if ty == "space_reply_mention" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected space_reply_mention for local user, got %v", types)
	}
}

func TestHandlePostSpaceMessage_StreamTokensStayInThread(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("Huginn Co", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel("Huginn", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var mu sync.Mutex
	var events []WSMessage
	srv.onSpaceWS = func(msg WSMessage) {
		mu.Lock()
		events = append(events, msg)
		mu.Unlock()
	}
	started := make(chan struct{})
	release := make(chan struct{})
	srv.SetSpaceThreadRunner(func(_ context.Context, spaceID, parentID, agent, _ string) (string, error) {
		if agent != "Steve" {
			t.Errorf("woke %s", agent)
		}
		srv.emitSpaceReplyToken(spaceID, parentID, agent, "on ")
		srv.emitSpaceReplyToken(spaceID, parentID, agent, "it")
		close(started)
		<-release
		return "on it", nil
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postSpaceJSON(srv, ch.ID, `{"content":"can you look @Steve","parent_id":"`+root.ID+`"}`)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner never started")
	}
	select {
	case w := <-done:
		if w.Code != 201 {
			t.Fatalf("POST %d %s", w.Code, w.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST blocked on stream")
	}

	mu.Lock()
	var tokens []string
	for _, e := range events {
		if e.Type == "token" || e.Type == "thread_token" {
			t.Fatalf("hallway/thread token leaked: %+v", e)
		}
		if e.Type != "space_reply_token" {
			continue
		}
		if e.Payload["space_id"] != ch.ID {
			t.Fatalf("token space_id=%v", e.Payload["space_id"])
		}
		if e.Payload["parent_id"] != root.ID {
			t.Fatalf("token parent_id=%v", e.Payload["parent_id"])
		}
		if e.Payload["agent"] != "Steve" {
			t.Fatalf("token agent=%v", e.Payload["agent"])
		}
		tok, _ := e.Payload["token"].(string)
		tokens = append(tokens, tok)
	}
	mu.Unlock()
	if got := strings.Join(tokens, ""); got != "on it" {
		t.Fatalf("tokens=%q events=%+v", got, events)
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway during stream: %+v", listed.Messages)
	}

	other, err := store.CreateChannel("Other", "Winston", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv.emitSpaceReplyToken(other.ID, "not-this-parent", "Steve", "LEAK")
	listed = getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 {
		t.Fatalf("other space_id painted hallway: %+v", listed.Messages)
	}
	mu.Lock()
	var otherTok bool
	for _, e := range events {
		if e.Type == "space_reply_token" && e.Payload["space_id"] == other.ID {
			otherTok = true
			if e.Payload["parent_id"] == root.ID {
				t.Fatal("other space token reused this parent_id")
			}
		}
	}
	mu.Unlock()
	if !otherTok {
		t.Fatal("expected other space_id token so clients can ignore it")
	}

	close(release)
	srv.waitSpaceThreadWakes()
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Fatalf("want user+steve, got %+v", replies)
	}
	if replies[1].Role != "assistant" || replies[1].Agent != "Steve" || replies[1].ParentID != root.ID || replies[1].Content != "on it" {
		t.Fatalf("final row: %+v", replies[1])
	}
	listed = getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway after done: %+v", listed.Messages)
	}
}

func TestEmitSpaceReplyToken_SkipsEmpty(t *testing.T) {
	srv := testServer(t)
	var n int
	srv.onSpaceWS = func(msg WSMessage) { n++ }
	srv.emitSpaceReplyToken("", "p", "Steve", "hi")
	srv.emitSpaceReplyToken("space", "p", "Steve", "")
	if n != 0 {
		t.Fatalf("empty emit leaked %d", n)
	}
}

func TestWiredSpaceThreadRunner_EmitsSpeechTokenNotHarness(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "test-model"})
	srv.orch.SetAgentRegistry(reg)
	members := []string{"Steve"}
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Members: &members}); err != nil {
		t.Fatal(err)
	}
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	var events []WSMessage
	srv.onSpaceWS = func(msg WSMessage) { events = append(events, msg) }
	w := postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve look at this","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	var tok string
	for _, e := range events {
		if e.Type == "token" {
			t.Fatalf("generic token painted hallway: %+v", e)
		}
		if e.Type == "space_reply_token" {
			if e.Payload["parent_id"] != root.ID || e.Payload["space_id"] != ch.ID {
				t.Fatalf("unscoped token: %+v", e.Payload)
			}
			tok, _ = e.Payload["token"].(string)
		}
	}
	if strings.TrimSpace(tok) == "" {
		t.Fatalf("wired runner must stream speech token, events=%+v", events)
	}
	if strings.Contains(tok, "wait_for_threads") || strings.Contains(tok, "TOOL_FAIL") {
		t.Fatalf("harness streamed as speech: %q", tok)
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 {
		t.Fatalf("hallway leaked: %+v", listed.Messages)
	}
}

func TestHandlePostSpaceMessage_UnseatMidFlightDropsSpeech(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("LabMid", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannelForCompany("lab-chat", "Winston", []string{"Steve"}, "", "", co.ID)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		close(started)
		<-release
		return "late speech from " + agent, nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve look","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}
	if err := store.UnseatMember(co.ID, "Steve"); err != nil {
		t.Fatal(err)
	}
	close(release)
	srv.waitSpaceThreadWakes()
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range replies {
		if r.Role == "assistant" && r.Agent == "Steve" {
			t.Fatalf("unseat mid-flight must not persist Steve speech: %+v", replies)
		}
		if strings.Contains(r.Content, "late speech") {
			t.Fatalf("late speech leaked: %+v", replies)
		}
	}
}

func TestHandlePostSpaceMessage_LastPreviewKeepsPlaybookProse(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	prose := `Don't paste {"name":"wait_for_threads"} into the chip; just say done.`
	body, _ := json.Marshal(map[string]string{"content": prose, "parent_id": root.ID})
	w := postSpaceJSON(srv, ch.ID, string(body))
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var resp postSpaceMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.LastPreview, "just say done") {
		t.Fatalf("legit prose lost from last_preview: %q", resp.LastPreview)
	}
	if resp.LastPreview == "" || strings.HasPrefix(strings.TrimSpace(resp.LastPreview), `{"name"`) {
		t.Fatalf("last_preview injected playbook JSON: %q", resp.LastPreview)
	}
}

func TestHandlePostSpaceMessage_WinstonAskSteveWakesBoth(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("Huginn Co", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel("Huginn", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	woke := map[string]int{}
	var mu sync.Mutex
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, task string) (string, error) {
		mu.Lock()
		woke[agent]++
		mu.Unlock()
		if agent == "Steve" {
			return "hostname is testhost", nil
		}
		return "Steve said testhost. 7 times 8 is 56.", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"@Winston Ask Steve hostname + 7*8.","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	mu.Lock()
	defer mu.Unlock()
	if woke["Steve"] != 1 {
		t.Fatalf("Steve must wake (bash specialist), woke=%v", woke)
	}
	if woke["Winston"] != 1 {
		t.Fatalf("Winston must still wake to relay, woke=%v", woke)
	}
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	agents := map[string]string{}
	for _, r := range replies {
		if r.Role == "assistant" {
			agents[r.Agent] = r.Content
		}
	}
	if !strings.Contains(agents["Steve"], "testhost") {
		t.Fatalf("Steve speech missing hostname: %+v", replies)
	}
	if !strings.Contains(agents["Winston"], "56") && !strings.Contains(agents["Winston"], "testhost") {
		t.Fatalf("Winston relay missing: %+v", replies)
	}
}

func TestFinishSpaceThreadWake_LiveWinstonInvalidToolPongBecomesTeammate(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("Huginn Co", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel("Huginn", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	const live = "Reggie attempted to confirm his status by responding to a ping, but the 'PONG' response was not recognized as a valid tool. Please verify the tool usage or contact support for further assistance."
	srv.SetSpaceThreadRunner(func(_ context.Context, _, parentID, agent, _ string) (string, error) {
		if parentID != root.ID {
			t.Errorf("parent=%s want %s", parentID, root.ID)
		}
		if agent != "Winston" {
			t.Errorf("agent=%s want Winston", agent)
		}
		return live, nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"@Winston ping Reggie","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	var winston *spaces.SpaceMessage
	for i := range replies {
		if replies[i].Agent == "Winston" && replies[i].Role == "assistant" {
			winston = &replies[i]
		}
	}
	if winston == nil || winston.ParentID != root.ID {
		t.Fatalf("Winston persist missing: %+v", replies)
	}
	got := winston.Content
	if got != "Reggie confirmed status: PONG." {
		t.Fatalf("thread persist leftover rewrite: %q", got)
	}
	for _, leak := range []string{"contact support", "further assistance", "verify the tool usage", "not a valid tool", "not recognized"} {
		if strings.Contains(got, leak) {
			t.Fatalf("leaked %q in persisted thread: %q", leak, got)
		}
	}
	listed := getHallway(t, srv, ch.ID)
	if len(listed.Messages) != 1 || listed.Messages[0].ParentID != "" {
		t.Fatalf("hallway leaked: %+v", listed.Messages)
	}
}

func TestSpaceThreadUserTurn_TrivialPingIgnoresTranscript(t *testing.T) {
	parent := &spaces.SpaceMessage{Role: "user", Content: "drawer-parent-5a (ignore)"}
	prompt := spaces.BuildThreadWakePrompt(parent, nil, "@Winston ping")
	if agent.IsTrivialAsk(prompt) {
		t.Fatalf("transcript+mention must not look trivial: %q", prompt)
	}
	if !agent.IsTrivialAsk("@Winston ping") {
		t.Fatal("mention line must stay trivial")
	}
	if got := spaceThreadUserTurn("@Winston ping", prompt); got != "@Winston ping" {
		t.Fatalf("userTurn=%q, want mention line", got)
	}
	if got := spaceThreadUserTurn("@Steve what is 2+2?", prompt); !strings.Contains(got, "drawer-parent-5a") {
		t.Fatalf("non-trivial should keep transcript, got %q", got)
	}
}
