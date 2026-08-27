package spaces_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func TestResolveThreadWakes_NoMention(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakes(sp, "just a comment", nil)
	if len(plan.Agents) != 0 || plan.MentionedHuman {
		t.Fatalf("no @ should wake nobody: %+v", plan)
	}
}

func TestResolveThreadWakes_LabCannotWakeReggie(t *testing.T) {
	sp := &spaces.Space{
		Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"},
		CompanyID: "lab",
	}
	inCompany := func(agent string) (bool, error) {
		return agent == "Steve", nil
	}
	plan := spaces.ResolveThreadWakes(sp, "hey @Reggie take a look", inCompany)
	if len(plan.Agents) != 0 {
		t.Fatalf("Reggie must not wake: %+v", plan)
	}
	if len(plan.Errors) != 1 || plan.Errors[0].Reason != "not_in_roster" {
		t.Fatalf("want not_in_roster for non-member, got %+v", plan.Errors)
	}
}

func TestResolveThreadWakes_CompanyMemberNotSeated(t *testing.T) {
	sp := &spaces.Space{
		Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Reggie"},
		CompanyID: "lab",
	}
	inCompany := func(agent string) (bool, error) {
		return false, nil
	}
	plan := spaces.ResolveThreadWakes(sp, "@Reggie", inCompany)
	if len(plan.Agents) != 0 {
		t.Fatalf("unseated company member must not wake: %+v", plan)
	}
	if len(plan.Errors) != 1 || plan.Errors[0].Reason != "not_in_company" {
		t.Fatalf("want not_in_company, got %+v", plan.Errors)
	}
	if plan.Errors[0].Message != "Reggie isn't in this company." {
		t.Fatalf("fail copy: %q", plan.Errors[0].Message)
	}
}

func TestResolveThreadWakes_HuginnCanWakeSteve(t *testing.T) {
	sp := &spaces.Space{
		Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"},
		CompanyID: "huginn",
	}
	inCompany := func(agent string) (bool, error) {
		return agent == "Steve" || agent == "Winston", nil
	}
	plan := spaces.ResolveThreadWakes(sp, "can you look @Steve", inCompany)
	if len(plan.Agents) != 1 || plan.Agents[0] != "Steve" {
		t.Fatalf("Steve should wake: %+v", plan)
	}
}

func TestResolveThreadWakes_DeskNotInRoster(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakes(sp, "@Reggie hi", nil)
	if len(plan.Agents) != 0 {
		t.Fatalf("desk non-member must not wake: %+v", plan)
	}
	if len(plan.Errors) != 1 || plan.Errors[0].Reason != "not_in_roster" {
		t.Fatalf("want not_in_roster, got %+v", plan.Errors)
	}
}

func TestResolveThreadWakes_HumanMentionIsNotify(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakes(sp, "@you need to see this", nil)
	if !plan.MentionedHuman {
		t.Fatal("expected human mention")
	}
	if len(plan.Agents) != 0 {
		t.Fatalf("@you must not run an agent: %+v", plan)
	}
}

func TestResolveThreadWakes_MultipleValidOnce(t *testing.T) {
	sp := &spaces.Space{
		Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve", "Sam"},
	}
	plan := spaces.ResolveThreadWakes(sp, "@Steve and @Sam and @Steve again", nil)
	if len(plan.Agents) != 2 {
		t.Fatalf("want Steve+Sam once each, got %v", plan.Agents)
	}
}

func TestInsertSpaceThreadMessage_StaysOffHallway(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Chat", "Winston", []string{"Steve"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.PostSpaceMessage(ch.ID, "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.InsertSpaceThreadMessage(ch.ID, "on it", root.ID, "assistant", "Steve")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got.ParentID != root.ID || got.Role != "assistant" || got.Agent != "Steve" {
		t.Fatalf("got %+v", got)
	}
	listed, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 || listed.Messages[0].ID != root.ID {
		t.Fatalf("hallway leaked thread speech: %+v", listed.Messages)
	}
	if listed.Messages[0].ReplyCount != 1 {
		t.Fatalf("chip count=%d", listed.Messages[0].ReplyCount)
	}
	replies, _ := store.ListSpaceReplies(ch.ID, root.ID)
	if len(replies) != 1 || replies[0].ID != got.ID {
		t.Fatalf("replies=%+v", replies)
	}
}

