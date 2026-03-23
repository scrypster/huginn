// Package testutil provides test helpers for relay tests.
package testutil

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
)

const testSecret = "huginn-mock-secret-for-testing"

// MockCloud is an in-process test double for HuginnCloud.
type MockCloud struct {
	pendingMu    sync.Mutex
	pendingCodes map[string]string // one-time code → machine_id
}

// generateJWT produces a simple HMAC-SHA256 signed token for testing.
func generateJWT(machineID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(
		`{"machine_id":%q,"iss":"mockcloud","iat":%d}`, machineID, time.Now().Unix(),
	)))
	data := header + "." + payload
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(data))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return data + "." + sig
}

// newMux builds the HTTP mux for the mock cloud.
func (mc *MockCloud) newMux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /register", func(w http.ResponseWriter, r *http.Request) {
		machineID := r.URL.Query().Get("machine_id")
		callback := r.URL.Query().Get("callback")
		var b [8]byte
		rand.Read(b[:])
		code := hex.EncodeToString(b[:])
		mc.pendingMu.Lock()
		mc.pendingCodes[code] = machineID
		mc.pendingMu.Unlock()
		http.Redirect(w, r, callback+"?code="+code, http.StatusFound)
	})

	mux.HandleFunc("POST /exchange", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		code := r.FormValue("code")
		mc.pendingMu.Lock()
		machineID, ok := mc.pendingCodes[code]
		if ok {
			delete(mc.pendingCodes, code)
		}
		mc.pendingMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			http.Error(w, `{"error":"invalid or expired code"}`, http.StatusBadRequest)
			return
		}
		token := generateJWT(machineID)
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	})

	return mux
}

// StartMockCloud starts a mockcloud HTTP server for testing.
// Returns the server and its base URL. Call server.Close() when done.
func StartMockCloud() (*httptest.Server, string) {
	mc := &MockCloud{
		pendingCodes: make(map[string]string),
	}
	srv := httptest.NewServer(mc.newMux())
	return srv, srv.URL
}

// MockCloudWS extends MockCloud with a WebSocket endpoint at /ws/satellite,
// matching the path that relay.Satellite dials in production. It captures all
// relay.Message frames sent by the satellite so tests can assert on them.
type MockCloudWS struct {
	*httptest.Server

	// Embedded HTTP mock for /health, /register, /exchange.
	httpMC *MockCloud

	// received is a buffered channel that receives every relay.Message sent by
	// a connected satellite. Capacity 32 is generous for single-test usage.
	received chan relay.Message

	upgrader websocket.Upgrader

	connMu sync.Mutex
	conns  []*websocket.Conn // tracked for forced cleanup on test teardown
}

// StartMockCloudWS starts a combined HTTP+WebSocket mock server.
// Registers a t.Cleanup that force-closes all WebSocket connections and the
// HTTP server so goroutines do not leak across tests.
func StartMockCloudWS(t testing.TB) *MockCloudWS {
	t.Helper()
	mc := &MockCloudWS{
		httpMC:   &MockCloud{pendingCodes: make(map[string]string)},
		received: make(chan relay.Message, 32),
		upgrader: websocket.Upgrader{
			// Allow all origins — this is a test server, not production.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()

	// Wire existing HTTP endpoints from MockCloud.
	baseHandler := mc.httpMC.newMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		baseHandler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		baseHandler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/exchange", func(w http.ResponseWriter, r *http.Request) {
		baseHandler.ServeHTTP(w, r)
	})

	// Wire the satellite WebSocket endpoint.
	mux.HandleFunc("/ws/satellite", mc.handleWS)

	mc.Server = httptest.NewServer(mux)

	t.Cleanup(func() {
		// Force-close all tracked WebSocket connections so read-pump goroutines exit.
		mc.connMu.Lock()
		for _, c := range mc.conns {
			_ = c.Close()
		}
		mc.connMu.Unlock()
		mc.Server.Close()
	})

	return mc
}

// URL returns the base URL of the mock server (http://...).
func (mc *MockCloudWS) URL() string { return mc.Server.URL }

// WSURL returns the WebSocket URL for /ws/satellite (ws://...).
func (mc *MockCloudWS) WSURL() string {
	return strings.Replace(mc.Server.URL, "http://", "ws://", 1) + "/ws/satellite"
}

// handleWS upgrades the connection to WebSocket and pumps incoming JSON frames
// into mc.received. Rejects connections without an Authorization header.
func (mc *MockCloudWS) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := mc.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// 30-second read deadline prevents goroutine leaks when a satellite
	// disconnects without sending a clean close frame.
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	mc.connMu.Lock()
	mc.conns = append(mc.conns, conn)
	mc.connMu.Unlock()

	go func() {
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg relay.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
			select {
			case mc.received <- msg:
			default:
				// Buffer full — test is not draining; drop the frame silently.
				// A deadlock would be worse than a missed assertion.
			}
		}
	}()
}

// WaitMessage blocks until a relay.Message arrives from the satellite or the
// timeout elapses. Returns the message and true on success, zero value and false
// on timeout.
func (mc *MockCloudWS) WaitMessage(timeout time.Duration) (relay.Message, bool) {
	select {
	case msg := <-mc.received:
		return msg, true
	case <-time.After(timeout):
		return relay.Message{}, false
	}
}

// WaitMessageOfType blocks until a relay.Message with the given Type arrives,
// skipping any messages of other types (e.g. "satellite_hello" handshake).
// Returns the message and true on success, zero value and false on timeout.
func (mc *MockCloudWS) WaitMessageOfType(msgType relay.MessageType, timeout time.Duration) (relay.Message, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case msg := <-mc.received:
			if msg.Type == msgType {
				return msg, true
			}
			// Wrong type — keep draining.
		case <-time.After(remaining):
			return relay.Message{}, false
		}
	}
	return relay.Message{}, false
}

// ExchangeToken performs a POST /exchange against the provided httptest.Server to
// obtain a JWT for machineID. It bypasses the /register OAuth redirect by
// inserting a code directly into the pending codes map via /register with a
// no-redirect callback, then exchanging it.
//
// This helper is intended for use in tests that need a valid JWT without standing
// up a real browser-based OAuth flow.
func ExchangeToken(t testing.TB, srv *httptest.Server, machineID string) string {
	t.Helper()

	// Call /register with a loopback callback that never fires — we capture the
	// redirect URL to extract the code.
	callbackURL := srv.URL + "/noop"
	regURL := srv.URL + "/register?machine_id=" + machineID + "&callback=" + callbackURL

	resp, err := noRedirectClient().Get(regURL)
	if err != nil {
		t.Fatalf("ExchangeToken: GET /register: %v", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatalf("ExchangeToken: /register did not redirect; status=%d", resp.StatusCode)
	}

	// Extract code from Location header query string.
	idx := strings.Index(loc, "code=")
	if idx < 0 {
		t.Fatalf("ExchangeToken: no code in Location %q", loc)
	}
	code := loc[idx+5:]

	// Exchange the code for a JWT.
	exchURL := srv.URL + "/exchange"
	exchResp, err := http.PostForm(exchURL, map[string][]string{"code": {code}})
	if err != nil {
		t.Fatalf("ExchangeToken: POST /exchange: %v", err)
	}
	defer exchResp.Body.Close()

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(exchResp.Body).Decode(&body); err != nil {
		t.Fatalf("ExchangeToken: decode /exchange response: %v", err)
	}
	if body.Token == "" {
		t.Fatalf("ExchangeToken: empty token in response")
	}
	return body.Token
}

// noRedirectClient returns an http.Client that does not follow redirects.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
