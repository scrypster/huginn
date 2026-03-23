package relay_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// noBrowser is a no-op OpenBrowserFn for tests — prevents real browser windows from opening.
func noBrowser(_ string) error { return nil }

// newTestRegistrar creates a Registrar with a no-op browser opener for safe testing.
func newTestRegistrar(baseURL string, store relay.TokenStorer) *relay.Registrar {
	reg := relay.NewRegistrarWithStore(baseURL, store)
	reg.OpenBrowserFn = noBrowser
	return reg
}

func TestRegistrar_Status_NotRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := newTestRegistrar("http://localhost:9090", store)
	registered, id := reg.Status()
	if registered {
		t.Error("expected registered == false when token store is empty")
	}
	if id == "" {
		t.Error("expected non-empty machine ID")
	}
}

// TestRegistrar_BrowserFlow_Success tests the happy path: browser flow callback
// delivers api_key and machine_id back to the local server.
func TestRegistrar_BrowserFlow_Success(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	// We need to capture the callback URL that the registrar builds, then
	// simulate the cloud service redirecting back to it with api_key + machine_id.
	var capturedURL string
	reg := relay.NewRegistrarWithStore("http://localhost:0", store)
	reg.OpenBrowserFn = func(rawURL string) error {
		capturedURL = rawURL
		// Parse the callback URL from the connect URL, then hit it with api_key params.
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		cbURL := u.Query().Get("cb")
		if cbURL == "" {
			return fmt.Errorf("no cb param in URL")
		}
		// Simulate cloud redirecting to callback with API key
		go func() {
			time.Sleep(50 * time.Millisecond)
			resp, err := http.Get(cbURL + "?api_key=test-key-123&machine_id=test-machine-42")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := reg.Register(ctx, "test-machine")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Register returned nil result")
	}
	if result.APIKey != "test-key-123" {
		t.Errorf("APIKey = %q, want %q", result.APIKey, "test-key-123")
	}
	if result.MachineID != "test-machine-42" {
		t.Errorf("MachineID = %q, want %q", result.MachineID, "test-machine-42")
	}
	if capturedURL == "" {
		t.Error("expected OpenBrowserFn to be called with a URL")
	}

	// Token should be saved
	registered, _ := reg.Status()
	if !registered {
		t.Error("expected registered == true after successful Register")
	}
}

// TestRegistrar_BrowserFlow_UserDenied tests the error callback path.
func TestRegistrar_BrowserFlow_UserDenied(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	reg := relay.NewRegistrarWithStore("http://localhost:0", store)
	reg.OpenBrowserFn = func(rawURL string) error {
		u, _ := url.Parse(rawURL)
		cbURL := u.Query().Get("cb")
		go func() {
			time.Sleep(50 * time.Millisecond)
			resp, err := http.Get(cbURL + "?error=access_denied")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test-machine")
	if err == nil {
		t.Error("expected error when user denies registration")
	}
}

func TestRegistrar_Unregister(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("test-jwt")
	reg := newTestRegistrar("", store)

	registered, _ := reg.Status()
	if !registered {
		t.Fatal("expected registered == true after saving token")
	}

	if err := reg.Unregister(); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	registered, _ = reg.Status()
	if registered {
		t.Error("expected registered == false after Unregister")
	}
}

func TestRegistrar_Status_RegisteredAfterSave(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("some-token")
	reg := newTestRegistrar("http://localhost:9090", store)
	registered, id := reg.Status()
	if !registered {
		t.Error("expected registered == true")
	}
	if id == "" {
		t.Error("expected non-empty machine ID")
	}
}

// TestRegistrar_RegisterURL_ContainsMachineID verifies the connect URL is built correctly.
func TestRegistrar_RegisterURL_ContainsMachineID(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	var capturedURL string
	reg := relay.NewRegistrarWithStore("http://localhost:0", store)
	reg.OpenBrowserFn = func(rawURL string) error {
		capturedURL = rawURL
		// Parse the callback URL and simulate success
		u, _ := url.Parse(rawURL)
		cbURL := u.Query().Get("cb")
		go func() {
			time.Sleep(50 * time.Millisecond)
			resp, _ := http.Get(cbURL + "?api_key=k&machine_id=m")
			if resp != nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test-host")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if capturedURL == "" {
		t.Fatal("OpenBrowserFn was not called")
	}
	u, err := url.Parse(capturedURL)
	if err != nil {
		t.Fatalf("parse captured URL: %v", err)
	}
	if u.Query().Get("machine_id") == "" {
		t.Error("connect URL missing machine_id query param")
	}
	if u.Query().Get("name") != "test-host" {
		t.Errorf("connect URL name = %q, want %q", u.Query().Get("name"), "test-host")
	}
}

// TestRegistrar_Register_ContextCancelled_Immediate tests pre-cancelled context.
func TestRegistrar_Register_ContextCancelled_Immediate(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := newTestRegistrar("http://localhost:9999", store)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately
	cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestRegistrar_Register_DeadlineExceeded_Short tests short timeout behavior.
func TestRegistrar_Register_DeadlineExceeded_Short(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := newTestRegistrar("http://localhost:9999", store)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected error from expired context")
	}
}

// TestRegistrar_DeliverCode_NoOp verifies DeliverCode doesn't panic (it's a no-op now).
func TestRegistrar_DeliverCode_NoOp(t *testing.T) {
	reg := relay.NewRegistrarWithStore("https://example.com", &relay.MemoryTokenStore{})
	reg.DeliverCode("code1")
	reg.DeliverCode("code2") // multiple calls — still safe
}

// TestRegistrar_BrowserFlow_SaveError tests that a token store save failure is propagated.
func TestRegistrar_BrowserFlow_SaveError(t *testing.T) {
	reg := relay.NewRegistrarWithStore("http://localhost:0", &failSaveTokenStore{})
	reg.OpenBrowserFn = func(rawURL string) error {
		u, _ := url.Parse(rawURL)
		cbURL := u.Query().Get("cb")
		go func() {
			time.Sleep(50 * time.Millisecond)
			resp, _ := http.Get(cbURL + "?api_key=k&machine_id=m")
			if resp != nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected save error from Register, got nil")
	}
}

// TestRegistrar_DeviceCodeFlow_Approved tests the device code polling path.
func TestRegistrar_DeviceCodeFlow_Approved(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	pollCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/device/poll" {
			pollCount++
			w.Header().Set("Content-Type", "application/json")
			if pollCount < 2 {
				w.Write([]byte(`{"status":"pending"}`))
			} else {
				w.Write([]byte(`{"status":"approved","api_key":"device-key","machine_id":"device-machine"}`))
			}
		}
	}))
	defer srv.Close()

	reg := relay.NewRegistrarWithStore(srv.URL, store)
	// Force browser flow to fail so we hit device code flow
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("no browser") }
	reg.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := reg.Register(ctx, "test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.APIKey != "device-key" {
		t.Errorf("APIKey = %q, want %q", result.APIKey, "device-key")
	}
	if result.MachineID != "device-machine" {
		t.Errorf("MachineID = %q, want %q", result.MachineID, "device-machine")
	}

	registered, _ := reg.Status()
	if !registered {
		t.Error("expected registered after device code flow")
	}
}

