# Connections

**Files**: `internal/connections/`, `internal/server/handlers_connections.go`, `internal/server/handlers_cli.go`
**Related**: [integrations.md](integrations.md)

---

## Overview

Huginn connects to external services through two distinct categories of integrations, each solving a different authentication and credential management problem.

| Category | Examples | Auth Method | Credentials Stored | User Action |
|---|---|---|---|---|
| **OAuth Integrations** | Google, Slack, Jira, Bitbucket | OAuth2 PKCE | In system keychain | Click "Connect" → browser login |
| **CLI Integrations** | GitHub (`gh`), AWS (`aws`), Google Cloud (`gcloud`) | Native CLI auth | None | Install CLI → run `auth login` |

Both categories are exposed to the LLM through the tool system — OAuth tokens become HTTP clients passed to tool implementations, while CLI tools are detected and exposed as fully available if the user has already authenticated them locally.

---

## OAuth Integrations

### The Problem

OAuth providers (Google, Slack, Jira, Bitbucket) require:
- A configured OAuth app (client_id and client_secret) registered with the provider
- A way to authenticate users without storing their passwords
- Token refresh so that connections don't expire unexpectedly
- Secure storage of access and refresh tokens

Huginn solves this with a full PKCE OAuth2 implementation that:
1. Initiates flows locally or via the HuginnCloud broker
2. Stores metadata in a JSON file and tokens in the OS keychain (or in-memory for CI)
3. Automatically refreshes tokens when they expire
4. Exposes authenticated HTTP clients to tools that need them

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Client Browser                        │
│  (user clicks "Connect" button in web UI)                   │
└──────────────────────┬──────────────────────────────────────┘
                       │ POST /api/v1/connections/start
                       │ {"provider": "google"}
                       ▼
┌──────────────────────────────────────────────────────────────┐
│  Server HTTP Handler: handleStartOAuth                       │
│  - Checks if BrokerClient is set                            │
│  - Routes to local flow or broker flow                      │
└──────────────────┬───────────────────────────────────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼ (no broker)         ▼ (has broker)
   LOCAL FLOW             BROKER FLOW
        │                     │
        │                 Generate relay_secret
        │                 Compute relay_challenge =
        │                   base64url(SHA-256(relay_secret))
        │                 Open ephemeral listener on :0
        │                 POST /oauth/start to broker
        │                     │
        │                     ▼
        │              Broker returns auth_url
        │                     │
        │              (user logs in with provider)
        │                     │
        │              Broker redirects to
        │              http://127.0.0.1:PORT/oauth/relay?token=...
        │                     │
        │                     ▼
        │              Ephemeral server verifies relay JWT
        │              using relay_challenge as HMAC key
        │              (connection stored, server closes)
        │                     │
        ▼                     ▼
    Manager.StartOAuthFlow   Manager.StoreExternalTokenWithMeta
    ↓                        ↓
    Generate state token     (upsert to Store)
    Generate PKCE verifier   (store token in SecretStore)
    ↓
    OAuth provider redirects to /oauth/callback?state=...&code=...
    ↓
    handleOAuthCallback → Manager.HandleOAuthCallback
    ↓
    Exchange code + PKCE verifier for tokens
    ↓
    Store metadata in connStore (JSON)
    Store tokens in secrets (Keychain/Memory)
    ↓
    Browser redirected to /#/connections?connected=google
