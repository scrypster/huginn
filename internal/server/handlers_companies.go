package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
)

// companyAPI is the company subset of the space store. *spaces.SQLiteSpaceStore
// implements it; StoreInterface mocks used in other tests do not have to.
type companyAPI interface {
	ListCompanies() ([]*spaces.Company, error)
	CreateCompany(name, vault string, members []string, icon, color string) (*spaces.Company, error)
	GetCompany(id string) (*spaces.Company, error)
	UpdateCompany(id string, updates spaces.CompanyUpdates) (*spaces.Company, error)
	SeatMember(companyID, agentName string) error
	UnseatMember(companyID, agentName string) error
	DeleteCompany(id string) error
	AgentInCompany(agent, companyID string) (bool, error)
}

func (s *Server) companyAPI() companyAPI {
	if s.spaceStore == nil {
		return nil
	}
	cs, ok := s.spaceStore.(companyAPI)
	if !ok {
		return nil
	}
	return cs
}

func (s *Server) lookupCompanyName(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	cs := s.companyAPI()
	if cs == nil {
		return ""
	}
	c, err := cs.GetCompany(id)
	if err != nil || c == nil {
		return ""
	}
	return strings.TrimSpace(c.Name)
}

func (s *Server) companyRosterMap() map[string][]string {
	cs := s.companyAPI()
	if cs == nil {
		return nil
	}
	list, err := cs.ListCompanies()
	if err != nil {
		return nil
	}
	out := make(map[string][]string, len(list))
	for _, c := range list {
		if c == nil || strings.TrimSpace(c.Name) == "" {
			continue
		}
		out[c.Name] = append([]string(nil), c.Members...)
	}
	return out
}

func (s *Server) lookupCompanyMembers(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for k, v := range s.companyRosterMap() {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

// fillEmptyHarnessPersist harness-fills leftover-empty persist for named-company
// roster (Lab) and this-channel headcount. Ack/ping fills already ran in
// PersistVisibleAssistantContent.
func (s *Server) fillEmptyHarnessPersist(content, ask string, sess *session.Session) string {
	if strings.TrimSpace(content) != "" {
		return content
	}
	if company, ok := backend.NamedCompanyRosterAsk(ask); ok {
		if names := s.lookupCompanyMembers(company); len(names) > 0 {
			if filled := backend.FillNamedCompanyRosterPersist("", ask, company, names); filled != "" {
				return filled
			}
		}
	}
	var members []string
	if sess != nil && s.spaceStore != nil {
		if spaceID := sess.SpaceID(); spaceID != "" {
			if sp, err := s.spaceStore.GetSpace(spaceID); err == nil && sp != nil {
				members = append(members, sp.Members...)
				if lead := strings.TrimSpace(sp.LeadAgent); lead != "" {
					members = append(members, lead)
				}
			}
		}
	}
	return backend.FillTrivialHeadcountPersist(content, ask, members)
}

func (s *Server) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	cs := s.companyAPI()
	if cs == nil {
		jsonError(w, 503, "companies not configured")
		return
	}
	list, err := cs.ListCompanies()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*spaces.Company{}
	}
	jsonOK(w, map[string]any{"companies": list})
}

