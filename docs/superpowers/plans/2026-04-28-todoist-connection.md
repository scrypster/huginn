# Todoist Connection Provider — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Todoist as a connection provider so agents can list tasks and projects, create tasks, and mark tasks complete via the Todoist REST API v2.

**Architecture:** Four files change. The catalog JSON entry drives the UI automatically. A `validateTodoistCredentials` function is registered in `credential_validators.go`. A new `internal/connections/tools/todoist.go` file implements `tasks_list`, `task_create`, `task_complete`, and `tasks_list_projects` tools following the existing `notion.go` pattern. `register.go` gains a new entry in `providerToolNames` and a new case in `RegisterForProvider`.

**Tech Stack:** Go standard library (`net/http`, `io`, `fmt`, `encoding/json`, `strings`). No new dependencies.

---

## File Changes

| File | Change |
|------|--------|
| `internal/connections/connection.go` | Add `ProviderTodoist` constant |
| `internal/connections/catalog/catalog.json` | Insert Todoist entry in the `personal` category |
| `internal/server/credential_validators.go` | Register `"todoist"` in `buildCredentialValidatorRegistry`; add `validateTodoistCredentials` |
| `internal/server/credential_validators_test.go` | Add 3 `TestValidateTodoist*` tests |
| `internal/connections/tools/todoist.go` | New file: `todoistDo`, `todoistCreds`, tool structs, `registerTodoistTools` |
| `internal/connections/tools/register.go` | Add to `providerToolNames` map and add `ProviderTodoist` case in `RegisterForProvider` |

---

### Task 1: Provider Constant

**Files:**
- Modify: `internal/connections/connection.go`

- [ ] **Step 1: Add the constant**

Open `internal/connections/connection.go`. After the last constant in the `const` block (currently `ProviderMonday` or `ProviderWeather` if that plan was already applied), add:

```go
	ProviderTodoist     Provider = "todoist"
```

- [ ] **Step 2: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/connections/connection.go
git commit -m "feat(connections): add ProviderTodoist constant"
```

---

### Task 2: Catalog Entry

**Files:**
- Modify: `internal/connections/catalog/catalog.json`

- [ ] **Step 1: Find the insertion point**

Open `internal/connections/catalog/catalog.json` and find a suitable location in the `"personal"` category. Insert the Todoist entry after any other personal-category entries.

- [ ] **Step 2: Insert the catalog entry**

Add the following JSON object:

```json
  {
    "id": "todoist",
    "name": "Todoist",
    "description": "Tasks, projects, and productivity management",
    "category": "personal",
    "icon": "TD",
    "icon_color": "#e44332",
    "type": "credentials",
    "default_label": "Todoist",
    "multi_account": false,
    "fields": [
      {
        "key": "api_key",
        "label": "API Token",
        "type": "password",
        "required": true,
        "stored_in": "creds",
        "placeholder": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
        "help_text": "Found under Todoist Settings → Integrations → Developer → API token"
      }
    ],
    "validation": {
      "available": true,
      "description": "Calls GET /rest/v2/projects to verify the API token."
    }
  },
```

- [ ] **Step 3: Verify JSON is valid**

```bash
python3 -m json.tool internal/connections/catalog/catalog.json > /dev/null && echo "valid"
```
Expected: `valid`

- [ ] **Step 4: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/connections/catalog/catalog.json
git commit -m "feat(connections): add Todoist catalog entry"
```

---

### Task 3: Credential Validator

**Files:**
- Modify: `internal/server/credential_validators.go`
- Modify: `internal/server/credential_validators_test.go` (create if absent)

- [ ] **Step 1: Write the failing tests**

Check if the test file exists:

```bash
ls internal/server/credential_validators_test.go 2>/dev/null || echo "missing"
```

If missing, create `internal/server/credential_validators_test.go` with:

```go
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)
```

If the file already exists, simply append the following tests to it.

Append these three tests:

