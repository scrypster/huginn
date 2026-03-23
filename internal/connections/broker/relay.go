package broker

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

// jwtParserOptions configures strict expiry validation for relay JWTs.
// jwt/v5 validates "exp" automatically when the claim is present, so this is
// belt-and-suspenders: we also check it explicitly below to guard against
// future library changes or missing "exp" fields entirely.
var jwtParserOptions = []jwt.ParserOption{
	jwt.WithExpirationRequired(),
}

// ParseRelayJWT verifies a relay JWT using the relay_challenge as the signing key
// and returns the extracted fields.
func ParseRelayJWT(tokenStr, relayChallenge string) (*RelayResult, error) {
	key, err := base64.RawURLEncoding.DecodeString(relayChallenge)
	if err != nil {
		return nil, fmt.Errorf("decode relay_challenge: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("relay_challenge must decode to 32 bytes, got %d", len(key))
	}

	var claims jwt.MapClaims
	// jwt/v5 validates "exp" automatically; jwtParserOptions makes "exp" required
	// so tokens without an expiry claim are also rejected.
	tok, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return key, nil
	}, jwtParserOptions...)
	if err != nil || !tok.Valid {
		return nil, fmt.Errorf("invalid relay JWT: %w", err)
	}

	provider, _ := claims["provider"].(string)
	accessToken, _ := claims["access_token"].(string)
	refreshToken, _ := claims["refresh_token"].(string)
	accountLabel, _ := claims["account_label"].(string)
	var expiry int64
	if v, ok := claims["expiry"].(float64); ok {
		expiry = int64(v)
	}

	if provider == "" || accessToken == "" {
		return nil, fmt.Errorf("relay JWT missing required fields (provider, access_token)")
	}
	return &RelayResult{
		Provider:     provider,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		AccountLabel: accountLabel,
		Expiry:       expiry,
	}, nil
}

// ToOAuthToken converts a RelayResult to an *oauth2.Token for storage.
func (r *RelayResult) ToOAuthToken() *oauth2.Token {
	tok := &oauth2.Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		TokenType:    "Bearer",
	}
	if r.Expiry > 0 {
		tok.Expiry = time.Unix(r.Expiry, 0)
	}
	return tok
}
