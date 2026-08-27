package server

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func meshHuginnSpace(t *testing.T) (*Server, *spaces.SQLiteSpaceStore, *spaces.Space) {
	t.Helper()
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("Huginn", "", []string{"Winston", "Steve", "Ava"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	lead := "Ava"
	if _, err := store.UpdateCompany(co.ID, spaces.CompanyUpdates{Lead: &lead}); err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel("eng", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	return srv, store, ch
}

func TestMesh_SteveAtWinstonWakesWinstonSameParent(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, err := store.PostSpaceMessage(ch.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, parentID, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		if parentID != root.ID {
			t.Errorf("%s parent=%s want %s", agent, parentID, root.ID)
		}
		if agent == "Steve" {
			return "@Winston pong", nil
		}
		return "ack", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	mu.Lock()
	defer mu.Unlock()
	got := map[string]int{}
	for _, a := range ran {
		got[a]++
	}
	if got["Steve"] != 1 || got["Winston"] != 1 {
		t.Fatalf("want Steve then Winston once, got %v", ran)
	}
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
		t.Fatalf("Winston speech missing on same parent: %+v", replies)
	}
}

func TestMesh_WinstonReplyDoesNotRewakeSteveUnlessAtSteve(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		if agent == "Steve" {
			return "@Winston pong", nil
		}
		return "hostname is box-1", nil // no @Steve
	})
	postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	mu.Lock()
	got := append([]string{}, ran...)
	mu.Unlock()
	steve := 0
	for _, a := range got {
		if a == "Steve" {
			steve++
		}
	}
	if steve != 1 {
		t.Fatalf("Winston no-@ reply must not re-wake Steve: %v", got)
	}

	// Fresh parent: Winston @Steve does wake Steve once more (hop 2), then stops.
	root2, _ := store.PostSpaceMessage(ch.ID, "root2", "")
	mu.Lock()
	ran = nil
	mu.Unlock()
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		switch agent {
		case "Steve":
			return "@Winston pong", nil
		case "Winston":
			return "@Steve thanks", nil
		default:
			return "ok", nil
		}
	})
	postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root2.ID+`"}`)
	srv.waitSpaceThreadWakes()
	mu.Lock()
	got = append([]string{}, ran...)
	mu.Unlock()
	counts := map[string]int{}
	for _, a := range got {
		counts[a]++
	}
	if counts["Steve"] < 2 {
		t.Fatalf("Winston @Steve should re-wake Steve: %v", got)
	}
	if counts["Steve"] > 2 || counts["Winston"] > 1 {
		t.Fatalf("hop cap should stop the ping-pong: %v", got)
	}
}