func TestResolveThreadWakes_MJAndLocalUserAreHuman(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	t.Setenv("USER", "mjbonanno")
	for _, content := range []string{"@MJ please look", "@mj heads up", "@mjbonanno can you see this", "@you ping"} {
		plan := spaces.ResolveThreadWakes(sp, content, nil)
		if !plan.MentionedHuman {
			t.Fatalf("%q should notify the human: %+v", content, plan)
		}
		if len(plan.Agents) != 0 {
			t.Fatalf("%q must not run an agent: %+v", content, plan)
		}
	}
	plan := spaces.ResolveThreadWakes(sp, "can you look @Steve", nil)
	if plan.MentionedHuman {
		t.Fatalf("@Steve is not a human mention: %+v", plan)
	}
	if len(plan.Agents) != 1 || plan.Agents[0] != "Steve" {
		t.Fatalf("Steve should still wake: %+v", plan)
	}
}

func TestIsHumanMention_SteveIsNotHuman(t *testing.T) {
	if spaces.IsHumanMention("Steve") || spaces.IsHumanMention("Reggie") {
		t.Fatal("agent names must not be human mentions")
	}
	if !spaces.IsHumanMention("you") || !spaces.IsHumanMention("MJ") {
		t.Fatal("you/MJ must be human mentions")
	}
}

func TestResolveThreadWakes_WinstonAskSteveBareNameWakesSteve(t *testing.T) {
	sp := &spaces.Space{
		Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve", "Reggie"},
		CompanyID: "huginn",
	}
	inCompany := func(agent string) (bool, error) {
		return agent == "Steve" || agent == "Winston" || agent == "Reggie", nil
	}
	plan := spaces.ResolveThreadWakes(sp, "@Winston Ask Steve hostname + 7*8.", inCompany)
	got := map[string]bool{}
	for _, a := range plan.Agents {
		got[a] = true
	}
	if !got["Winston"] || !got["Steve"] {
		t.Fatalf("want Winston+Steve, got %v", plan.Agents)
	}
	if got["Reggie"] {
		t.Fatalf("Reggie was not named: %v", plan.Agents)
	}
}

func TestResolveThreadWakes_BareSteveWithoutAtStaysAsleep(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakes(sp, "Ask Steve hostname", nil)
	if len(plan.Agents) != 0 {
		t.Fatalf("no @ still wakes nobody: %+v", plan)
	}
}

func TestResolveThreadWakes_BareSteveNotOnRosterSilent(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Sam"}}
	plan := spaces.ResolveThreadWakes(sp, "@Winston Ask Steve hostname", nil)
	if len(plan.Agents) != 1 || plan.Agents[0] != "Winston" {
		t.Fatalf("want only Winston, got %v", plan.Agents)
	}
	if len(plan.Errors) != 0 {
		t.Fatalf("bare non-member must not error: %+v", plan.Errors)
	}
}

func TestNameAppears_WordBoundaryAndEmail(t *testing.T) {
	if !spaces.NameAppears("Ask Steve hostname", "Steve") {
		t.Fatal("bare Steve")
	}
	if !spaces.NameAppears("@Winston Ask Steve hostname", "steve") {
		t.Fatal("case")
	}
	if spaces.NameAppears("Steven hostname", "Steve") {
		t.Fatal("Steven must not match Steve")
	}
	if spaces.NameAppears("alice@Steve.com", "Steve") {
		t.Fatal("email must not match")
	}
	if !spaces.NameAppears("can you look @Steve", "Steve") {
		t.Fatal("@Steve")
	}
}

