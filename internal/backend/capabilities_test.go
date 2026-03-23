package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mockShowServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/show" {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

func TestDetectVision_LlavaFamily_ReturnsTrue(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"details": map[string]any{"family": "llava", "families": []string{"llava", "clip"}}})
	srv := mockShowServer(t, string(body))
	defer srv.Close()
	got, err := DetectVision(srv.URL, "llava:13b")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !got {
		t.Error("expected vision=true for llava")
	}
}

func TestDetectVision_NoVision_ReturnsFalse(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"details": map[string]any{"family": "qwen2"}})
	srv := mockShowServer(t, string(body))
	defer srv.Close()
	got, _ := DetectVision(srv.URL, "qwen2:14b")
	if got {
		t.Error("expected vision=false")
	}
}

func TestDetectVision_ServerError_GracefulFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()
	got, err := DetectVision(srv.URL, "anymodel")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got {
		t.Error("expected false on 404")
	}
}

func TestDetectVision_ConnectionRefused_GracefulFalse(t *testing.T) {
	got, err := DetectVision("http://127.0.0.1:1", "model")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got {
		t.Error("expected false on connection refused")
	}
}
