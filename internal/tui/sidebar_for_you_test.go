package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestCompanyRailForYou_BadgeOnlyWhenForYou(t *testing.T) {
	spaces := []SidebarSpace{
		{ID: "ch-lab", Name: "alerts", Kind: "channel", CompanyID: "lab", Unseen: 0, ForYou: true},
		{ID: "ch-quiet", Name: "standup", Kind: "channel", CompanyID: "lab", Unseen: 9, ForYou: false},
		{ID: "ch-other", Name: "ops", Kind: "channel", CompanyID: "other", Unseen: 0, ForYou: true},
	}
	if !companyRailForYou("lab", spaces) {
		t.Fatal("collapsed company must badge when a child has for_you")
	}
	if companyRailForYou("empty", spaces) {
		t.Fatal("unknown company must not badge")
	}
	spectator := []SidebarSpace{
		{ID: "ch-lab", Name: "alerts", Kind: "channel", CompanyID: "lab", Unseen: 9, ForYou: false},
	}
	if companyRailForYou("lab", spectator) {
		t.Fatal("spectator unseen_count must not badge the company rail")
	}
}

func TestSidebar_SetSpaceRail_ForYouBadgeNotUnseen(t *testing.T) {
	s := newSidebarModel()
	s.visible = true
	s.SetSpaceRail([]SidebarSpace{
		{ID: "mentions", Name: "mentions", Kind: "channel", Unseen: 2, ForYou: true},
		{ID: "noise", Name: "noise", Kind: "channel", Unseen: 7, ForYou: false},
	}, nil)

	var mentions, noise *sidebarItem
	for i := range s.items {
		switch s.items[i].name {
		case "mentions":
			mentions = &s.items[i]
		case "noise":
			noise = &s.items[i]
		}
	}
	if mentions == nil || noise == nil {
		t.Fatalf("expected mentions+noise in rail, items=%v", s.items)
	}
	if !mentions.forYou {
		t.Fatal("API for_you must map onto the space row")
	}
	if mentions.unread != 2 {
		t.Fatalf("quiet count: got %d", mentions.unread)
	}
	if noise.forYou {
		t.Fatal("spectator row must not set forYou")
	}
	if noise.unread != 7 {
		t.Fatalf("spectator quiet count: got %d", noise.unread)
	}

	rendered := s.View(40)
	// Strip ANSI so the badge/count assertions are stable.
	plain := stripANSI(rendered)
	if !strings.Contains(plain, "mentions "+forYouGlyph) {
		t.Fatalf("for_you space must draw the badge, got:\n%s", plain)
	}
	if !strings.Contains(plain, "noise (7)") {
		t.Fatalf("spectator unseen must stay a quiet count, got:\n%s", plain)
	}
	// noise must not carry the for-you glyph next to its name.
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "noise") && strings.Contains(line, forYouGlyph) {
			t.Fatalf("spectator line must not badge: %q", line)
		}
	}
}

func TestSidebar_CollapsedCompany_BadgesForYouOnly(t *testing.T) {
	s := newSidebarModel()
	s.visible = true
	s.collapsed = map[string]bool{"lab": true, "quiet": true}
	s.SetSpaceRail([]SidebarSpace{
		{ID: "ch-lab", Name: "alerts", Kind: "channel", CompanyID: "lab", Unseen: 0, ForYou: true},
		{ID: "ch-quiet", Name: "standup", Kind: "channel", CompanyID: "quiet", Unseen: 4, ForYou: false},
	}, []SidebarCompany{
		{ID: "lab", Name: "Lab"},
		{ID: "quiet", Name: "Quiet Co"},
	})

	var lab, quiet *sidebarItem
	for i := range s.items {
		if s.items[i].kind != sidebarSectionCompany {
			continue
		}
		switch s.items[i].companyID {
		case "lab":
			lab = &s.items[i]
		case "quiet":
			quiet = &s.items[i]
		}
	}
	if lab == nil || quiet == nil {
		t.Fatalf("expected both company rows, items=%v", s.items)
	}
	if !lab.collapsed || !lab.forYou {
		t.Fatalf("lab collapsed forYou=%v collapsed=%v", lab.forYou, lab.collapsed)
	}
	if !quiet.collapsed || quiet.forYou {
		t.Fatal("quiet company has spectator unseen only — no badge")
	}

	plain := stripANSI(s.View(40))
	if !strings.Contains(plain, "Lab "+forYouGlyph) && !strings.Contains(plain, "Lab "+forYouGlyph) {
		// header is "▸ Lab •"
		if !strings.Contains(plain, "Lab") || !strings.Contains(plain, forYouGlyph) {
			t.Fatalf("collapsed for_you company must draw badge, got:\n%s", plain)
		}
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "Quiet") && strings.Contains(line, forYouGlyph) {
			t.Fatalf("collapsed spectator company must not badge: %q", line)
		}
		if strings.Contains(line, "standup") || strings.Contains(line, "alerts") {
			t.Fatalf("collapsed company must hide child spaces: %q", line)
		}
	}
}

func TestSidebar_SetSpaceRail_PatchesDeskDM(t *testing.T) {
	s := newSidebarModel()
	s.visible = true
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Winston", Color: "#58A6FF"})
	s.SetAgents(reg)
	s.SetSpaceRail([]SidebarSpace{
		{ID: "dm-w", Name: "Winston", Kind: "dm", LeadAgent: "Winston", Unseen: 3, ForYou: true},
	}, nil)

	found := false
	for _, it := range s.items {
		if it.kind == sidebarSectionDMs && it.name == "Winston" {
			found = true
			if !it.forYou || it.unread != 3 {
				t.Fatalf("DM forYou=%v unread=%d", it.forYou, it.unread)
			}
		}
	}
	if !found {
		t.Fatal("Winston DM missing after SetSpaceRail")
	}
	plain := stripANSI(s.View(40))
	if !strings.Contains(plain, "Winston") || !strings.Contains(plain, forYouGlyph) {
		t.Fatalf("DM for_you must badge, got:\n%s", plain)
	}
	if !strings.Contains(plain, "(3)") {
		t.Fatalf("DM quiet count missing, got:\n%s", plain)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
