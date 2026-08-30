package spaces_test

import (
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func TestCreateCompany_EmptyVaultStaysEmpty(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("Acme", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	if c.Vault != "" {
		t.Errorf("empty vault was substituted: got %q", c.Vault)
	}
	got, err := store.GetCompany(c.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if got.Vault != "" {
		t.Errorf("empty vault did not stay empty after reload: got %q", got.Vault)
	}
}

func TestCreateCompany_ExampleVaultPersists(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("Default Co", "default", nil, "🏢", "#336699")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	if c.Vault != "default" {
		t.Errorf("vault = %q, want default", c.Vault)
	}
	if c.Icon != "🏢" || c.Color != "#336699" {
		t.Errorf("icon/color = %q %q", c.Icon, c.Color)
	}
}

func TestCreateCompany_EmptyNameRejected(t *testing.T) {
	store := newTestStore(t)
	_, err := store.CreateCompany("   ", "huginn", nil, "", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	var se *spaces.SpaceError
	if !errors.As(err, &se) || se.Code != "invalid_name" {
		t.Errorf("expected invalid_name, got %v", err)
	}
}

func TestCompanyRoster_Isolation(t *testing.T) {
	store := newTestStore(t)
	a, err := store.CreateCompany("Company A", "huginn", []string{"Winston", "coder"}, "", "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.CreateCompany("Company B", "", []string{"Winston", "reviewer"}, "", "")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	rosterA, err := store.CompanyRoster(a.ID)
	if err != nil {
		t.Fatalf("roster A: %v", err)
	}
	rosterB, err := store.CompanyRoster(b.ID)
	if err != nil {
		t.Fatalf("roster B: %v", err)
	}

	if !containsName(rosterA, "Winston") || !containsName(rosterA, "coder") {
		t.Errorf("roster A = %v, want Winston and coder", rosterA)
	}
	if containsName(rosterA, "reviewer") {
		t.Errorf("reviewer leaked into company A roster: %v", rosterA)
	}
	if !containsName(rosterB, "Winston") || !containsName(rosterB, "reviewer") {
		t.Errorf("roster B = %v, want Winston and reviewer", rosterB)
	}
	if containsName(rosterB, "coder") {
		t.Errorf("coder leaked into company B roster: %v", rosterB)
	}

	inA, err := store.AgentInCompany("coder", a.ID)
	if err != nil || !inA {
		t.Errorf("coder should be in A: in=%v err=%v", inA, err)
	}
	inB, err := store.AgentInCompany("coder", b.ID)
	if err != nil || inB {
		t.Errorf("coder must not be in B: in=%v err=%v", inB, err)
	}
}

func TestSeatWinstonInTwoCompanies_SpecialistStaysIsolated(t *testing.T) {
	store := newTestStore(t)
	a, err := store.CreateCompany("Company A", "huginn", nil, "", "")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.CreateCompany("Company B", "default", nil, "", "")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	if err := store.SeatMember(a.ID, "Winston"); err != nil {
		t.Fatalf("seat Winston in A: %v", err)
	}
	if err := store.SeatMember(b.ID, "Winston"); err != nil {
		t.Fatalf("seat Winston in B: %v", err)
	}
	if err := store.SeatMember(a.ID, "coder"); err != nil {
		t.Fatalf("seat coder in A: %v", err)
	}

	rosterA, _ := store.CompanyRoster(a.ID)
	rosterB, _ := store.CompanyRoster(b.ID)
	if !containsName(rosterA, "Winston") || !containsName(rosterB, "Winston") {
		t.Errorf("Winston should be in both rosters: A=%v B=%v", rosterA, rosterB)
	}
	if !containsName(rosterA, "coder") {
		t.Errorf("coder missing from A: %v", rosterA)
	}
	if containsName(rosterB, "coder") {
		t.Errorf("specialist coder must not appear in B: %v", rosterB)
	}

	winA, _ := store.AgentInCompany("Winston", a.ID)
	winB, _ := store.AgentInCompany("Winston", b.ID)
	if !winA || !winB {
		t.Errorf("Winston in A/B = %v/%v, want true/true", winA, winB)
	}
	coderB, _ := store.AgentInCompany("coder", b.ID)
	if coderB {
		t.Error("coder must not be in company B")
	}

	seated, err := store.CompaniesIn("Winston")
	if err != nil {
		t.Fatalf("CompaniesIn Winston: %v", err)
	}
	if len(seated) != 2 {
		t.Fatalf("Winston should be seated in 2 companies, got %d", len(seated))
	}
	ids := map[string]bool{seated[0].ID: true, seated[1].ID: true}
	if !ids[a.ID] || !ids[b.ID] {
		t.Errorf("CompaniesIn Winston = %v, want A and B", ids)
	}

	coderCos, err := store.CompaniesIn("coder")
	if err != nil {
		t.Fatalf("CompaniesIn coder: %v", err)
	}
	if len(coderCos) != 1 || coderCos[0].ID != a.ID {
		t.Errorf("coder CompaniesIn = %+v, want only A", coderCos)
	}
}

func TestCompaniesIn_EmptyMeansDeskOnly(t *testing.T) {
	store := newTestStore(t)
	_, err := store.CreateCompany("Someone Else", "huginn", []string{"coder"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	got, err := store.CompaniesIn("Winston")
	if err != nil {
		t.Fatalf("CompaniesIn: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("desk-only Winston should have empty CompaniesIn, got %+v", got)
	}
	nobody, err := store.CompaniesIn("ghost")
	if err != nil {
		t.Fatalf("CompaniesIn ghost: %v", err)
	}
	if len(nobody) != 0 {
		t.Errorf("unknown agent should be desk-only, got %+v", nobody)
	}
}

func TestUnseatMember_RemovesFromRosterOnly(t *testing.T) {
	store := newTestStore(t)
	a, _ := store.CreateCompany("A", "", []string{"Winston", "coder"}, "", "")
	b, _ := store.CreateCompany("B", "", []string{"Winston"}, "", "")
	if err := store.UnseatMember(a.ID, "Winston"); err != nil {
		t.Fatalf("UnseatMember: %v", err)
	}
	rosterA, _ := store.CompanyRoster(a.ID)
	if containsName(rosterA, "Winston") {
		t.Errorf("Winston still in A after unseat: %v", rosterA)
	}
	if !containsName(rosterA, "coder") {
		t.Errorf("coder should remain in A: %v", rosterA)
	}
	rosterB, _ := store.CompanyRoster(b.ID)
	if !containsName(rosterB, "Winston") {
		t.Errorf("unseat from A must not remove Winston from B: %v", rosterB)
	}
}

func TestUpdateCompany_VaultCanBeCleared(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("Acme", "huginn", nil, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	empty := ""
	updated, err := store.UpdateCompany(c.ID, spaces.CompanyUpdates{Vault: &empty})
	if err != nil {
		t.Fatalf("UpdateCompany: %v", err)
	}
	if updated.Vault != "" {
		t.Errorf("cleared vault was substituted: got %q", updated.Vault)
	}
}

func TestGetCompany_Unknown(t *testing.T) {
	store := newTestStore(t)
	_, err := store.GetCompany("does-not-exist")
	if !errors.Is(err, spaces.ErrCompanyNotFound) {
		t.Errorf("expected ErrCompanyNotFound, got %v", err)
	}
}

func TestListCompanies_ReturnsCreated(t *testing.T) {
	store := newTestStore(t)
	_, _ = store.CreateCompany("Zebra", "", nil, "", "")
	_, _ = store.CreateCompany("Alpha", "default", nil, "", "")
	list, err := store.ListCompanies()
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 companies, got %d", len(list))
	}
	if list[0].Name != "Alpha" || list[1].Name != "Zebra" {
		t.Errorf("expected name order Alpha, Zebra; got %q, %q", list[0].Name, list[1].Name)
	}
}

func TestSpace_BelongsToCompany_AndListFilter(t *testing.T) {
	store := newTestStore(t)
	co, err := store.CreateCompany("Acme", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	other, err := store.CreateCompany("Other", "", nil, "", "")
	if err != nil {
		t.Fatalf("CreateCompany other: %v", err)
	}

	desk, err := store.CreateChannel("desk-channel", "Winston", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel desk: %v", err)
	}
	if desk.CompanyID != "" {
		t.Errorf("desk-level space should have empty company_id, got %q", desk.CompanyID)
	}

	ch, err := store.CreateChannel("acme-eng", "Winston", []string{"coder"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	updated, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &co.ID})
	if err != nil {
		t.Fatalf("UpdateSpace company_id: %v", err)
	}
	if updated.CompanyID != co.ID {
		t.Errorf("space company_id = %q, want %q", updated.CompanyID, co.ID)
	}

	got, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatalf("GetSpace: %v", err)
	}
	if got.CompanyID != co.ID {
		t.Errorf("reloaded space company_id = %q, want %q", got.CompanyID, co.ID)
	}

	res, err := store.ListSpaces(spaces.ListOpts{CompanyID: co.ID})
	if err != nil {
		t.Fatalf("ListSpaces by company: %v", err)
	}
	if len(res.Spaces) != 1 || res.Spaces[0].ID != ch.ID {
		t.Errorf("list-by-company A: got %d spaces", len(res.Spaces))
	}

	resOther, err := store.ListSpaces(spaces.ListOpts{CompanyID: other.ID})
	if err != nil {
		t.Fatalf("ListSpaces other: %v", err)
	}
	if len(resOther.Spaces) != 0 {
		t.Errorf("company B should have no spaces, got %d", len(resOther.Spaces))
	}

	all, err := store.ListSpaces(spaces.ListOpts{})
	if err != nil {
		t.Fatalf("ListSpaces all: %v", err)
	}
	if len(all.Spaces) < 2 {
		t.Errorf("unfiltered list should include desk + company spaces, got %d", len(all.Spaces))
	}

	empty := ""
	cleared, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &empty})
	if err != nil {
		t.Fatalf("clear company_id: %v", err)
	}
	if cleared.CompanyID != "" {
		t.Errorf("cleared space should be desk-level, got company_id %q", cleared.CompanyID)
	}
}

func TestUpdateSpace_UnknownCompanyRejected(t *testing.T) {
	store := newTestStore(t)
	ch, err := store.CreateChannel("orphan", "Winston", nil, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	bad := "no-such-company"
	_, err = store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &bad})
	if !errors.Is(err, spaces.ErrCompanyNotFound) {
		t.Errorf("expected ErrCompanyNotFound, got %v", err)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestSpaceCompanyID_DeskAndCompanyAndUnknown(t *testing.T) {
	store := newTestStore(t)
	co, err := store.CreateCompany("Acme", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	desk, err := store.CreateChannel("desk", "Winston", nil, "", "")
	if err != nil {
		t.Fatalf("CreateChannel desk: %v", err)
	}
	id, err := store.SpaceCompanyID(desk.ID)
	if err != nil {
		t.Fatalf("SpaceCompanyID desk: %v", err)
	}
	if id != "" {
		t.Errorf("desk space company_id = %q, want empty", id)
	}

	ch, err := store.CreateChannel("acme", "Winston", []string{"coder"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &co.ID}); err != nil {
		t.Fatalf("assign company: %v", err)
	}
	id, err = store.SpaceCompanyID(ch.ID)
	if err != nil {
		t.Fatalf("SpaceCompanyID company: %v", err)
	}
	if id != co.ID {
		t.Errorf("company space id = %q, want %q", id, co.ID)
	}

	id, err = store.SpaceCompanyID("does-not-exist")
	if err != nil {
		t.Fatalf("unknown space should not error: %v", err)
	}
	if id != "" {
		t.Errorf("unknown space company_id = %q, want empty", id)
	}
	id, err = store.SpaceCompanyID("")
	if err != nil || id != "" {
		t.Errorf("empty spaceID = %q err=%v, want empty", id, err)
	}
}

func TestDeleteCompany_HasSpacesRejected_HuginnReserved(t *testing.T) {
	store := newTestStore(t)
	keep, err := store.CreateCompany("Huginn", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	leftover, err := store.CreateCompany("WringerH-wr87035", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannelForCompany("wringer-chat", "Winston", nil, "", "", leftover.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteCompany(leftover.ID); !errors.Is(err, spaces.ErrCompanyHasSpaces) {
		t.Fatalf("delete with spaces must fail closed, got %v", err)
	}
	if err := store.DeleteCompany(keep.ID); !errors.Is(err, spaces.ErrCompanyReserved) {
		t.Fatalf("Huginn delete must fail closed, got %v", err)
	}
	if _, err := store.GetCompany(keep.ID); err != nil {
		t.Fatalf("Huginn must survive: %v", err)
	}
	sp, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sp.CompanyID != leftover.ID {
		t.Fatalf("space must stay attached, company_id=%q", sp.CompanyID)
	}
	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteCompany(leftover.ID); err != nil {
		t.Fatalf("delete leftover after archive: %v", err)
	}
	if _, err := store.GetCompany(leftover.ID); !errors.Is(err, spaces.ErrCompanyNotFound) {
		t.Fatalf("leftover still loadable: %v", err)
	}
}

func TestTwoCompaniesSameChannelName(t *testing.T) {
	store := newTestStore(t)
	a, err := store.CreateCompany("SpaceX", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.CreateCompany("Tesla", "", []string{"Winston", "Reggie"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	engA, err := store.CreateChannelForCompany("eng", "Winston", []string{"Steve"}, "", "", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	engB, err := store.CreateChannelForCompany("eng", "Winston", []string{"Reggie"}, "", "", b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if engA.ID == engB.ID || engA.CompanyID != a.ID || engB.CompanyID != b.ID {
		t.Fatalf("same-name spaces must be distinct: %+v %+v", engA, engB)
	}
	_, err = store.CreateChannelForCompany("eng", "Winston", nil, "", "", a.ID)
	if !errors.Is(err, spaces.ErrChannelNameTaken) {
		t.Fatalf("same company same name must conflict, got %v", err)
	}
}

func TestSeatAfterDeleteFailsClosed(t *testing.T) {
	store := newTestStore(t)
	co, err := store.CreateCompany("RaceLab", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteCompany(co.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SeatMember(co.ID, "Steve"); !errors.Is(err, spaces.ErrCompanyNotFound) {
		t.Fatalf("seat after delete must be not-found, got %v", err)
	}
}

func TestPostSpaceMessage_AfterCompanyGoneFailsClosed(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	co, err := store.CreateCompany("GoneCo", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannelForCompany("hall", "Winston", nil, "", "", co.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write().Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write().Exec(`DELETE FROM companies WHERE id=?`, co.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write().Exec(`UPDATE spaces SET company_id=? WHERE id=?`, co.ID, ch.ID); err != nil {
		t.Fatal(err)
	}
	_, err = store.PostSpaceMessage(ch.ID, "reply after delete", "")
	if !errors.Is(err, spaces.ErrCompanyNotFound) {
		t.Fatalf("reply after company gone must fail closed, got %v", err)
	}
}


func TestCreateCompany_DuplicateNameConflict(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateCompany("DupCo", "", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	_, err := store.CreateCompany("dupco", "", nil, "", "")
	if !errors.Is(err, spaces.ErrCompanyNameTaken) {
		t.Fatalf("case-fold duplicate must be name_taken, got %v", err)
	}
}

func TestCreateCompany_ConcurrentSameName(t *testing.T) {
	store := newTestStore(t)
	errc := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := store.CreateCompany("RaceCo", "huginn", nil, "", "")
			errc <- err
		}()
	}
	var ok, taken, other int
	for i := 0; i < 2; i++ {
		err := <-errc
		switch {
		case err == nil:
			ok++
		case errors.Is(err, spaces.ErrCompanyNameTaken):
			taken++
		default:
			other++
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 || taken != 1 {
		t.Fatalf("concurrent same-name: ok=%d taken=%d other=%d (want 1/1/0)", ok, taken, other)
	}
	list, err := store.ListCompanies()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want exactly 1 RaceCo, got %d", len(list))
	}
}

func TestSeatCaseFoldAfterUnseat(t *testing.T) {
	store := newTestStore(t)
	co, err := store.CreateCompany("Lab", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UnseatMember(co.ID, "winston"); err != nil {
		t.Fatalf("unseat case-fold: %v", err)
	}
	roster, _ := store.CompanyRoster(co.ID)
	if containsName(roster, "Winston") || containsName(roster, "winston") {
		t.Fatalf("unseat winston must remove Winston: %v", roster)
	}
	if err := store.SeatMember(co.ID, "winston"); err != nil {
		t.Fatal(err)
	}
	if err := store.SeatMember(co.ID, "Winston"); err != nil {
		t.Fatal(err)
	}
	roster, _ = store.CompanyRoster(co.ID)
	if len(roster) != 1 {
		t.Fatalf("case-fold seat must be one row, got %v", roster)
	}
	in, _ := store.AgentInCompany("WINSTON", co.ID)
	if !in {
		t.Fatal("WINSTON must match seated winston")
	}
}

func TestTwoCompaniesSameVaultLabelStayIsolated(t *testing.T) {
	store := newTestStore(t)
	a, err := store.CreateCompany("CoA", "huginn", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.CreateCompany("CoB", "huginn", []string{"Winston", "Reggie"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Vault != "huginn" || b.Vault != "huginn" {
		t.Fatalf("vaults = %q %q", a.Vault, b.Vault)
	}
	steveB, _ := store.AgentInCompany("Steve", b.ID)
	reggieA, _ := store.AgentInCompany("Reggie", a.ID)
	if steveB || reggieA {
		t.Fatal("same vault label must not merge rosters")
	}
	winA, _ := store.AgentInCompany("Winston", a.ID)
	winB, _ := store.AgentInCompany("Winston", b.ID)
	if !winA || !winB {
		t.Fatal("Winston may sit in both")
	}
}

func TestSeatWinstonInManyCompanies(t *testing.T) {
	store := newTestStore(t)
	var ids []string
	for i := 0; i < 8; i++ {
		c, err := store.CreateCompany("Many-"+string(rune('A'+i)), "", []string{"Winston"}, "", "")
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ids = append(ids, c.ID)
	}
	if err := store.SeatMember(ids[0], "coder"); err != nil {
		t.Fatal(err)
	}
	seated, err := store.CompaniesIn("Winston")
	if err != nil || len(seated) != 8 {
		t.Fatalf("Winston in %d companies, want 8 err=%v", len(seated), err)
	}
	coderCos, err := store.CompaniesIn("coder")
	if err != nil || len(coderCos) != 1 || coderCos[0].ID != ids[0] {
		t.Fatalf("coder leaked: %+v", coderCos)
	}
}

func TestCreateCompany_UnicodeNameHexID(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("WringerÜ-wr87035", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "WringerÜ-wr87035" {
		t.Fatalf("name=%q", c.Name)
	}
	for _, r := range c.ID {
		if r > 127 || !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("company id must be hex, got %q", c.ID)
		}
	}
}

func TestDeleteCompany_Unknown(t *testing.T) {
	store := newTestStore(t)
	if err := store.DeleteCompany("nope"); !errors.Is(err, spaces.ErrCompanyNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestDeleteCompany_TwoArchivedSameName(t *testing.T) {
	store := newTestStore(t)
	a, err := store.CreateCompany("ArchA", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.CreateCompany("ArchB", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	engA, err := store.CreateChannelForCompany("eng", "Winston", nil, "", "", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	engB, err := store.CreateChannelForCompany("eng", "Winston", nil, "", "", b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveSpace(engA.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveSpace(engB.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteCompany(a.ID); err != nil {
		t.Fatalf("delete A after archive: %v", err)
	}
	if err := store.DeleteCompany(b.ID); err != nil {
		t.Fatalf("delete B after archive same name: %v", err)
	}
}

func TestCreateCompany_DefaultsLeadToWinstonIfSeated(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("Huginn", "huginn", []string{"Steve", "Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Lead != "Winston" {
		t.Fatalf("lead=%q want Winston", c.Lead)
	}
}

func TestCreateCompany_DefaultsLeadToFirstSeated(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("Lab", "", []string{"Ava", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Lead != "Ava" {
		t.Fatalf("lead=%q want Ava", c.Lead)
	}
}

func TestUpdateCompany_LeadMustBeSeated(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("Huginn", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	bad := "Reggie"
	_, err = store.UpdateCompany(c.ID, spaces.CompanyUpdates{Lead: &bad})
	if !errors.Is(err, spaces.ErrCompanyLeadNotSeated) {
		t.Fatalf("unseated specialist as lead: %v", err)
	}
	ok := "Steve"
	updated, err := store.UpdateCompany(c.ID, spaces.CompanyUpdates{Lead: &ok})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Lead != "Steve" {
		t.Fatalf("seated lead=%q", updated.Lead)
	}
}

func TestUnseatLead_Redefaults(t *testing.T) {
	store := newTestStore(t)
	c, err := store.CreateCompany("Huginn", "", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UnseatMember(c.ID, "Winston"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetCompany(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lead != "Steve" {
		t.Fatalf("after unseat Winston, lead=%q want Steve", got.Lead)
	}
}
