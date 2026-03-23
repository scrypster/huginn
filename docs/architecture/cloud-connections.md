# Cloud OAuth Connections

**Files**: `internal/connections/broker/`, `internal/server/handlers_connections.go`, `internal/server/handlers_ephemeral_relay.go`, `internal/server/relay_keys.go`
**Related**: [connections.md](connections.md), [relay-protocol.md](relay-protocol.md)

---

## Overview

Huginn supports two OAuth modes depending on whether the satellite is registered with HuginnCloud.

| Mode | When | Who handles the OAuth app | Token delivery |
|---|---|---|---|
| **Local** | No broker client configured | Developer provides own client_id/client_secret | OAuth provider redirects to local callback on `localhost` |
| **Cloud (broker-mediated)** | Satellite is registered with HuginnCloud | HuginnCloud broker hosts a certified app per provider | Relay JWT delivered via WebSocket relay |

The routing decision happens in `handleStartOAuth`:

```go
// Broker path: if machine is registered with HuginnCloud, use broker flow.
if broker != nil && isKnownProvider(body.Provider) {
    s.handleStartOAuthBroker(w, r, body.Provider)
    return
}
```

Within the broker path, a further check on `s.satellite != nil` determines whether to use the cloud-UI relay flow (this document) or the older local-relay ephemeral-server flow. Both are broker-mediated; the cloud-UI flow is used when the satellite has an active WebSocket relay connection to HuginnCloud.

---

## Full Cloud OAuth Flow

When a satellite is registered with HuginnCloud, the OAuth flow runs entirely in the cloud app popup and the resulting tokens arrive back at the satellite via the WebSocket relay.

```
 Satellite (local Huginn)              HuginnCloud API               OAuth Provider
         │                                    │                              │
  1.     │── POST /oauth/start ──────────────>│                              │
         │   {provider, relay_key}            │                              │
         │   Authorization: Bearer <machine JWT>                             │
         │                                    │                              │
  2.     │<── {auth_url} ────────────────────│                              │
         │                                    │                              │
  3.     │   [store relay_key in memory]       │                              │
         │   [return auth_url to cloud app]    │                              │
         │                                    │                              │
         │                       Cloud app opens popup at auth_url            │
         │                                    │                              │
  4.     │                       GET /oauth/authorize?state=...              │
         │                                    │── 302 redirect ─────────────>│
         │                                    │                              │
  5.     │                                    │                   user logs in│
         │                                    │<── GET /oauth/callback?code= │
         │                                    │                              │
  6.     │                                    │   [ClaimBrokerState(state)]  │
         │                                    │   [exchange code for tokens] │
         │                                    │   [sign relay JWT with       │
         │                                    │    relay_key as HMAC key]    │
         │                                    │   [delete state from DynamoDB]
         │                                    │                              │
  7.     │                       <HTML: window.opener.postMessage({          │
         │                          type:"oauth_complete",                   │
         │                          relay_jwt: "..."                         │
         │                       }, "https://app.huginncloud.com")>          │
         │                                    │                              │
  8.     │<── WebSocket relay: relay_jwt ─────│                              │
         │   (cloud app sends relay JWT via   │                              │
         │    existing WS connection)         │                              │
         │                                    │                              │
  9.     │   [POST /api/v1/connections/        │                              │
         │    oauth/relay]                    │                              │
         │   [claimRelayKey(provider)]        │                              │
         │   [ParseRelayJWT(jwt, relayKey)]   │                              │
         │   [connMgr.StoreExternalToken]     │                              │
         │   [OS Keychain write]              │                              │
```

### Step-by-step

**Step 1 — Satellite calls `/oauth/start`.**
`startOAuthViaCloudBroker` generates 32 cryptographically random bytes, base64url-encodes them as `relay_key`, and POSTs them to the broker along with the provider name. The request is authenticated with the satellite's machine JWT (`Authorization: Bearer <machine JWT>`).

**Step 2 — Broker stores state and returns `auth_url`.**
`OAuthBrokerHandler.Start` validates the relay_key (must decode to exactly 32 bytes), generates a 16-byte hex `state` token, stores `{state → machineID, provider, relay_key}` in DynamoDB with a 10-minute TTL, and returns `https://api.huginncloud.com/oauth/authorize?state=<state>`.

**Step 3 — Satellite stores the relay_key.**
`storeRelayKey(provider, relayKey)` saves the key in `s.relayKeys` (an in-memory map protected by `relayKeysMu`). The key is keyed by provider name and will be claimed exactly once.

**Step 4 — Cloud app opens popup.**
The cloud app opens the returned `auth_url` in a popup window. `Authorize` looks up the state in DynamoDB, loads provider credentials from Secrets Manager, builds the provider-specific authorization URL, and 302-redirects the popup to the OAuth provider.

**Step 5 — User authenticates.**
The user approves the OAuth app at the provider's login page. The provider redirects back to `BROKER_CALLBACK_URL`.

