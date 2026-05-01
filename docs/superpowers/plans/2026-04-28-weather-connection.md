# OpenWeatherMap Connection Provider — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OpenWeatherMap as a connection provider so agents can retrieve current weather conditions and multi-day forecasts for any city worldwide.

**Architecture:** Four files change. The catalog JSON entry drives the UI automatically. A `validateWeatherCredentials` function is registered in `credential_validators.go`. A new `internal/connections/tools/weather.go` file implements `weather_current` and `weather_forecast` tools following the existing `notion.go` pattern. `register.go` gains a new entry in `providerToolNames` and a new case in `RegisterForProvider`.

**Tech Stack:** Go standard library (`net/http`, `io`, `fmt`, `net/url`). No new dependencies.

---

## File Changes

| File | Change |
|------|--------|
| `internal/connections/connection.go` | Add `ProviderWeather` constant |
| `internal/connections/catalog/catalog.json` | Insert OpenWeatherMap entry in the `personal` category |
| `internal/server/credential_validators.go` | Register `"weather"` in `buildCredentialValidatorRegistry`; add `validateWeatherCredentials` |
| `internal/server/credential_validators_test.go` | Add 3 `TestValidateWeather*` tests |
| `internal/connections/tools/weather.go` | New file: `weatherDo`, `weatherCreds`, tool structs, `registerWeatherTools` |
| `internal/connections/tools/register.go` | Add to `providerToolNames` map and add `ProviderWeather` case in `RegisterForProvider` |

---

### Task 1: Provider Constant

**Files:**
- Modify: `internal/connections/connection.go`

- [ ] **Step 1: Add the constant**

Open `internal/connections/connection.go`. After the `ProviderMonday` constant (the last entry in the `const` block, around line 28), add:

```go
	ProviderWeather     Provider = "weather"
```

The `const` block should now end:

```go
	ProviderMonday      Provider = "monday"
	ProviderWeather     Provider = "weather"
)
```

