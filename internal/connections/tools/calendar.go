package conntools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

var calendarDoFn = calendarDo

// calendarDo performs an authenticated Google Calendar API request using an
// OAuth HTTP client obtained from mgr.GetHTTPClient (same pattern as gmail.go).
func calendarDo(ctx context.Context, client *http.Client, method, apiURL string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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

// --- calendar_today ---

type calendarTodayTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *calendarTodayTool) Name() string                      { return "calendar_today" }
func (t *calendarTodayTool) Description() string               { return "List Google Calendar events for today." }
func (t *calendarTodayTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *calendarTodayTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "calendar_today",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"calendar_id": {Type: "string", Description: "Calendar ID (default 'primary')"},
					"account":     {Type: "string", Description: "Account email (optional, defaults to first connected account)"},
				},
			},
		},
	}
}
func (t *calendarTodayTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, gmailProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("calendar_today: auth: %v", err)}
	}
	calendarID, _ := args["calendar_id"].(string)
	if calendarID == "" {
		calendarID = "primary"
	}
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayEnd := todayStart.Add(24 * time.Hour)
	apiURL := fmt.Sprintf(
		"https://www.googleapis.com/calendar/v3/calendars/%s/events?timeMin=%s&timeMax=%s&singleEvents=true&orderBy=startTime",
		url.PathEscape(calendarID),
		todayStart.Format(time.RFC3339),
		todayEnd.Format(time.RFC3339),
	)
	out, err := calendarDoFn(ctx, client, http.MethodGet, apiURL, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- calendar_create ---

type calendarCreateTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *calendarCreateTool) Name() string { return "calendar_create" }
func (t *calendarCreateTool) Description() string {
	return "Create a Google Calendar event. Requires user approval."
}
func (t *calendarCreateTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *calendarCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "calendar_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"title", "start", "end"},
				Properties: map[string]backend.ToolProperty{
					"title":       {Type: "string", Description: "Event title / summary"},
					"start":       {Type: "string", Description: "Start datetime in RFC3339 format or date-only (YYYY-MM-DD)"},
					"end":         {Type: "string", Description: "End datetime in RFC3339 format or date-only (YYYY-MM-DD)"},
					"description": {Type: "string", Description: "Event description (optional)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default 'primary')"},
					"account":     {Type: "string", Description: "Account email (optional, defaults to first connected account)"},
				},
			},
		},
	}
}
func (t *calendarCreateTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, gmailProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("calendar_create: auth: %v", err)}
	}
	title, _ := args["title"].(string)
	start, _ := args["start"].(string)
	end, _ := args["end"].(string)
	if title == "" || start == "" || end == "" {
		return tools.ToolResult{IsError: true, Error: "calendar_create: title, start, and end are required"}
	}
	calendarID, _ := args["calendar_id"].(string)
	if calendarID == "" {
		calendarID = "primary"
	}
	buildTimeField := func(s string) map[string]string {
		if len(s) == 10 {
			return map[string]string{"date": s}
		}
		return map[string]string{"dateTime": s}
	}
	event := map[string]any{
		"summary": title,
		"start":   buildTimeField(start),
		"end":     buildTimeField(end),
	}
	if desc, ok := args["description"].(string); ok && desc != "" {
		event["description"] = desc
	}
	bodyBytes, _ := json.Marshal(event)
	apiURL := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events", url.PathEscape(calendarID))
	out, err := calendarDoFn(ctx, client, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- calendar_find_free ---

type calendarFindFreeTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *calendarFindFreeTool) Name() string { return "calendar_find_free" }
func (t *calendarFindFreeTool) Description() string {
	return "Find free time slots on a given date using the Google Calendar Freebusy API."
}
func (t *calendarFindFreeTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *calendarFindFreeTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "calendar_find_free",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"date"},
				Properties: map[string]backend.ToolProperty{
					"date":             {Type: "string", Description: "Date to check in YYYY-MM-DD format"},
					"duration_minutes": {Type: "integer", Description: "Desired slot duration in minutes (default 60)"},
					"attendees":        {Type: "array", Description: "List of attendee email addresses to also check for availability (optional)"},
					"account":          {Type: "string", Description: "Account email (optional, defaults to first connected account)"},
				},
			},
		},
	}
}
func (t *calendarFindFreeTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, gmailProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("calendar_find_free: auth: %v", err)}
	}
	dateStr, ok := args["date"].(string)
	if !ok || dateStr == "" {
		return tools.ToolResult{IsError: true, Error: "calendar_find_free: date is required (YYYY-MM-DD)"}
	}
	durationMins := int(floatArg(args, "duration_minutes"))
	if durationMins <= 0 {
		durationMins = 60
	}

	loc := time.Now().Location()
	dayStart, err := time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "calendar_find_free: date must be YYYY-MM-DD: " + err.Error()}
	}
	windowStart := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 9, 0, 0, 0, loc)
	windowEnd := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 17, 0, 0, 0, loc)

	items := []map[string]string{{"id": "primary"}}
	if rawAttendees, ok := args["attendees"].([]any); ok {
		for _, a := range rawAttendees {
			if email, ok := a.(string); ok && email != "" {
				items = append(items, map[string]string{"id": email})
			}
		}
	}

	freebusyReq := map[string]any{
		"timeMin": windowStart.Format(time.RFC3339),
		"timeMax": windowEnd.Format(time.RFC3339),
		"items":   items,
	}
	reqBytes, _ := json.Marshal(freebusyReq)
	out, err := calendarDoFn(ctx, client, http.MethodPost,
		"https://www.googleapis.com/calendar/v3/freeBusy",
		bytes.NewReader(reqBytes))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}

	freeSlots, parseErr := computeFreeSlots(out, windowStart, windowEnd, time.Duration(durationMins)*time.Minute)
	if parseErr != nil {
		return tools.ToolResult{Output: out}
	}
	result, _ := json.MarshalIndent(map[string]any{
		"date":             dateStr,
		"duration_minutes": durationMins,
		"free_slots":       freeSlots,
	}, "", "  ")
	return tools.ToolResult{Output: string(result)}
}