// TestRegistrar_DeviceCodeFlow_BrowserFails verifies that when the browser cannot
// be opened, Register falls through to the device code flow and succeeds.
func TestRegistrar_DeviceCodeFlow_BrowserFails(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/device/start":
			w.Write([]byte(`{"code":"TST-001","expires_at":"2099-01-01T00:00:00Z"}`))
		case "/api/device/poll":
			w.Write([]byte(`{"status":"approved","api_key":"fb-key","machine_id":"fb-machine"}`))
		}
	}))
	defer srv.Close()

	reg := relay.NewRegistrarWithStore(srv.URL, store)
	// Simulate browser open failing.
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("browser unavailable") }
	reg.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := reg.Register(ctx, "test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.APIKey != "fb-key" {
		t.Errorf("APIKey = %q, want %q", result.APIKey, "fb-key")
	}
	if result.MachineID != "fb-machine" {
		t.Errorf("MachineID = %q, want %q", result.MachineID, "fb-machine")
	}
}

// TestRegistrar_DeviceCodeFlow_429Retry verifies that a 429 response is treated as
// transient and polling continues until the server approves.
func TestRegistrar_DeviceCodeFlow_429Retry(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/device/start":
			w.Write([]byte(`{"code":"TST-429","expires_at":"2099-01-01T00:00:00Z"}`))
		case "/api/device/poll":
			requestCount++
			if requestCount == 1 {
				// First attempt: rate-limited.
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			// Second attempt: approved.
			w.Write([]byte(`{"status":"approved","api_key":"retry-key","machine_id":"retry-machine"}`))
		}
	}))
	defer srv.Close()

	reg := relay.NewRegistrarWithStore(srv.URL, store)
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("no browser") }
	reg.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := reg.Register(ctx, "test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.APIKey != "retry-key" {
		t.Errorf("APIKey = %q, want %q", result.APIKey, "retry-key")
	}
	if requestCount < 2 {
		t.Errorf("expected at least 2 poll requests (1 rate-limited + 1 approved), got %d", requestCount)
	}
}

