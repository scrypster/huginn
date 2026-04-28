package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/connections"
	"golang.org/x/oauth2"
)

func newToolTestManager(t *testing.T) (*connections.Manager, *connections.Store, *connections.MemoryStore) {
	t.Helper()
	store, err := connections.NewStore(t.TempDir() + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	return mgr, store, secrets
}

func newAPIKeyConnection(t *testing.T, provider connections.Provider, metadata, creds map[string]string) (*connections.Manager, connections.Connection) {
	t.Helper()
	mgr, _, _ := newToolTestManager(t)
	conn, err := mgr.StoreAPIKeyConnection(provider, "test-account", metadata, creds)
	if err != nil {
		t.Fatalf("StoreAPIKeyConnection: %v", err)
	}
	return mgr, conn
}

func newGoogleOAuthConnection(t *testing.T) (*connections.Manager, connections.Connection) {
	t.Helper()
	mgr, store, secrets := newToolTestManager(t)
	conn := connections.Connection{
		ID:           "google-test-1",
		Provider:     connections.ProviderGoogle,
		Type:         connections.ConnectionTypeOAuth,
		AccountLabel: "user@gmail.com",
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}
	if err := store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	if err := secrets.StoreToken(conn.ID, &oauth2.Token{
		AccessToken: "token-123",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("secrets.StoreToken: %v", err)
	}
	return mgr, conn
}

func TestTodoistTools_ExecutePaths(t *testing.T) {
	mgr, conn := newAPIKeyConnection(t, connections.ProviderTodoist, nil, map[string]string{"api_key": "todo-token"})
	restore := todoistDoFn
	t.Cleanup(func() { todoistDoFn = restore })

	t.Run("tasks_list encodes query parameters", func(t *testing.T) {
		var gotMethod, gotURL, gotToken string
		todoistDoFn = func(_ context.Context, method, apiURL, token string, _ io.Reader) (string, error) {
			gotMethod, gotURL, gotToken = method, apiURL, token
			return `[]`, nil
		}
		tool := &todoistTasksListTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{
			"project_id": "proj 1/alpha",
			"filter":     "today & overdue",
		})
		if res.IsError {
			t.Fatalf("unexpected error: %v", res.Error)
		}
		if gotMethod != http.MethodGet || gotToken != "todo-token" {
			t.Fatalf("unexpected method/token: %s %s", gotMethod, gotToken)
		}
		u, err := url.Parse(gotURL)
		if err != nil {
			t.Fatalf("url.Parse: %v", err)
		}
		if u.Query().Get("project_id") != "proj 1/alpha" || u.Query().Get("filter") != "today & overdue" {
			t.Fatalf("unexpected encoded query: %q", gotURL)
		}
	})

	t.Run("task_create validates required content", func(t *testing.T) {
		todoistDoFn = func(_ context.Context, _, _, _ string, _ io.Reader) (string, error) {
			return `{"id":"x"}`, nil
		}
		tool := &todoistTaskCreateTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{})
		if !res.IsError || !strings.Contains(res.Error, "content is required") {
			t.Fatalf("expected content validation error, got: %+v", res)
		}
	})

	t.Run("task_create sends expected payload", func(t *testing.T) {
		var gotBody map[string]any
		todoistDoFn = func(_ context.Context, method, apiURL, token string, body io.Reader) (string, error) {
			if method != http.MethodPost || apiURL != "https://api.todoist.com/rest/v2/tasks" || token != "todo-token" {
				t.Fatalf("unexpected request metadata: method=%s url=%s token=%s", method, apiURL, token)
			}
			b, _ := io.ReadAll(body)
			if err := json.Unmarshal(b, &gotBody); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			return `{"id":"task-1"}`, nil
		}
		tool := &todoistTaskCreateTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{
			"content":    "Ship confidence patch",
			"project_id": "abc123",
			"due_string": "tomorrow",
		})
		if res.IsError {
			t.Fatalf("unexpected error: %v", res.Error)
		}
		if gotBody["content"] != "Ship confidence patch" || gotBody["project_id"] != "abc123" || gotBody["due_string"] != "tomorrow" {
			t.Fatalf("unexpected payload: %#v", gotBody)
		}
	})

	t.Run("task_complete escapes task id in URL", func(t *testing.T) {
		var gotURL string
		todoistDoFn = func(_ context.Context, _, apiURL, _ string, _ io.Reader) (string, error) {
			gotURL = apiURL
			return `{"ok":true}`, nil
		}
		tool := &todoistTaskCompleteTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{"task_id": "abc/123"})
		if res.IsError {
			t.Fatalf("unexpected error: %v", res.Error)
		}
		if !strings.Contains(gotURL, "abc%2F123/close") {
			t.Fatalf("expected escaped task id, got URL: %s", gotURL)
		}
	})

	t.Run("tasks_list_projects calls projects endpoint", func(t *testing.T) {
		var gotMethod, gotURL string
		todoistDoFn = func(_ context.Context, method, apiURL, token string, _ io.Reader) (string, error) {
			gotMethod, gotURL = method, apiURL
			if token != "todo-token" {
				t.Fatalf("unexpected token: %s", token)
			}
			return `[]`, nil
		}
		tool := &todoistListProjectsTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{})
		if res.IsError {
			t.Fatalf("unexpected error: %v", res.Error)
		}
		if gotMethod != http.MethodGet || gotURL != "https://api.todoist.com/rest/v2/projects" {
			t.Fatalf("unexpected request: %s %s", gotMethod, gotURL)
		}
	})
}

