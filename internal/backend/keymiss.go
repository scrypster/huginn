package backend

import (
	"log/slog"
	"strings"
	"sync"
)

// TeammateKeyMissSpeech is the hallway / drawer / thread-summary line when
// ChatCompletion failed because the OS keyring has no API key. Never include
// the raw Go error or a secret.
func TeammateKeyMissSpeech(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "This teammate"
	}
	return name + " couldn't get a key for this."
}

// IsKeyMiss reports a keyring / API-key resolution miss. Matches the live
// ChatWithAgent leak: `resolve api key: api key: keyring lookup failed…`.
// Does not treat a hostname miss or a generic network error as a key miss.
func IsKeyMiss(err error) bool {
	if err == nil {
		return false
	}
	return IsKeyMissText(err.Error())
}

// IsKeyMissText reports the same miss from an already-stringified error.
func IsKeyMissText(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "couldn't get a key") {
		return true
	}
	if strings.Contains(lower, "keyring lookup failed") {
		return true
	}
	if strings.Contains(lower, "resolve api key") {
		return true
	}
	if strings.Contains(lower, "api key: keyring") {
		return true
	}
	if strings.Contains(lower, "key not found in") && (strings.Contains(lower, "keyring") || strings.Contains(lower, "keychain") || strings.Contains(lower, "secrets")) {
		return true
	}
	return false
}

var keyMissLogged sync.Map

// LogKeyMissOnce writes one INFO line per agent name. Never logs the raw
// error (it can carry a secret) and never invents a key.
func LogKeyMissOnce(name string, err error) {
	if !IsKeyMiss(err) {
		return
	}
	key := strings.TrimSpace(name)
	if key == "" {
		key = "teammate"
	}
	if _, loaded := keyMissLogged.LoadOrStore(key, true); loaded {
		return
	}
	slog.Info("keyring miss", "agent", key)
}

// PersistKeyMissSpeech returns teammate key-miss speech when leftover is
// empty and err is a key miss. Otherwise returns leftover.
func PersistKeyMissSpeech(name, leftover string, err error) string {
	if strings.TrimSpace(leftover) != "" {
		return leftover
	}
	if !IsKeyMiss(err) {
		return leftover
	}
	LogKeyMissOnce(name, err)
	return TeammateKeyMissSpeech(name)
}