func (s *Server) handleCreateCompany(w http.ResponseWriter, r *http.Request) {
	cs := s.companyAPI()
	if cs == nil {
		jsonError(w, 503, "companies not configured")
		return
	}
	var body struct {
		Name    string   `json:"name"`
		Vault   string   `json:"vault"`
		Members []string `json:"members"`
		Lead    string   `json:"lead"`
		Icon    string   `json:"icon"`
		Color   string   `json:"color"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		jsonError(w, 400, "name is required")
		return
	}
	if len(body.Name) > 80 {
		jsonError(w, 400, "name must be 80 characters or fewer")
		return
	}
	if spaces.HasControlRunes(body.Name) {
		jsonError(w, 400, "name cannot contain control characters")
		return
	}
	if len(body.Members) > 20 {
		jsonError(w, 400, "too many members (max 20)")
		return
	}
	seen := make(map[string]struct{}, len(body.Members))
	var members []string
	for _, m := range body.Members {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, dup := seen[strings.ToLower(m)]; dup {
			jsonError(w, 400, fmt.Sprintf("duplicate member %q", m))
			return
		}
		seen[strings.ToLower(m)] = struct{}{}
		if err := validateSubdomain(m); err != nil {
			jsonError(w, 400, fmt.Sprintf("member %q: %s", m, err.Error()))
			return
		}
		if err := s.validateAgentExists(m); err != nil {
			jsonError(w, 422, fmt.Sprintf("member %q: %s", m, err.Error()))
			return
		}
		members = append(members, m)
	}
	if lead := strings.TrimSpace(body.Lead); lead != "" {
		if spaces.CanonicalSeatedName(members, lead) == "" {
			jsonError(w, 400, lead+" must be seated in this company.")
			return
		}
	}
	c, err := cs.CreateCompany(body.Name, body.Vault, members, body.Icon, body.Color)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	if lead := strings.TrimSpace(body.Lead); lead != "" {
		if err := validateSubdomain(lead); err != nil {
			jsonError(w, 400, fmt.Sprintf("lead %q: %s", lead, err.Error()))
			return
		}
		if err := s.validateAgentExists(lead); err != nil {
			jsonError(w, 422, fmt.Sprintf("lead %q: %s", lead, err.Error()))
			return
		}
		updated, err := cs.UpdateCompany(c.ID, spaces.CompanyUpdates{Lead: &lead})
		if err != nil {
			jsonSpaceError(w, err)
			return
		}
		c = updated
	}
	jsonCreated(w, c)
}

func (s *Server) handleGetCompany(w http.ResponseWriter, r *http.Request) {
	cs := s.companyAPI()
	if cs == nil {
		jsonError(w, 503, "companies not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, 400, "company id is required")
		return
	}
	c, err := cs.GetCompany(id)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	jsonOK(w, c)
}

func (s *Server) handleSeatCompanyMember(w http.ResponseWriter, r *http.Request) {
	cs := s.companyAPI()
	if cs == nil {
		jsonError(w, 503, "companies not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, 400, "company id is required")
		return
	}
	var body struct {
		Agent string `json:"agent"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	agent := strings.TrimSpace(body.Agent)
	if agent == "" {
		jsonError(w, 400, "agent is required")
		return
	}
	if err := validateSubdomain(agent); err != nil {
		jsonError(w, 400, fmt.Sprintf("agent %q: %s", agent, err.Error()))
		return
	}
	if err := s.validateAgentExists(agent); err != nil {
		jsonError(w, 422, err.Error())
		return
	}
	if err := cs.SeatMember(id, agent); err != nil {
		jsonSpaceError(w, err)
		return
	}
	c, err := cs.GetCompany(id)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	jsonOK(w, c)
}

func (s *Server) handleUnseatCompanyMember(w http.ResponseWriter, r *http.Request) {
	cs := s.companyAPI()
	if cs == nil {
		jsonError(w, 503, "companies not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	agent := strings.TrimSpace(r.PathValue("agent"))
	if id == "" || agent == "" {
		jsonError(w, 400, "company id and agent are required")
		return
	}
	if err := validateSubdomain(agent); err != nil {
		jsonError(w, 400, fmt.Sprintf("agent %q: %s", agent, err.Error()))
		return
	}
	if err := cs.UnseatMember(id, agent); err != nil {
		jsonSpaceError(w, err)
		return
	}
	c, err := cs.GetCompany(id)
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	jsonOK(w, c)
}

func (s *Server) handleUpdateCompany(w http.ResponseWriter, r *http.Request) {
	cs := s.companyAPI()
	if cs == nil {
		jsonError(w, 503, "companies not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, 400, "company id is required")
		return
	}
	var body struct {
		Name  *string `json:"name"`
		Vault *string `json:"vault"`
		Icon  *string `json:"icon"`
		Color *string `json:"color"`
		Lead  *string `json:"lead"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	if body.Lead != nil {
		lead := strings.TrimSpace(*body.Lead)
		if lead != "" {
			if err := validateSubdomain(lead); err != nil {
				jsonError(w, 400, fmt.Sprintf("lead %q: %s", lead, err.Error()))
				return
			}
			if err := s.validateAgentExists(lead); err != nil {
				jsonError(w, 422, fmt.Sprintf("lead %q: %s", lead, err.Error()))
				return
			}
		}
	}
	updated, err := cs.UpdateCompany(id, spaces.CompanyUpdates{
		Name: body.Name, Vault: body.Vault, Icon: body.Icon, Color: body.Color, Lead: body.Lead,
	})
	if err != nil {
		jsonSpaceError(w, err)
		return
	}
	jsonOK(w, updated)
}

func (s *Server) handleDeleteCompany(w http.ResponseWriter, r *http.Request) {
	cs := s.companyAPI()
	if cs == nil {
		jsonError(w, 503, "companies not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		jsonError(w, 400, "company id is required")
		return
	}
	if err := cs.DeleteCompany(id); err != nil {
		jsonSpaceError(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
