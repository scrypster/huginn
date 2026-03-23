package memory

import "testing"

func TestMCPURLFromEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://localhost:8475", "http://localhost:8750/mcp"},
		{"http://192.168.1.100:8475", "http://192.168.1.100:8750/mcp"},
		{"http://localhost:8475/", "http://localhost:8750/mcp"},
	}
	for _, tc := range tests {
		got, err := MCPURLFromEndpoint(tc.in)
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("input %q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestVaultTokenFor(t *testing.T) {
	cfg := &GlobalConfig{
		Endpoint:    "http://localhost:8475",
		VaultTokens: map[string]string{"huginn:agent:mj:alice": "tok-abc"},
	}
	tok, err := VaultTokenFor(cfg, "huginn:agent:mj:alice")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok-abc" {
		t.Fatalf("expected tok-abc, got %q", tok)
	}
	_, err = VaultTokenFor(cfg, "huginn:agent:mj:unknown")
	if err == nil {
		t.Fatal("expected error for unknown vault")
	}
}
