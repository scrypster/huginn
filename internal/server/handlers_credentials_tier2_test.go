package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestPagerDuty_SaveMissingAPIToken tests PagerDuty save with missing api_token.
func TestPagerDuty_SaveMissingAPIToken(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/pagerduty", strings.NewReader(
		`{"label":"Test PagerDuty"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestPagerDuty_TestEndpoint tests PagerDuty test endpoint with invalid credentials.
func TestPagerDuty_TestEndpoint(t *testing.T) {
	t.Skip("requires live external API")
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/pagerduty/test", strings.NewReader(
		`{"api_token":"invalid-token"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != false {
		t.Errorf("expected ok=false for invalid credentials, got %v", result)
	}
}

// TestNewRelic_SaveMissingAPIKey tests New Relic save with missing api_key.
func TestNewRelic_SaveMissingAPIKey(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/newrelic", strings.NewReader(
		`{"account_id":"12345","label":"Test New Relic"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestNewRelic_TestEndpoint tests New Relic test endpoint.
func TestNewRelic_TestEndpoint(t *testing.T) {
	t.Skip("requires live external API")
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/newrelic/test", strings.NewReader(
		`{"api_key":"invalid-key"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != false {
		t.Errorf("expected ok=false for invalid credentials, got %v", result)
	}
}

// TestElastic_SaveMissingURL tests Elastic save with missing url.
func TestElastic_SaveMissingURL(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/elastic", strings.NewReader(
		`{"api_key":"test-key","label":"Test Elastic"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestElastic_SaveMissingAPIKey tests Elastic save with missing api_key.
func TestElastic_SaveMissingAPIKey(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/elastic", strings.NewReader(
		`{"url":"https://example.elastic.com","label":"Test Elastic"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestElastic_TestEndpoint tests Elastic test endpoint.
func TestElastic_TestEndpoint(t *testing.T) {
	t.Skip("requires live external API")
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/elastic/test", strings.NewReader(
		`{"url":"https://example.elastic.com","api_key":"invalid"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != false {
		t.Errorf("expected ok=false for invalid credentials, got %v", result)
	}
}

// TestGrafana_SaveMissingURL tests Grafana save with missing url.
func TestGrafana_SaveMissingURL(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/grafana", strings.NewReader(
		`{"token":"test-token","label":"Test Grafana"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestGrafana_SaveMissingToken tests Grafana save with missing token.
func TestGrafana_SaveMissingToken(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/grafana", strings.NewReader(
		`{"url":"https://example.grafana.net","label":"Test Grafana"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestGrafana_TestEndpoint tests Grafana test endpoint.
func TestGrafana_TestEndpoint(t *testing.T) {
	t.Skip("requires live external API")
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/grafana/test", strings.NewReader(
		`{"url":"https://example.grafana.net","token":"invalid"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != false {
		t.Errorf("expected ok=false for invalid credentials, got %v", result)
	}
}

// TestCrowdStrike_SaveMissingClientID tests CrowdStrike save with missing client_id.
func TestCrowdStrike_SaveMissingClientID(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/crowdstrike", strings.NewReader(
		`{"client_secret":"secret","label":"Test CrowdStrike"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestCrowdStrike_SaveMissingClientSecret tests CrowdStrike save with missing client_secret.
func TestCrowdStrike_SaveMissingClientSecret(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/crowdstrike", strings.NewReader(
		`{"client_id":"id","label":"Test CrowdStrike"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestCrowdStrike_TestEndpoint tests CrowdStrike test endpoint.
func TestCrowdStrike_TestEndpoint(t *testing.T) {
	t.Skip("requires live external API")
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/crowdstrike/test", strings.NewReader(
		`{"client_id":"invalid","client_secret":"invalid"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != false {
		t.Errorf("expected ok=false for invalid credentials, got %v", result)
	}
}

// TestTerraform_SaveMissingToken tests Terraform save with missing token.
func TestTerraform_SaveMissingToken(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/terraform", strings.NewReader(
		`{"label":"Test Terraform"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestTerraform_TestEndpoint tests Terraform test endpoint.
func TestTerraform_TestEndpoint(t *testing.T) {
	t.Skip("requires live external API")
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/terraform/test", strings.NewReader(
		`{"token":"invalid"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != false {
		t.Errorf("expected ok=false for invalid credentials, got %v", result)
	}
}

// TestServiceNow_SaveMissingInstanceURL tests ServiceNow save with missing instance_url.
func TestServiceNow_SaveMissingInstanceURL(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/servicenow", strings.NewReader(
		`{"username":"admin","password":"pass","label":"Test ServiceNow"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestServiceNow_SaveMissingUsername tests ServiceNow save with missing username.
func TestServiceNow_SaveMissingUsername(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/servicenow", strings.NewReader(
		`{"instance_url":"https://dev12345.service-now.com","password":"pass","label":"Test ServiceNow"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestServiceNow_SaveMissingPassword tests ServiceNow save with missing password.
func TestServiceNow_SaveMissingPassword(t *testing.T) {
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/servicenow", strings.NewReader(
		`{"instance_url":"https://dev12345.service-now.com","username":"admin","label":"Test ServiceNow"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestServiceNow_TestEndpoint tests ServiceNow test endpoint.
func TestServiceNow_TestEndpoint(t *testing.T) {
	t.Skip("requires live external API")
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/credentials/servicenow/test", strings.NewReader(
		`{"instance_url":"https://dev12345.service-now.com","username":"admin","password":"invalid"}`,
	))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["ok"] != false {
		t.Errorf("expected ok=false for invalid credentials, got %v", result)
	}
}

// TestAllTier2ProvidersSaveWithConnMgrNil tests that all tier-2 credential endpoints handle nil connMgr.
func TestAllTier2ProvidersSaveWithConnMgrNil(t *testing.T) {
	// Use newTestServer (without connections) to get nil connMgr
	_, ts := newTestServer(t)

	endpoints := []string{
		"/api/v1/credentials/pagerduty",
		"/api/v1/credentials/newrelic",
		"/api/v1/credentials/elastic",
		"/api/v1/credentials/grafana",
		"/api/v1/credentials/crowdstrike",
		"/api/v1/credentials/terraform",
		"/api/v1/credentials/servicenow",
	}

	for _, endpoint := range endpoints {
		req, _ := http.NewRequest("POST", ts.URL+endpoint, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request to %s failed: %v", endpoint, err)
		}
		resp.Body.Close()

		if resp.StatusCode != 503 {
			t.Errorf("%s: expected 503 for nil connMgr, got %d", endpoint, resp.StatusCode)
		}
	}
}
