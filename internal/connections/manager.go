package connections

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	mrand "math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// proactiveRefreshLead is how far before expiry we trigger a proactive refresh.
const proactiveRefreshLead = 5 * time.Minute

// proactiveRefreshJitter is the maximum random jitter added to the refresh schedule
// to prevent thundering-herd when many tokens expire simultaneously.
const proactiveRefreshJitter = 60 * time.Second

// tokenRefreshBuffer is the minimum time remaining before expiry at which we
// consider a token as "needs refresh". This matches industry standard (Google,
// AWS all use 30-60s buffer) and avoids a boundary condition where a token
// expiring in exactly 0 seconds is considered valid.
const tokenRefreshBuffer = 30 * time.Second

// tokenNeedsRefresh returns true when the token's expiry is non-zero and the
// token will expire within tokenRefreshBuffer (30 seconds). Zero expiry means
// the provider does not issue expiring tokens (e.g. GitHub fine-grained PATs
// stored as OAuth), so we skip scheduling for those.
func tokenNeedsRefresh(expiry time.Time) bool {
	if expiry.IsZero() {
		return false // provider omits expiry; treat as perpetually valid
	}
	return time.Until(expiry) < tokenRefreshBuffer
}

// defaultPendingFlowTTL is the default lifetime for an OAuth flow state token.
// After this time the pending entry is removed to prevent memory leaks from
// abandoned flows. Use WithPendingFlowTTL to override.
const defaultPendingFlowTTL = 10 * time.Minute

// pendingFlow holds the state for an in-progress OAuth flow.
type pendingFlow struct {
	provider      IntegrationProvider
	config        *oauth2.Config
	codeVerifier  string // PKCE verifier (plain text, never sent to browser)
	codeChallenge string // PKCE challenge stored for post-callback verification
	redirectURL   string
	expiresAt     time.Time // wall-clock expiry; flows not completed by this time are purged
}

// RefreshEventFn is called when a proactive token refresh succeeds or fails.
// event is "connection_token_refreshed" or "connection_token_refresh_failed".
// errMsg is non-empty only on failure.
type RefreshEventFn func(event string, connID string, provider Provider, errMsg string)

// ManagerOption is a functional option for configuring a Manager.
type ManagerOption func(*Manager)

// WithRefreshEventFn registers a callback called on each proactive refresh success/failure.
// The callback must be non-blocking. Calls originate from background timer goroutines.
func WithRefreshEventFn(fn RefreshEventFn) ManagerOption {
	return func(m *Manager) { m.onRefreshEvent = fn }
}

// SetOnRefreshEvent installs (or replaces) the refresh event callback after construction.
// Thread-safe; may be called at any time before or after proactive refresh goroutines start.
func (m *Manager) SetOnRefreshEvent(fn RefreshEventFn) {
	m.mu.Lock()
	m.onRefreshEvent = fn
	m.mu.Unlock()
}

// WithPendingFlowTTL sets the lifetime for pending OAuth flow state tokens.
// Flows not completed within this duration are purged from memory. Defaults to
// defaultPendingFlowTTL (10 minutes). Useful in tests to reduce TTL to milliseconds
// so expiry behaviour can be exercised without real wall-clock waits.
func WithPendingFlowTTL(d time.Duration) ManagerOption {
	return func(m *Manager) { m.pendingFlowTTL = d }
}

