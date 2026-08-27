package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/spaces"
)

func companyTestServer(t *testing.T) (*Server, *spaces.SQLiteSpaceStore) {
	t.Helper()
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Winston"},
			{Name: "Steve"},
			{Name: "Reggie"},
			{Name: "Sam"},
			{Name: "Ava"},
		}}, nil
	}
	return srv, store
}

func TestHandleListCompanies_Empty(t *testing.T) {
	srv, _ := companyTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil)
	w := httptest.NewRecorder()
	srv.handleListCompanies(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Companies []*spaces.Company `json:"companies"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Companies == nil {
		t.Fatal("expected non-nil companies list")
	}
	if len(result.Companies) != 0 {
		t.Errorf("expected empty list, got %d", len(result.Companies))
	}
}

func TestHandleListCompanies_NoStore(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil)
	w := httptest.NewRecorder()
	srv.handleListCompanies(w, req)
	if w.Code != 503 {
		t.Errorf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateCompany_AndList(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"Huginn","vault":"huginn","members":["Winston","Steve"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" || created.Name != "Huginn" || created.Vault != "huginn" {
		t.Errorf("created = %+v", created)
	}
	if len(created.Members) != 2 {
		t.Errorf("members = %v", created.Members)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil)
	w = httptest.NewRecorder()
	srv.handleListCompanies(w, req)
	if w.Code != 200 {
		t.Fatalf("list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Companies []*spaces.Company `json:"companies"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	if len(result.Companies) != 1 || result.Companies[0].ID != created.ID {
		t.Errorf("list = %+v", result.Companies)
	}
}

