package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/scrypster/huginn/internal/connections"
	"golang.org/x/oauth2"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func (s *Server) handleOAuthRelay(w http.ResponseWriter, r *http.Request) {
	// Error path from broker.
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML(errMsg))
		return
	}

	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML("missing token parameter"))
		return
	}

	// Load machine JWT to get machine ID and JWT secret.
	if s.relayTokenStorer == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML("relay not configured"))
		return
	}
	machineJWT, err := s.relayTokenStorer.Load()
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML("machine not registered"))
		return
	}

	// Parse machine JWT (unverified) to extract machine_id.
	// The machine JWT is already trusted (it came from our own keychain).
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	var machineClaims jwt.MapClaims
	_, _, err = parser.ParseUnverified(machineJWT, &machineClaims)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML("invalid machine token"))
		return
	}
	machineID, _ := machineClaims["machine_id"].(string)
	if machineID == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML("machine_id not found in token"))
		return
	}

	// Derive relay signing key: SHA-256(machineID + ":" + jwtSecret)
	signingKey := relayTokenSigningKey(machineID, s.jwtSecret)

	// Verify relay JWT.
	var relayClaims jwt.MapClaims
	relayToken, err := jwt.ParseWithClaims(tokenStr, &relayClaims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("relay: unexpected signing method: %v", t.Header["alg"])
		}
		return signingKey, nil
	})
	if err != nil || !relayToken.Valid {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML("invalid relay token"))
		return
	}

	// Extract fields from relay JWT.
	provider, _ := relayClaims["provider"].(string)
	accessToken, _ := relayClaims["access_token"].(string)
	refreshToken, _ := relayClaims["refresh_token"].(string)
	accountLabel, _ := relayClaims["account_label"].(string)

	if provider == "" || accessToken == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, relayErrorHTML("relay token missing required fields"))
		return
	}

	// Build oauth2.Token.
	oauthTok := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
	}
	if exp, ok := relayClaims["expiry"].(float64); ok && exp > 0 {
		oauthTok.Expiry = time.Unix(int64(exp), 0)
	}

	// Store the token.
	if s.connMgr != nil {
		if err := s.connMgr.StoreExternalToken(r.Context(), connections.Provider(provider), oauthTok, accountLabel); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, relayErrorHTML("Failed to store token: "+err.Error()))
			return
		}
	}

	// Success — return HTML with window.close().
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	providerDisplay := cases.Title(language.English).String(provider)
	fmt.Fprint(w, relaySuccessHTML(providerDisplay))
}

// relayTokenSigningKey derives the HMAC key for verifying relay JWTs.
// Must match huginncloud's broker.RelayTokenKey.
func relayTokenSigningKey(machineID, jwtSecret string) []byte {
	h := sha256.Sum256([]byte(machineID + ":" + jwtSecret))
	return h[:]
}

func relaySuccessHTML(provider string) string {
	// Use json.Marshal for safe JavaScript string encoding and
	// html.EscapeString for HTML content to prevent XSS.
	providerJSON, err := json.Marshal(provider)
	if err != nil {
		providerJSON = []byte(`""`)
	}
	providerHTML := html.EscapeString(provider)
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>%s Connected</title></head>
<body style="font-family:sans-serif;text-align:center;padding:60px">
  <h2>%s connected successfully</h2>
  <p>You may close this tab.</p>
  <script>
    setTimeout(function() {
      if (window.opener) {
        window.opener.postMessage({type:'huginn_oauth_complete',provider:%s}, '*');
      }
      window.close();
    }, 1500);
  </script>
</body>
</html>`, providerHTML, providerHTML, providerJSON)
}

func relayErrorHTML(msg string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Authorization Error</title></head>
<body style="font-family:sans-serif;text-align:center;padding:60px">
  <h2>Authorization failed</h2>
  <p>%s</p>
  <p>You may close this tab and try again.</p>
</body>
</html>`, html.EscapeString(msg))
}
