package memory

import (
	"fmt"
	"net"
	"net/url"
)

const mcpPort = "8750"

// MCPURLFromEndpoint converts a MuninnDB server endpoint (e.g. http://localhost:8475)
// to its MCP HTTP endpoint (e.g. http://localhost:8750/mcp) by replacing the port
// and appending the /mcp path.
func MCPURLFromEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint %q: %w", endpoint, err)
	}
	host := u.Hostname()
	u.Host = net.JoinHostPort(host, mcpPort)
	u.Path = "/mcp"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// VaultTokenFor retrieves the authentication token for a specific vault from the global config.
// Returns an error if the vault is not configured or has no token.
func VaultTokenFor(cfg *GlobalConfig, vaultName string) (string, error) {
	if cfg == nil || cfg.VaultTokens == nil {
		return "", fmt.Errorf("muninn: no vault tokens configured")
	}
	tok, ok := cfg.VaultTokens[vaultName]
	if !ok || tok == "" {
		return "", fmt.Errorf("muninn: no token for vault %q — create the vault first", vaultName)
	}
	return tok, nil
}