- [ ] **Step 2: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/connections/connection.go
git commit -m "feat(connections): add ProviderWeather constant"
```

---

### Task 2: Catalog Entry

**Files:**
- Modify: `internal/connections/catalog/catalog.json`

- [ ] **Step 1: Find the insertion point**

Open `internal/connections/catalog/catalog.json` and search for an existing `"personal"` category entry (e.g. `"category": "personal"`). Insert the new OpenWeatherMap entry after the last personal-category entry's closing `},`. If no personal category exists yet, insert before the final `]` of the file.

- [ ] **Step 2: Insert the catalog entry**

Add the following JSON object:

```json
  {
    "id": "weather",
    "name": "OpenWeatherMap",
    "description": "Current weather and forecasts for any city worldwide",
    "category": "personal",
    "icon": "WX",
    "icon_color": "#e96e0f",
    "type": "credentials",
    "default_label": "OpenWeatherMap",
    "multi_account": false,
    "fields": [
      {
        "key": "api_key",
        "label": "API Key",
        "type": "password",
        "required": true,
        "stored_in": "creds",
        "placeholder": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
        "help_text": "Free API key from openweathermap.org/api"
      },
      {
        "key": "units",
        "label": "Units",
        "type": "select",
        "required": false,
        "stored_in": "metadata",
        "default": "metric",
        "options": [
          {"label": "Metric (°C, m/s)", "value": "metric"},
          {"label": "Imperial (°F, mph)", "value": "imperial"},
          {"label": "Standard (K, m/s)", "value": "standard"}
        ],
        "help_text": "Unit system for temperature and wind speed"
      }
    ],
    "validation": {
      "available": true,
      "description": "Calls current weather for London to verify the API key."
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
git commit -m "feat(connections): add OpenWeatherMap catalog entry"
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
// ── OpenWeatherMap validator tests ────────────────────────────────────────────

func TestValidateWeatherCredentials_MissingKey(t *testing.T) {
	err := validateWeatherCredentials(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
	if err.Error() != "api_key is required" {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateWeatherCredentials_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/2.5/weather" {
			t.Errorf("expected /data/2.5/weather, got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized) // 401
	}))
	defer srv.Close()

	origURL := weatherValidationURL
	weatherValidationURL = srv.URL + "/data/2.5/weather"
	defer func() { weatherValidationURL = origURL }()

	err := validateWeatherCredentials(context.Background(), "badkey")
	if err == nil || err.Error() != "invalid API key" {
		t.Errorf("expected 'invalid API key', got %v", err)
	}
}

func TestValidateWeatherCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origURL := weatherValidationURL
	weatherValidationURL = srv.URL + "/data/2.5/weather"
	defer func() { weatherValidationURL = origURL }()

	err := validateWeatherCredentials(context.Background(), "goodkey")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/server/... -run TestValidateWeather -v 2>&1 | head -20
```
Expected: FAIL — `validateWeatherCredentials` and `weatherValidationURL` not defined yet.

- [ ] **Step 3: Add the validator to `credential_validators.go`**

In `buildCredentialValidatorRegistry`, add in the `── Communication ─` block (after the `discord` registration, before the `// ── Observability` comment):

```go
	r.Register("weather", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateWeatherCredentials(ctx, f["api_key"])
	}))
```

Then add a package-level variable and the standalone function. Place the variable immediately before the `buildCredentialValidatorRegistry` function:

```go
// weatherValidationURL is the OpenWeatherMap endpoint used to validate API keys.
// Overridable in tests.
var weatherValidationURL = "https://api.openweathermap.org/data/2.5/weather"
```

Add the function at the end of the file:

```go
func validateWeatherCredentials(ctx context.Context, apiKey string) error {
	if apiKey == "" {
		return errors.New("api_key is required")
	}
	reqURL := fmt.Sprintf("%s?q=London&appid=%s", weatherValidationURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("weather: validation request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
		return errors.New("invalid API key")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather: validation returned %d", resp.StatusCode)
	}
	return nil
}
```

Add `"errors"` to the imports in `credential_validators.go` if not already present.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/... -run TestValidateWeather -v
```
Expected: all 3 `TestValidateWeather*` tests PASS.

- [ ] **Step 5: Build and full test run**

```bash
go build ./... && go test ./internal/server/... 2>&1 | tail -10
```
Expected: no build errors, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/server/credential_validators.go internal/server/credential_validators_test.go
git commit -m "feat(connections): add OpenWeatherMap credential validator"
```

---

### Task 4: Tools File

**Files:**
- Create: `internal/connections/tools/weather.go`

- [ ] **Step 1: Create the file**

Create `internal/connections/tools/weather.go` with the following content:

```go
package conntools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// weatherDo performs an unauthenticated GET to the OpenWeatherMap API.
// The API key is embedded in the URL query string as required by the API.
func weatherDo(ctx context.Context, apiURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
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
	return string(data), nil
}

// weatherCreds extracts the api_key and units from a connection.
func weatherCreds(mgr *connections.Manager, conn connections.Connection) (apiKey, units string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	units = conn.Metadata["units"]
	if units == "" {
		units = "metric"
	}
	return creds["api_key"], units, nil
}

// --- weather_current ---

type weatherCurrentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *weatherCurrentTool) Name() string        { return "weather_current" }
func (t *weatherCurrentTool) Description() string { return "Get current weather conditions for any city." }
func (t *weatherCurrentTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *weatherCurrentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "weather_current",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"city"},
				Properties: map[string]backend.ToolProperty{
					"city": {Type: "string", Description: "City name (e.g. 'London', 'New York', 'Tokyo')"},
				},
			},
		},
	}
}
func (t *weatherCurrentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, units, err := weatherCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "weather_current: auth: " + err.Error()}
	}
	city, ok := args["city"].(string)
	if !ok || city == "" {
		return tools.ToolResult{IsError: true, Error: "weather_current: city is required"}
	}
	apiURL := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=%s",
		url.QueryEscape(city), apiKey, units,
	)
	out, err := weatherDo(ctx, apiURL)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- weather_forecast ---

type weatherForecastTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *weatherForecastTool) Name() string        { return "weather_forecast" }
func (t *weatherForecastTool) Description() string { return "Get a multi-day weather forecast for any city (up to 5 days)." }
func (t *weatherForecastTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *weatherForecastTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "weather_forecast",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"city"},
				Properties: map[string]backend.ToolProperty{
					"city": {Type: "string", Description: "City name (e.g. 'London', 'New York', 'Tokyo')"},
					"days": {Type: "integer", Description: "Number of days to forecast (default 3, max 5)"},
				},
			},
		},
	}
}
func (t *weatherForecastTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, units, err := weatherCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "weather_forecast: auth: " + err.Error()}
	}
	city, ok := args["city"].(string)
	if !ok || city == "" {
		return tools.ToolResult{IsError: true, Error: "weather_forecast: city is required"}
	}
	days := int(floatArg(args, "days"))
	if days <= 0 {
		days = 3
	}
	if days > 5 {
		days = 5
	}
	// The /forecast endpoint returns data in 3-hour steps; cnt = days * 8 gives one full day per step count.
	cnt := days * 8
	apiURL := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/forecast?q=%s&cnt=%d&appid=%s&units=%s",
		url.QueryEscape(city), cnt, apiKey, units,
	)
	out, err := weatherDo(ctx, apiURL)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerWeatherTools registers weather_current and weather_forecast tools.
func registerWeatherTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	reg.Unregister("weather_current")
	reg.Unregister("weather_forecast")
	strictInject(reg, &weatherCurrentTool{mgr: mgr, conns: conns})
	strictInject(reg, &weatherForecastTool{mgr: mgr, conns: conns})
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
git add internal/connections/tools/weather.go
git commit -m "feat(connections/tools): add OpenWeatherMap weather_current and weather_forecast tools"
```

---

### Task 5: Register in register.go

**Files:**
- Modify: `internal/connections/tools/register.go`

- [ ] **Step 1: Add to `providerToolNames`**

In `internal/connections/tools/register.go`, inside the `providerToolNames` map (after the `connections.ProviderMonday` entry), add:

```go
	connections.ProviderWeather: {"weather_current", "weather_forecast"},
```

- [ ] **Step 2: Add case to `RegisterForProvider`**

In the `switch provider` block inside `RegisterForProvider`, after the `case connections.ProviderMonday:` case (and before the closing `}`), add:

```go
	case connections.ProviderWeather:
		err = registerWeatherTools(reg, mgr, conns)
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
git commit -m "feat(connections/tools): register OpenWeatherMap provider in tool registry"
```

---

### Final Verification

- [ ] **Full build and test**

```bash
go build ./... && go test ./... 2>&1 | grep -E "^(ok|FAIL|---)" | tail -30
```
Expected: no FAIL lines.
