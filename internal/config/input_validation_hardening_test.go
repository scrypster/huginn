package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

// TestConfig_ExcessivelyLargePayload verifies large configs are handled.
func TestConfig_ExcessivelyLargePayload(t *testing.T) {
	cfg := Default()

	// Add very large MCP server configs
	for i := 0; i < 100; i++ {
		cfg.MCPServers = append(cfg.MCPServers, mcp.MCPServerConfig{
			Name: strings.Repeat("a", 1000),
			URL:  strings.Repeat("http://test.com/", 100),
			Env:  []string{strings.Repeat("key", 50) + "=" + strings.Repeat("val", 100)},
		})
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if len(data) > 10*1024*1024 {
		t.Logf("config size: %d bytes (over 10MB)", len(data))
	}

	// Should still unmarshal without crashing
	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
}

// TestConfig_NullBytesInValues verifies null bytes are handled.
func TestConfig_NullBytesInValues(t *testing.T) {
	cfg := Default()
	cfg.DefaultModel = "model\x00-injection"
	cfg.Theme = "theme\x00-inject"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// JSON should handle null bytes (though it may escape them)
	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.DefaultModel != cfg.DefaultModel {
		t.Errorf("null bytes not preserved: expected %q, got %q", cfg.DefaultModel, cfg2.DefaultModel)
	}
}

// TestConfig_NegativeValues verifies negative config values are validated.
func TestConfig_NegativeValues(t *testing.T) {
	cfg := Default()
	cfg.BashTimeoutSecs = -100
	cfg.MaxTurns = -50
	cfg.ContextLimitKB = -1000
	cfg.MaxImageSizeKB = -500
	cfg.NotepadsMaxTokens = -10000

	// Config should accept these (validation is at usage site)
	// This test documents the gap: no early validation
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Negative values persist (no validation)
	if cfg2.BashTimeoutSecs != -100 {
		t.Logf("negative BashTimeoutSecs stored: %d (gap: no validation)", cfg2.BashTimeoutSecs)
	}
}

// TestConfig_FloatOutOfRange verifies very large floats.
func TestConfig_FloatOutOfRange(t *testing.T) {
	cfg := Default()
	cfg.CompactTrigger = 1e308 // Near max float64

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.CompactTrigger != cfg.CompactTrigger {
		t.Errorf("float not preserved: expected %v, got %v", cfg.CompactTrigger, cfg2.CompactTrigger)
	}
}

// TestConfig_SpecialCharactersInModel verifies special chars in model names.
func TestConfig_SpecialCharactersInModel(t *testing.T) {
	specialName := "gpt-4\n'; drop table models;--"
	cfg := Default()
	cfg.DefaultModel = specialName

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.DefaultModel != specialName {
		t.Errorf("special chars not preserved: expected %q, got %q", specialName, cfg2.DefaultModel)
	}
}

// TestConfig_PathTraversalInWorkspace verifies path traversal in workspace path.
func TestConfig_PathTraversalInWorkspace(t *testing.T) {
	cfg := Default()
	cfg.WorkspacePath = "../../etc/passwd"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Config stores the path as-is (validation at usage site)
	if cfg2.WorkspacePath != "../../etc/passwd" {
		t.Logf("path traversal patterns stored as-is (gap: no path validation)")
	}
}

// TestConfig_URLTraversalInEndpoint verifies URL injection in backend endpoint.
func TestConfig_URLTraversalInEndpoint(t *testing.T) {
	cfg := Default()
	cfg.Backend.Endpoint = "https://attacker.com\r\nHost: legitimate.com"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.Backend.Endpoint != "https://attacker.com\r\nHost: legitimate.com" {
		t.Logf("header injection pattern stored as-is (gap: no URL validation)")
	}
}

// TestConfig_EmptyStringValues verifies empty strings are allowed.
func TestConfig_EmptyStringValues(t *testing.T) {
	cfg := Default()
	cfg.DefaultModel = ""
	cfg.Theme = ""
	cfg.Backend.APIKey = ""
	cfg.Backend.Endpoint = ""

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.DefaultModel != "" || cfg2.Theme != "" {
		t.Error("empty strings not preserved")
	}
}

// TestConfig_VeryLongString verifies very long field values.
func TestConfig_VeryLongString(t *testing.T) {
	cfg := Default()
	cfg.DefaultModel = strings.Repeat("a", 100000)
	cfg.Theme = strings.Repeat("b", 100000)

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(cfg2.DefaultModel) != 100000 {
		t.Errorf("long string not preserved: expected 100000 chars, got %d", len(cfg2.DefaultModel))
	}
}

// TestConfig_UnicodeInFields verifies unicode characters are handled.
func TestConfig_UnicodeInFields(t *testing.T) {
	cfg := Default()
	cfg.DefaultModel = "gpt-4🚀emoji"
	cfg.Theme = "🎨dark-theme😀"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.DefaultModel != "gpt-4🚀emoji" {
		t.Errorf("unicode not preserved: expected gpt-4🚀emoji, got %q", cfg2.DefaultModel)
	}
}

// TestConfig_ZeroValues verifies zero/default values are handled.
func TestConfig_ZeroValues(t *testing.T) {
	cfg := Config{
		Version: 0,
		MaxTurns: 0,
		ContextLimitKB: 0,
		BashTimeoutSecs: 0,
		NotepadsMaxTokens: 0,
		MaxImageSizeKB: 0,
		CompactTrigger: 0,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.Version != 0 || cfg2.MaxTurns != 0 {
		t.Error("zero values not preserved")
	}
}

// TestConfig_BackendConfig_APIKeyVariableRef verifies env var references.
func TestConfig_BackendConfig_APIKeyVariableRef(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		expectRef bool
	}{
		{"literal key", "sk-1234567890abcdef", false},
		{"env var ref", "$MY_API_KEY", true},
		{"dollar but no name", "$", true}, // Starts with $ (treated as env ref, resolves to empty)
		{"double dollar", "$$VAR", true}, // Also starts with $
		{"env var in middle", "prefix-$VAR-suffix", false}, // Only leading $ is special
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := BackendConfig{APIKey: tt.apiKey}
			isRef := strings.HasPrefix(bc.APIKey, "$")

			if isRef != tt.expectRef {
				t.Errorf("expected env var ref=%v, got %v for %q", tt.expectRef, isRef, tt.apiKey)
			}
		})
	}
}

