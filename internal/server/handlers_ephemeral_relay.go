package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/scrypster/huginn/internal/connections"
	"golang.org/x/oauth2"
)

// isKnownProvider returns true if name matches one of the built-in provider slugs.
func isKnownProvider(name string) bool {
	for _, p := range knownProviders {
		if p.Name == name {
			return true
		}
	}
	return false
}

// serveEphemeralRelay starts a single-use HTTP server on ln that accepts exactly
// one /oauth/relay request. The relay JWT is verified with HMAC-SHA256 keyed on
// the raw bytes obtained by base64url-decoding relayChallenge. After handling
// one request (success or error) the server shuts itself down.
func (s *Server) serveEphemeralRelay(ln net.Listener, relayChallenge, provider string) {
	// Derive the HMAC key from the challenge.
	key, err := base64.RawURLEncoding.DecodeString(relayChallenge)
	if err != nil {
		// Bad challenge — close immediately.
		ln.Close()
		return
	}

	once := make(chan struct{}, 1) // closed after first request is handled

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/oauth/relay", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
		defer func() {
			select {
			case once <- struct{}{}:
				// Signal shutdown after response is written.
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					srv.Shutdown(ctx) //nolint:errcheck
				}()
			default:
			}
		}()

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Error path from broker.
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			fmt.Fprint(w, relayErrorHTML(errMsg))
			return
		}

		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			fmt.Fprint(w, relayErrorHTML("missing token parameter"))
			return
		}

		// Verify relay JWT with the derived key.
		var claims jwt.MapClaims
		tok, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return key, nil
		})
		if err != nil || !tok.Valid {
			fmt.Fprint(w, relayErrorHTML("invalid relay token"))
			return
		}

		// Validate provider matches expectation.
		claimedProvider, _ := claims["provider"].(string)
		if claimedProvider != provider {
			fmt.Fprint(w, relayErrorHTML("provider mismatch"))
			return
		}

		accessToken, _ := claims["access_token"].(string)
		refreshToken, _ := claims["refresh_token"].(string)
		accountLabel, _ := claims["account_label"].(string)

		if accessToken == "" {
			fmt.Fprint(w, relayErrorHTML("relay token missing required fields"))
			return
		}

		oauthTok := &oauth2.Token{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			TokenType:    "Bearer",
		}
		if exp, ok := claims["expiry"].(float64); ok && exp > 0 {
			oauthTok.Expiry = time.Unix(int64(exp), 0)
		}

		if s.connMgr != nil {
			meta := map[string]string{"relay_challenge": relayChallenge}
			if err := s.connMgr.StoreExternalTokenWithMeta(
				r.Context(),
				connections.Provider(provider),
				oauthTok,
				accountLabel,
				meta,
			); err != nil {
				slog.Error("ephemeral relay: store token failed", "provider", provider, "err", err)
				fmt.Fprint(w, relayErrorHTML("token storage failed"))
				return
			}
		}

		providerDisplay := provider
		if len(providerDisplay) > 0 {
			providerDisplay = string(providerDisplay[0]-32) + providerDisplay[1:]
		}
		fmt.Fprint(w, relaySuccessHTML(providerDisplay))
	})

	srv.Serve(ln) //nolint:errcheck
}

// startOAuthViaBroker opens an ephemeral listener, derives a relay challenge,
// calls the broker to get the auth URL, and starts serveEphemeralRelay in the
// background. It returns the auth URL the user should open.
func (s *Server) startOAuthViaBroker(ctx context.Context, broker BrokerClient, provider string) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("ephemeral relay: listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	// Generate a 32-byte random secret and derive the relay challenge.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		ln.Close()
		return "", fmt.Errorf("ephemeral relay: rand: %w", err)
	}
	hash := sha256.Sum256(secret)
	relayChallenge := base64.RawURLEncoding.EncodeToString(hash[:])

	authURL, err := broker.Start(ctx, provider, relayChallenge, port)
	if err != nil {
		ln.Close()
		return "", fmt.Errorf("ephemeral relay: broker start: %w", err)
	}

	go s.serveEphemeralRelay(ln, relayChallenge, provider)
	return authURL, nil
}

// startOAuthViaCloudBroker generates a relay_key and a unique flow ID, calls
// broker.StartCloudFlow, stores the relay_key keyed by flow ID for later
// retrieval in handleOAuthRelayFromCloud, and returns the auth_url and flow ID.
// Using a per-flow ID (rather than provider name) prevents concurrent OAuth
// flows for the same provider from overwriting each other's relay key.
func (s *Server) startOAuthViaCloudBroker(ctx context.Context, broker BrokerClient, provider string) (authURL, flowID string, err error) {
	// Generate a fresh 32-byte relay_key for this flow.
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", "", fmt.Errorf("cloud broker: generate relay key: %w", err)
	}
	relayKey := base64.RawURLEncoding.EncodeToString(keyBytes)

	// Generate a unique flow ID so concurrent flows for the same provider
	// do not collide in the relay key map.
	flowIDBytes := make([]byte, 16)
	if _, err := rand.Read(flowIDBytes); err != nil {
		return "", "", fmt.Errorf("cloud broker: generate flow ID: %w", err)
	}
	flowID = hex.EncodeToString(flowIDBytes)

	authURL, err = broker.StartCloudFlow(ctx, provider, relayKey)
	if err != nil {
		return "", "", fmt.Errorf("cloud broker: start: %w", err)
	}

	// Store key keyed by flow ID so handleOAuthRelayFromCloud can verify
	// the incoming relay JWT without a provider-level collision.
	s.storeRelayKey(flowID, relayKey)

	return authURL, flowID, nil
}

// handleStartOAuthBroker is an internal helper used by handleStartOAuth when a
// broker client is configured. It routes to the cloud-UI flow when the satellite
// is registered with HuginnCloud (s.satellite != nil), otherwise uses the ephemeral
// local relay flow.
func (s *Server) handleStartOAuthBroker(w http.ResponseWriter, r *http.Request, provider string) {
	s.mu.Lock()
	broker := s.brokerClient
	isRegistered := s.satellite != nil
	s.mu.Unlock()

	if isRegistered {
		authURL, flowID, err := s.startOAuthViaCloudBroker(r.Context(), broker, provider)
		if err != nil {
			jsonError(w, 500, "start broker oauth: "+err.Error())
			return
		}
		jsonOK(w, map[string]string{"auth_url": authURL, "flow_id": flowID})
	} else {
		authURL, err := s.startOAuthViaBroker(r.Context(), broker, provider)
		if err != nil {
			jsonError(w, 500, "start broker oauth: "+err.Error())
			return
		}
		jsonOK(w, map[string]string{"auth_url": authURL})
	}
}
