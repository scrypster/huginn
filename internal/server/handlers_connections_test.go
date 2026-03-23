package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestStoreAndClaimRelayKey(t *testing.T) {
	s := &Server{relayKeys: make(map[string]string)}

	s.storeRelayKey("github", "base64key123")

	got, ok := s.claimRelayKey("github")
	if !ok {
		t.Fatal("expected to find relay key")
	}
	if got != "base64key123" {
		t.Fatalf("expected base64key123, got %q", got)
	}

	// Second claim should fail (key consumed).
	_, ok2 := s.claimRelayKey("github")
	if ok2 {
		t.Fatal("relay key should be deleted after claim")
	}
}

func TestClaimRelayKey_NotFound(t *testing.T) {
	s := &Server{relayKeys: make(map[string]string)}
	_, ok := s.claimRelayKey("google")
	if ok {
		t.Fatal("expected not found for unstored provider")
	}
}

func TestKnownProviders_ExpectedSet(t *testing.T) {
	// Test that knownProviders contains exactly the expected OAuth-capable providers.
	// This is a regression test to prevent silent drift between knownProviders and
	// the actual set of supported OAuth providers.
	_, ts := newTestServerWithConnections(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/providers", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var providers []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		t.Fatal(err)
	}

	if len(providers) != 5 {
		t.Fatalf("expected exactly 5 OAuth providers, got %d", len(providers))
	}

	expectedNames := map[string]bool{
		"google":    true,
		"github":    true,
		"slack":     true,
		"jira":      true,
		"bitbucket": true,
	}

	for i, p := range providers {
		name, ok := p["name"].(string)
		if !ok {
			t.Fatalf("provider %d: name field is not a string", i)
		}
		if !expectedNames[name] {
			t.Fatalf("provider %d: unexpected provider name %q", i, name)
		}
		delete(expectedNames, name)
	}

	if len(expectedNames) > 0 {
		var missing []string
		for name := range expectedNames {
			missing = append(missing, name)
		}
		t.Fatalf("expected but missing providers: %v", missing)
	}
}

func TestHandleListProviders_WithBroker_AllConfigured(t *testing.T) {
	// Test that when brokerClient is set (machine registered with HuginnCloud),
	// all providers have Configured=true, even without local configuration.
	// This verifies that the broker path correctly marks all providers as available.

	// Create a test server with connections store.
	srv, ts := newTestServerWithConnections(t)

	// Simulate registering with HuginnCloud by setting a broker client.
	// We use the existing mock broker client from test_stubs_test.go.
	mockBroker := &mockBrokerClient{}
	srv.mu.Lock()
	srv.brokerClient = mockBroker
	srv.mu.Unlock()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/providers", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var providers []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		t.Fatal(err)
	}

	if len(providers) != 5 {
		t.Fatalf("expected 5 providers, got %d", len(providers))
	}

	for i, p := range providers {
		configured, ok := p["configured"].(bool)
		if !ok {
			t.Fatalf("provider %d: configured field is not a bool", i)
		}
		if !configured {
			name := p["name"]
			t.Fatalf("provider %d (%v): expected configured=true with broker, got false", i, name)
		}
	}
}
