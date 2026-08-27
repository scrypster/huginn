package spaces

import (
	"strings"
	"time"
)

// Company is the isolation boundary: roster, connections, and vault.
// Company bots only see teammates seated in that company.
//
// Desk people (Winston and the desk circle) sit above companies. They are
// not required to belong to a company and may be seated in zero, one, or
// many. The same agent name may appear on more than one company roster.
//
// An empty Vault is allowed. Huginn still works if Muninn is down; do not
// silently substitute a vault. Example vault names: huginn, default.
type Company struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Vault     string    `json:"vault"`
	Members   []string  `json:"members"`
	Lead      string    `json:"lead,omitempty"`
	Icon      string    `json:"icon"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CompanyUpdates carries optional fields to update on a Company.
// A nil pointer means "leave unchanged".
// Setting Vault to "" is a real write (empty vault stays empty).
type CompanyUpdates struct {
	Name  *string
	Vault *string
	Icon  *string
	Color *string
	Lead  *string
}

// ErrCompanyNotFound is returned when a company ID does not exist.
var ErrCompanyNotFound = &SpaceError{Code: "company_not_found", Message: "company not found"}

// ErrCompanyNameTaken is returned when CreateCompany hits a case-insensitive
// unique name (concurrent same-name creates included).
var ErrCompanyNameTaken = &SpaceError{Code: "company_name_taken", Message: "a company with that name already exists"}

// ReservedCompanyName reports the fail-closed Huginn company. It cannot be
// deleted or renamed; leftover wringer companies are not reserved.
func ReservedCompanyName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "Huginn")
}

// ErrCompanyReserved is returned when a caller tries to delete or rename Huginn.
var ErrCompanyReserved = &SpaceError{Code: "company_reserved", Message: "Huginn cannot be deleted"}

// ErrCompanyHasSpaces is returned when DeleteCompany is called while the
// company still has non-archived spaces. Detach-to-desk would fail-open A2A.
var ErrCompanyHasSpaces = &SpaceError{Code: "company_has_spaces", Message: "company still has spaces"}

// ErrChannelNameTaken is a per-company (or desk) channel name collision.
var ErrChannelNameTaken = &SpaceError{Code: "channel_name_taken", Message: "a channel with that name already exists"}

// ErrCompanyLeadNotSeated is returned when Company.Lead is not on the roster.
var ErrCompanyLeadNotSeated = &SpaceError{Code: "lead_not_seated", Message: "lead must be seated in this company"}

// DefaultCompanyLead picks Winston if seated, else the first seated member.
// A specialist who is not seated can never be the lead.
func DefaultCompanyLead(members []string) string {
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m != "" && strings.EqualFold(m, "Winston") {
			return m
		}
	}
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m != "" {
			return m
		}
	}
	return ""
}

// EffectiveLead is the persisted lead, or DefaultCompanyLead when empty.
func (c *Company) EffectiveLead() string {
	if c == nil {
		return ""
	}
	if lead := strings.TrimSpace(c.Lead); lead != "" {
		return lead
	}
	return DefaultCompanyLead(c.Members)
}

// CanonicalSeatedName returns the roster spelling of name, or "".
func CanonicalSeatedName(members []string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for _, m := range members {
		if strings.EqualFold(strings.TrimSpace(m), name) {
			return strings.TrimSpace(m)
		}
	}
	return ""
}