```go
// ── Todoist validator tests ───────────────────────────────────────────────────

func TestValidateTodoistCredentials_MissingKey(t *testing.T) {
	err := validateTodoistCredentials(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
	if err.Error() != "api_key is required" {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateTodoistCredentials_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/v2/projects" {
			t.Errorf("expected /rest/v2/projects, got %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer badtoken" {
			t.Errorf("expected Bearer badtoken, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized) // 401
	}))
	defer srv.Close()

	origURL := todoistValidationURL
	todoistValidationURL = srv.URL + "/rest/v2/projects"
	defer func() { todoistValidationURL = origURL }()

	err := validateTodoistCredentials(context.Background(), "badtoken")
	if err == nil || err.Error() != "invalid API token" {
		t.Errorf("expected 'invalid API token', got %v", err)
	}
}

func TestValidateTodoistCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origURL := todoistValidationURL
	todoistValidationURL = srv.URL + "/rest/v2/projects"
	defer func() { todoistValidationURL = origURL }()

	err := validateTodoistCredentials(context.Background(), "goodtoken")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/server/... -run TestValidateTodoist -v 2>&1 | head -20
```
Expected: FAIL — `validateTodoistCredentials` and `todoistValidationURL` not defined yet.

- [ ] **Step 3: Add the validator to `credential_validators.go`**

In `buildCredentialValidatorRegistry`, add in the `── Communication ─` block (after the `discord` registration, before the `// ── Observability` comment):

```go
	r.Register("todoist", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateTodoistCredentials(ctx, f["api_key"])
	}))
```

Then add a package-level variable immediately before the `buildCredentialValidatorRegistry` function:

```go
// todoistValidationURL is the endpoint used to validate Todoist API tokens.
// Overridable in tests.
var todoistValidationURL = "https://api.todoist.com/rest/v2/projects"
```

Add the function at the end of the file:

```go
func validateTodoistCredentials(ctx context.Context, apiKey string) error {
	if apiKey == "" {
		return errors.New("api_key is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, todoistValidationURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("todoist: validation request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("invalid API token")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("todoist: validation returned %d", resp.StatusCode)
	}
	return nil
}
```

Add `"errors"` to the imports in `credential_validators.go` if not already present.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/... -run TestValidateTodoist -v
```
Expected: all 3 `TestValidateTodoist*` tests PASS.

- [ ] **Step 5: Build and full test run**

```bash
go build ./... && go test ./internal/server/... 2>&1 | tail -10
```
Expected: no build errors, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/server/credential_validators.go internal/server/credential_validators_test.go
git commit -m "feat(connections): add Todoist credential validator (GET /rest/v2/projects)"
```

---

### Task 4: Tools File

**Files:**
- Create: `internal/connections/tools/todoist.go`

- [ ] **Step 1: Create the file**

Create `internal/connections/tools/todoist.go` with the following content:

