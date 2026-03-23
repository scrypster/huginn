package connections

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConnectionType_Constants(t *testing.T) {
	// Verify the constants have correct string values.
	tests := []struct {
		ct   ConnectionType
		want string
	}{
		{ConnectionTypeOAuth, "oauth"},
		{ConnectionTypeAPIKey, "api_key"},
		{ConnectionTypeServiceAccount, "service_account"},
		{ConnectionTypeDatabase, "database"},
		{ConnectionTypeSSH, "ssh"},
	}
	for _, tt := range tests {
		if string(tt.ct) != tt.want {
			t.Errorf("ConnectionType %q: got %q, want %q", tt.ct, string(tt.ct), tt.want)
		}
	}
}

func TestConnection_TypeField_JSONRoundtrip(t *testing.T) {
	c := Connection{
		ID:       "test-id",
		Provider: ProviderGitHub,
		Type:     ConnectionTypeOAuth,
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var c2 Connection
	if err := json.Unmarshal(b, &c2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c2.Type != ConnectionTypeOAuth {
		t.Errorf("Type: got %q, want %q", c2.Type, ConnectionTypeOAuth)
	}
}

func TestConnection_TypeField_OmitEmpty(t *testing.T) {
	// A connection with no type set should serialize without "type" key (omitempty).
	c := Connection{
		ID:       "test-id",
		Provider: ProviderGitHub,
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"type"`) {
		t.Errorf("expected omitempty to omit 'type' field, got: %s", string(b))
	}
}