// computeFreeSlots parses a Google Freebusy JSON response and returns free slot strings.
func computeFreeSlots(freebusyJSON string, windowStart, windowEnd time.Time, duration time.Duration) ([]string, error) {
	var resp struct {
		Calendars map[string]struct {
			Busy []struct {
				Start string `json:"start"`
				End   string `json:"end"`
			} `json:"busy"`
		} `json:"calendars"`
	}
	if err := json.Unmarshal([]byte(freebusyJSON), &resp); err != nil {
		return nil, err
	}

	type interval struct{ start, end time.Time }
	var busy []interval
	for _, cal := range resp.Calendars {
		for _, b := range cal.Busy {
			s, err1 := time.Parse(time.RFC3339, b.Start)
			e, err2 := time.Parse(time.RFC3339, b.End)
			if err1 != nil || err2 != nil {
				continue
			}
			busy = append(busy, interval{s, e})
		}
	}

	var free []string
	cursor := windowStart
	for cursor.Add(duration).Before(windowEnd) || cursor.Add(duration).Equal(windowEnd) {
		slotEnd := cursor.Add(duration)
		overlaps := false
		for _, b := range busy {
			if cursor.Before(b.end) && slotEnd.After(b.start) {
				overlaps = true
				break
			}
		}
		if !overlaps {
			free = append(free, fmt.Sprintf("%s–%s",
				cursor.Format("15:04"),
				slotEnd.Format("15:04"),
			))
		}
		cursor = cursor.Add(duration)
	}
	return free, nil
}

// registerCalendarTools registers calendar_today, calendar_create, calendar_find_free.
func registerCalendarTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"calendar_today", "calendar_create", "calendar_find_free"}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &calendarTodayTool{mgr: mgr, conns: conns})
	strictInject(reg, &calendarCreateTool{mgr: mgr, conns: conns})
	strictInject(reg, &calendarFindFreeTool{mgr: mgr, conns: conns})
	return nil
}