// Manager orchestrates OAuth flows and token lifecycle.
// It coordinates between the Store (connection metadata), SecretStore (tokens),
// and IntegrationProviders (provider-specific OAuth configs).
type Manager struct {
	store          StoreInterface
	secrets        SecretStore
	redirectURL    string // e.g. "http://localhost:PORT/oauth/callback"
	pendingFlowTTL time.Duration // how long a pending flow state is valid

	mu           sync.Mutex
	pendingFlows map[string]*pendingFlow // keyed by OAuth state token

	done chan struct{} // closed by Close() to stop the background cleanup goroutine

	// onRefreshEvent is called (non-blocking, from timer goroutines) on each
	// proactive refresh success or failure. nil = no callback.
	onRefreshEvent RefreshEventFn

	// Proactive token rotation: schedule a best-effort refresh 5 minutes before
	// each OAuth token expires, avoiding mid-request token failures.
	providersMu   sync.RWMutex
	providers     map[Provider]IntegrationProvider // name → provider
	refreshMu     sync.Mutex
	refreshTimers map[string]*time.Timer  // connID → pending refresh timer
	refreshNonces map[string]uint64       // connID → generation counter; guards against stale timer callbacks
	refreshCtx    context.Context
	refreshCancel context.CancelFunc
}

// NewManager creates a Manager.
// redirectURL is the callback URL the OAuth provider will redirect to after auth.
// opts are optional functional options (e.g. WithPendingFlowTTL).
func NewManager(store StoreInterface, secrets SecretStore, redirectURL string, opts ...ManagerOption) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		store:          store,
		secrets:        secrets,
		redirectURL:    redirectURL,
		pendingFlowTTL: defaultPendingFlowTTL,
		pendingFlows:   make(map[string]*pendingFlow),
		done:           make(chan struct{}),
		providers:      make(map[Provider]IntegrationProvider),
		refreshTimers:  make(map[string]*time.Timer),
		refreshNonces:  make(map[string]uint64),
		refreshCtx:     ctx,
		refreshCancel:  cancel,
	}
	for _, opt := range opts {
		opt(m)
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.mu.Lock()
				m.purgeStalePendingFlows()
				m.mu.Unlock()
			case <-m.done:
				return
			}
		}
	}()
	return m
}

// RegisterProviders stores the integration provider instances so the Manager
// can use them for proactive token rotation. Call once at startup before any
// connections are established.
func (m *Manager) RegisterProviders(ps map[Provider]IntegrationProvider) {
	m.providersMu.Lock()
	defer m.providersMu.Unlock()
	for k, v := range ps {
		m.providers[k] = v
	}
}

// scheduleProactiveRefresh schedules a best-effort token refresh for connID
// proactiveRefreshLead before expiry, with up to proactiveRefreshJitter of
// random delay to prevent thundering-herd bursts.
// If expiry is zero or in the past, no timer is scheduled.
// Any previously scheduled timer for the same connID is cancelled first.
func (m *Manager) scheduleProactiveRefresh(connID string, providerName Provider, expiry time.Time) {
	if expiry.IsZero() {
		return
	}
	refreshAt := expiry.Add(-proactiveRefreshLead)
	jitter := time.Duration(mrand.Int63n(int64(proactiveRefreshJitter)))
	refreshAt = refreshAt.Add(jitter)

	delay := time.Until(refreshAt)
	if delay <= 0 {
		// Token already within or past the proactive-refresh window; skip
		// scheduling. The next real request will trigger an on-demand refresh
		// via the oauth2.TokenSource. This also handles the boundary case where
		// a token expires in exactly 0 seconds (time.Until == 0) which
		// previously slipped through without being considered expired.
		return
	}

	m.refreshMu.Lock()
	// Cancel any existing timer for this connection.
	if old, ok := m.refreshTimers[connID]; ok {
		old.Stop()
	}
	// Increment the nonce so any in-flight callback from the old timer is a
	// no-op. time.Timer.Stop() does not guarantee the callback has not already
	// started; the nonce provides the second line of defence.
	m.refreshNonces[connID]++
	nonce := m.refreshNonces[connID]
	timer := time.AfterFunc(delay, func() {
		// Guard: if the nonce changed since we were scheduled, a newer timer
		// was registered (connection deleted+recreated) — skip stale refresh.
		m.refreshMu.Lock()
		currentNonce := m.refreshNonces[connID]
		m.refreshMu.Unlock()
		if currentNonce != nonce {
			return
		}
		m.doProactiveRefresh(connID, providerName)
	})
	m.refreshTimers[connID] = timer
	m.refreshMu.Unlock()
}