func TestHandleCreateCompany_EmptyName(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"   ","vault":"huginn"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateCompany_EmptyVaultStaysEmpty(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"Lab","vault":"","members":["Winston"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Vault != "" {
		t.Errorf("empty vault was substituted: got %q", created.Vault)
	}
}

func TestHandleCreateCompany_InvalidJSON(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListSpaces_FilterByCompanyID(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Acme", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	if _, err := store.CreateChannel("desk", "Winston", nil, "", ""); err != nil {
		t.Fatalf("CreateChannel desk: %v", err)
	}
	ch, err := store.CreateChannel("acme-eng", "Winston", nil, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if _, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{CompanyID: &co.ID}); err != nil {
		t.Fatalf("UpdateSpace: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces?company_id="+co.ID, nil)
	w := httptest.NewRecorder()
	srv.handleListSpaces(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result spaces.ListSpacesResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Spaces) != 1 || result.Spaces[0].ID != ch.ID {
		t.Errorf("company filter: got %d spaces", len(result.Spaces))
	}
	if len(result.Spaces) == 1 && result.Spaces[0].CompanyID != co.ID {
		t.Errorf("space company_id = %q, want %q", result.Spaces[0].CompanyID, co.ID)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	w = httptest.NewRecorder()
	srv.handleListSpaces(w, req)
	var all spaces.ListSpacesResult
	if err := json.NewDecoder(w.Body).Decode(&all); err != nil {
		t.Fatalf("decode all: %v", err)
	}
	if len(all.Spaces) < 2 {
		t.Errorf("unfiltered list should include desk + company spaces, got %d", len(all.Spaces))
	}
}

func TestHandleCreateSpace_WithCompanyID(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Acme", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	body := strings.NewReader(`{"name":"acme-chat","lead_agent":"Winston","member_agents":[],"company_id":"` + co.ID + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", body)
	w := httptest.NewRecorder()
	srv.handleCreateSpace(w, req)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sp spaces.Space
	if err := json.NewDecoder(w.Body).Decode(&sp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sp.CompanyID != co.ID {
		t.Errorf("created space company_id = %q, want %q", sp.CompanyID, co.ID)
	}
}

func TestHandleCreateSpace_UnknownCompany(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"orphan","lead_agent":"Winston","company_id":"nosuchcompany"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", body)
	w := httptest.NewRecorder()
	srv.handleCreateSpace(w, req)
	if w.Code != 404 {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateSpace_SetCompanyID(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Acme", "default", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	ch, err := store.CreateChannel("attach-me", "Winston", nil, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	body := strings.NewReader(`{"company_id":"` + co.ID + `"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/spaces/"+ch.ID, body)
	req.SetPathValue("id", ch.ID)
	w := httptest.NewRecorder()
	srv.handleUpdateSpace(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sp spaces.Space
	if err := json.NewDecoder(w.Body).Decode(&sp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sp.CompanyID != co.ID {
		t.Errorf("updated space company_id = %q, want %q", sp.CompanyID, co.ID)
	}
}

func TestHandleSeatAndUnseatCompanyMember(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Lab", "", []string{"Winston", "Sam"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}

	body := strings.NewReader(`{"agent":"Reggie"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+co.ID+"/members", body)
	req.SetPathValue("id", co.ID)
	w := httptest.NewRecorder()
	srv.handleSeatCompanyMember(w, req)
	if w.Code != 200 {
		t.Fatalf("seat: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var seated spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&seated); err != nil {
		t.Fatalf("seat decode: %v", err)
	}
	if !containsName(seated.Members, "Reggie") {
		t.Fatalf("seated members = %v, want Reggie", seated.Members)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+co.ID+"/members/Reggie", nil)
	req.SetPathValue("id", co.ID)
	req.SetPathValue("agent", "Reggie")
	w = httptest.NewRecorder()
	srv.handleUnseatCompanyMember(w, req)
	if w.Code != 200 {
		t.Fatalf("unseat: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var unseated spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&unseated); err != nil {
		t.Fatalf("unseat decode: %v", err)
	}
	if containsName(unseated.Members, "Reggie") {
		t.Fatalf("unseated still has Reggie: %v", unseated.Members)
	}
	if !containsName(unseated.Members, "Winston") {
		t.Fatalf("unseat dropped Winston: %v", unseated.Members)
	}
}

func TestHandleSeatCompanyMember_UnknownAgent(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Lab", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	body := strings.NewReader(`{"agent":"nobody"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+co.ID+"/members", body)
	req.SetPathValue("id", co.ID)
	w := httptest.NewRecorder()
	srv.handleSeatCompanyMember(w, req)
	if w.Code != 422 {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetCompany(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Huginn", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+co.ID, nil)
	req.SetPathValue("id", co.ID)
	w := httptest.NewRecorder()
	srv.handleGetCompany(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != co.ID || got.Name != "Huginn" {
		t.Fatalf("got %+v", got)
	}
}

func containsName(list []string, name string) bool {
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

func TestHandleCreateCompany_ControlName(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"bad\u0000name","vault":"huginn","members":["Winston"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 400 {
		t.Fatalf("NUL in name must be 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteCompany(t *testing.T) {
	srv, store := companyTestServer(t)
	keep, err := store.CreateCompany("Huginn", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	leftover, err := store.CreateCompany("WringerL-wr87035", "lab", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+leftover.ID, nil)
	req.SetPathValue("id", leftover.ID)
	w := httptest.NewRecorder()
	srv.handleDeleteCompany(w, req)
	if w.Code != 200 {
		t.Fatalf("delete leftover: %d %s", w.Code, w.Body.String())
	}
	if _, err := store.GetCompany(leftover.ID); !errorsIsCompanyNotFound(err) {
		t.Fatalf("leftover still present: %v", err)
	}
	if _, err := store.GetCompany(keep.ID); err != nil {
		t.Fatalf("Huginn deleted: %v", err)
	}
}

func errorsIsCompanyNotFound(err error) bool {
	return err != nil && (err == spaces.ErrCompanyNotFound || strings.Contains(err.Error(), "company not found"))
}

func TestHandleDeleteCompany_Unknown(t *testing.T) {
	srv, _ := companyTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/companies/nope", nil)
	req.SetPathValue("id", "nope")
	w := httptest.NewRecorder()
	srv.handleDeleteCompany(w, req)
	if w.Code != 404 {
		t.Fatalf("want 404, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateCompany_ConcurrentSameName(t *testing.T) {
	srv, _ := companyTestServer(t)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			body := strings.NewReader(`{"name":"RaceCo","vault":"huginn","members":["Winston"]}`)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
			w := httptest.NewRecorder()
			srv.handleCreateCompany(w, req)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	var created, conflict int
	for c := range codes {
		switch c {
		case 201:
			created++
		case 409:
			conflict++
		default:
			t.Fatalf("unexpected code %d", c)
		}
	}
	if created != 1 || conflict != 1 {
		t.Fatalf("concurrent same-name: created=%d conflict=%d (want 1/1)", created, conflict)
	}
}

func TestHandleCreateCompany_UnicodeNameHexID(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"WringerÜ-bullet2","vault":"","members":["Winston"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 201 {
		t.Fatalf("unicode name: %d %s", w.Code, w.Body.String())
	}
	var created spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "WringerÜ-bullet2" {
		t.Fatalf("name=%q", created.Name)
	}
	for _, r := range created.ID {
		if r > 127 {
			t.Fatalf("company id leaked unicode: %q", created.ID)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/companies/WringerÜ-bullet2", nil)
	req.SetPathValue("id", "WringerÜ-bullet2")
	w = httptest.NewRecorder()
	srv.handleGetCompany(w, req)
	if w.Code != 404 {
		t.Fatalf("unicode-as-id must 404, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleSeatCaseFoldAfterUnseat(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("FoldLab", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+co.ID+"/members/winston", nil)
	req.SetPathValue("id", co.ID)
	req.SetPathValue("agent", "winston")
	w := httptest.NewRecorder()
	srv.handleUnseatCompanyMember(w, req)
	if w.Code != 200 {
		t.Fatalf("unseat: %d %s", w.Code, w.Body.String())
	}
	var after spaces.Company
	json.NewDecoder(w.Body).Decode(&after)
	if containsName(after.Members, "Winston") {
		t.Fatalf("case-fold unseat left Winston: %v", after.Members)
	}
	body := strings.NewReader(`{"agent":"winston"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+co.ID+"/members", body)
	req.SetPathValue("id", co.ID)
	w = httptest.NewRecorder()
	srv.handleSeatCompanyMember(w, req)
	if w.Code != 200 {
		t.Fatalf("reseat: %d %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&after)
	if len(after.Members) != 1 {
		t.Fatalf("want 1 member after case-fold reseat, got %v", after.Members)
	}
}

func TestHandleCreateCompany_SameVaultLabelOK(t *testing.T) {
	srv, _ := companyTestServer(t)
	for _, name := range []string{"VaultA", "VaultB"} {
		body := strings.NewReader(`{"name":"` + name + `","vault":"huginn","members":["Winston"]}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
		w := httptest.NewRecorder()
		srv.handleCreateCompany(w, req)
		if w.Code != 201 {
			t.Fatalf("%s same vault: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestHandleSeatWinstonInManyCompanies(t *testing.T) {
	srv, store := companyTestServer(t)
	for i := 0; i < 8; i++ {
		co, err := store.CreateCompany("WinMany-"+string(rune('A'+i)), "", nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		body := strings.NewReader(`{"agent":"Winston"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+co.ID+"/members", body)
		req.SetPathValue("id", co.ID)
		w := httptest.NewRecorder()
		srv.handleSeatCompanyMember(w, req)
		if w.Code != 200 {
			t.Fatalf("seat %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	seated, err := store.CompaniesIn("Winston")
	if err != nil || len(seated) != 8 {
		t.Fatalf("Winston in %d, want 8 err=%v", len(seated), err)
	}
}

func TestHandleDeleteCompany_HuginnReserved(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Huginn", "huginn", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+co.ID, nil)
	req.SetPathValue("id", co.ID)
	w := httptest.NewRecorder()
	srv.handleDeleteCompany(w, req)
	if w.Code != 409 {
		t.Fatalf("delete Huginn must 409, got %d %s", w.Code, w.Body.String())
	}
	if _, err := store.GetCompany(co.ID); err != nil {
		t.Fatalf("Huginn gone: %v", err)
	}
}

func TestHandleDeleteCompany_HasSpaces(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("HasSpacesCo", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateChannelForCompany("eng", "Winston", nil, "", "", co.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+co.ID, nil)
	req.SetPathValue("id", co.ID)
	w := httptest.NewRecorder()
	srv.handleDeleteCompany(w, req)
	if w.Code != 409 {
		t.Fatalf("delete with spaces must 409, got %d %s", w.Code, w.Body.String())
	}
	if _, err := store.GetCompany(co.ID); err != nil {
		t.Fatalf("company deleted despite spaces: %v", err)
	}
}

func TestHandleCreateSpace_SameNameTwoCompanies(t *testing.T) {
	srv, store := companyTestServer(t)
	a, err := store.CreateCompany("CoA", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.CreateCompany("CoB", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a.ID, b.ID} {
		body := strings.NewReader(`{"name":"eng","lead_agent":"Winston","member_agents":[],"company_id":"` + id + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", body)
		w := httptest.NewRecorder()
		srv.handleCreateSpace(w, req)
		if w.Code != 201 {
			t.Fatalf("same name in company %s: %d %s", id[:8], w.Code, w.Body.String())
		}
	}
}

func TestHandleListCompanies_AfterLeftoverDelete(t *testing.T) {
	srv, store := companyTestServer(t)
	keep, err := store.CreateCompany("Huginn", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	leftover, err := store.CreateCompany("WringerL-b3", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+leftover.ID, nil)
	req.SetPathValue("id", leftover.ID)
	w := httptest.NewRecorder()
	srv.handleDeleteCompany(w, req)
	if w.Code != 200 {
		t.Fatalf("delete leftover: %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil)
	w = httptest.NewRecorder()
	srv.handleListCompanies(w, req)
	var result struct {
		Companies []*spaces.Company `json:"companies"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Companies) != 1 || result.Companies[0].ID != keep.ID {
		t.Fatalf("list after leftover delete: %+v", result.Companies)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+leftover.ID, nil)
	req.SetPathValue("id", leftover.ID)
	w = httptest.NewRecorder()
	srv.handleGetCompany(w, req)
	if w.Code != 404 {
		t.Fatalf("GET leftover after delete must 404, got %d %s", w.Code, w.Body.String())
	}
}

func TestHandleSeatAfterDeleteRace(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("RaceSeat", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+co.ID, nil)
		req.SetPathValue("id", co.ID)
		w := httptest.NewRecorder()
		srv.handleDeleteCompany(w, req)
		codes <- w.Code
	}()
	go func() {
		defer wg.Done()
		body := strings.NewReader(`{"agent":"Steve"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+co.ID+"/members", body)
		req.SetPathValue("id", co.ID)
		w := httptest.NewRecorder()
		srv.handleSeatCompanyMember(w, req)
		codes <- w.Code
	}()
	wg.Wait()
	close(codes)
	for c := range codes {
		if c >= 500 {
			t.Fatalf("race must not 500, got %d", c)
		}
	}
	if _, err := store.GetCompany(co.ID); err == nil {
		// delete lost the race — company still there, Steve may be seated
		return
	}
	in, err := store.AgentInCompany("Steve", co.ID)
	if err != nil {
		t.Fatal(err)
	}
	if in {
		t.Fatal("orphan seat after company delete")
	}
}

func TestHandleCreateCompany_DefaultsLeadWinston(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"Huginn","members":["Steve","Winston"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Lead != "Winston" {
		t.Fatalf("lead=%q want Winston", created.Lead)
	}
}

func TestHandleCreateCompany_LeadMustBeSeated(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"Huginn","members":["Winston"],"lead":"Reggie"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for unseated lead, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateCompany_ExplicitSeatedLead(t *testing.T) {
	srv, _ := companyTestServer(t)
	body := strings.NewReader(`{"name":"Huginn","members":["Winston","Ava"],"lead":"Ava"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", body)
	w := httptest.NewRecorder()
	srv.handleCreateCompany(w, req)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Lead != "Ava" {
		t.Fatalf("lead=%q want Ava", created.Lead)
	}
}

func TestHandleUpdateCompany_LeadEmptyInvalidLabSam(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Huginn", "huginn", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateCompany(co.ID, spaces.CompanyUpdates{Lead: ptr("Winston")}); err != nil {
		t.Fatal(err)
	}

	// Empty lead snaps back to default Winston — never a blank CoS.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/companies/"+co.ID, strings.NewReader(`{"lead":""}`))
	req.SetPathValue("id", co.ID)
	srv.handleUpdateCompany(w, req)
	if w.Code != 200 {
		t.Fatalf("empty lead: want 200, got %d %s", w.Code, w.Body.String())
	}
	var got spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Lead != "Winston" {
		t.Fatalf("empty lead must become Winston, got %q", got.Lead)
	}

	// Invalid / unknown agent.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/companies/"+co.ID, strings.NewReader(`{"lead":"NotAnAgent"}`))
	req.SetPathValue("id", co.ID)
	srv.handleUpdateCompany(w, req)
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("unknown lead must be 4xx not 500, got %d %s", w.Code, w.Body.String())
	}

	// Lab Sam is a real agent but not seated in Huginn.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/companies/"+co.ID, strings.NewReader(`{"lead":"Sam"}`))
	req.SetPathValue("id", co.ID)
	srv.handleUpdateCompany(w, req)
	if w.Code != 400 {
		t.Fatalf("Lab Sam lead must be 400, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "lead") && !strings.Contains(w.Body.String(), "seated") {
		t.Errorf("error should mention lead/seated, got %s", w.Body.String())
	}

	// Huginn lead unchanged.
	cur, err := store.GetCompany(co.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Lead != "Winston" {
		t.Fatalf("Huginn lead mutated to %q", cur.Lead)
	}
}

func TestHandleUpdateCompany_SeatedLeadSteve(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Huginn", "huginn", []string{"Winston", "Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/companies/"+co.ID, strings.NewReader(`{"lead":"Steve"}`))
	req.SetPathValue("id", co.ID)
	srv.handleUpdateCompany(w, req)
	if w.Code != 200 {
		t.Fatalf("seated Steve: want 200, got %d %s", w.Code, w.Body.String())
	}
	var got spaces.Company
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Lead != "Steve" {
		t.Fatalf("lead=%q want Steve", got.Lead)
	}
}

func ptr(s string) *string { return &s }
