# Google Calendar Tools — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing Google OAuth connection provider with three Google Calendar tools: `calendar_today` (list today's events), `calendar_create` (create a new event), and `calendar_find_free` (find free time slots using the Freebusy API).

**Architecture:** No new provider constant and no new catalog entry — this extends the existing `ProviderGoogle` connection which already includes the `calendar` scope in `gmailProvider`. A new `internal/connections/tools/calendar.go` file is created in the same `conntools` package, reusing the `gmailProvider` OAuth client (via `mgr.GetHTTPClient`) exactly as `gmail.go` does. `register.go` is updated to add the 3 calendar tool names to the existing `ProviderGoogle` entry in `providerToolNames` and to call `registerCalendarTools` from the `ProviderGoogle` switch case.

**Tech Stack:** Go standard library (`net/http`, `io`, `fmt`, `encoding/json`, `strings`, `time`). No new dependencies.

---

## File Changes

| File | Change |
|------|--------|
| `internal/connections/tools/calendar.go` | New file: `calendarDo`, tool structs, `registerCalendarTools` |
| `internal/connections/tools/register.go` | Expand `ProviderGoogle` entry in `providerToolNames`; call `registerCalendarTools` in the Google case |

---

### Task 1: Calendar Tools File

**Files:**
- Create: `internal/connections/tools/calendar.go`

- [ ] **Step 1: Understand the auth pattern**

The calendar tools use the same OAuth HTTP client as Gmail. In `gmail.go`, `gmailProvider` is defined as:

```go
var gmailProvider = connproviders.NewGoogle("", "", []string{"gmail", "calendar", "drive", "docs", "sheets", "contacts"})
```

The `calendar` scope is already included. Calendar tools call `mgr.GetHTTPClient(ctx, conn.ID, gmailProvider)` to obtain an auto-refreshing `*http.Client`, then pass that client to API helper functions. There is no separate `calendarProvider` variable needed — reuse `gmailProvider` directly.

- [ ] **Step 2: Create the file**

Create `internal/connections/tools/calendar.go` with the following content:

```go
package conntools

import (
	"bytes"
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

func (t *calendarTodayTool) Name() string        { return "calendar_today" }
func (t *calendarTodayTool) Description() string { return "List Google Calendar events for today." }
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
	// Build today's start and end in RFC3339 format using local midnight boundaries.
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayEnd := todayStart.Add(24 * time.Hour)
	apiURL := fmt.Sprintf(
		"https://www.googleapis.com/calendar/v3/calendars/%s/events?timeMin=%s&timeMax=%s&singleEvents=true&orderBy=startTime",
		calendarID,
		todayStart.Format(time.RFC3339),
		todayEnd.Format(time.RFC3339),
	)
	out, err := calendarDo(ctx, client, http.MethodGet, apiURL, nil)
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

func (t *calendarCreateTool) Name() string        { return "calendar_create" }
func (t *calendarCreateTool) Description() string { return "Create a Google Calendar event. Requires user approval." }
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
					"start":       {Type: "string", Description: "Start datetime in RFC3339 format (e.g. '2026-05-01T10:00:00-05:00') or date-only (e.g. '2026-05-01')"},
					"end":         {Type: "string", Description: "End datetime in RFC3339 format or date-only"},
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
	// Determine if the value is a date-only string (10 chars: YYYY-MM-DD) or datetime.
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
	apiURL := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events", calendarID)
	out, err := calendarDo(ctx, client, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
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

func (t *calendarFindFreeTool) Name() string        { return "calendar_find_free" }
func (t *calendarFindFreeTool) Description() string { return "Find free time slots on a given date using the Google Calendar Freebusy API." }
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
					"attendees":        {Type: "array", Description: "List of attendee email addresses to also check for availability (optional)", Items: &backend.ToolProperty{Type: "string"}},
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

	// Parse the date; use the local timezone of the running process.
	loc := time.Now().Location()
	dayStart, err := time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "calendar_find_free: date must be YYYY-MM-DD: " + err.Error()}
	}
	// Business hours: 09:00–17:00.
	windowStart := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 9, 0, 0, 0, loc)
	windowEnd := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 17, 0, 0, 0, loc)

	// Build Freebusy request items. Always include the primary calendar.
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
	out, err := calendarDo(ctx, client, http.MethodPost,
		"https://www.googleapis.com/calendar/v3/freeBusy",
		bytes.NewReader(reqBytes))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}

	// Parse the freebusy response and compute free slots.
	freeSlots, err := computeFreeSlots(out, windowStart, windowEnd, time.Duration(durationMins)*time.Minute)
	if err != nil {
		// Return raw freebusy JSON on parse failure — still useful.
		return tools.ToolResult{Output: out}
	}
	result, _ := json.MarshalIndent(map[string]any{
		"date":             dateStr,
		"duration_minutes": durationMins,
		"free_slots":       freeSlots,
	}, "", "  ")
	return tools.ToolResult{Output: string(result)}
}

// computeFreeSlots parses a Google Freebusy JSON response and returns a list
// of free slot strings (e.g. "09:00–10:00") within the given window of the
// desired duration. Busy intervals from all calendars/attendees are merged.
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

	// Collect all busy intervals across all calendars.
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

	// Walk the window in `duration` increments; include slots with no overlap.
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

// calendarAccountsDesc returns a comma-separated list of account labels for
// use in tool descriptions. Mirrors the pattern in registerGmailTools.
func calendarAccountsDesc(conns []connections.Connection) string {
	labels := make([]string, 0, len(conns))
	for _, c := range conns {
		labels = append(labels, c.AccountLabel)
	}
	return strings.Join(labels, ", ")
}
```