**Step 6 — Broker exchanges code and signs relay JWT.**
`Callback` atomically claims the DynamoDB state using a conditional delete (preventing replay). It exchanges the authorization code for tokens using the provider's token endpoint, then calls `signRelayJWT(relayKey, provider, accessToken, refreshToken, accountLabel, expiry)`. The relay JWT is signed with HMAC-SHA256 using the raw 32-byte relay_key. The JWT expires in 5 minutes.

**Step 7 — Relay JWT returned to cloud app via postMessage.**
The callback response is an HTML page that calls `window.opener.postMessage({type:"oauth_complete", provider, relay_jwt}, "https://app.huginncloud.com")` with an explicit targetOrigin and then closes itself.

**Step 8 — Cloud app forwards relay JWT to satellite.**
The cloud app receives the postMessage event and forwards the relay JWT to the satellite via the existing WebSocket relay connection.

**Step 9 — Satellite verifies and stores tokens.**
`handleOAuthRelayFromCloud` parses the JWT header without verification to extract the `provider` claim (used only to look up the correct relay_key). It then calls `claimRelayKey(provider)` — consuming and removing the stored key — and calls `broker.ParseRelayJWT(jwt, relayKey)` to perform full HMAC-SHA256 signature and expiry verification. On success, `connMgr.StoreExternalToken` writes the tokens to the OS Keychain.

---

## relay_key Security Scheme

### What it is

The `relay_key` is a 32-byte cryptographically random value generated by the satellite at the start of each OAuth flow. It serves as a pre-shared HMAC signing key between the satellite and the broker.

It is **not PKCE**. PKCE (RFC 7636) uses a verifier/challenge pair to bind an authorization code to the client that initiated it, and the challenge is sent to the authorization server. The relay_key scheme solves a different problem: it binds the relay JWT (which carries tokens) to the specific satellite that initiated the flow, without the satellite ever receiving the tokens directly from the broker over the network.

### How it is generated

```go
keyBytes := make([]byte, 32)
if _, err := rand.Read(keyBytes); err != nil {
    return "", fmt.Errorf("cloud broker: generate relay key: %w", err)
}
relayKey := base64.RawURLEncoding.EncodeToString(keyBytes)
```

`crypto/rand.Read` produces 32 bytes from the OS CSPRNG. The base64url encoding produces a 43-character string sent to the broker.

### Why it binds the JWT to the originating satellite

The relay_key is the HMAC signing key for the relay JWT. Because only the originating satellite generated and knows this key:

- No other satellite or process can produce a valid relay JWT for this flow.
- The cloud app (browser JS) receives the relay JWT but cannot forge a new one — it never has access to the relay_key.
- Even if the relay JWT is intercepted in transit (e.g., captured in browser memory or network), it cannot be replayed against a different satellite because the target satellite will not have a matching relay_key for that provider.

The broker holds the relay_key in DynamoDB only long enough to sign the JWT (10-minute TTL on the state record), and the record is atomically deleted when the callback claims it.

### Storage of the relay_key

On the satellite, the relay_key is stored in `s.relayKeys` (keyed by provider name) in process memory only. It is never written to disk, the keychain, or any external system. `claimRelayKey` deletes it from the map on first use.

---

## Token Storage Guarantees

Tokens (access_token and refresh_token) **never touch HuginnCloud infrastructure**. The sequence is:

1. The OAuth provider sends tokens to the HuginnCloud broker callback endpoint.
2. The broker immediately wraps them in a signed relay JWT and writes the JWT to the callback HTML response.
3. The relay JWT travels: callback HTML → browser JS (postMessage) → cloud app JS → WebSocket relay → satellite process memory.
4. The satellite's `handleOAuthRelayFromCloud` calls `connMgr.StoreExternalToken`, which writes to the OS Keychain via `go-keyring`.
5. The relay JWT is discarded; no token values are logged or persisted by the broker.

DynamoDB stores only the flow metadata (state token, machineID, provider, relay_key) — never any OAuth token values.

---

## Threat Model

### Protected

| Threat | Mitigation |
|---|---|
| **Replay attacks** | DynamoDB state is atomically deleted on first claim (`ConditionalCheckFailedException` on duplicate). A second callback request for the same state returns `ErrAlreadyClaimed`. |
| **Token interception on broker** | Tokens are wrapped in a relay JWT before leaving the broker callback. The JWT is signed with relay_key; only the originating satellite can verify it. |
| **CSRF on callback endpoint** | The `state` parameter is 16 bytes of random hex (32 hex chars = 128 bits of entropy). It is impossible to predict or forge. |
| **JWT replay to wrong satellite** | Each satellite's relay_key is unique per flow and stored in-memory only. A relay JWT signed with one satellite's relay_key will fail verification on any other satellite. |
| **Forged postMessage from attacker page** | The callback HTML uses an explicit `targetOrigin` of `"https://app.huginncloud.com"` in `postMessage`. The cloud app should validate the `origin` field of incoming message events (enforcement is on the app side). |
| **Stale relay_key** | The relay JWT has a 5-minute expiry (`exp` claim). `ParseRelayJWT` uses `jwt.WithExpirationRequired()`, which rejects tokens without an `exp` claim and tokens where `exp` has passed. |