```

### OAuth Broker Relay Flow

When a BrokerClient is set (e.g., `HuginnCloud`), OAuth flows delegate to an external broker for three reasons:

1. **Simplified cert management**: The broker has a real DNS name; local machines often run on `localhost:PORT` which can't have HTTPS certs.
2. **Multi-device support**: Phones and tablets can authenticate via the broker even if Huginn is running on a desktop.
3. **Centralized OAuth app**: The broker hosts one certified app per provider; users don't each need to create their own.

The relay handshake is cryptographic:

- **relay_secret** (32 random bytes) is generated and held only by the local instance.
- **relay_challenge** (base64url SHA-256(secret)) is sent to the broker.
- The broker authenticates the user and returns a JWT signed with the challenge as the HMAC key.
- The local ephemeral server verifies the JWT signature — only the instance that created the challenge can read it.
- Tokens are never sent to the broker or exposed in transit; only a signed relay JWT is returned.

Metadata includes `relay_challenge` so that future token refreshes can also be validated by the same broker (see `StoreExternalTokenWithMeta`).

### Token Storage

Tokens are persisted in two layers:

| Layer | What | Where | Failure Mode |
|---|---|---|---|
| **Store** | Connection metadata (ID, provider, account label, scopes, expiry) | `~/.huginn/connections.json` | Atomic JSON write with temp-file pattern |
| **SecretStore** | OAuth tokens (access_token, refresh_token, expiry) | OS keychain or in-memory map | Keychain backend depends on OS availability |

The **SecretStore interface** has two implementations:

- **KeychainStore** (default): Uses `github.com/zalando/go-keyring` to access macOS Keychain, GNOME Keyring, or Windows Credential Manager.
- **MemoryStore**: Falls back automatically if the keychain is unavailable (CI, Docker, SSH sessions).

`NewSecretStore()` probes the keychain by attempting a test write; if it fails, it returns a MemoryStore instead.

### Token Refresh

When tools need an HTTP client for an OAuth connection:

```go
client, err := m.GetHTTPClient(ctx, connID, provider)
```

The manager wraps the underlying token source with `persistingTokenSource`, which:
1. Asks the provider for a refreshed token if the current one is expired
2. Persists the refreshed token back to both the SecretStore and Store
3. Returns an *http.Client that automatically handles refresh on each request

Token refresh is best-effort: if persistence fails, the request still succeeds (the token in memory is still good). Failures are logged.

### Adding a New OAuth Provider

1. Create a provider implementation in `internal/connections/providers/`:
   ```go
   type GoogleProvider struct{}

   func (p *GoogleProvider) Name() Provider { return ProviderGoogle }
   func (p *GoogleProvider) DisplayName() string { return "Google" }
   func (p *GoogleProvider) OAuthConfig(redirectURL string) *oauth2.Config { ... }
   func (p *GoogleProvider) GetAccountInfo(ctx context.Context, client *http.Client) (*AccountInfo, error) { ... }
   ```

2. Implement the `IntegrationProvider` interface (defined in `internal/connections/oauth.go`):
   - **Name()**: Return the Provider constant (e.g., `ProviderGoogle`)
   - **DisplayName()**: Return the human-readable name (e.g., "Google")
   - **OAuthConfig(redirectURL)**: Return the `*oauth2.Config` with client_id, client_secret, and auth/token endpoints
   - **GetAccountInfo(ctx, client)**: Fetch the authenticated user's account info (ID, label, display name, avatar URL)

3. Register the provider in your server setup (typically `cmd/huginn/main.go`):
   ```go
   providers := []connections.IntegrationProvider{
       &providers.GoogleProvider{},
       &providers.SlackProvider{},
       // ... more
   }
   connMgr := connections.NewManager(store, secrets, redirectURL)
   server := server.New(cfg, orch, sessionStore, token, huginnDir, connMgr, connStore, providers)
   ```

4. Add it to the `knownProviders` list in `internal/server/handlers_connections.go` so the UI knows about it:
   ```go
   {
       Name: "myservice", DisplayName: "My Service", Icon: "MS",
       Description: "My service integration",
       Scopes: []string{"scope1", "scope2"},
       MultiAccount: true,
   }
   ```

5. Create tools (in `internal/connections/tools/`) that use the OAuth client to make authenticated requests:
   ```go
   func SendMessage(ctx context.Context, m *connections.Manager, connID, message string) error {
       p := &providers.SlackProvider{}
       client, err := m.GetHTTPClient(ctx, connID, p)
       if err != nil { return err }
       // Use client to call Slack API
       return nil
   }
   ```

---

## CLI Integrations

### The Problem

Some tools (GitHub, AWS, Google Cloud) are complex CLIs that users install and authenticate independently on their local machine.

Instead of reimplementing OAuth for each one, Huginn:
- **Detects** which CLIs are installed and authenticated
- **Exposes** them directly to tools via the BashTool (full shell access)
- **Reports** auth status and account info in the UI

No credentials are stored by Huginn because the CLIs manage their own state (SSH keys, token files in `~/.config`, etc.).

### Architecture

```
┌──────────────────────────────────────────────────────────┐
│         Client Browser: GET /api/v1/cli/status          │
└────────────────────────┬─────────────────────────────────┘
                         │
                         ▼
           ┌─────────────────────────────┐
           │  handleCLIStatus (handler)  │
           │  For each CLI tool:         │
           │    - Run detect function    │
           │    - Collect results        │
           └─────────┬───────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
        ▼            ▼            ▼
     detectGH    detectAWS   detectGCloud
        │            │            │
        │ exec.Cmd    │            │
        │            │            │
        ▼            ▼            ▼
   gh --version   aws --version  gcloud --version
   gh auth status aws sts get... gcloud auth list
        │            │            │
        └────────────┼────────────┘
                     │
                     ▼
         ┌────────────────────────────┐
         │ cliToolStatus objects      │
         │ []{                        │
         │   name, version,           │
         │   authenticated,           │
         │   account, icon_color, ... │
         │ }                          │
         │                            │
         │ JSON response to browser   │
         └────────────────────────────┘
