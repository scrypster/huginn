package connections

// proactive_refresh_retry_test.go — verifies the 3-attempt retry loop and
// warning/error logging in doProactiveRefresh.

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// alwaysFailProvider is an IntegrationProvider whose OAuthConfig points at an
// unreachable endpoint so every Token() call fails fast (no network, immediate
// connection refused), simulating a refresh failure.
type alwaysFailProvider struct{}

func (p *alwaysFailProvider) Name() Provider      { return ProviderGoogle }
func (p *alwaysFailProvider) DisplayName() string { return "always-fail provider" }
func (p *alwaysFailProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "id",
		ClientSecret: "secret",
		RedirectURL:  redirectURL,
		Endpoint: oauth2.Endpoint{
			// Port 0 is unreachable: Token() will fail immediately with a
			// "connection refused" error, exercising the retry path.
			TokenURL: "http://127.0.0.1:0/token",
			AuthURL:  "http://127.0.0.1:0/auth",
		},
	}
}
func (p *alwaysFailProvider) GetAccountInfo(_ context.Context, _ *http.Client) (*AccountInfo, error) {
	return &AccountInfo{ID: "x", Label: "x"}, nil
}

// newCapturingLogger installs a capturing slog logger for the duration of the
// test and returns the buffer where all log output is written.
func newCapturingLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

// seedToken stores an expired token for connID so doProactiveRefresh can fetch
// it and attempt a refresh (an already-valid token would be returned by the
// oauth2 TokenSource without making a network call, bypassing the retry path).
func seedToken(t *testing.T, m *Manager, store StoreInterface, secrets SecretStore, connID string) {
	t.Helper()
	conn := Connection{
		ID:        connID,
		Provider:  ProviderGoogle,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	// Use an already-expired token so the oauth2 TokenSource must contact the
	// token endpoint to refresh it. The alwaysFailProvider points at an
	// unreachable endpoint (port 0) so the refresh always fails.
	tok := &oauth2.Token{
		AccessToken:  "old-token",
		RefreshToken: "old-refresh",
		Expiry:       time.Now().Add(-time.Hour), // expired
	}
	if err := secrets.StoreToken(connID, tok); err != nil {
		t.Fatalf("secrets.StoreToken: %v", err)
	}
}

// ─── Test: Warn is logged on each failed attempt ──────────────────────────────

// TestDoProactiveRefresh_FailedAttempts_LogsWarn verifies that when the token
// source fails, slog.Warn is emitted for each failed attempt, and after
// exhausting all 3 attempts, slog.Error is emitted.
func TestDoProactiveRefresh_FailedAttempts_LogsWarn(t *testing.T) {
	buf := newCapturingLogger(t)

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost/cb")
	defer m.Close()

	const connID = "conn-warn-test"
	seedToken(t, m, store, secrets, connID)

	m.RegisterProviders(map[Provider]IntegrationProvider{
		ProviderGoogle: &alwaysFailProvider{},
	})

	// Use tiny backoffs so the test finishes in milliseconds.
	oldBackoff := proactiveRefreshBackoff
	proactiveRefreshBackoff = []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}
	defer func() { proactiveRefreshBackoff = oldBackoff }()

	m.doProactiveRefresh(connID, ProviderGoogle)

	output := buf.String()
	// Each failed attempt must emit a Warn log.
	if !strings.Contains(output, "proactive token refresh failed") {
		t.Errorf("expected Warn log about failed refresh, got:\n%s", output)
	}
	// After exhausting all retries, an Error log must be emitted.
	if !strings.Contains(output, "proactive token refresh exhausted") {
		t.Errorf("expected Error log about exhausted retries, got:\n%s", output)
	}
}

// ─── Test: Error is logged after exhausting all retries ───────────────────────

// TestDoProactiveRefresh_ExhaustsRetries_LogsError verifies that after all 3
// attempts fail, an Error-level log is emitted and no panic occurs.
func TestDoProactiveRefresh_ExhaustsRetries_LogsError(t *testing.T) {
	buf := newCapturingLogger(t)

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost/cb")
	defer m.Close()

	const connID = "conn-error-test"
	seedToken(t, m, store, secrets, connID)

	m.RegisterProviders(map[Provider]IntegrationProvider{
		ProviderGoogle: &alwaysFailProvider{},
	})

	oldBackoff := proactiveRefreshBackoff
	proactiveRefreshBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { proactiveRefreshBackoff = oldBackoff }()

	m.doProactiveRefresh(connID, ProviderGoogle)

	output := buf.String()
	if !strings.Contains(output, "exhausted") {
		t.Errorf("expected Error log containing 'exhausted', got:\n%s", output)
	}
}

// ─── Test: Context cancellation stops retries immediately ─────────────────────

// TestDoProactiveRefresh_ContextCancellation_StopsRetries verifies that when
// the Manager is closed (context cancelled) while waiting between retry
// attempts, the retry loop exits promptly.
func TestDoProactiveRefresh_ContextCancellation_StopsRetries(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost/cb")

	const connID = "conn-cancel-test"
	seedToken(t, m, store, secrets, connID)

	m.RegisterProviders(map[Provider]IntegrationProvider{
		ProviderGoogle: &alwaysFailProvider{},
	})

	// Use a long backoff so the cancellation fires during the inter-retry sleep.
	oldBackoff := proactiveRefreshBackoff
	proactiveRefreshBackoff = []time.Duration{500 * time.Millisecond, 500 * time.Millisecond}
	defer func() { proactiveRefreshBackoff = oldBackoff }()

	// Close the manager (cancel context) shortly after the first attempt.
	go func() {
		time.Sleep(30 * time.Millisecond)
		m.Close()
	}()

	done := make(chan struct{})
	go func() {
		m.doProactiveRefresh(connID, ProviderGoogle)
		close(done)
	}()

	select {
	case <-done:
		// Good: returned well before the full 500ms backoff elapsed.
	case <-time.After(2 * time.Second):
		t.Error("doProactiveRefresh did not respect context cancellation within 2s")
	}
}
