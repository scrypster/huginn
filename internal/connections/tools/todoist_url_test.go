package conntools

import (
	"net/url"
	"testing"
)

func TestBuildTodoistTasksListURL_NoFilters(t *testing.T) {
	got := buildTodoistTasksListURL(map[string]any{})
	want := "https://api.todoist.com/rest/v2/tasks"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildTodoistTasksListURL_EncodesQueryValues(t *testing.T) {
	got := buildTodoistTasksListURL(map[string]any{
		"project_id": "proj 1/alpha",
		"filter":     "today & overdue",
	})
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Path != "/rest/v2/tasks" {
		t.Fatalf("expected path /rest/v2/tasks, got %q", u.Path)
	}
	q := u.Query()
	if q.Get("project_id") != "proj 1/alpha" {
		t.Fatalf("expected project_id to round-trip, got %q", q.Get("project_id"))
	}
	if q.Get("filter") != "today & overdue" {
		t.Fatalf("expected filter to round-trip, got %q", q.Get("filter"))
	}
}

func TestBuildTodoistTaskCompleteURL_EscapesTaskIDPathSegment(t *testing.T) {
	got := buildTodoistTaskCompleteURL("abc/123")
	want := "https://api.todoist.com/rest/v2/tasks/abc%2F123/close"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