```

### Detection Logic

Each CLI tool has a `cliToolDef` with a `detect` function that:

1. Checks if the binary is in $PATH using `exec.LookPath()`
2. If installed, runs a status command to check authentication
3. Extracts version and account info from the output
4. Returns (version string, account string, authenticated bool)

Example: GitHub detection (from `detectGH`):

```go
func detectGH() (version, account string, authenticated bool) {
	if out, err := runCLI("gh", "--version"); err == nil {
		// "gh version 2.45.0 (2024-01-15)"
		if parts := strings.Fields(out); len(parts) >= 3 {
			version = parts[2]
		}
	}
	if out, err := runCLI("gh", "auth", "status"); err == nil {
		authenticated = true
		// Parse account from output
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "account") {
				// Extract account name
			}
		}
	}
	return
}
```

Each CLI command has a 5-second timeout (see `runCLI`). Timeouts are treated as "not authenticated" — if the CLI hangs, it doesn't block the UI.

### Why CLI Instead of OAuth

CLI tools are not exposed through OAuth integrations for several reasons:

1. **No credential storage**: CLIs manage their own secrets. AWS stores credentials in `~/.aws/config`; GitHub stores SSH keys and tokens in `~/.ssh` and `~/.config/gh`; Google Cloud uses `~/.config/gcloud`. Huginn doesn't need (and shouldn't) access these.

2. **Distributed auth**: Users already have these tools installed and authenticated for other use cases (local development, CI/CD, etc.). Reusing existing auth is simpler than adding another OAuth flow.

3. **Full shell access anyway**: The BashTool gives the LLM arbitrary shell execution, so the CLIs are already available if installed. Exposing them here is just for discovery and status reporting.

4. **Complex per-provider setup**: Each CLI has different auth mechanisms. GitHub uses SSH + personal access tokens; AWS uses IAM credentials; GCP uses service accounts and user identity. Each would need custom OAuth integration code.

---

## API Surface

All endpoints are under `/api/v1/` and require the `Authorization: Bearer <token>` header (see `internal/server/middleware.go`).

| Method | Endpoint | Handler | Body | Response | Notes |
|---|---|---|---|---|---|
| GET | `/connections` | `handleListConnections` | — | `[]Connection` | List all stored OAuth connections |
| POST | `/connections/start` | `handleStartOAuth` | `{"provider": "google"}` | `{"auth_url": "..."}` | Start OAuth flow; returns URL to open in browser |
| GET | `/oauth/callback` | `handleOAuthCallback` | — | Redirect | OAuth provider redirect target; completes flow, redirects to SPA |
| GET | `/oauth/relay` | `serveEphemeralRelay` | — | HTML | Broker relay target; verifies JWT, stores connection, closes server |
| DELETE | `/connections/{id}` | `handleDeleteConnection` | — | `{"deleted": true}` | Remove a connection and its stored tokens |
| GET | `/providers` | `handleListProviders` | — | `[]ProviderMeta` | List known providers + whether they're configured |
| GET | `/cli/status` | `handleCLIStatus` | — | `[]CLIToolStatus` | Detect installed CLIs + auth status |

---

## Why This Design?

### Separation of Categories

Keeping OAuth and CLI integrations separate in the code (different packages, different handlers) makes sense because:

- **OAuth** requires managing credentials and state. It has a full lifecycle: start, authenticate, store, refresh, delete.
- **CLI** requires only detection and reporting. There's no state to manage.

Mixing them would force CLI tools to implement credential storage they don't need.

### PKCE + Relay for Security

PKCE (RFC 7636) protects against authorization code interception attacks. The relay JWT signing prevents man-in-the-middle attacks on the broker path:

- Only the instance that generated `relay_secret` can verify a relay JWT because only that instance knows the HMAC key.
- The secret never leaves the local machine.
- The broker never has access to user tokens or the secret.

### Keychain Fallback

Probing the keychain at startup and falling back to MemoryStore means:

- **Local machines** use the OS keychain (secure, persistent, integrated).
- **CI/Docker/SSH** use in-memory storage (works without a keychain).
- No configuration required; the right backend is chosen automatically.

### No Token Polling

Tokens are refreshed **lazily** when needed (in the `persistingTokenSource`), not polled in the background. This means:

- If a token expires and the connection is never used again, we don't waste work refreshing it.
- If a tool makes 10 requests in quick succession, only the first request triggers a refresh (subsequent requests reuse the in-memory token).
- Refreshed tokens are persisted to survive restarts.

---

## Limitations

1. **No multi-device refresh**: CLI tools depend on local file state. If a user authenticates on one machine and runs Huginn on another, the CLIs won't work on the second machine.

2. **Keychain not available on all systems**: Some headless systems (containers, SSH sessions) have no keychain. MemoryStore works but tokens don't persist.

3. **No token expiry warnings**: Huginn doesn't proactively alert when OAuth tokens are expiring soon. Failures only surface when tools actually try to use them.

4. **CLI detection is ambient**: The UI queries CLI status on-demand, but there's no notification if the user authenticates a new CLI after Huginn starts. A page refresh is needed.

5. **No scope negotiation**: OAuth scopes are static per provider. Users can't request additional scopes at runtime.

6. **Broker coupling**: When using the HuginnCloud broker, token refresh and relay verification depend on the broker being available. If the broker goes down, refreshes fail (but the existing token is still usable in-memory).

---

## See Also

- [integrations.md](integrations.md) — MCP servers, skills, WebSocket relay
- [relay-protocol.md](relay-protocol.md) — WebSocket message format
- [permissions-and-safety.md](permissions-and-safety.md) — Tool authorization
- `golang.org/x/oauth2` — Official Go OAuth2 library
- `github.com/zalando/go-keyring` — Cross-platform keychain access