func TestResolveThreadWakes_SelfMentionDoesNotWake(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Steve pong", nil, spaces.WakeOpts{Speaker: "Steve"})
	if len(plan.Agents) != 0 {
		t.Fatalf("self @Steve must not loop: %+v", plan)
	}
}

func TestResolveThreadWakes_NoAtAgentSpeechWakesNobody(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakesOpts(sp, "hostname is box-1", nil, spaces.WakeOpts{Speaker: "Steve"})
	if len(plan.Agents) != 0 || len(plan.Errors) != 0 {
		t.Fatalf("no-@ agent speech must wake nobody: %+v", plan)
	}
}

func TestResolveThreadWakes_DeskCanWakeCompanyLead(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{}}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Ava take hostname", nil, spaces.WakeOpts{
		Speaker:    "Winston",
		ExtraLeads: []string{"Ava"},
	})
	if len(plan.Agents) != 1 || plan.Agents[0] != "Ava" {
		t.Fatalf("desk Winston should wake Huginn lead Ava: %+v", plan)
	}
}

func TestResolveThreadWakes_DeskCannotWakeHuginnSpecialist(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{}}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Steve hostname", nil, spaces.WakeOpts{
		Speaker:    "Winston",
		ExtraLeads: []string{"Ava"},
	})
	if len(plan.Agents) != 0 {
		t.Fatalf("desk must not wake Huginn-only Steve: %+v", plan)
	}
	if len(plan.Errors) != 1 || plan.Errors[0].Reason != "not_in_roster" {
		t.Fatalf("want not_in_roster for Steve, got %+v", plan.Errors)
	}
}

func TestResolveThreadWakes_LabSteveCannotWakeHuginnReggie(t *testing.T) {
	sp := &spaces.Space{
		Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"},
		CompanyID: "lab",
	}
	inCompany := func(agent string) (bool, error) {
		return agent == "Steve" || agent == "Winston", nil
	}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Reggie look", inCompany, spaces.WakeOpts{Speaker: "Steve"})
	if len(plan.Agents) != 0 {
		t.Fatalf("Lab Steve must not wake Huginn Reggie: %+v", plan)
	}
	if len(plan.Errors) != 1 || plan.Errors[0].Reason != "not_in_roster" {
		t.Fatalf("want not_in_roster, got %+v", plan.Errors)
	}
}

func TestResolveThreadWakes_CompanyMeshSteveWakesWinston(t *testing.T) {
	sp := &spaces.Space{
		Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"},
		CompanyID: "huginn",
	}
	inCompany := func(agent string) (bool, error) {
		return agent == "Steve" || agent == "Winston", nil
	}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Winston pong", inCompany, spaces.WakeOpts{Speaker: "Steve"})
	if len(plan.Agents) != 1 || plan.Agents[0] != "Winston" {
		t.Fatalf("Steve @Winston should wake Winston: %+v", plan)
	}
}