// proactiveRefreshBackoff is the delay schedule for the 3-attempt retry loop
// inside doProactiveRefresh. Attempt 1 is immediate; attempts 2 and 3 wait
// 5 s and 15 s respectively before retrying.
var proactiveRefreshBackoff = []time.Duration{5 * time.Second, 15 * time.Second}

// doProactiveRefresh performs the actual token refresh for connID.
// It is called by the scheduled timer and must never panic.
// On failure it retries up to 3 times (1 immediate + 2 delayed) with
// exponential back-off (5 s, 15 s) and respects context cancellation between
// retries. If all attempts fail it logs at Error level and returns.
func (m *Manager) doProactiveRefresh(connID string, providerName Provider) {
	// Check context hasn't been cancelled (Manager closed).
	if m.refreshCtx.Err() != nil {
		return
	}
	m.providersMu.RLock()
	p, ok := m.providers[providerName]
	m.providersMu.RUnlock()
	if !ok {
		slog.Debug("connections: proactive refresh: provider not registered", "provider", providerName, "conn_id", connID)
		return
	}

	token, err := m.secrets.GetToken(connID)
	if err != nil {
		slog.Debug("connections: proactive refresh: no token", "conn_id", connID, "err", err)
		return
	}

	m.mu.Lock()
	redirectURL := m.redirectURL
	m.mu.Unlock()

	cfg := p.OAuthConfig(redirectURL)

	const maxAttempts = 3
	var newToken *oauth2.Token

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Wait between retries (skip wait on first attempt).
		if attempt > 0 {
			delay := proactiveRefreshBackoff[attempt-1]
			select {
			case <-m.refreshCtx.Done():
				return
			case <-time.After(delay):
			}
		}

		if m.refreshCtx.Err() != nil {
			return
		}

		ts := cfg.TokenSource(m.refreshCtx, token)
		newToken, err = ts.Token()
		if err == nil {
			break
		}
		slog.Warn("connection: proactive token refresh failed",
			"provider", providerName,
			"conn_id", connID,
			"attempt", attempt+1,
			"error", err)
	}

	if err != nil {
		slog.Error("connection: proactive token refresh exhausted; token will expire",
			"provider", providerName,
			"conn_id", connID)
		if updateErr := m.store.UpdateRefreshError(connID, err.Error()); updateErr != nil {
			slog.Warn("connections: proactive refresh: persist failure state", "conn_id", connID, "err", updateErr)
		}
		if m.onRefreshEvent != nil {
			m.onRefreshEvent("connection_token_refresh_failed", connID, providerName, err.Error())
		}
		return
	}

	if err := m.secrets.StoreToken(connID, newToken); err != nil {
		slog.Warn("connections: proactive refresh: store token failed", "conn_id", connID, "err", err)
		return
	}
	if err := m.store.UpdateExpiry(connID, newToken.Expiry); err != nil {
		slog.Warn("connections: proactive refresh: update expiry failed", "conn_id", connID, "err", err)
	}
	// Clear any previously recorded refresh error — this refresh succeeded.
	if err := m.store.UpdateRefreshError(connID, ""); err != nil {
		slog.Warn("connections: proactive refresh: clear failure state", "conn_id", connID, "err", err)
	}

	slog.Info("connections: proactively refreshed token", "conn_id", connID)
	if m.onRefreshEvent != nil {
		m.onRefreshEvent("connection_token_refreshed", connID, providerName, "")
	}

	// Schedule the next refresh cycle.
	m.scheduleProactiveRefresh(connID, providerName, newToken.Expiry)
}

// Close releases any resources held by the Manager, stops the background
// cleanup goroutine, and cancels any pending proactive refresh timers.
func (m *Manager) Close() {
	select {
	case <-m.done:
		// already closed
	default:
		close(m.done)
	}
	// Cancel the refresh context to abort any in-flight refresh calls.
	m.refreshCancel()
	// Stop all pending timers.
	m.refreshMu.Lock()
	for _, t := range m.refreshTimers {
		t.Stop()
	}
	m.refreshTimers = make(map[string]*time.Timer)
	m.refreshMu.Unlock()
}

