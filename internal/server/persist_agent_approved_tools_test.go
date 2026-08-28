package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestPersistAgent_GrantSurvivesUntouchedEditorSave is the regression test
// for the AgentsView-editor / permission-banner-grant RMW race (vet E): a
// grant lands via grantApprovedTool (read-modify-write against fresh disk
// state) while an editor tab is open; the editor then saves the whole form
// WITHOUT the user having touched the approved-tools chips. Because the
// editor echoes back both its current value AND the snapshot it loaded
// (loaded_approved_tools), persistAgent can tell "unedited" apart from "user
// edited" and must preserve the disk value — the just-landed grant must
// survive.
func TestPersistAgent_GrantSurvivesUntouchedEditorSave(t *testing.T) {
	setupAgentsDir(t, map[string]string{
		"codey.yaml": "name: Codey\nmodel: qwen2.5-coder:14b\napproved_tools:\n  - bash\n",
	})
	srv, ts := newTestServer(t)

	// Editor "opens": loads the current def (approved_tools: [bash]).
	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	var loaded agents.AgentDef
	for _, a := range cfg.Agents {
		if a.Name == "Codey" {
			loaded = a
		}
	}
	if len(loaded.ApprovedTools) != 1 || loaded.ApprovedTools[0] != "bash" {
		t.Fatalf("test setup: expected loaded approved_tools=[bash], got %v", loaded.ApprovedTools)
	}

	// Grant lands via the permission banner while the editor tab sits open —
	// read-modify-write against fresh disk state.
	if err := srv.grantApprovedTool("Codey", "write_file"); err != nil {
		t.Fatalf("grantApprovedTool: %v", err)
	}

	// Editor tab saves without the user touching the approved-tools chips:
	// approved_tools == what it loaded, plus loaded_approved_tools echoed back.
	body := `{"name":"Codey","model":"qwen2.5-coder:14b","approved_tools":["bash"],"loaded_approved_tools":["bash"]}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Codey", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}

	cfg, err = agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	var saved agents.AgentDef
	for _, a := range cfg.Agents {
		if a.Name == "Codey" {
			saved = a
		}
	}
	if !stringSetEqual(saved.ApprovedTools, []string{"bash", "write_file"}) {
		t.Errorf("approved_tools after untouched editor save = %v, want [bash write_file] (grant must survive)", saved.ApprovedTools)
	}
}

// TestPersistAgent_ExplicitChipEditIsAuthoritative verifies the other half:
// when the client's approved_tools genuinely differs from what it loaded —
// a real user edit — that edit wins, even though it drops a tool, exactly
// as if no concurrent grant had ever happened. Last-writer-wins on a real
// edit is documented/intended, not a bug.
func TestPersistAgent_ExplicitChipEditIsAuthoritative(t *testing.T) {
	setupAgentsDir(t, map[string]string{
		"codey.yaml": "name: Codey\nmodel: qwen2.5-coder:14b\napproved_tools:\n  - bash\n  - write_file\n",
	})
	srv, ts := newTestServer(t)
	_ = srv

	// Editor loaded [bash, write_file], user removes write_file, saves [bash].
	body := `{"name":"Codey","model":"qwen2.5-coder:14b","approved_tools":["bash"],"loaded_approved_tools":["bash","write_file"]}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Codey", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}

	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	var saved agents.AgentDef
	for _, a := range cfg.Agents {
		if a.Name == "Codey" {
			saved = a
		}
	}
	if !stringSetEqual(saved.ApprovedTools, []string{"bash"}) {
		t.Errorf("approved_tools after explicit chip edit = %v, want [bash] (edit must win)", saved.ApprovedTools)
	}
}

// TestPersistAgent_MissingApprovedToolsFieldPreservesDisk covers an old
// client / direct API caller that never sends approved_tools at all
// (nil, not empty-array): the on-disk value must be preserved, not wiped.
func TestPersistAgent_MissingApprovedToolsFieldPreservesDisk(t *testing.T) {
	setupAgentsDir(t, map[string]string{
		"codey.yaml": "name: Codey\nmodel: qwen2.5-coder:14b\napproved_tools:\n  - bash\n",
	})
	_, ts := newTestServer(t)

	body := `{"name":"Codey","model":"qwen2.5-coder:14b","color":"#123456"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Codey", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}

	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	var saved agents.AgentDef
	for _, a := range cfg.Agents {
		if a.Name == "Codey" {
			saved = a
		}
	}
	if !stringSetEqual(saved.ApprovedTools, []string{"bash"}) {
		t.Errorf("approved_tools after field-omitted PUT = %v, want [bash] preserved", saved.ApprovedTools)
	}
}

// TestStringSetEqual covers the set-equality helper directly.
func TestStringSetEqual(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{}, nil, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"b", "a"}, true}, // order-independent
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a"}, nil, false},
	}
	for _, c := range cases {
		if got := stringSetEqual(c.a, c.b); got != c.want {
			t.Errorf("stringSetEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestPersistAgent_EmptyArrayFromOldClientCannotClear pins a KNOWN,
// deliberate limitation of the differs-as-set rule rather than leaving it
// silent.
//
// An old client / direct API caller that sends `"approved_tools": []` with
// no loaded_approved_tools is indistinguishable, under this rule, from one
// that loaded an agent with zero chips and saved without touching them:
// both arrive as (current=[], loaded=nil), which compares set-equal, so the
// on-disk value wins and the "clear everything" intent is not honored.
//
// This is accepted because the trade is asymmetric: preserving disk costs a
// legacy caller one unsupported operation, while honoring the empty array
// would re-open the RMW race (a banner grant silently clobbered) for exactly
// the clients that cannot protect themselves. The chip UI is unaffected —
// AgentsView always echoes loaded_approved_tools (useAgentsViewState.ts
// saveAgent), so a real "remove every chip" edit arrives as
// (current=[], loaded=[...]) and IS honored — see
// TestPersistAgent_ExplicitChipEditIsAuthoritative.
//
// To clear approved tools without the chip UI, edit the agent's YAML.
func TestPersistAgent_EmptyArrayFromOldClientCannotClear(t *testing.T) {
	setupAgentsDir(t, map[string]string{
		"codey.yaml": "name: Codey\nmodel: qwen2.5-coder:14b\napproved_tools:\n  - bash\n",
	})
	_, ts := newTestServer(t)

	body := `{"name":"Codey","model":"qwen2.5-coder:14b","approved_tools":[]}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Codey", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}

	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	var saved agents.AgentDef
	for _, a := range cfg.Agents {
		if a.Name == "Codey" {
			saved = a
		}
	}
	if !stringSetEqual(saved.ApprovedTools, []string{"bash"}) {
		t.Errorf("approved_tools after empty-array PUT with no loaded snapshot = %v, want [bash] preserved "+
			"(documented limitation — if this now clears, update the doc comment on approvedToolsExplicitlyEdited)",
			saved.ApprovedTools)
	}
}