- [ ] **Step 3: Build check**

```bash
go build ./...
```

If you see a compile error about `backend.ToolProperty` missing an `Items` field, check the `backend.ToolProperty` struct definition:

```bash
grep -n "Items" internal/backend/types.go 2>/dev/null || grep -rn "ToolProperty" internal/backend/ | head -20
```

If `Items` is not a field on `ToolProperty`, change the `attendees` property definition to omit the `Items` field:

```go
"attendees": {Type: "array", Description: "List of attendee email addresses to also check for availability (optional)"},
```

Re-run `go build ./...` until it passes.

- [ ] **Step 4: Commit**

```bash
git add internal/connections/tools/calendar.go
git commit -m "feat(connections/tools): add Google Calendar calendar_today, calendar_create, calendar_find_free tools"
```

---

### Task 2: Update register.go

**Files:**
- Modify: `internal/connections/tools/register.go`

- [ ] **Step 1: Expand the Google entry in `providerToolNames`**

In `internal/connections/tools/register.go`, find the existing `connections.ProviderGoogle` entry:

```go
	connections.ProviderGoogle: {"gmail_search", "gmail_read", "gmail_send"},
```

Replace it with:

```go
	connections.ProviderGoogle: {"gmail_search", "gmail_read", "gmail_send", "calendar_today", "calendar_create", "calendar_find_free"},
```

- [ ] **Step 2: Call `registerCalendarTools` in the Google case**

In the `switch provider` block inside `RegisterForProvider`, find the existing Google case:

```go
	case connections.ProviderGoogle:
		err = registerGmailTools(reg, mgr, conns)
```

Replace it with:

```go
	case connections.ProviderGoogle:
		err = registerGmailTools(reg, mgr, conns)
		if err == nil {
			err = registerCalendarTools(reg, mgr, conns)
		}
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
git commit -m "feat(connections/tools): add Google Calendar tools to Google provider registration"
```

---

### Final Verification

- [ ] **Full build and test**

```bash
go build ./... && go test ./... 2>&1 | grep -E "^(ok|FAIL|---)" | tail -30
```
Expected: no FAIL lines.

---

### Notes for Implementor

- The `calendar` OAuth scope is already requested in `gmailProvider` (defined in `gmail.go`) — no scope changes are needed.
- No catalog changes are needed. The Google OAuth flow already handles credential storage.
- No new credential validator is needed. The existing Google OAuth validator covers calendar access.
- `calendarAccountsDesc` is defined in `calendar.go` but not used by the current tools (they each accept an explicit `account` param). It is provided for future use if per-tool descriptions should enumerate accounts. The Go compiler will flag it as unused; remove it if it causes a build error, or use it in one of the tool `Description()` methods.
- The `computeFreeSlots` function walks the business-hours window in exact `duration`-minute increments (non-overlapping slots). This is a simple implementation; it does not attempt to find the earliest possible slot boundary after a busy block.
