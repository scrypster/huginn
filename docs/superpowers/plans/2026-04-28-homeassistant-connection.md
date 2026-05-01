# Home Assistant Connection Provider — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Home Assistant as a connection provider so agents can read device states, call any service (lights, switches, scripts, automations), and activate scenes via the Home Assistant REST API.

**Architecture:** Four files change. The catalog JSON entry drives the UI automatically. A `validateHomeAssistantCredentials` function is registered in `credential_validators.go`. A new `internal/connections/tools/homeassistant.go` file implements `ha_states`, `ha_call_service`, and `ha_scene_activate` tools following the existing `notion.go` pattern. `register.go` gains a new entry in `providerToolNames` and a new case in `RegisterForProvider`.

**Tech Stack:** Go standard library (`net/http`, `io`, `fmt`, `encoding/json`, `strings`). No new dependencies.

---

## File Changes

| File | Change |
|------|--------|
| `internal/connections/connection.go` | Add `ProviderHomeAssistant` constant |
| `internal/connections/catalog/catalog.json` | Insert Home Assistant entry in the `personal` category |
| `internal/server/credential_validators.go` | Register `"homeassistant"` in `buildCredentialValidatorRegistry`; add `validateHomeAssistantCredentials` |
| `internal/server/credential_validators_test.go` | Add 3 `TestValidateHomeAssistant*` tests |
| `internal/connections/tools/homeassistant.go` | New file: `haDo`, `haCreds`, tool structs, `registerHomeAssistantTools` |
| `internal/connections/tools/register.go` | Add to `providerToolNames` map and add `ProviderHomeAssistant` case in `RegisterForProvider` |

---

### Task 1: Provider Constant

**Files:**
- Modify: `internal/connections/connection.go`

- [ ] **Step 1: Add the constant**

Open `internal/connections/connection.go`. After the last constant in the `const` block, add:

```go
	ProviderHomeAssistant Provider = "homeassistant"
```