// SetRedirectURL updates the OAuth callback URL used for new flows.
// Call this after the HTTP server has bound to its port.
func (m *Manager) SetRedirectURL(url string) {
	m.mu.Lock()
	m.redirectURL = url
	m.mu.Unlock()
}

// StartOAuthFlow initiates a PKCE OAuth flow for the given provider.
// It returns the URL the user should open in their browser.
//
// PKCE (RFC 7636):
//   - code_verifier: 32 random bytes, hex-encoded (64 chars)
//   - code_challenge: base64url(SHA-256(verifier)), no padding
//   - state: 16 random bytes, hex-encoded (32 chars)
func (m *Manager) StartOAuthFlow(p IntegrationProvider) (authURL string, err error) {
	m.mu.Lock()
	if len(m.pendingFlows) >= 1000 {
		m.mu.Unlock()
		return "", fmt.Errorf("oauth: server busy, too many pending flows")
	}
	redirectURL := m.redirectURL
	m.mu.Unlock()

	if err := validateRedirectURL(redirectURL); err != nil {
		return "", err
	}

	// Generate state token (16 random bytes, hex-encoded)
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("oauth: generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	// Generate PKCE code verifier (32 random bytes, hex-encoded)
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return "", fmt.Errorf("oauth: generate verifier: %w", err)
	}
	codeVerifier := hex.EncodeToString(verifierBytes)

	// PKCE code challenge: base64url(SHA-256(verifier)), no padding
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	cfg := p.OAuthConfig(m.redirectURL)

	url := cfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	m.mu.Lock()
	m.pendingFlows[state] = &pendingFlow{
		provider:      p,
		config:        cfg,
		codeVerifier:  codeVerifier,
		codeChallenge: codeChallenge,
		redirectURL:   m.redirectURL,
		expiresAt:     time.Now().Add(m.pendingFlowTTL),
	}
	m.purgeStalePendingFlows()
	m.mu.Unlock()

	return url, nil
}

// purgeStalePendingFlows removes pending OAuth flows that have exceeded their TTL.
// Must be called with m.mu held.
func (m *Manager) purgeStalePendingFlows() {
	now := time.Now()
	for state, flow := range m.pendingFlows {
		if now.After(flow.expiresAt) {
			delete(m.pendingFlows, state)
		}
	}
}

