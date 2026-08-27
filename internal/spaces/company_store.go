package spaces

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// CreateCompany persists a new company. vault may be empty; members are
// agent names seated at create time (same convention as Space.Members).
func (s *SQLiteSpaceStore) CreateCompany(name, vault string, members []string, icon, color string) (*Company, error) {
	if strings.TrimSpace(name) == "" {
		return nil, &SpaceError{Code: "invalid_name", Message: "company name cannot be empty"}
	}
	if len(name) > 80 {
		return nil, &SpaceError{Code: "invalid_name", Message: "company name must be 80 characters or fewer"}
	}
	if HasControlRunes(name) {
		return nil, &SpaceError{Code: "invalid_name", Message: "company name cannot contain control characters"}
	}
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.db.Write().Begin()
	if err != nil {
		return nil, fmt.Errorf("companies: begin tx for create: %w", err)
	}
	defer tx.Rollback() // noop if committed

	lead := DefaultCompanyLead(members)
	if _, err := tx.Exec(
		`INSERT INTO companies(id, name, vault, icon, color, lead, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		id, name, vault, icon, color, lead, now, now,
	); err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrCompanyNameTaken
		}
		return nil, fmt.Errorf("companies: create: %w", err)
	}
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO company_members(company_id, agent_name) VALUES (?,?)`, id, m,
		); err != nil {
			return nil, fmt.Errorf("companies: seat member %q: %w", m, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("companies: commit create: %w", err)
	}
	return s.loadCompany(id)
}

// GetCompany fetches a single company by ID, including seated members.
func (s *SQLiteSpaceStore) GetCompany(id string) (*Company, error) {
	return s.loadCompany(id)
}

// ListCompanies returns every company, ordered by name.
func (s *SQLiteSpaceStore) ListCompanies() ([]*Company, error) {
	rows, err := s.db.Read().Query(`SELECT id FROM companies ORDER BY name ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("companies: list: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("companies: list scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("companies: list rows: %w", err)
	}

	result := make([]*Company, 0, len(ids))
	for _, id := range ids {
		c, err := s.loadCompany(id)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

// UpdateCompany applies the given updates. A nil pointer is left unchanged.
// Vault may be set to "" — empty stays empty; nothing is substituted.
func (s *SQLiteSpaceStore) UpdateCompany(id string, updates CompanyUpdates) (*Company, error) {
	if _, err := s.loadCompany(id); err != nil {
		return nil, err
	}
	if updates.Name != nil {
		if strings.TrimSpace(*updates.Name) == "" {
			return nil, &SpaceError{Code: "invalid_name", Message: "company name cannot be empty"}
		}
		if len(*updates.Name) > 80 {
			return nil, &SpaceError{Code: "invalid_name", Message: "company name must be 80 characters or fewer"}
		}
		cur, err := s.loadCompany(id)
		if err != nil {
			return nil, err
		}
		if ReservedCompanyName(cur.Name) && !ReservedCompanyName(*updates.Name) {
			return nil, ErrCompanyReserved
		}
	}
	if updates.Name == nil && updates.Vault == nil && updates.Icon == nil && updates.Color == nil && updates.Lead == nil {
		return s.loadCompany(id)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Write().Begin()
	if err != nil {
		return nil, fmt.Errorf("companies: begin update tx: %w", err)
	}
	defer tx.Rollback()

	if updates.Name != nil {
		if _, err := tx.Exec(`UPDATE companies SET name=?, updated_at=? WHERE id=?`, *updates.Name, now, id); err != nil {
			if isUniqueConstraintError(err) {
				return nil, ErrCompanyNameTaken
			}
			return nil, fmt.Errorf("companies: update name: %w", err)
		}
	}
	if updates.Vault != nil {
		if _, err := tx.Exec(`UPDATE companies SET vault=?, updated_at=? WHERE id=?`, *updates.Vault, now, id); err != nil {
			return nil, fmt.Errorf("companies: update vault: %w", err)
		}
	}
	if updates.Icon != nil {
		if _, err := tx.Exec(`UPDATE companies SET icon=?, updated_at=? WHERE id=?`, *updates.Icon, now, id); err != nil {
			return nil, fmt.Errorf("companies: update icon: %w", err)
		}
	}
	if updates.Color != nil {
		if _, err := tx.Exec(`UPDATE companies SET color=?, updated_at=? WHERE id=?`, *updates.Color, now, id); err != nil {
			return nil, fmt.Errorf("companies: update color: %w", err)
		}
	}
	if updates.Lead != nil {
		cur, err := s.loadCompany(id)
		if err != nil {
			return nil, err
		}
		lead := strings.TrimSpace(*updates.Lead)
		if lead == "" {
			lead = DefaultCompanyLead(cur.Members)
		} else {
			canon := CanonicalSeatedName(cur.Members, lead)
			if canon == "" {
				return nil, ErrCompanyLeadNotSeated
			}
			lead = canon
		}
		if _, err := tx.Exec(`UPDATE companies SET lead=?, updated_at=? WHERE id=?`, lead, now, id); err != nil {
			return nil, fmt.Errorf("companies: update lead: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("companies: commit update: %w", err)
	}
	return s.loadCompany(id)
}

// SeatMember seats agentName on the company roster. Idempotent, including
// case-fold: Winston and winston are the same seat (first spelling wins).
// The same agent may be seated in more than one company (desk people).
// Existence is checked inside the write tx so a racing DeleteCompany cannot
// leave an orphan seat (FK / missing row → ErrCompanyNotFound).
func (s *SQLiteSpaceStore) SeatMember(companyID, agentName string) error {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return &SpaceError{Code: "invalid_agent", Message: "agent name is required"}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Write().Begin()
	if err != nil {
		return fmt.Errorf("companies: begin seat tx: %w", err)
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRow(`SELECT id FROM companies WHERE id=?`, companyID).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return ErrCompanyNotFound
		}
		return fmt.Errorf("companies: seat load: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO company_members(company_id, agent_name) VALUES (?,?)`,
		companyID, agentName,
	); err != nil {
		if isFKConstraintError(err) {
			return ErrCompanyNotFound
		}
		return fmt.Errorf("companies: seat %q: %w", agentName, err)
	}
	if _, err := tx.Exec(`UPDATE companies SET updated_at=? WHERE id=?`, now, companyID); err != nil {
		return fmt.Errorf("companies: bump updated_at on seat: %w", err)
	}
	if err := tx.Commit(); err != nil {
		if isFKConstraintError(err) {
			return ErrCompanyNotFound
		}
		return fmt.Errorf("companies: commit seat: %w", err)
	}
	return nil
}

// UnseatMember removes agentName from the company roster. Idempotent and
// case-insensitive so unseat "winston" removes a seated "Winston".
func (s *SQLiteSpaceStore) UnseatMember(companyID, agentName string) error {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return &SpaceError{Code: "invalid_agent", Message: "agent name is required"}
	}
	if _, err := s.loadCompany(companyID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Write().Begin()
	if err != nil {
		return fmt.Errorf("companies: begin unseat tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM company_members WHERE company_id=? AND LOWER(agent_name)=LOWER(?)`,
		companyID, agentName,
	); err != nil {
		return fmt.Errorf("companies: unseat %q: %w", agentName, err)
	}
	if _, err := tx.Exec(`UPDATE companies SET updated_at=? WHERE id=?`, now, companyID); err != nil {
		return fmt.Errorf("companies: bump updated_at on unseat: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("companies: commit unseat: %w", err)
	}
	// Lead cannot stay a specialist who is no longer seated.
	cur, err := s.loadCompany(companyID)
	if err != nil {
		return err
	}
	if cur.Lead != "" && CanonicalSeatedName(cur.Members, cur.Lead) == "" {
		def := DefaultCompanyLead(cur.Members)
		if _, err := s.UpdateCompany(companyID, CompanyUpdates{Lead: &def}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCompany removes a company and its roster. Huginn is reserved.
// Non-archived spaces still attached fail closed (no detach-to-desk — that
// would fail-open A2A on the leftover channels). Missing id is ErrCompanyNotFound.
func (s *SQLiteSpaceStore) DeleteCompany(id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrCompanyNotFound
	}
	c, err := s.loadCompany(id)
	if err != nil {
		return err
	}
	if ReservedCompanyName(c.Name) {
		return ErrCompanyReserved
	}
	n, err := s.CountSpacesForCompany(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrCompanyHasSpaces
	}
	tx, err := s.db.Write().Begin()
	if err != nil {
		return fmt.Errorf("companies: begin delete tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM company_members WHERE company_id=?`, id); err != nil {
		return fmt.Errorf("companies: delete members: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM companies WHERE id=?`, id); err != nil {
		return fmt.Errorf("companies: delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("companies: commit delete: %w", err)
	}
	return nil
}

// CountSpacesForCompany returns non-archived spaces still attached to companyID.
func (s *SQLiteSpaceStore) CountSpacesForCompany(companyID string) (int, error) {
	if strings.TrimSpace(companyID) == "" {
		return 0, nil
	}
	var n int
	err := s.db.Read().QueryRow(
		`SELECT COUNT(*) FROM spaces WHERE company_id=? AND archived_at IS NULL`,
		companyID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("companies: count spaces: %w", err)
	}
	return n, nil
}

// CompanyRoster returns the seated members of companyID.
// Specialists not seated in the company are not in the roster.
func (s *SQLiteSpaceStore) CompanyRoster(companyID string) ([]string, error) {
	if _, err := s.loadCompany(companyID); err != nil {
		return nil, err
	}
	return s.loadCompanyMembers(companyID)
}

// SpaceCompanyID returns the space's company_id. Empty means desk-level
// (today's space-roster / not_in_roster behavior). Unknown or archived
// spaces return ("", nil) so callers fall through to the space-roster
// deny-all path rather than fail-open.
func (s *SQLiteSpaceStore) SpaceCompanyID(spaceID string) (string, error) {
	if strings.TrimSpace(spaceID) == "" {
		return "", nil
	}
	sp, err := s.GetSpace(spaceID)
	if err != nil {
		var se *SpaceError
		if errors.As(err, &se) && se.Code == "space_not_found" {
			return "", nil
		}
		return "", err
	}
	if sp.ArchivedAt != nil {
		return "", nil
	}
	return strings.TrimSpace(sp.CompanyID), nil
}

// AgentInCompany reports whether agent is seated in companyID.
// Unknown company or unseated agent is false, not an error.
func (s *SQLiteSpaceStore) AgentInCompany(agent, companyID string) (bool, error) {
	if strings.TrimSpace(agent) == "" || strings.TrimSpace(companyID) == "" {
		return false, nil
	}
	var n int
	err := s.db.Read().QueryRow(
		`SELECT COUNT(*) FROM company_members WHERE company_id=? AND LOWER(agent_name)=LOWER(?)`,
		companyID, agent,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("companies: agent in company: %w", err)
	}
	return n > 0, nil
}

// CompaniesIn lists companies agent is seated in. An empty result means
// the agent is desk-only (not required to belong to any company).
func (s *SQLiteSpaceStore) CompaniesIn(agent string) ([]*Company, error) {
	if strings.TrimSpace(agent) == "" {
		return []*Company{}, nil
	}
	rows, err := s.db.Read().Query(
		`SELECT c.id FROM companies c
		 JOIN company_members m ON m.company_id = c.id
		 WHERE LOWER(m.agent_name) = LOWER(?)
		 ORDER BY c.name ASC, c.id ASC`,
		agent,
	)
	if err != nil {
		return nil, fmt.Errorf("companies: companies in: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("companies: companies in scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("companies: companies in rows: %w", err)
	}

	result := make([]*Company, 0, len(ids))
	for _, id := range ids {
		c, err := s.loadCompany(id)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (s *SQLiteSpaceStore) loadCompany(id string) (*Company, error) {
	var c Company
	var createdAt, updatedAt string
	err := s.db.Read().QueryRow(
		`SELECT id, name, vault, icon, color, COALESCE(lead, ''), created_at, updated_at
		 FROM companies WHERE id=?`, id,
	).Scan(&c.ID, &c.Name, &c.Vault, &c.Icon, &c.Color, &c.Lead, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrCompanyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("companies: load %q: %w", id, err)
	}

	var parseErr error
	c.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
	if parseErr != nil {
		slog.Warn("companies: loadCompany: failed to parse created_at", "company_id", c.ID, "value", createdAt, "err", parseErr)
	}
	c.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
	if parseErr != nil {
		slog.Warn("companies: loadCompany: failed to parse updated_at", "company_id", c.ID, "value", updatedAt, "err", parseErr)
	}

	members, err := s.loadCompanyMembers(id)
	if err != nil {
		return nil, err
	}
	c.Members = members
	if c.Members == nil {
		c.Members = []string{}
	}
	if strings.TrimSpace(c.Lead) == "" {
		c.Lead = DefaultCompanyLead(c.Members)
	}
	return &c, nil
}

func (s *SQLiteSpaceStore) loadCompanyMembers(companyID string) ([]string, error) {
	rows, err := s.db.Read().Query(
		`SELECT agent_name FROM company_members WHERE company_id=? ORDER BY agent_name`, companyID,
	)
	if err != nil {
		return nil, fmt.Errorf("companies: load members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("companies: scan member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("companies: members rows: %w", err)
	}
	if members == nil {
		members = []string{}
	}
	return members, nil
}

// HasControlRunes reports ASCII/Unicode control characters (NUL, CR, etc.).
// Company and space names must not include them — they break JSON/SQL/GUI.
func HasControlRunes(s string) bool {
	for _, r := range s {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

// FindChannelByNameInCompany returns the first non-archived channel whose name
// matches name (case-insensitive) inside companyID. Empty companyID is desk.
func (s *SQLiteSpaceStore) FindChannelByNameInCompany(name, companyID string) (*Space, error) {
	name = strings.TrimSpace(name)
	companyID = strings.TrimSpace(companyID)
	if name == "" {
		return nil, nil
	}
	var id string
	var err error
	if companyID == "" {
		err = s.db.Read().QueryRow(
			`SELECT id FROM spaces
			 WHERE kind = 'channel'
			   AND archived_at IS NULL
			   AND LOWER(name) = LOWER(?)
			   AND COALESCE(company_id, '') = ''
			 LIMIT 1`,
			name,
		).Scan(&id)
	} else {
		err = s.db.Read().QueryRow(
			`SELECT id FROM spaces
			 WHERE kind = 'channel'
			   AND archived_at IS NULL
			   AND LOWER(name) = LOWER(?)
			   AND company_id = ?
			 LIMIT 1`,
			name, companyID,
		).Scan(&id)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("spaces: find channel by name in company: %w", err)
	}
	return s.loadSpace(id)
}

func isFKConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key")
}
