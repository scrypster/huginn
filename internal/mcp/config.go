package mcp

type MCPServerConfig struct {
	Name      string   `json:"name"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	Transport string   `json:"transport,omitempty"` // "stdio" or "sse"
	URL       string   `json:"url,omitempty"`
	Env       []string `json:"env,omitempty"`
}