// verifyPKCE checks that the given PKCE code_verifier produces the expected
// code_challenge using the S256 method (SHA-256 + base64url, no padding).
//
//	challenge = BASE64URL(SHA256(verifier))
//
// Returns true when the verifier is valid. This is an additional defence-in-depth
// check performed on the server side before exchanging the authorization code;
// the authorization server also enforces PKCE independently.
func verifyPKCE(verifier, challenge string) bool {
	h := sha256.New()
	h.Write([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return computed == challenge
}

// HandleOAuthCallback completes the OAuth flow for the given state + code pair.
// Call this from your HTTP handler at GET /oauth/callback?state=...&code=...
// On success it stores the connection metadata and token, then returns the new Connection.
func (m *Manager) HandleOAuthCallback(ctx context.Context, state, code string) (Connection, error) {
	m.mu.Lock()
	flow, ok := m.pendingFlows[state]
	if ok {
		delete(m.pendingFlows, state)
	}
	m.mu.Unlock()

	if !ok {
		return Connection{}, fmt.Errorf("oauth: unknown or expired state %q", state)
	}
	if time.Now().After(flow.expiresAt) {
		return Connection{}, fmt.Errorf("oauth: state %q has expired", state)
	}

	// PKCE server-side verification (defence-in-depth):
	// Confirm the stored code_verifier reproduces the code_challenge that was
	// sent in the authorization request. This detects any tampering with the
	// pending flow's stored state before the code is exchanged.
	if !verifyPKCE(flow.codeVerifier, flow.codeChallenge) {
		slog.Warn("oauth: PKCE verification failed", "state", state)
		return Connection{}, fmt.Errorf("oauth: PKCE verification failed")
	}

	// Exchange code for token, supplying the PKCE verifier
	token, err := flow.config.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", flow.codeVerifier),
	)
	if err != nil {
		return Connection{}, fmt.Errorf("oauth: token exchange: %w", err)
	}

	// Fetch account info using the new token
	httpClient := flow.config.Client(ctx, token)
	info, err := flow.provider.GetAccountInfo(ctx, httpClient)
	if err != nil {
		return Connection{}, fmt.Errorf("oauth: get account info: %w", err)
	}

	connID := uuid.New().String()
	conn := Connection{
		ID:           connID,
		Provider:     flow.provider.Name(),
		AccountLabel: info.Label,
		AccountID:    info.ID,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    token.Expiry,
	}

	if err := m.store.Add(conn); err != nil {
		return Connection{}, fmt.Errorf("oauth: store connection: %w", err)
	}

	if err := m.secrets.StoreToken(connID, token); err != nil {
		// Best-effort cleanup of the store entry
		_ = m.store.Remove(connID)
		return Connection{}, fmt.Errorf("oauth: store token: %w", err)
	}

	// Schedule proactive token rotation before the token expires.
	m.scheduleProactiveRefresh(connID, conn.Provider, token.Expiry)

	return conn, nil
}

// GetHTTPClient returns an *http.Client for the given connection ID.
// The client automatically refreshes the access token when it expires.
func (m *Manager) GetHTTPClient(ctx context.Context, connID string, p IntegrationProvider) (*http.Client, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("oauth: connection %q not found", connID)
	}

	token, err := m.secrets.GetToken(connID)
	if err != nil {
		return nil, fmt.Errorf("oauth: get token for %q: %w", connID, err)
	}

	cfg := p.OAuthConfig(m.redirectURL)
	ts := cfg.TokenSource(ctx, token)

	// Wrap in a token-refreshing client that also persists refreshed tokens
	client := oauth2.NewClient(ctx, &persistingTokenSource{
		inner:   ts,
		connID:  connID,
		store:   m.store,
		secrets: m.secrets,
	})

	_ = conn // conn validated above; keeping reference for future use
	return client, nil
}

// RemoveConnection deletes the connection metadata and its stored token.
func (m *Manager) RemoveConnection(connID string) error {
	if err := m.store.Remove(connID); err != nil {
		return fmt.Errorf("oauth: remove connection %q: %w", connID, err)
	}
	// Delete the token; ignore "not found" errors (best-effort cleanup)
	_ = m.secrets.DeleteToken(connID)
	_ = m.secrets.DeleteCredentials(connID)
	// Cancel any pending proactive refresh timer.
	m.refreshMu.Lock()
	if t, ok := m.refreshTimers[connID]; ok {
		t.Stop()
		delete(m.refreshTimers, connID)
	}
	m.refreshMu.Unlock()
	return nil
}

// SetDefaultConnection makes the given connection the first (default) for its provider.
func (m *Manager) SetDefaultConnection(connID string) error {
	return m.store.SetDefault(connID)
}

// StoreExternalToken saves an OAuth token received from the HuginnCloud broker relay.
// It creates a new connection entry for the given provider.
func (m *Manager) StoreExternalToken(ctx context.Context, provider Provider, token *oauth2.Token, accountLabel string) error {
	connID := uuid.New().String()
	conn := Connection{
		ID:           connID,
		Provider:     provider,
		Type:         ConnectionTypeOAuth,
		AccountLabel: accountLabel,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    token.Expiry,
	}
	if err := m.store.Add(conn); err != nil {
		return fmt.Errorf("connections: save: %w", err)
	}
	if err := m.secrets.StoreToken(connID, token); err != nil {
		// Best-effort cleanup of the store entry
		_ = m.store.Remove(connID)
		return fmt.Errorf("connections: store token: %w", err)
	}
	m.scheduleProactiveRefresh(connID, provider, token.Expiry)
	return nil
}

