// internal/server/secrets.go
package server

// SecretStore persists sensitive values securely.
// The canonical implementation lives in internal/connections/secrets.go.
// This file exists to document that the server package depends on SecretStore
// but does not own it.