- [ ] **Step 2: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/connections/connection.go
git commit -m "feat(connections): add ProviderHomeAssistant constant"
```

---

### Task 2: Catalog Entry

**Files:**
- Modify: `internal/connections/catalog/catalog.json`

- [ ] **Step 1: Find the insertion point**

Open `internal/connections/catalog/catalog.json` and find a suitable location in the `"personal"` category. Insert the Home Assistant entry after any other personal-category entries.

- [ ] **Step 2: Insert the catalog entry**

Add the following JSON object:

```json
  {
    "id": "homeassistant",
    "name": "Home Assistant",
    "description": "Smart home control — lights, switches, scenes, devices",
    "category": "personal",
    "icon": "HA",
    "icon_color": "#18bcf2",
    "type": "credentials",
    "default_label": "Home Assistant",
    "multi_account": false,
    "fields": [
      {
        "key": "base_url",
        "label": "Base URL",
        "type": "url",
        "required": true,
        "stored_in": "metadata",
        "placeholder": "http://homeassistant.local:8123",
        "help_text": "URL of your Home Assistant instance (e.g. http://192.168.1.10:8123)"
      },
      {
        "key": "token",
        "label": "Long-Lived Access Token",
        "type": "password",
        "required": true,
        "stored_in": "creds",
        "placeholder": "eyJ...",
        "help_text": "Create under your Profile → Long-Lived Access Tokens in Home Assistant"
      }
    ],
    "validation": {
      "available": true,
      "description": "Calls GET /api/ to verify connectivity and the token."
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
git commit -m "feat(connections): add Home Assistant catalog entry"
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
// ── Home Assistant validator tests ────────────────────────────────────────────

func TestValidateHomeAssistantCredentials_MissingToken(t *testing.T) {
	err := validateHomeAssistantCredentials(context.Background(), "http://localhost:8123", "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if err.Error() != "token is required" {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateHomeAssistantCredentials_InvalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/" {
			t.Errorf("expected /api/, got %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer badtoken" {
			t.Errorf("expected Bearer badtoken, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized) // 401
	}))
	defer srv.Close()

	err := validateHomeAssistantCredentials(context.Background(), srv.URL, "badtoken")
	if err == nil || err.Error() != "invalid token or cannot reach Home Assistant" {
		t.Errorf("expected 'invalid token or cannot reach Home Assistant', got %v", err)
	}
}

func TestValidateHomeAssistantCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := validateHomeAssistantCredentials(context.Background(), srv.URL, "goodtoken")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/server/... -run TestValidateHomeAssistant -v 2>&1 | head -20
```
Expected: FAIL — `validateHomeAssistantCredentials` not defined yet.

- [ ] **Step 3: Add the validator to `credential_validators.go`**

In `buildCredentialValidatorRegistry`, add in the `── Communication ─` block (after the `discord` registration, before the `// ── Observability` comment):

```go
	r.Register("homeassistant", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateHomeAssistantCredentials(ctx, f["base_url"], f["token"])
	}))
```

Add the function at the end of the file:

```go
func validateHomeAssistantCredentials(ctx context.Context, baseURL, token string) error {
	if token == "" {
		return errors.New("token is required")
	}
	if baseURL == "" {
		baseURL = "http://homeassistant.local:8123"
	}
	// Trim trailing slash so the /api/ path is always correct.
	baseURL = strings.TrimRight(baseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("homeassistant: validation request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("invalid token or cannot reach Home Assistant")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("homeassistant: validation returned %d", resp.StatusCode)
	}
	return nil
}
```

Add `"errors"` and `"strings"` to the imports in `credential_validators.go` if not already present.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/... -run TestValidateHomeAssistant -v
```
Expected: all 3 `TestValidateHomeAssistant*` tests PASS.

- [ ] **Step 5: Build and full test run**

```bash
go build ./... && go test ./internal/server/... 2>&1 | tail -10
```
Expected: no build errors, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/server/credential_validators.go internal/server/credential_validators_test.go
git commit -m "feat(connections): add Home Assistant credential validator (GET /api/)"
```

---

### Task 4: Tools File

**Files:**
- Create: `internal/connections/tools/homeassistant.go`

- [ ] **Step 1: Create the file**

Create `internal/connections/tools/homeassistant.go` with the following content:

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

// haDo performs an authenticated Home Assistant REST API request.
func haDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
	if len(data) == 0 {
		return `{"ok": true}`, nil
	}
	return string(data), nil
}

// haCreds extracts the base URL and token from a connection.
func haCreds(mgr *connections.Manager, conn connections.Connection) (baseURL, token string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	baseURL = strings.TrimRight(conn.Metadata["base_url"], "/")
	if baseURL == "" {
		baseURL = "http://homeassistant.local:8123"
	}
	return baseURL, creds["token"], nil
}

// --- ha_states ---

type haStatesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *haStatesTool) Name() string        { return "ha_states" }
func (t *haStatesTool) Description() string { return "Get Home Assistant entity states. Omit entity_id to list all states." }
func (t *haStatesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *haStatesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "ha_states",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"entity_id": {Type: "string", Description: "Entity ID to retrieve (e.g. 'light.living_room'). Omit to list all states."},
				},
			},
		},
	}
}
func (t *haStatesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := haCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "ha_states: auth: " + err.Error()}
	}
	apiURL := baseURL + "/api/states"
	if entityID, ok := args["entity_id"].(string); ok && entityID != "" {
		apiURL = fmt.Sprintf("%s/api/states/%s", baseURL, entityID)
	}
	out, err := haDo(ctx, http.MethodGet, apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- ha_call_service ---

type haCallServiceTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *haCallServiceTool) Name() string        { return "ha_call_service" }
func (t *haCallServiceTool) Description() string { return "Call a Home Assistant service (e.g. turn on a light). Requires user approval." }
func (t *haCallServiceTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *haCallServiceTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "ha_call_service",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"domain", "service"},
				Properties: map[string]backend.ToolProperty{
					"domain":       {Type: "string", Description: "Service domain (e.g. 'light', 'switch', 'script', 'automation')"},
					"service":      {Type: "string", Description: "Service name (e.g. 'turn_on', 'turn_off', 'toggle')"},
					"entity_id":    {Type: "string", Description: "Target entity ID (optional, e.g. 'light.living_room')"},
					"service_data": {Type: "object", Description: "Additional service data as a JSON object (optional)"},
				},
			},
		},
	}
}
func (t *haCallServiceTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := haCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "ha_call_service: auth: " + err.Error()}
	}
	domain, ok := args["domain"].(string)
	if !ok || domain == "" {
		return tools.ToolResult{IsError: true, Error: "ha_call_service: domain is required"}
	}
	service, ok := args["service"].(string)
	if !ok || service == "" {
		return tools.ToolResult{IsError: true, Error: "ha_call_service: service is required"}
	}
	payload := map[string]any{}
	if entityID, ok := args["entity_id"].(string); ok && entityID != "" {
		payload["entity_id"] = entityID
	}
	if sd, ok := args["service_data"].(map[string]any); ok {
		for k, v := range sd {
			payload[k] = v
		}
	}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("%s/api/services/%s/%s", baseURL, domain, service)
	out, err := haDo(ctx, http.MethodPost, apiURL, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- ha_scene_activate ---

type haSceneActivateTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *haSceneActivateTool) Name() string        { return "ha_scene_activate" }
func (t *haSceneActivateTool) Description() string { return "Activate a Home Assistant scene. Requires user approval." }
func (t *haSceneActivateTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *haSceneActivateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "ha_scene_activate",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"scene_id"},
				Properties: map[string]backend.ToolProperty{
					"scene_id": {Type: "string", Description: "Scene entity ID (e.g. 'scene.movie_night')"},
				},
			},
		},
	}
}
func (t *haSceneActivateTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := haCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "ha_scene_activate: auth: " + err.Error()}
	}
	sceneID, ok := args["scene_id"].(string)
	if !ok || sceneID == "" {
		return tools.ToolResult{IsError: true, Error: "ha_scene_activate: scene_id is required"}
	}
	payload := map[string]any{"entity_id": sceneID}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("%s/api/services/scene/turn_on", baseURL)
	out, err := haDo(ctx, http.MethodPost, apiURL, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerHomeAssistantTools registers all 3 Home Assistant tools.
func registerHomeAssistantTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"ha_states", "ha_call_service", "ha_scene_activate"}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &haStatesTool{mgr: mgr, conns: conns})
	strictInject(reg, &haCallServiceTool{mgr: mgr, conns: conns})
	strictInject(reg, &haSceneActivateTool{mgr: mgr, conns: conns})
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
git add internal/connections/tools/homeassistant.go
git commit -m "feat(connections/tools): add Home Assistant ha_states, ha_call_service, ha_scene_activate tools"
```

---

### Task 5: Register in register.go

**Files:**
- Modify: `internal/connections/tools/register.go`

- [ ] **Step 1: Add to `providerToolNames`**

In `internal/connections/tools/register.go`, inside the `providerToolNames` map (after the last existing entry), add:

```go
	connections.ProviderHomeAssistant: {"ha_states", "ha_call_service", "ha_scene_activate"},
```

- [ ] **Step 2: Add case to `RegisterForProvider`**

In the `switch provider` block inside `RegisterForProvider`, after the last existing case and before the closing `}`, add:

```go
	case connections.ProviderHomeAssistant:
		err = registerHomeAssistantTools(reg, mgr, conns)
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
git commit -m "feat(connections/tools): register Home Assistant provider in tool registry"
```

---

### Final Verification

- [ ] **Full build and test**

```bash
go build ./... && go test ./... 2>&1 | grep -E "^(ok|FAIL|---)" | tail -30
```
Expected: no FAIL lines.
