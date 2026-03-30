package backend

import (
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

// ResolveAPIKey resolves an API key from its raw configuration value.
//   - "$ENV_VAR" → reads os.Getenv("ENV_VAR")
//   - "keyring:<service>:<user>" → reads from the OS keyring
//   - anything else → returned as-is
//
// Resolved values are never included in error messages.
func ResolveAPIKey(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	if strings.HasPrefix(raw, "$") {
		envVar := strings.TrimPrefix(raw, "$")
		val := os.Getenv(envVar)
		if val == "" {
			return "", fmt.Errorf("api key: environment variable %q is empty or unset", envVar)
		}
		return val, nil
	}

	if strings.HasPrefix(raw, "keyring:") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "keyring:"), ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", fmt.Errorf("api key: invalid keyring format, expected \"keyring:<service>:<user>\"")
		}
		secret, err := keyring.Get(parts[0], parts[1])
		if err != nil {
			return "", fmt.Errorf("api key: keyring lookup failed for service %q", parts[0])
		}
		return secret, nil
	}

	return raw, nil
}

// IsLiteralAPIKey reports whether raw is a literal API key (not an env-var
// reference like "$VAR", not a keyring reference like "keyring:svc:user", and
// not the UI redaction sentinel "[REDACTED]").
// Empty strings return false (no key configured at all).
func IsLiteralAPIKey(raw string) bool {
	if raw == "" {
		return false
	}
	return !strings.HasPrefix(raw, "$") &&
		!strings.HasPrefix(raw, "keyring:") &&
		raw != "[REDACTED]"
}

// StoreAPIKey stores value in the OS keyring under "huginn"/<slot> and returns
// the canonical keyring reference "keyring:huginn:<slot>" for storing in config.
// If keyring storage fails (e.g. no keyring daemon on Linux/CI), the raw value
// is returned as-is with a non-nil error so callers can log the fallback.
// The returned value is always safe to persist in config — the caller must not
// skip saving it even when err != nil.
func StoreAPIKey(slot, value string) (string, error) {
	service := "huginn"
	user := slot
	if err := keyring.Set(service, user, value); err != nil {
		// Keyring unavailable (common on Linux without a keyring daemon, or CI).
		// Return the literal so the caller can still persist the key in config.json.
		return value, fmt.Errorf("keychain unavailable, storing literal in config (safe on this platform): %w", err)
	}
	return fmt.Sprintf("keyring:%s:%s", service, user), nil
}
