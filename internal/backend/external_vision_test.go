package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureServer(t *testing.T, captured *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		*captured = buf.Bytes()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\ndata: [DONE]\n")
	}))
}

func TestBuildRequest_TextOnly_ContentIsString(t *testing.T) {
	var captured []byte
	srv := captureServer(t, &captured)
	defer srv.Close()
	b := NewExternalBackend(srv.URL)
	b.ChatCompletion(context.Background(), ChatRequest{Model: "test", Messages: []Message{{Role: "user", Content: "hello"}}})
	var parsed map[string]json.RawMessage
	json.Unmarshal(captured, &parsed)
	var msgs []map[string]json.RawMessage
	json.Unmarshal(parsed["messages"], &msgs)
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	contentRaw := string(msgs[0]["content"])
	if !strings.HasPrefix(contentRaw, `"`) {
		t.Errorf("expected string content, got %s", contentRaw)
	}
}

func TestBuildRequest_WithImageParts_ContentIsArray(t *testing.T) {
	var captured []byte
	srv := captureServer(t, &captured)
	defer srv.Close()
	b := NewExternalBackend(srv.URL)
	b.ChatCompletion(context.Background(), ChatRequest{Model: "llava", Messages: []Message{{
		Role: "user",
		Parts: []ContentPart{
			{Type: "text", Text: "What is this?"},
			{Type: "image_url", ImageURL: "data:image/png;base64,abc123"},
		},
	}}})
	var parsed map[string]json.RawMessage
	json.Unmarshal(captured, &parsed)
	var msgs []map[string]json.RawMessage
	json.Unmarshal(parsed["messages"], &msgs)
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	contentRaw := string(msgs[0]["content"])
	if !strings.HasPrefix(contentRaw, "[") {
		t.Errorf("expected array content, got %s", contentRaw)
	}
	if !strings.Contains(contentRaw, "image_url") {
		t.Errorf("expected image_url, got %s", contentRaw)
	}
}
