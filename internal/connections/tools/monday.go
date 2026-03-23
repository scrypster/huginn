package conntools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// mondayDo performs an authenticated Monday.com GraphQL API request.
// Note: Monday.com uses the token directly without a "Bearer" prefix.
func mondayDo(ctx context.Context, token, query string) (string, error) {
	body := strings.NewReader(fmt.Sprintf(`{"query":"%s"}`, strings.ReplaceAll(query, `"`, `\"`)))
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.monday.com/v2", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
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

// mondayCreds extracts the token from a connection.
func mondayCreds(mgr *connections.Manager, conn connections.Connection) (string, error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["token"], nil
}

// --- monday_list_boards ---

type mondayListBoardsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *mondayListBoardsTool) Name() string        { return "monday_list_boards" }
func (t *mondayListBoardsTool) Description() string { return "List Monday.com boards." }
func (t *mondayListBoardsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *mondayListBoardsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "monday_list_boards",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of boards to return (default 25)"},
				},
			},
		},
	}
}
func (t *mondayListBoardsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := mondayCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "monday_list_boards: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	query := fmt.Sprintf("{ boards(limit:%d) { id name description } }", limit)
	out, err := mondayDo(ctx, token, query)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- monday_get_board ---

type mondayGetBoardTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *mondayGetBoardTool) Name() string        { return "monday_get_board" }
func (t *mondayGetBoardTool) Description() string { return "Get a Monday.com board by ID including its columns." }
func (t *mondayGetBoardTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *mondayGetBoardTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "monday_get_board",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"board_id"},
				Properties: map[string]backend.ToolProperty{
					"board_id": {Type: "integer", Description: "The Monday.com board ID"},
				},
			},
		},
	}
}
func (t *mondayGetBoardTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := mondayCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "monday_get_board: auth: " + err.Error()}
	}

	boardID := int(floatArg(args, "board_id"))
	if boardID == 0 {
		return tools.ToolResult{IsError: true, Error: "monday_get_board: board_id is required"}
	}

	query := fmt.Sprintf("{ boards(ids:[%d]) { id name columns { id title type } } }", boardID)
	out, err := mondayDo(ctx, token, query)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- monday_list_items ---

type mondayListItemsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *mondayListItemsTool) Name() string        { return "monday_list_items" }
func (t *mondayListItemsTool) Description() string { return "List items in a Monday.com board." }
func (t *mondayListItemsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *mondayListItemsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "monday_list_items",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"board_id"},
				Properties: map[string]backend.ToolProperty{
					"board_id": {Type: "integer", Description: "The Monday.com board ID"},
					"limit":    {Type: "integer", Description: "Maximum number of items to return (default 25)"},
				},
			},
		},
	}
}
func (t *mondayListItemsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := mondayCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "monday_list_items: auth: " + err.Error()}
	}

	boardID := int(floatArg(args, "board_id"))
	if boardID == 0 {
		return tools.ToolResult{IsError: true, Error: "monday_list_items: board_id is required"}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	query := fmt.Sprintf("{ boards(ids:[%d]) { items_page(limit:%d) { items { id name state } } } }", boardID, limit)
	out, err := mondayDo(ctx, token, query)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- monday_create_item ---

type mondayCreateItemTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *mondayCreateItemTool) Name() string        { return "monday_create_item" }
func (t *mondayCreateItemTool) Description() string { return "Create a new item in a Monday.com board. Requires user approval." }
func (t *mondayCreateItemTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *mondayCreateItemTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "monday_create_item",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"board_id", "item_name"},
				Properties: map[string]backend.ToolProperty{
					"board_id":  {Type: "integer", Description: "The Monday.com board ID"},
					"item_name": {Type: "string", Description: "Name for the new item"},
				},
			},
		},
	}
}
func (t *mondayCreateItemTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := mondayCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "monday_create_item: auth: " + err.Error()}
	}

	boardID := int(floatArg(args, "board_id"))
	itemName, _ := args["item_name"].(string)
	if boardID == 0 || itemName == "" {
		return tools.ToolResult{IsError: true, Error: "monday_create_item: board_id and item_name are required"}
	}

	// Escape item_name for use in GraphQL string
	escapedName := strings.ReplaceAll(itemName, `"`, `\"`)
	query := fmt.Sprintf(`mutation { create_item (board_id: %d, item_name: "%s") { id } }`, boardID, escapedName)
	out, err := mondayDo(ctx, token, query)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- monday_update_item ---

type mondayUpdateItemTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *mondayUpdateItemTool) Name() string        { return "monday_update_item" }
func (t *mondayUpdateItemTool) Description() string { return "Update column values of a Monday.com item. Requires user approval." }
func (t *mondayUpdateItemTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *mondayUpdateItemTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "monday_update_item",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"board_id", "item_id", "column_values"},
				Properties: map[string]backend.ToolProperty{
					"board_id":      {Type: "integer", Description: "The Monday.com board ID"},
					"item_id":       {Type: "integer", Description: "The item ID to update"},
					"column_values": {Type: "string", Description: "JSON string of column values to update (e.g. {\"status\": {\"label\": \"Done\"}})"},
				},
			},
		},
	}
}
func (t *mondayUpdateItemTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := mondayCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "monday_update_item: auth: " + err.Error()}
	}

	boardID := int(floatArg(args, "board_id"))
	itemID := int(floatArg(args, "item_id"))
	columnValues, _ := args["column_values"].(string)
	if boardID == 0 || itemID == 0 || columnValues == "" {
		return tools.ToolResult{IsError: true, Error: "monday_update_item: board_id, item_id, and column_values are required"}
	}

	// Escape column_values JSON string for embedding in GraphQL
	escapedValues := strings.ReplaceAll(columnValues, `"`, `\"`)
	query := fmt.Sprintf(`mutation { change_multiple_column_values(item_id: %d, board_id: %d, column_values: "%s") { id } }`, itemID, boardID, escapedValues)
	out, err := mondayDo(ctx, token, query)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerMondayTools registers all 5 Monday.com tools.
func registerMondayTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"monday_list_boards", "monday_get_board", "monday_list_items",
		"monday_create_item", "monday_update_item",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &mondayListBoardsTool{mgr: mgr, conns: conns})
	strictInject(reg, &mondayGetBoardTool{mgr: mgr, conns: conns})
	strictInject(reg, &mondayListItemsTool{mgr: mgr, conns: conns})
	strictInject(reg, &mondayCreateItemTool{mgr: mgr, conns: conns})
	strictInject(reg, &mondayUpdateItemTool{mgr: mgr, conns: conns})
	return nil
}
