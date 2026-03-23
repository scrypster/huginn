package tools

import (
	"context"
	"strings"

	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/backend"
)

// AgentNameResolver is a small interface to avoid an import cycle between tools ↔ agents.
type AgentNameResolver interface {
	DefaultAgentName() string
}

// UpdateMemoryTool lets the agent read and update its persistent memory file.
type UpdateMemoryTool struct {
	huginnHome string
	resolver   AgentNameResolver
}

func (t *UpdateMemoryTool) Name() string { return "update_memory" }
func (t *UpdateMemoryTool) Description() string {
	return "Read or update your persistent memory file. This file survives across conversations. Use 'read' to see current contents, 'append' to add new entries, or 'rewrite' to replace the entire file (use for consolidation)."
}
func (t *UpdateMemoryTool) Permission() PermissionLevel { return PermWrite }

func (t *UpdateMemoryTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "update_memory",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"action"},
				Properties: map[string]backend.ToolProperty{
					"action": {
						Type:        "string",
						Description: "read: return current file contents. append: add content to the end. rewrite: replace entire file contents (use to consolidate).",
					},
					"content": {
						Type:        "string",
						Description: "Required for append/rewrite. Markdown content to write.",
					},
				},
			},
		},
	}
}

func (t *UpdateMemoryTool) Execute(_ context.Context, args map[string]any) ToolResult {
	action, _ := args["action"].(string)

	agentName := ""
	if t.resolver != nil {
		agentName = t.resolver.DefaultAgentName()
	}
	if agentName == "" {
		return ToolResult{IsError: true, Error: "update_memory: no active agent"}
	}

	switch strings.ToLower(strings.TrimSpace(action)) {
	case "read":
		content, err := mem.ReadNotes(t.huginnHome, agentName)
		if err != nil {
			return ToolResult{IsError: true, Error: "update_memory: " + err.Error()}
		}
		if strings.TrimSpace(content) == "" {
			return ToolResult{Output: "(empty — no notes saved yet)"}
		}
		return ToolResult{Output: content}

	case "append":
		content, _ := args["content"].(string)
		if strings.TrimSpace(content) == "" {
			return ToolResult{IsError: true, Error: "update_memory: 'content' is required for append"}
		}
		if err := mem.AppendNotes(t.huginnHome, agentName, content); err != nil {
			return ToolResult{IsError: true, Error: "update_memory: " + err.Error()}
		}
		return ToolResult{Output: "Memory updated."}

	case "rewrite":
		content, _ := args["content"].(string)
		if strings.TrimSpace(content) == "" {
			return ToolResult{IsError: true, Error: "update_memory: 'content' is required for rewrite"}
		}
		if err := mem.RewriteNotes(t.huginnHome, agentName, content); err != nil {
			return ToolResult{IsError: true, Error: "update_memory: " + err.Error()}
		}
		return ToolResult{Output: "Memory rewritten."}

	default:
		return ToolResult{IsError: true, Error: "update_memory: action must be one of: read, append, rewrite"}
	}
}

// RegisterNotesTool registers the update_memory tool.
// resolver is called at execution time to get the current active agent name.
func RegisterNotesTool(reg *Registry, huginnHome string, resolver AgentNameResolver) {
	reg.Register(&UpdateMemoryTool{huginnHome: huginnHome, resolver: resolver})
}
