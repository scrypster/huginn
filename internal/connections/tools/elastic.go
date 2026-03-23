package conntools

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// elasticEncodeKey returns the base64-encoded form of an Elastic API key.
// Elastic's ApiKey scheme requires the key to be base64(id:api_key).
// If the key already looks base64-encoded (no colon present after decode check)
// we pass it through unchanged to support pre-encoded keys.
func elasticEncodeKey(apiKey string) string {
	// If the value contains a colon it is in "id:secret" raw form and must be base64-encoded.
	if strings.Contains(apiKey, ":") {
		return base64.StdEncoding.EncodeToString([]byte(apiKey))
	}
	// Otherwise assume it is already encoded (e.g. the "encoded" field from the Create API Key response).
	return apiKey
}

// elasticDo performs an authenticated Elasticsearch API request.
func elasticDo(ctx context.Context, method, baseURL, apiKey, path string, body []byte) (string, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bodyReader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "ApiKey "+elasticEncodeKey(apiKey))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	return string(data), nil
}

// elasticCreds extracts base URL and API key from a connection.
func elasticCreds(mgr *connections.Manager, conn connections.Connection) (baseURL, apiKey string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	baseURL = conn.Metadata["url"]
	if baseURL == "" {
		return "", "", fmt.Errorf("elastic: url not configured")
	}
	// Normalize: strip trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	return baseURL, creds["api_key"], nil
}

// --- elastic_cluster_health ---

type elasticClusterHealthTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *elasticClusterHealthTool) Name() string        { return "elastic_cluster_health" }
func (t *elasticClusterHealthTool) Description() string { return "Get Elasticsearch cluster health status." }
func (t *elasticClusterHealthTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *elasticClusterHealthTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "elastic_cluster_health",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *elasticClusterHealthTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, err := elasticCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "elastic_cluster_health: auth: " + err.Error()}
	}
	out, err := elasticDo(ctx, "GET", baseURL, apiKey, "/_cluster/health", nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- elastic_list_indices ---

type elasticListIndicesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *elasticListIndicesTool) Name() string        { return "elastic_list_indices" }
func (t *elasticListIndicesTool) Description() string { return "List all Elasticsearch indices with their health, status, and document count." }
func (t *elasticListIndicesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *elasticListIndicesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "elastic_list_indices",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"pattern": {Type: "string", Description: "Index pattern filter (e.g. logs-*)"},
				},
			},
		},
	}
}
func (t *elasticListIndicesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, err := elasticCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "elastic_list_indices: auth: " + err.Error()}
	}
	pattern, _ := args["pattern"].(string)
	var elasticURL string
	if pattern != "" {
		elasticURL = "/_cat/indices/" + pattern + "?format=json"
	} else {
		elasticURL = "/_cat/indices?format=json"
	}
	out, err := elasticDo(ctx, "GET", baseURL, apiKey, elasticURL, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- elastic_search ---

type elasticSearchTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *elasticSearchTool) Name() string        { return "elastic_search" }
func (t *elasticSearchTool) Description() string { return "Execute an Elasticsearch search query against one or more indices." }
func (t *elasticSearchTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *elasticSearchTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "elastic_search",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"index", "query"},
				Properties: map[string]backend.ToolProperty{
					"index": {Type: "string", Description: "Index name or pattern to search (e.g. 'logs-*')"},
					"query": {Type: "string", Description: "JSON query body (Elasticsearch Query DSL)"},
					"size":  {Type: "integer", Description: "Maximum number of results (default 10)"},
				},
			},
		},
	}
}
func (t *elasticSearchTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, err := elasticCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "elastic_search: auth: " + err.Error()}
	}
	index, _ := args["index"].(string)
	query, _ := args["query"].(string)
	if index == "" || query == "" {
		return tools.ToolResult{IsError: true, Error: "elastic_search: index and query are required"}
	}
	size := 10
	if s, ok := args["size"].(float64); ok && s > 0 {
		size = int(s)
	}
	body := fmt.Sprintf(`{"size":%d,"query":%s}`, size, query)
	path := fmt.Sprintf("/%s/_search", index)
	out, err := elasticDo(ctx, "POST", baseURL, apiKey, path, []byte(body))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- elastic_get_document ---

type elasticGetDocumentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *elasticGetDocumentTool) Name() string        { return "elastic_get_document" }
func (t *elasticGetDocumentTool) Description() string { return "Retrieve a specific document from Elasticsearch by index and document ID." }
func (t *elasticGetDocumentTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *elasticGetDocumentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "elastic_get_document",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"index", "id"},
				Properties: map[string]backend.ToolProperty{
					"index": {Type: "string", Description: "Index name"},
					"id":    {Type: "string", Description: "Document ID"},
				},
			},
		},
	}
}
func (t *elasticGetDocumentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, err := elasticCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "elastic_get_document: auth: " + err.Error()}
	}
	index, _ := args["index"].(string)
	docID, _ := args["id"].(string)
	if index == "" || docID == "" {
		return tools.ToolResult{IsError: true, Error: "elastic_get_document: index and id are required"}
	}
	path := fmt.Sprintf("/%s/_doc/%s", index, docID)
	out, err := elasticDo(ctx, "GET", baseURL, apiKey, path, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- elastic_index_document ---

type elasticIndexDocumentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *elasticIndexDocumentTool) Name() string        { return "elastic_index_document" }
func (t *elasticIndexDocumentTool) Description() string { return "Index (create or update) a document in Elasticsearch." }
func (t *elasticIndexDocumentTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *elasticIndexDocumentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "elastic_index_document",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"index", "document"},
				Properties: map[string]backend.ToolProperty{
					"index":    {Type: "string", Description: "The index name"},
					"document": {Type: "string", Description: "JSON document content"},
				},
			},
		},
	}
}
func (t *elasticIndexDocumentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, err := elasticCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "elastic_index_document: auth: " + err.Error()}
	}
	index, _ := args["index"].(string)
	document, _ := args["document"].(string)
	if index == "" || document == "" {
		return tools.ToolResult{IsError: true, Error: "elastic_index_document: index and document are required"}
	}
	path := fmt.Sprintf("/%s/_doc", index)
	out, err := elasticDo(ctx, "POST", baseURL, apiKey, path, []byte(document))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- elastic_delete_document ---

type elasticDeleteDocumentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *elasticDeleteDocumentTool) Name() string        { return "elastic_delete_document" }
func (t *elasticDeleteDocumentTool) Description() string { return "Delete a document from Elasticsearch by index and document ID." }
func (t *elasticDeleteDocumentTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *elasticDeleteDocumentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "elastic_delete_document",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"index", "id"},
				Properties: map[string]backend.ToolProperty{
					"index": {Type: "string", Description: "Index name"},
					"id":    {Type: "string", Description: "Document ID"},
				},
			},
		},
	}
}
func (t *elasticDeleteDocumentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, err := elasticCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "elastic_delete_document: auth: " + err.Error()}
	}
	index, _ := args["index"].(string)
	docID, _ := args["id"].(string)
	if index == "" || docID == "" {
		return tools.ToolResult{IsError: true, Error: "elastic_delete_document: index and id are required"}
	}
	path := fmt.Sprintf("/%s/_doc/%s", index, docID)
	out, err := elasticDo(ctx, "DELETE", baseURL, apiKey, path, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerElasticTools registers all Elasticsearch tools.
func registerElasticTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"elastic_cluster_health", "elastic_list_indices", "elastic_search",
		"elastic_get_document", "elastic_index_document", "elastic_delete_document",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &elasticClusterHealthTool{mgr: mgr, conns: conns})
	strictInject(reg, &elasticListIndicesTool{mgr: mgr, conns: conns})
	strictInject(reg, &elasticSearchTool{mgr: mgr, conns: conns})
	strictInject(reg, &elasticGetDocumentTool{mgr: mgr, conns: conns})
	strictInject(reg, &elasticIndexDocumentTool{mgr: mgr, conns: conns})
	strictInject(reg, &elasticDeleteDocumentTool{mgr: mgr, conns: conns})
	return nil
}
