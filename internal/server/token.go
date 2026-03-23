package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const tokenFile = "server.token"

// LoadOrCreateToken loads the server auth token from huginnDir/server.token,
// creating it with a fresh random value if it doesn't exist.
func LoadOrCreateToken(huginnDir string) (string, error) {
	path := filepath.Join(huginnDir, tokenFile)
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) == 64 { // valid 32-byte hex
			return token, nil
		}
	}
	// Generate new token
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("server: generate token: %w", err)
	}
	token := hex.EncodeToString(b[:])
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("server: write token: %w", err)
	}
	return token, nil
}
