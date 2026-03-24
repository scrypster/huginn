package tools

import (
	"context"
	"fmt"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/workforce"
)

// ArtifactTool lets agents persist named artifacts (documents, patches, data)
// so they survive beyond the current session and are accessible via the UI.
type ArtifactTool struct {
	store artifact.Store
}

// NewArtifactTool creates an ArtifactTool backed by the given store.
func NewArtifactTool(store artifact.Store) *ArtifactTool {
	return &ArtifactTool{store: store}
}

func (t *ArtifactTool) Name() string { return "create_artifact" }
func (t *ArtifactTool) Description() string {
	return "Save a document, code patch, or structured data as a persistent artifact that survives the session."
}
func (t *ArtifactTool) Permission() PermissionLevel { return PermWrite }

func (t *ArtifactTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "create_artifact",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"title", "kind", "content"},
				Properties: map[string]backend.ToolProperty{
					"title": {
						Type:        "string",
						Description: "Human-readable title for the artifact (e.g. 'Auth refactor patch').",
					},
					"kind": {
						Type:        "string",
						Description: "Artifact type: code_patch | document | timeline | structured_data | file_bundle",
					},
					"content": {
						Type:        "string",
						Description: "The artifact content (text, patch diff, JSON, Markdown, etc.).",
					},
					"mime_type": {
						Type:        "string",
						Description: "Optional MIME type (e.g. 'text/markdown', 'application/json'). Defaults to text/plain.",
					},
				},
			},
		},
	}
}

func (t *ArtifactTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	if t.store == nil {
		return ToolResult{IsError: true, Error: "create_artifact: artifact store not available"}
	}

	title, _ := args["title"].(string)
	kindStr, _ := args["kind"].(string)
	content, _ := args["content"].(string)
	mimeType, _ := args["mime_type"].(string)

	if title == "" {
		return ToolResult{IsError: true, Error: "create_artifact: 'title' is required"}
	}
	if content == "" {
		return ToolResult{IsError: true, Error: "create_artifact: 'content' is required"}
	}
	if !workforce.ValidateKind(workforce.ArtifactKind(kindStr)) {
		return ToolResult{IsError: true, Error: fmt.Sprintf("create_artifact: unknown kind %q (must be code_patch|document|timeline|structured_data|file_bundle)", kindStr)}
	}
	if mimeType == "" {
		mimeType = "text/plain"
	}

	a := &workforce.Artifact{
		Kind:     workforce.ArtifactKind(kindStr),
		Title:    title,
		MimeType: mimeType,
		Content:  []byte(content),
		Status:   workforce.StatusDraft,
	}
	if err := t.store.Write(ctx, a); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("create_artifact: %v", err)}
	}
	return ToolResult{Output: fmt.Sprintf("artifact created: %q (id: %s)", title, a.ID)}
}

var _ Tool = (*ArtifactTool)(nil)