func TestDefaultCompanyLead_WinstonIfSeatedElseFirst(t *testing.T) {
	if got := spaces.DefaultCompanyLead([]string{"Steve", "Winston", "Reggie"}); got != "Winston" {
		t.Fatalf("Winston seated: got %q", got)
	}
	if got := spaces.DefaultCompanyLead([]string{"Ava", "Steve"}); got != "Ava" {
		t.Fatalf("first seated: got %q", got)
	}
	if got := spaces.DefaultCompanyLead(nil); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestResolveThreadWakes_UnicodeWinstonDoesNotMatch(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakes(sp, "@Winstón pong", nil)
	if len(plan.Agents) != 0 {
		t.Fatalf("accented @Winstón must not wake Winston: %+v", plan)
	}
}

func TestResolveThreadWakes_LowercaseWinstonMatches(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakes(sp, "@winston pong", nil)
	if len(plan.Agents) != 1 || plan.Agents[0] != "Winston" {
		t.Fatalf("@winston should wake Winston: %+v", plan)
	}
}

func TestResolveThreadWakes_LongAtListOnlyRosterWakes(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve", "Reggie"}}
	var b strings.Builder
	for i := 0; i < 24; i++ {
		b.WriteString("@Ghost")
		b.WriteString(string(rune('A' + (i % 26))))
		b.WriteByte(' ')
	}
	b.WriteString("@Steve @Reggie pong")
	plan := spaces.ResolveThreadWakes(sp, b.String(), nil)
	got := map[string]bool{}
	for _, a := range plan.Agents {
		got[a] = true
	}
	if !got["Steve"] || !got["Reggie"] || len(plan.Agents) != 2 {
		t.Fatalf("long @ list should wake only roster: %v", plan.Agents)
	}
	if len(plan.Errors) < 20 {
		t.Fatalf("want many not_in_roster errors, got %d", len(plan.Errors))
	}
}

func TestResolveThreadWakes_AssistantAtYouIsHuman(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{"Steve"}}
	plan := spaces.ResolveThreadWakesOpts(sp, "@you pong", nil, spaces.WakeOpts{Speaker: "Steve"})
	if !plan.MentionedHuman {
		t.Fatal("assistant @you should notify the human")
	}
	if len(plan.Agents) != 0 {
		t.Fatalf("assistant @you must not wake an agent: %+v", plan)
	}
}

func TestResolveThreadWakes_DeskDMPeerWinston(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindDM, LeadAgent: "Steve"}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Winston what time is it", nil, spaces.WakeOpts{
		Speaker:    "Steve",
		ExtraPeers: []string{"Winston"},
	})
	if len(plan.Agents) != 1 || plan.Agents[0] != "Winston" {
		t.Fatalf("desk DM @Winston from Steve should wake Winston: %+v", plan)
	}
}

func TestResolveThreadWakes_DeskSpecialistCannotWakeOtherCompanyLead(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{}}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Sam lab secrets", nil, spaces.WakeOpts{
		Speaker:    "Steve",
		ExtraLeads: []string{"Ava", "Sam"},
	})
	if len(plan.Agents) != 0 {
		t.Fatalf("desk specialist Steve must not wake Lab lead Sam: %+v", plan)
	}
	if len(plan.Errors) != 1 || plan.Errors[0].Reason != "not_in_roster" {
		t.Fatalf("want not_in_roster for Sam, got %+v", plan.Errors)
	}
}

func TestResolveThreadWakes_DeskHuginnLeadCannotHopIntoLab(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{}}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Sam take this", nil, spaces.WakeOpts{
		Speaker:    "Ava",
		ExtraLeads: []string{"Ava", "Sam"},
	})
	if len(plan.Agents) != 0 {
		t.Fatalf("Huginn lead on desk must not ExtraLead-hop into Lab Sam: %+v", plan)
	}
}

func TestResolveThreadWakes_HumanOnDeskStillWakesCompanyLead(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindChannel, LeadAgent: "Winston", Members: []string{}}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Ava take hostname", nil, spaces.WakeOpts{
		Speaker:    "",
		ExtraLeads: []string{"Ava"},
	})
	if len(plan.Agents) != 1 || plan.Agents[0] != "Ava" {
		t.Fatalf("human desk @Ava should wake Huginn lead: %+v", plan)
	}
}

func TestResolveThreadWakes_DeskDMStrangerStillDenied(t *testing.T) {
	sp := &spaces.Space{Kind: spaces.KindDM, LeadAgent: "Steve"}
	plan := spaces.ResolveThreadWakesOpts(sp, "@Reggie lab secrets", nil, spaces.WakeOpts{
		Speaker:    "Steve",
		ExtraLeads: []string{"Winston"},
		ExtraPeers: []string{"Winston"},
	})
	if len(plan.Agents) != 0 {
		t.Fatalf("desk DM must not wake stranger Reggie: %+v", plan)
	}
	if len(plan.Errors) != 1 || plan.Errors[0].Reason != "not_in_roster" {
		t.Fatalf("want not_in_roster for Reggie, got %+v", plan.Errors)
	}
}