func TestWeatherTools_ExecutePaths(t *testing.T) {
	mgr, conn := newAPIKeyConnection(t, connections.ProviderWeather, map[string]string{"units": "imperial"}, map[string]string{"api_key": "weather-token"})
	restore := weatherDoFn
	t.Cleanup(func() { weatherDoFn = restore })

	t.Run("weather_current validates required city", func(t *testing.T) {
		tool := &weatherCurrentTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{})
		if !res.IsError || !strings.Contains(res.Error, "city is required") {
			t.Fatalf("expected city validation error, got: %+v", res)
		}
	})

	t.Run("weather_current builds expected URL", func(t *testing.T) {
		var gotURL string
		weatherDoFn = func(_ context.Context, apiURL string) (string, error) {
			gotURL = apiURL
			return `{"weather":"ok"}`, nil
		}
		tool := &weatherCurrentTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{"city": "New York/US"})
		if res.IsError {
			t.Fatalf("unexpected error: %v", res.Error)
		}
		u, err := url.Parse(gotURL)
		if err != nil {
			t.Fatalf("url.Parse: %v", err)
		}
		if u.Query().Get("q") != "New York/US" || u.Query().Get("units") != "imperial" || u.Query().Get("appid") != "weather-token" {
			t.Fatalf("unexpected weather_current URL: %s", gotURL)
		}
	})

	t.Run("weather_forecast defaults and clamps day count", func(t *testing.T) {
		var urls []string
		weatherDoFn = func(_ context.Context, apiURL string) (string, error) {
			urls = append(urls, apiURL)
			return `{"forecast":"ok"}`, nil
		}
		tool := &weatherForecastTool{mgr: mgr, conns: []connections.Connection{conn}}
		if res := tool.Execute(context.Background(), map[string]any{"city": "Boston"}); res.IsError {
			t.Fatalf("unexpected default-days error: %v", res.Error)
		}
		if res := tool.Execute(context.Background(), map[string]any{"city": "Boston", "days": float64(99)}); res.IsError {
			t.Fatalf("unexpected clamped-days error: %v", res.Error)
		}
		if len(urls) != 2 {
			t.Fatalf("expected 2 weather calls, got %d", len(urls))
		}
		u1, _ := url.Parse(urls[0])
		u2, _ := url.Parse(urls[1])
		if u1.Query().Get("cnt") != "24" {
			t.Fatalf("expected default cnt=24, got URL: %s", urls[0])
		}
		if u2.Query().Get("cnt") != "40" {
			t.Fatalf("expected clamped cnt=40, got URL: %s", urls[1])
		}
	})
}

