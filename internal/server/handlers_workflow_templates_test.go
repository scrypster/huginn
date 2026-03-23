package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleListWorkflowTemplates(t *testing.T) {
	_, ts := newTestServer(t)

	req, err := http.NewRequest("GET", ts.URL+"/api/v1/workflows/templates", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Assert 200 OK.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	// Decode response body.
	var templates []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&templates); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Assert at least 4 templates returned.
	if len(templates) < 4 {
		t.Fatalf("want at least 4 templates, got %d", len(templates))
	}

	// Assert each template has non-empty ID, Name, Description, and at least one step.
	for i, tpl := range templates {
		id, _ := tpl["id"].(string)
		if id == "" {
			t.Errorf("template[%d]: want non-empty id", i)
		}

		name, _ := tpl["name"].(string)
		if name == "" {
			t.Errorf("template[%d] (id=%q): want non-empty name", i, id)
		}

		description, _ := tpl["description"].(string)
		if description == "" {
			t.Errorf("template[%d] (id=%q): want non-empty description", i, id)
		}

		workflow, ok := tpl["workflow"].(map[string]any)
		if !ok {
			t.Errorf("template[%d] (id=%q): missing workflow object", i, id)
			continue
		}

		steps, ok := workflow["steps"].([]any)
		if !ok || len(steps) == 0 {
			t.Errorf("template[%d] (id=%q): want at least one step, got %v", i, id, workflow["steps"])
		}
	}

	// Assert the four required built-in templates are present.
	requiredIDs := map[string]bool{
		"daily-standup":  false,
		"code-review":    false,
		"weekly-summary": false,
		"health-check":   false,
	}
	for _, tpl := range templates {
		id, _ := tpl["id"].(string)
		if _, ok := requiredIDs[id]; ok {
			requiredIDs[id] = true
		}
	}
	for id, found := range requiredIDs {
		if !found {
			t.Errorf("required template %q not found in response", id)
		}
	}
}