```go
package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// todoistDo performs an authenticated Todoist REST API v2 request.
func todoistDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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
	if len(data) == 0 {
		return `{"ok": true}`, nil
	}
	return string(data), nil
}

// todoistCreds extracts the API token from a connection.
func todoistCreds(mgr *connections.Manager, conn connections.Connection) (string, error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["api_key"], nil
}

// --- tasks_list ---

type todoistTasksListTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistTasksListTool) Name() string        { return "tasks_list" }
func (t *todoistTasksListTool) Description() string { return "List Todoist tasks, optionally filtered by project or filter string." }
func (t *todoistTasksListTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *todoistTasksListTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "tasks_list",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"project_id": {Type: "string", Description: "Filter tasks by project ID (optional)"},
					"filter":     {Type: "string", Description: "Todoist filter expression, e.g. 'today' or 'overdue' (optional)"},
				},
			},
		},
	}
}
func (t *todoistTasksListTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "tasks_list: auth: " + err.Error()}
	}
	apiURL := "https://api.todoist.com/rest/v2/tasks"
	params := []string{}
	if pid, ok := args["project_id"].(string); ok && pid != "" {
		params = append(params, "project_id="+pid)
	}
	if f, ok := args["filter"].(string); ok && f != "" {
		params = append(params, "filter="+f)
	}
	if len(params) > 0 {
		apiURL += "?" + strings.Join(params, "&")
	}
	out, err := todoistDo(ctx, http.MethodGet, apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- task_create ---

type todoistTaskCreateTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistTaskCreateTool) Name() string        { return "task_create" }
func (t *todoistTaskCreateTool) Description() string { return "Create a new Todoist task. Requires user approval." }
func (t *todoistTaskCreateTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *todoistTaskCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "task_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"content"},
				Properties: map[string]backend.ToolProperty{
					"content":    {Type: "string", Description: "Task content / title"},
					"project_id": {Type: "string", Description: "Project ID to add the task to (optional, defaults to Inbox)"},
					"due_string": {Type: "string", Description: "Natural language due date, e.g. 'tomorrow', 'next Monday' (optional)"},
				},
			},
		},
	}
}
func (t *todoistTaskCreateTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "task_create: auth: " + err.Error()}
	}
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return tools.ToolResult{IsError: true, Error: "task_create: content is required"}
	}
	payload := map[string]any{"content": content}
	if pid, ok := args["project_id"].(string); ok && pid != "" {
		payload["project_id"] = pid
	}
	if due, ok := args["due_string"].(string); ok && due != "" {
		payload["due_string"] = due
	}
	bodyBytes, _ := json.Marshal(payload)
	out, err := todoistDo(ctx, http.MethodPost, "https://api.todoist.com/rest/v2/tasks", token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- task_complete ---

type todoistTaskCompleteTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistTaskCompleteTool) Name() string        { return "task_complete" }
func (t *todoistTaskCompleteTool) Description() string { return "Mark a Todoist task as complete. Requires user approval." }
func (t *todoistTaskCompleteTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *todoistTaskCompleteTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "task_complete",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"task_id"},
				Properties: map[string]backend.ToolProperty{
					"task_id": {Type: "string", Description: "The Todoist task ID to mark complete"},
				},
			},
		},
	}
}
func (t *todoistTaskCompleteTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "task_complete: auth: " + err.Error()}
	}
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return tools.ToolResult{IsError: true, Error: "task_complete: task_id is required"}
	}
	apiURL := fmt.Sprintf("https://api.todoist.com/rest/v2/tasks/%s/close", taskID)
	out, err := todoistDo(ctx, http.MethodPost, apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- tasks_list_projects ---

type todoistListProjectsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistListProjectsTool) Name() string        { return "tasks_list_projects" }
func (t *todoistListProjectsTool) Description() string { return "List all Todoist projects." }
func (t *todoistListProjectsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *todoistListProjectsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "tasks_list_projects",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *todoistListProjectsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "tasks_list_projects: auth: " + err.Error()}
	}
	out, err := todoistDo(ctx, http.MethodGet, "https://api.todoist.com/rest/v2/projects", token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerTodoistTools registers all 4 Todoist tools.
func registerTodoistTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"tasks_list", "task_create", "task_complete", "tasks_list_projects"}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &todoistTasksListTool{mgr: mgr, conns: conns})
	strictInject(reg, &todoistTaskCreateTool{mgr: mgr, conns: conns})
	strictInject(reg, &todoistTaskCompleteTool{mgr: mgr, conns: conns})
	strictInject(reg, &todoistListProjectsTool{mgr: mgr, conns: conns})
	return nil
}
```

- [ ] **Step 2: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/connections/tools/todoist.go
git commit -m "feat(connections/tools): add Todoist tasks_list, task_create, task_complete, tasks_list_projects tools"
```

---

### Task 5: Register in register.go

**Files:**
- Modify: `internal/connections/tools/register.go`

- [ ] **Step 1: Add to `providerToolNames`**

In `internal/connections/tools/register.go`, inside the `providerToolNames` map (after the last existing entry), add:

```go
	connections.ProviderTodoist: {"tasks_list", "task_create", "task_complete", "tasks_list_projects"},
```

- [ ] **Step 2: Add case to `RegisterForProvider`**

In the `switch provider` block inside `RegisterForProvider`, after the last existing case and before the closing `}`, add:

```go
	case connections.ProviderTodoist:
		err = registerTodoistTools(reg, mgr, conns)
```

- [ ] **Step 3: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 4: Run all connection tools tests**

```bash
go test ./internal/connections/tools/... 2>&1 | tail -10
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/connections/tools/register.go
git commit -m "feat(connections/tools): register Todoist provider in tool registry"
```

---

### Final Verification

- [ ] **Full build and test**

```bash
go build ./... && go test ./... 2>&1 | grep -E "^(ok|FAIL|---)" | tail -30
```
Expected: no FAIL lines.
