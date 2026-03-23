// cmd/mockcloud/main.go
// A minimal HTTP server that simulates HuginnCloud behavior for local development and tests.
// Endpoints:
//
//	GET  /health                                        → {"status": "ok"}
//	GET  /register?machine_id=X&callback=Y&name=Z      → redirects to callback with ?code=<one-time-code>
//	POST /exchange                                      → form: code=X → {"token": "<test-jwt>"}
//	WS   /satellite?machine_id=X                       → satellite connection (accepts, echoes)
//	WS   /client?machine_id=X                          → test client endpoint
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

const testSecret = "huginn-mock-secret-for-testing"

var (
	port         = flag.Int("port", 9090, "Port to listen on")
	pendingMu    sync.Mutex
	pendingCodes = map[string]string{} // one-time code → machine_id
)

func generateJWT(machineID string) string {
	// Simple HMAC-SHA256 signed JWT (not standard JWT, just for testing)
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

func main() {
	flag.Parse()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /register", func(w http.ResponseWriter, r *http.Request) {
		machineID := r.URL.Query().Get("machine_id")
		callback := r.URL.Query().Get("callback")
		// Generate one-time code
		var b [8]byte
		rand.Read(b[:])
		code := hex.EncodeToString(b[:])
		pendingMu.Lock()
		pendingCodes[code] = machineID
		pendingMu.Unlock()
		// Redirect to callback with code
		http.Redirect(w, r, callback+"?code="+code, http.StatusFound)
	})

	mux.HandleFunc("POST /exchange", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		code := r.FormValue("code")
		pendingMu.Lock()
		machineID, ok := pendingCodes[code]
		if ok {
			delete(pendingCodes, code)
		}
		pendingMu.Unlock()
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"invalid or expired code"}`, http.StatusBadRequest)
			return
		}
		token := generateJWT(machineID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("MockCloud listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