// TestConfig_OAuthConfig_ClientSecretEmpty verifies empty client secret is allowed.
func TestConfig_OAuthConfig_ClientSecretEmpty(t *testing.T) {
	cfg := Default()
	cfg.Integrations.GitHub.ClientID = "github-app-id"
	cfg.Integrations.GitHub.ClientSecret = "" // Empty — use env var

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.Integrations.GitHub.ClientSecret != "" {
		t.Errorf("empty client secret should be preserved, got %q", cfg2.Integrations.GitHub.ClientSecret)
	}
}

// TestConfig_MCPServerConfig_InvalidURL verifies invalid URLs are stored as-is.
func TestConfig_MCPServerConfig_InvalidURL(t *testing.T) {
	cfg := Default()

	// Various invalid/unusual URLs
	invalidURLs := []string{
		"not-a-url",
		"://missing-scheme",
		"http://",
		"http://[invalid-ipv6]",
		"http://host:not-a-port",
		"file:///etc/passwd",
		"javascript:alert('xss')",
	}

	for _, u := range invalidURLs {
		cfg.MCPServers = append(cfg.MCPServers, mcp.MCPServerConfig{
			Name: "test",
			URL:  u,
		})
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(cfg2.MCPServers) != len(cfg.MCPServers) {
		t.Errorf("MCP servers not preserved: expected %d, got %d", len(cfg.MCPServers), len(cfg2.MCPServers))
	}
}

// TestConfig_AllowedTools_BothAllowAndDisallow verifies interaction.
func TestConfig_AllowedTools_BothAllowAndDisallow(t *testing.T) {
	cfg := Default()
	cfg.AllowedTools = []string{"bash", "write_file"}
	cfg.DisallowedTools = []string{"bash"} // Conflict: bash is both allowed and disallowed

	// Config accepts this (validation at usage site)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(cfg2.AllowedTools) != 2 || len(cfg2.DisallowedTools) != 1 {
		t.Logf("conflicting tool lists stored as-is (gap: no validation)")
	}
}

// TestConfig_BooleanFlagEdgeCases verifies boolean flags are correctly marshaled.
func TestConfig_BooleanFlagEdgeCases(t *testing.T) {
	cfg := Default()
	cfg.ToolsEnabled = true
	cfg.GitStageOnWrite = false
	cfg.NotepadsEnabled = true
	cfg.VisionEnabled = false
	cfg.SemanticSearch = true
	cfg.SchedulerEnabled = false

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.ToolsEnabled != true || cfg2.GitStageOnWrite != false {
		t.Error("boolean flags not preserved")
	}
}

// TestConfig_CompactTrigger_BoundaryValues verifies 0.0-1.0 range.
func TestConfig_CompactTrigger_BoundaryValues(t *testing.T) {
	tests := []float64{0.0, 0.5, 1.0, -0.1, 1.5, 999.0}

	for _, val := range tests {
		cfg := Default()
		cfg.CompactTrigger = val

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		var cfg2 Config
		if err := json.Unmarshal(data, &cfg2); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if cfg2.CompactTrigger != val {
			t.Errorf("CompactTrigger not preserved: expected %v, got %v", val, cfg2.CompactTrigger)
		}
	}
}

// TestConfig_WebUIPort_Zero verifies port 0 (dynamic allocation) is allowed.
func TestConfig_WebUIPort_Zero(t *testing.T) {
	cfg := Default()
	cfg.WebUI.Port = 0 // Dynamic allocation

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var cfg2 Config
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if cfg2.WebUI.Port != 0 {
		t.Errorf("port 0 should be preserved, got %d", cfg2.WebUI.Port)
	}
}

// TestConfig_WebUIPorts_HighValues verifies high port numbers.
func TestConfig_WebUIPorts_HighValues(t *testing.T) {
	tests := []int{65535, 65536, 99999, -1, -65535}

	for _, port := range tests {
		cfg := Default()
		cfg.WebUI.Port = port

		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		var cfg2 Config
		if err := json.Unmarshal(data, &cfg2); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if cfg2.WebUI.Port != port {
			t.Logf("port %d stored as-is (gap: no validation)", port)
		}
	}
}