func TestMesh_LabSteveCannotWakeHuginnReggie(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	lab, err := store.CreateCompany("Lab", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	hug, err := store.CreateCompany("Huginn", "", []string{"Winston", "Reggie"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = hug
	ch, err := store.CreateChannel("lab-eng", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := lab.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		ran = append(ran, agent)
		if agent == "Steve" {
			return "@Reggie look", nil
		}
		return "hi", nil
	})
	postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	for _, a := range ran {
		if a == "Reggie" {
			t.Fatalf("Lab Steve woke Huginn Reggie: %v", ran)
		}
	}
}

func TestMesh_DeskWinstonWakesHuginnLeadNotSpecialist(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	hug, err := store.CreateCompany("Huginn", "", []string{"Winston", "Steve", "Ava"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	lead := "Ava"
	if _, err := store.UpdateCompany(hug.ID, spaces.CompanyUpdates{Lead: &lead}); err != nil {
		t.Fatal(err)
	}
	desk, err := store.CreateChannel("desk", "Winston", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(desk.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		if agent == "Winston" {
			return "@Ava take hostname — not Steve", nil
		}
		return "on it", nil
	})
	w := postSpaceJSON(srv, desk.ID, `{"content":"@Winston route this","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	mu.Lock()
	defer mu.Unlock()
	got := map[string]bool{}
	for _, a := range ran {
		got[a] = true
	}
	if !got["Winston"] || !got["Ava"] {
		t.Fatalf("desk Winston should wake Huginn lead Ava: %v", ran)
	}
	if got["Steve"] {
		t.Fatalf("desk must not wake Huginn-only Steve: %v", ran)
	}
}

func TestMesh_NoAtAgentSpeechWakesNobody(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		ran = append(ran, agent)
		return "hostname is box-1", nil
	})
	postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	if len(ran) != 1 || ran[0] != "Steve" {
		t.Fatalf("no-@ Steve speech must not wake others: %v", ran)
	}
}

func TestMesh_SelfAtSteveDoesNotLoop(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		ran = append(ran, agent)
		return "@Steve pong", nil
	})
	postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	if len(ran) != 1 || ran[0] != "Steve" {
		t.Fatalf("self @Steve must not loop: %v", ran)
	}
}

func TestMesh_SteveAtWinstonAndReggieOneLine(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	fresh, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SeatMember(fresh.CompanyID, "Reggie"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Members: &[]string{"Steve", "Reggie"}}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, parentID, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		if parentID != root.ID {
			t.Errorf("%s parent=%s want %s", agent, parentID, root.ID)
		}
		if agent == "Steve" {
			return "@Winston @Reggie pong", nil
		}
		return "ack", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	mu.Lock()
	defer mu.Unlock()
	got := map[string]int{}
	for _, a := range ran {
		got[a]++
	}
	if got["Steve"] != 1 || got["Winston"] != 1 || got["Reggie"] != 1 {
		t.Fatalf("want Steve+Winston+Reggie once, got %v", ran)
	}
}

func TestMesh_AssistantAtYouDoesNotWakeAgent(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		ran = append(ran, agent)
		return "@you pong", nil
	})
	postSpaceJSON(srv, ch.ID, `{"content":"hey @Steve","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	if len(ran) != 1 || ran[0] != "Steve" {
		t.Fatalf("assistant @you must not wake anyone else: %v", ran)
	}
}

func TestMesh_UnicodeWinstonDoesNotWake(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		ran = append(ran, agent)
		return "ack", nil
	})
	w := postSpaceJSON(srv, ch.ID, `{"content":"@Winstón pong","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	if len(ran) != 0 {
		t.Fatalf("accented @Winstón must not wake: %v", ran)
	}
}

func TestMesh_LowercaseWinstonWakesOnce(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		ran = append(ran, agent)
		return "pong", nil
	})
	postSpaceJSON(srv, ch.ID, `{"content":"@winston pong","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	if len(ran) != 1 || ran[0] != "Winston" {
		t.Fatalf("@winston should wake Winston once: %v", ran)
	}
}

func TestMesh_ParentWakeCapStopsNinth(t *testing.T) {
	srv, store, ch := meshHuginnSpace(t)
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		return "pong", nil
	})
	for i := 0; i < 9; i++ {
		w := postSpaceJSON(srv, ch.ID, `{"content":"@Steve pong","parent_id":"`+root.ID+`"}`)
		if w.Code != 201 {
			t.Fatalf("post %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	srv.waitSpaceThreadWakes()
	mu.Lock()
	defer mu.Unlock()
	if len(ran) > maxWakesPerParent {
		t.Fatalf("parent cap %d broken: %d wakes %v", maxWakesPerParent, len(ran), ran)
	}
	if len(ran) != maxWakesPerParent {
		t.Fatalf("want exactly %d reserved wakes, got %d %v", maxWakesPerParent, len(ran), ran)
	}
}

func TestMesh_TwoCompaniesSameLeadDeskWakesOnce(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	hug, err := store.CreateCompany("Huginn", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	lab, err := store.CreateCompany("Lab", "", []string{"Winston", "Sam"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if hug.EffectiveLead() != "Winston" || lab.EffectiveLead() != "Winston" {
		t.Fatalf("both companies should lead Winston: hug=%q lab=%q", hug.EffectiveLead(), lab.EffectiveLead())
	}
	desk, err := store.CreateChannel("desk", "Winston", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(desk.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		return "pong", nil
	})
	w := postSpaceJSON(srv, desk.ID, `{"content":"@Winston pong","parent_id":"`+root.ID+`"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	srv.waitSpaceThreadWakes()
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || ran[0] != "Winston" {
		t.Fatalf("two companies lead=Winston must desk-wake Winston once: %v", ran)
	}
}

func TestMesh_UnseatLeadThenDeskWakeFails(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("MeshX", "", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	lead := "Steve"
	if _, err := store.UpdateCompany(co.ID, spaces.CompanyUpdates{Lead: &lead}); err != nil {
		t.Fatal(err)
	}
	desk, err := store.CreateChannel("desk", "Winston", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(desk.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		return "pong", nil
	})
	postSpaceJSON(srv, desk.ID, `{"content":"@Steve pong","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	mu.Lock()
	if len(ran) != 1 || ran[0] != "Steve" {
		mu.Unlock()
		t.Fatalf("desk should wake company lead Steve: %v", ran)
	}
	ran = nil
	mu.Unlock()
	if err := store.UnseatMember(co.ID, "Steve"); err != nil {
		t.Fatal(err)
	}
	root2, _ := store.PostSpaceMessage(desk.ID, "root2", "")
	postSpaceJSON(srv, desk.ID, `{"content":"@Steve pong","parent_id":"`+root2.ID+`"}`)
	srv.waitSpaceThreadWakes()
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 0 {
		t.Fatalf("unseated lead must not desk-wake: %v", ran)
	}
}

func TestMesh_ChangeLeadMidThreadDeskFollowsNewLead(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("MeshLead", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if co.EffectiveLead() != "Winston" {
		t.Fatalf("want Winston lead, got %q", co.EffectiveLead())
	}
	ch, err := store.CreateChannel("eng", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	desk, err := store.CreateChannel("desk", "Winston", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		return "pong", nil
	})
	postSpaceJSON(srv, ch.ID, `{"content":"@Steve pong","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	if err := store.UnseatMember(co.ID, "Winston"); err != nil {
		t.Fatal(err)
	}
	cur, err := store.GetCompany(co.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.EffectiveLead() != "Steve" {
		t.Fatalf("after unseat Winston, lead should be Steve, got %q members=%v", cur.EffectiveLead(), cur.Members)
	}
	mu.Lock()
	ran = nil
	mu.Unlock()
	droot, _ := store.PostSpaceMessage(desk.ID, "desk-root", "")
	postSpaceJSON(srv, desk.ID, `{"content":"@Steve pong","parent_id":"`+droot.ID+`"}`)
	srv.waitSpaceThreadWakes()
	mu.Lock()
	got := append([]string{}, ran...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "Steve" {
		t.Fatalf("desk should wake new lead Steve: %v", got)
	}
}

func TestMesh_DeleteCompanyWhileWakeInFlightFailClosed(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	co, err := store.CreateCompany("MeshDel", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel("eng", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cid := co.ID
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &cid}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(ch.ID, "root", "")
	block := make(chan struct{})
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		<-block
		ran = append(ran, agent)
		return "should-not-persist", nil
	})
	postSpaceJSON(srv, ch.ID, `{"content":"@Steve pong","parent_id":"`+root.ID+`"}`)
	delErr := store.DeleteCompany(co.ID)
	if delErr == nil {
		close(block)
		t.Fatal("delete company with live space must fail closed")
	}
	if !strings.Contains(delErr.Error(), "spaces") && delErr != spaces.ErrCompanyHasSpaces {
		t.Fatalf("want company_has_spaces, got %v", delErr)
	}
	if err := store.UnseatMember(co.ID, "Steve"); err != nil {
		close(block)
		t.Fatal(err)
	}
	close(block)
	srv.waitSpaceThreadWakes()
	replies, err := store.ListSpaceReplies(ch.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range replies {
		if m.Role == "assistant" && m.Agent == "Steve" {
			t.Fatalf("unseated in-flight wake persisted: %+v", replies)
		}
	}
}

func TestMesh_DeskSpecialistCannotWakeLabLead(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	hug, err := store.CreateCompany("Huginn", "", []string{"Winston", "Steve", "Ava"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	lead := "Ava"
	if _, err := store.UpdateCompany(hug.ID, spaces.CompanyUpdates{Lead: &lead}); err != nil {
		t.Fatal(err)
	}
	lab, err := store.CreateCompany("Lab", "", []string{"Sam"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	labLead := "Sam"
	if _, err := store.UpdateCompany(lab.ID, spaces.CompanyUpdates{Lead: &labLead}); err != nil {
		t.Fatal(err)
	}
	desk, err := store.CreateChannel("desk", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(desk.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		if agent == "Steve" {
			return "@Sam lab secrets", nil
		}
		return "pong", nil
	})
	postSpaceJSON(srv, desk.ID, `{"content":"@Steve hostname","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	mu.Lock()
	got := append([]string{}, ran...)
	mu.Unlock()
	for _, a := range got {
		if a == "Sam" {
			t.Fatalf("desk specialist Steve must not wake Lab lead Sam: %v", got)
		}
	}
	if len(got) != 1 || got[0] != "Steve" {
		t.Fatalf("want only Steve, got %v", got)
	}
}

func TestMesh_DeskHuginnLeadHopCannotWakeLab(t *testing.T) {
	srv, store, _ := spaceReplyServer(t)
	hug, err := store.CreateCompany("Huginn", "", []string{"Winston", "Ava"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	lead := "Ava"
	if _, err := store.UpdateCompany(hug.ID, spaces.CompanyUpdates{Lead: &lead}); err != nil {
		t.Fatal(err)
	}
	lab, err := store.CreateCompany("Lab", "", []string{"Sam"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	labLead := "Sam"
	if _, err := store.UpdateCompany(lab.ID, spaces.CompanyUpdates{Lead: &labLead}); err != nil {
		t.Fatal(err)
	}
	desk, err := store.CreateChannel("desk", "Winston", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := store.PostSpaceMessage(desk.ID, "root", "")
	var mu sync.Mutex
	var ran []string
	srv.SetSpaceThreadRunner(func(_ context.Context, _, _, agent, _ string) (string, error) {
		mu.Lock()
		ran = append(ran, agent)
		mu.Unlock()
		if agent == "Winston" {
			return "@Ava take hostname", nil
		}
		if agent == "Ava" {
			return "@Sam lab secrets", nil
		}
		return "pong", nil
	})
	postSpaceJSON(srv, desk.ID, `{"content":"@Winston route this","parent_id":"`+root.ID+`"}`)
	srv.waitSpaceThreadWakes()
	mu.Lock()
	got := append([]string{}, ran...)
	mu.Unlock()
	for _, a := range got {
		if a == "Sam" {
			t.Fatalf("desk hop Winston→Ava must not reach Lab Sam: %v", got)
		}
	}
	hasW, hasA := false, false
	for _, a := range got {
		if a == "Winston" {
			hasW = true
		}
		if a == "Ava" {
			hasA = true
		}
	}
	if !hasW || !hasA {
		t.Fatalf("want Winston then Ava, got %v", got)
	}
}
