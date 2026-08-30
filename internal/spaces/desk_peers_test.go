package spaces_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func TestDeskPeerNames_ListsDeskDMLeadsOnly(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.OpenDM("Steve"); err != nil {
		t.Fatalf("OpenDM Steve: %v", err)
	}
	if _, err := store.OpenDM("Winston"); err != nil {
		t.Fatalf("OpenDM Winston: %v", err)
	}
	if _, err := store.CreateChannel("desk-hall", "Chris", []string{"Steve"}, "", ""); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	peers, err := store.DeskPeerNames()
	if err != nil {
		t.Fatalf("DeskPeerNames: %v", err)
	}
	if !containsName(peers, "Steve") || !containsName(peers, "Winston") {
		t.Fatalf("desk peers must include Steve+Winston, got %v", peers)
	}
	if containsName(peers, "Chris") {
		t.Fatalf("channel-only lead must not be a desk peer, got %v", peers)
	}
}

func TestSpaceIsDeskDM_OpenDMTrueCompanyFalseChannelFalse(t *testing.T) {
	store := newTestStore(t)
	dm, err := store.OpenDM("Steve")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}
	ok, err := store.SpaceIsDeskDM(dm.ID)
	if err != nil || !ok {
		t.Fatalf("OpenDM must be desk DM, ok=%v err=%v", ok, err)
	}
	ch, err := store.CreateChannel("desk-hall", "Steve", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	ok, err = store.SpaceIsDeskDM(ch.ID)
	if err != nil || ok {
		t.Fatalf("desk channel must not be desk DM, ok=%v err=%v", ok, err)
	}
	unknown, err := store.SpaceIsDeskDM("missing")
	if err != nil || unknown {
		t.Fatalf("unknown space must be false,nil got ok=%v err=%v", unknown, err)
	}
}

func TestIsDeskDM_Helper(t *testing.T) {
	if spaces.IsDeskDM(nil) {
		t.Fatal("nil is not a desk DM")
	}
	if !spaces.IsDeskDM(&spaces.Space{Kind: spaces.KindDM, LeadAgent: "Steve"}) {
		t.Fatal("empty company_id DM is desk")
	}
	if spaces.IsDeskDM(&spaces.Space{Kind: spaces.KindDM, LeadAgent: "Steve", CompanyID: "lab"}) {
		t.Fatal("company DM is not desk")
	}
	if spaces.IsDeskDM(&spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Steve"}) {
		t.Fatal("desk channel is not a desk DM")
	}
}

