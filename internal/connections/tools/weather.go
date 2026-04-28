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

var weatherDoFn = weatherDo

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

func (t *weatherCurrentTool) Name() string { return "weather_current" }
func (t *weatherCurrentTool) Description() string {
	return "Get current weather conditions for any city."
}
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
	out, err := weatherDoFn(ctx, apiURL)
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

func (t *weatherForecastTool) Name() string { return "weather_forecast" }
func (t *weatherForecastTool) Description() string {
	return "Get a multi-day weather forecast for any city (up to 5 days)."
}
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
	cnt := days * 8
	apiURL := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/forecast?q=%s&cnt=%d&appid=%s&units=%s",
		url.QueryEscape(city), cnt, apiKey, units,
	)
	out, err := weatherDoFn(ctx, apiURL)
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
