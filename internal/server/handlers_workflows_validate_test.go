package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestValidateWorkflow_ValidBody_Returns200 verifies that a valid workflow
// with empty steps returns 200 OK with {"valid": true}.
func TestValidateWorkflow_ValidBody_Returns200(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{"name":"valid-wf","enabled":false,"schedule":"","steps":[]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/validate", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var result map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	if !result["valid"] {
		t.Errorf("want valid=true, got %v", result)
	}
}

// TestValidateWorkflow_BadCron_Returns422 verifies that a workflow with
// an invalid cron schedule returns 422 Unprocessable Entity.
func TestValidateWorkflow_BadCron_Returns422(t *testing.T) {
	_, ts := newTestServer(t)

	// "not-a-cron" is not valid cron syntax
	payload := `{"name":"bad-cron-wf","enabled":true,"schedule":"not-a-cron","steps":[]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/validate", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", resp.StatusCode)
	}

	var errResult map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errResult); err != nil {
		t.Fatal(err)
	}

	if errResult["error"] == "" {
		t.Error("want non-empty error message")
	}
}

// TestValidateWorkflow_DanglingFromStep_Returns422 verifies that a workflow
// with a step that references a non-existent from_step returns 422.
func TestValidateWorkflow_DanglingFromStep_Returns422(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name":"dangling-from-step",
		"enabled":false,
		"schedule":"",
		"steps":[
			{
				"name":"step-a",
				"agent":"Chris",
				"prompt":"do something",
				"position":0,
				"inputs":[
					{"from_step":"nonexistent-step","as":"input_var"}
				]
			}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/validate", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", resp.StatusCode)
	}

	var errResult map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errResult); err != nil {
		t.Fatal(err)
	}

	if errResult["error"] == "" {
		t.Error("want non-empty error message")
	}
}

// TestValidateWorkflow_InvalidJSON_Returns400 verifies that malformed JSON
// in the request body returns 400 Bad Request.
func TestValidateWorkflow_InvalidJSON_Returns400(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{"name":"bad-json","enabled":true,"steps":[` // Incomplete JSON
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/validate", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}

	var errResult map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errResult); err != nil {
		t.Fatal(err)
	}

	if errResult["error"] == "" {
		t.Error("want non-empty error message")
	}
}