// StoreExternalTokenWithMeta saves an OAuth token with additional metadata.
// It is like StoreExternalToken but also persists key/value metadata on the connection.
func (m *Manager) StoreExternalTokenWithMeta(ctx context.Context, provider Provider, token *oauth2.Token, accountLabel string, meta map[string]string) error {
	connID := uuid.New().String()
	conn := Connection{
		ID:           connID,
		Provider:     provider,
		Type:         ConnectionTypeOAuth,
		AccountLabel: accountLabel,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    token.Expiry,
		Metadata:     meta,
	}
	if err := m.store.Add(conn); err != nil {
		return fmt.Errorf("connections: save: %w", err)
	}
	if err := m.secrets.StoreToken(connID, token); err != nil {
		_ = m.store.Remove(connID)
		return fmt.Errorf("connections: store token: %w", err)
	}
	m.scheduleProactiveRefresh(connID, provider, token.Expiry)
	return nil
}

// StoreAPIKeyConnection saves an API-key or bearer-token connection.
// label is the display name shown in the UI (e.g. "prod-us1" or "My Splunk").
// metadata contains service-specific config (e.g. "url" → base URL).
// creds contains the secret key/value pairs (e.g. "api_key", "app_key").
func (m *Manager) StoreAPIKeyConnection(provider Provider, label string, metadata map[string]string, creds map[string]string) (Connection, error) {
	connID := uuid.New().String()
	conn := Connection{
		ID:           connID,
		Provider:     provider,
		Type:         ConnectionTypeAPIKey,
		AccountLabel: label,
		CreatedAt:    time.Now().UTC(),
		Metadata:     metadata,
	}
	if err := m.store.Add(conn); err != nil {
		return Connection{}, fmt.Errorf("api_key: store connection: %w", err)
	}
	if err := m.secrets.StoreCredentials(connID, creds); err != nil {
		_ = m.store.Remove(connID)
		return Connection{}, fmt.Errorf("api_key: store credentials: %w", err)
	}
	return conn, nil
}

// GetCredentials returns the stored credentials for an API-key connection.
func (m *Manager) GetCredentials(connID string) (map[string]string, error) {
	_, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("connection %q not found", connID)
	}
	return m.secrets.GetCredentials(connID)
}

// persistingTokenSource wraps an oauth2.TokenSource and persists refreshed
// tokens back to the SecretStore and Store so expiry stays current.
type persistingTokenSource struct {
	inner   oauth2.TokenSource
	connID  string
	store   StoreInterface
	secrets SecretStore
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	// On-demand token refresh: retry up to 2 attempts with a 1-second delay.
	// This guards against transient network hiccups during mid-request token
	// refresh without introducing unbounded retries that could stall a request.
	const onDemandMaxAttempts = 2
	const onDemandRetryDelay = time.Second

	var (
		t   *oauth2.Token
		err error
	)
	for attempt := 0; attempt < onDemandMaxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(onDemandRetryDelay)
		}
		t, err = p.inner.Token()
		if err == nil {
			break
		}
		if attempt == 0 {
			slog.Warn("connections: on-demand token refresh failed, retrying",
				"conn_id", p.connID, "attempt", attempt+1, "err", err)
		}
	}
	if err != nil {
		slog.Error("connections: on-demand token refresh exhausted; request will fail",
			"conn_id", p.connID, "err", err)
		return nil, err
	}
	// Persist the (potentially refreshed) token
	if err := p.secrets.StoreToken(p.connID, t); err != nil {
		slog.Warn("connections: refreshed token could not be persisted; will need to re-authenticate on restart",
			"conn_id", p.connID, "err", err)
	}
	if err := p.store.UpdateExpiry(p.connID, t.Expiry); err != nil {
		slog.Warn("connections: token expiry update failed",
			"conn_id", p.connID, "err", err)
	}
	return t, nil
}