// TestRegistrar_DeviceCodeFlow_410Expired verifies that a 410 response surfaces
// ErrDeviceCodeExpired immediately (it is a fatal, non-retriable error).
func TestRegistrar_DeviceCodeFlow_410Expired(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/device/start":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":"TST-EXPIRE","expires_at":"2000-01-01T00:00:00Z"}`))
		case "/api/device/poll":
			// Code already expired.
			w.WriteHeader(http.StatusGone)
		}
	}))
	defer srv.Close()

	reg := relay.NewRegistrarWithStore(srv.URL, store)
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("no browser") }
	reg.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Fatal("expected error on 410 response, got nil")
	}
	if !errors.Is(err, relay.ErrDeviceCodeExpired) {
		t.Errorf("expected ErrDeviceCodeExpired, got: %v", err)
	}
}

// TestRegistrar_DeviceCodeFlow_StartEndpointCalled verifies that Register calls
// GET /api/device/start when the browser flow fails.
func TestRegistrar_DeviceCodeFlow_StartEndpointCalled(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	startCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/device/start":
			startCalled = true
			w.Write([]byte(`{"code":"CHK-001","expires_at":"2099-01-01T00:00:00Z"}`))
		case "/api/device/poll":
			w.Write([]byte(`{"status":"approved","api_key":"chk-key","machine_id":"chk-machine"}`))
		}
	}))
	defer srv.Close()

	reg := relay.NewRegistrarWithStore(srv.URL, store)
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("no browser") }
	reg.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !startCalled {
		t.Error("expected GET /api/device/start to be called, but it was not")
	}
}

type failSaveTokenStore struct {
	relay.MemoryTokenStore
}

func (f *failSaveTokenStore) Save(token string) error { return fmt.Errorf("disk full") }

// TestRegistrar_RegisterWithToken_StoresToken tests the MDM fleet token registration path.
func TestRegistrar_RegisterWithToken_StoresToken(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("https://example.com", store)

	result, err := reg.RegisterWithToken("pre-provisioned-jwt-token", "mdm-machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.MachineID == "" {
		t.Error("expected non-empty machine ID")
	}

	tok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "pre-provisioned-jwt-token" {
		t.Errorf("wrong token stored: got %q, want %q", tok, "pre-provisioned-jwt-token")
	}
}

// TestRegistrar_RegisterWithToken_EmptyTokenError tests that an empty token is rejected.
func TestRegistrar_RegisterWithToken_EmptyTokenError(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("https://example.com", store)

	_, err := reg.RegisterWithToken("", "machine-1")
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}

// TestRegistrar_RegisterWithToken_StoreFailurePropagates tests that token store
// Save failures are properly propagated to the caller.
func TestRegistrar_RegisterWithToken_StoreFailurePropagates(t *testing.T) {
	store := &failSaveTokenStore{}
	reg := relay.NewRegistrarWithStore("https://example.com", store)

	_, err := reg.RegisterWithToken("some-token", "machine-1")
	if err == nil {
		t.Fatal("expected error when store.Save fails")
	}
}