func TestHomeAssistantTools_ExecutePaths(t *testing.T) {
	mgr, conn := newAPIKeyConnection(
		t,
		connections.ProviderHomeAssistant,
		map[string]string{"base_url": "http://ha.local:8123/"},
		map[string]string{"token": "ha-token"},
	)
	restore := haDoFn
	t.Cleanup(func() { haDoFn = restore })

	t.Run("ha_states supports optional entity_id and escapes path", func(t *testing.T) {
		var urls []string
		haDoFn = func(_ context.Context, _, apiURL, token string, _ io.Reader) (string, error) {
			if token != "ha-token" {
				t.Fatalf("unexpected token: %s", token)
			}
			urls = append(urls, apiURL)
			return `{"ok":true}`, nil
		}
		tool := &haStatesTool{mgr: mgr, conns: []connections.Connection{conn}}
		if res := tool.Execute(context.Background(), map[string]any{}); res.IsError {
			t.Fatalf("unexpected no-entity error: %v", res.Error)
		}
		if res := tool.Execute(context.Background(), map[string]any{"entity_id": "light/kitchen.main"}); res.IsError {
			t.Fatalf("unexpected entity error: %v", res.Error)
		}
		if len(urls) != 2 {
			t.Fatalf("expected 2 ha_states calls, got %d", len(urls))
		}
		if urls[0] != "http://ha.local:8123/api/states" {
			t.Fatalf("unexpected states URL: %s", urls[0])
		}
		if !strings.Contains(urls[1], "light%2Fkitchen.main") {
			t.Fatalf("expected escaped entity_id in URL, got %s", urls[1])
		}
	})

	t.Run("ha_call_service validates args and sends merged payload", func(t *testing.T) {
		tool := &haCallServiceTool{mgr: mgr, conns: []connections.Connection{conn}}
		if res := tool.Execute(context.Background(), map[string]any{"service": "turn_on"}); !res.IsError || !strings.Contains(res.Error, "domain is required") {
			t.Fatalf("expected domain validation error, got %+v", res)
		}
		if res := tool.Execute(context.Background(), map[string]any{"domain": "light"}); !res.IsError || !strings.Contains(res.Error, "service is required") {
			t.Fatalf("expected service validation error, got %+v", res)
		}

		var gotURL string
		var gotPayload map[string]any
		haDoFn = func(_ context.Context, method, apiURL, _ string, body io.Reader) (string, error) {
			if method != http.MethodPost {
				t.Fatalf("expected POST, got %s", method)
			}
			gotURL = apiURL
			b, _ := io.ReadAll(body)
			if err := json.Unmarshal(b, &gotPayload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			return `{"ok":true}`, nil
		}
		res := tool.Execute(context.Background(), map[string]any{
			"domain":    "light",
			"service":   "turn_on",
			"entity_id": "light.kitchen",
			"service_data": map[string]any{
				"brightness": float64(180),
			},
		})
		if res.IsError {
			t.Fatalf("unexpected execute error: %v", res.Error)
		}
		if gotURL != "http://ha.local:8123/api/services/light/turn_on" {
			t.Fatalf("unexpected service URL: %s", gotURL)
		}
		if gotPayload["entity_id"] != "light.kitchen" || gotPayload["brightness"] != float64(180) {
			t.Fatalf("unexpected service payload: %#v", gotPayload)
		}
	})

	t.Run("ha_scene_activate validates and calls scene endpoint", func(t *testing.T) {
		tool := &haSceneActivateTool{mgr: mgr, conns: []connections.Connection{conn}}
		if res := tool.Execute(context.Background(), map[string]any{}); !res.IsError || !strings.Contains(res.Error, "scene_id is required") {
			t.Fatalf("expected scene_id validation error, got %+v", res)
		}
		var gotURL string
		var gotPayload map[string]any
		haDoFn = func(_ context.Context, method, apiURL, _ string, body io.Reader) (string, error) {
			if method != http.MethodPost {
				t.Fatalf("expected POST, got %s", method)
			}
			gotURL = apiURL
			b, _ := io.ReadAll(body)
			if err := json.Unmarshal(b, &gotPayload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			return `{"ok":true}`, nil
		}
		res := tool.Execute(context.Background(), map[string]any{"scene_id": "scene.movie_night"})
		if res.IsError {
			t.Fatalf("unexpected execute error: %v", res.Error)
		}
		if gotURL != "http://ha.local:8123/api/services/scene/turn_on" || gotPayload["entity_id"] != "scene.movie_night" {
			t.Fatalf("unexpected scene request: url=%s payload=%#v", gotURL, gotPayload)
		}
	})
}

func TestCalendarTools_ExecutePaths(t *testing.T) {
	mgr, conn := newGoogleOAuthConnection(t)
	restore := calendarDoFn
	t.Cleanup(func() { calendarDoFn = restore })

	t.Run("calendar_today calls events endpoint and escapes calendar id", func(t *testing.T) {
		var gotMethod, gotURL string
		calendarDoFn = func(_ context.Context, _ *http.Client, method, apiURL string, _ io.Reader) (string, error) {
			gotMethod, gotURL = method, apiURL
			return `{"items":[]}`, nil
		}
		tool := &calendarTodayTool{mgr: mgr, conns: []connections.Connection{conn}}
		res := tool.Execute(context.Background(), map[string]any{"calendar_id": "team/calendar"})
		if res.IsError {
			t.Fatalf("unexpected error: %v", res.Error)
		}
		if gotMethod != http.MethodGet {
			t.Fatalf("expected GET, got %s", gotMethod)
		}
		if !strings.Contains(gotURL, "/calendars/team%2Fcalendar/events") || !strings.Contains(gotURL, "singleEvents=true") {
			t.Fatalf("unexpected calendar_today URL: %s", gotURL)
		}
	})

	t.Run("calendar_create validates required fields and payload", func(t *testing.T) {
		tool := &calendarCreateTool{mgr: mgr, conns: []connections.Connection{conn}}
		if res := tool.Execute(context.Background(), map[string]any{"title": "x"}); !res.IsError || !strings.Contains(res.Error, "title, start, and end are required") {
			t.Fatalf("expected required-fields error, got %+v", res)
		}

		var gotURL string
		var gotPayload map[string]any
		calendarDoFn = func(_ context.Context, _ *http.Client, method, apiURL string, body io.Reader) (string, error) {
			if method != http.MethodPost {
				t.Fatalf("expected POST, got %s", method)
			}
			gotURL = apiURL
			b, _ := io.ReadAll(body)
			if err := json.Unmarshal(b, &gotPayload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			return `{"id":"evt-1"}`, nil
		}
		res := tool.Execute(context.Background(), map[string]any{
			"title":       "Sprint Planning",
			"start":       "2030-01-02",
			"end":         "2030-01-02",
			"description": "Weekly planning session",
			"calendar_id": "team/calendar",
		})
		if res.IsError {
			t.Fatalf("unexpected calendar_create error: %v", res.Error)
		}
		if !strings.Contains(gotURL, "/calendars/team%2Fcalendar/events") {
			t.Fatalf("expected escaped calendar id, got %s", gotURL)
		}
		if gotPayload["summary"] != "Sprint Planning" || gotPayload["description"] != "Weekly planning session" {
			t.Fatalf("unexpected event payload: %#v", gotPayload)
		}
		startField, ok := gotPayload["start"].(map[string]any)
		if !ok || startField["date"] != "2030-01-02" {
			t.Fatalf("expected date-only start payload, got %#v", gotPayload["start"])
		}
	})

	t.Run("calendar_find_free validates date and computes slots", func(t *testing.T) {
		tool := &calendarFindFreeTool{mgr: mgr, conns: []connections.Connection{conn}}
		if res := tool.Execute(context.Background(), map[string]any{"date": "invalid"}); !res.IsError || !strings.Contains(res.Error, "YYYY-MM-DD") {
			t.Fatalf("expected date format error, got %+v", res)
		}

		var gotURL string
		var gotReq map[string]any
		calendarDoFn = func(_ context.Context, _ *http.Client, method, apiURL string, body io.Reader) (string, error) {
			if method != http.MethodPost {
				t.Fatalf("expected POST, got %s", method)
			}
			gotURL = apiURL
			b, _ := io.ReadAll(body)
			if err := json.Unmarshal(b, &gotReq); err != nil {
				t.Fatalf("json.Unmarshal request: %v", err)
			}

			loc := time.Now().Location()
			dayStart, _ := time.ParseInLocation("2006-01-02", "2030-01-03", loc)
			busyStart := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 10, 0, 0, 0, loc)
			busyEnd := busyStart.Add(time.Hour)
			resp := fmt.Sprintf(`{"calendars":{"primary":{"busy":[{"start":"%s","end":"%s"}]}}}`, busyStart.Format(time.RFC3339), busyEnd.Format(time.RFC3339))
			return resp, nil
		}

		res := tool.Execute(context.Background(), map[string]any{
			"date":             "2030-01-03",
			"duration_minutes": float64(60),
			"attendees":        []any{"a@example.com", "b@example.com"},
		})
		if res.IsError {
			t.Fatalf("unexpected calendar_find_free error: %v", res.Error)
		}
		if gotURL != "https://www.googleapis.com/calendar/v3/freeBusy" {
			t.Fatalf("unexpected freeBusy URL: %s", gotURL)
		}
		items, ok := gotReq["items"].([]any)
		if !ok || len(items) != 3 {
			t.Fatalf("expected primary + 2 attendees in freebusy request, got %#v", gotReq["items"])
		}

		var parsed struct {
			Date            string   `json:"date"`
			DurationMinutes int      `json:"duration_minutes"`
			FreeSlots       []string `json:"free_slots"`
		}
		if err := json.Unmarshal([]byte(res.Output), &parsed); err != nil {
			t.Fatalf("json.Unmarshal response: %v", err)
		}
		if parsed.Date != "2030-01-03" || parsed.DurationMinutes != 60 {
			t.Fatalf("unexpected response metadata: %+v", parsed)
		}
		if len(parsed.FreeSlots) != 7 {
			t.Fatalf("expected 7 free 60-min slots with one busy hour, got %d (%v)", len(parsed.FreeSlots), parsed.FreeSlots)
		}
	})
}