### Accepted risks

| Risk | Notes |
|---|---|
| **relay_jwt briefly in browser JS heap** | The relay JWT passes through the cloud app's JavaScript between the postMessage event and the WebSocket send. It is not persisted to localStorage or any browser storage, but it exists in memory and may appear in browser devtools or crash reports. |
| **refresh_token traverses WebSocket relay in memory** | On subsequent token refreshes (via `POST /oauth/refresh`), the refresh_token is sent from satellite to broker over TLS but passes through broker process memory. The broker does not log it. |
| **relay_key in DynamoDB TTL window** | The relay_key is stored in DynamoDB for up to 10 minutes (or until claimed). DynamoDB encryption at rest protects it; access is limited to the broker's IAM role. |
| **Broker availability** | If the broker is unavailable, `POST /oauth/refresh` calls fail. The in-memory access token remains usable until it expires; after that, the connection must be re-established manually. |

---

## Retry Semantics

If `ParseRelayJWT` fails (e.g., the relay JWT arrived malformed, or the JWT expired in transit), `handleOAuthRelayFromCloud` re-stores the relay_key before returning the error:

```go
result, err := broker.ParseRelayJWT(body.RelayJWT, relayKey)
if err != nil {
    // Re-store the relay_key since validation failed — the flow may retry.
    s.storeRelayKey(providerHint, relayKey)
    jsonError(w, 401, "invalid relay_jwt: "+err.Error())
    return
}
```

This means the user can retry the OAuth flow from the cloud app popup without needing to start a fresh `POST /oauth/start` call. The same relay_key is still available for the next attempt.

Note that if the relay_key was consumed by a successful but subsequent-step-failing scenario, a new flow must be started. The relay_key is only re-stored on parse/validation failure, not on keychain write failure.

---

## Provider Support

| Provider | Available via broker | Available locally | Notes |
|---|---|---|---|
| Google | Yes | Yes (requires own OAuth app) | Broker requests `offline_access` scope for refresh tokens |
| GitHub | Yes | Yes (requires own OAuth app) | — |
| Slack | Yes | Yes (requires own OAuth app) | Slack does not support refresh token rotation; `POST /oauth/refresh` returns an error for Slack |
| Jira | Yes | Yes (requires own OAuth app) | Uses Atlassian identity platform |
| Bitbucket | Yes | Yes (requires own OAuth app) | Uses Atlassian identity platform |

All five providers are listed in `supportedProviders` in `oauth_broker.go` and in `knownProviders` in `handlers_connections.go`. A provider is shown as "Configured" in the UI if either a local `IntegrationProvider` is registered for it or a broker client is present.

---

## Differences from Local Flow

| Aspect | Local flow | Cloud broker flow |
|---|---|---|
| OAuth app | Developer's own (`client_id`/`client_secret` in config) | HuginnCloud-hosted certified app |
| Callback target | `http://localhost:PORT/oauth/callback` | `BROKER_CALLBACK_URL` (e.g., `https://api.huginncloud.com/oauth/callback`) |
| Local server | Ephemeral listener on `:0`, closed after one use | None; tokens arrive via WebSocket relay |
| Token delivery | OAuth provider → local callback → connMgr | OAuth provider → broker → relay JWT → WS relay → satellite |
| Requires registration | No | Yes (`huginn cloud register`) |
| Multi-device | No (localhost only) | Yes (satellite receives tokens regardless of which browser initiated the popup) |

When a broker is configured but the satellite has no active relay connection (`s.satellite == nil`), `handleStartOAuthBroker` falls back to `startOAuthViaBroker`, which uses the older local-relay ephemeral server pattern (see `handlers_ephemeral_relay.go`). In this mode the broker still provides the OAuth app, but token delivery uses a local HTTP listener instead of the WebSocket relay.

---

## See Also

- [connections.md](connections.md) — OAuth architecture, token storage, local flow details
- [relay-protocol.md](relay-protocol.md) — WebSocket message format and connection lifecycle
- `internal/connections/broker/relay.go` — `ParseRelayJWT` implementation
- `internal/server/relay_keys.go` — `storeRelayKey` / `claimRelayKey`
- `internal/server/handlers_ephemeral_relay.go` — `startOAuthViaCloudBroker`, `handleStartOAuthBroker`
- HuginnCloud API: `internal/api/oauth_broker.go` — broker endpoints
- HuginnCloud API: `internal/store/brokerstates.go` — DynamoDB state store
