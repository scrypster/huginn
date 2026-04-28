# SendGrid Connection Provider — Design Spec

**Date:** 2026-04-28
**Status:** Draft
**Phase:** 3 — Desktop UX Polish

---

## Problem

Huginn's delivery pipeline already references SendGrid credentials in comments (`"api_key", "from" for SendGrid` in `internal/scheduler/delivery.go`) but has no actual SendGrid provider. Agents that want to send email must use SMTP, which requires a mail server — a higher barrier than a SendGrid API key. Adding SendGrid as a first-class connection provider closes this gap with a single-field setup.

---

## Scope

**In scope:**
- Catalog entry in `catalog.json` (api_key + from address + optional from_name)
- `sendGridDeliverer` implementation in `internal/scheduler/delivery.go`
- Register deliverer in the registry so workflow `sendgrid` steps use it when a SendGrid connection is referenced
- Connection validator (API key validation via SendGrid v3 API)

**Out of scope:**
- `ProviderSendGrid` constant in `connection.go` — SendGrid is a `credentials`-type provider, not an OAuth integration provider. No agent tool switch-cases exist for it, so the constant would be dead code. The catalog ID string `"sendgrid"` is the sole identifier.
- Template rendering (existing email deliverer handles plain text; add HTML templating in Phase 4)
- Unsubscribe / list management (SendGrid marketing features)
- Multiple from addresses per account

---

## Architecture

### 1. Catalog Entry

Add to `internal/connections/catalog/catalog.json` in the `"communication"` category:

```json
{
  "id": "sendgrid",
  "name": "SendGrid",
  "description": "Transactional email via SendGrid API v3. Used by agents to send email notifications and reports.",
  "category": "communication",
  "icon": "SG",
  "icon_color": "#1a82e2",
  "type": "credentials",
  "default_label": "SendGrid",
  "multi_account": false,
  "fields": [
    {
      "key": "api_key",
      "label": "API Key",
      "type": "password",
      "required": true,
      "stored_in": "creds",
      "placeholder": "SG.xxxxxxxxxxxxxxxxxxxxxxxx",
      "help_text": "Create a key with 'Mail Send' permission at app.sendgrid.com → Settings → API Keys."
    },
    {
      "key": "from",
      "label": "From address",
      "type": "email",
      "required": true,
      "stored_in": "metadata",
      "placeholder": "agent@example.com",
      "help_text": "Must be a verified sender in your SendGrid account."
    },
    {
      "key": "from_name",
      "label": "From name",
      "type": "text",
      "required": false,
      "stored_in": "metadata",
      "placeholder": "Huginn",
      "help_text": "Display name shown in the recipient's email client."
    }
  ],
  "validation": {
    "available": true,
    "description": "Sends a test API call to verify the key is valid and has Mail Send permission."
  }
}
```

**Field storage:**
- `api_key` → `stored_in: "creds"` (encrypted SecretStore)
- `from`, `from_name` → `stored_in: "metadata"` (plain Connection.Metadata map)

---

### 2. SendGrid Deliverer

Add `sendGridDeliverer` to `internal/scheduler/delivery.go`, parallel to the existing `emailDeliverer`. Match the `Deliverer` interface exactly: `Deliver(ctx context.Context, n *notification.Notification, target NotificationDelivery) notification.DeliveryRecord`.

The struct holds a dedicated `*http.Client` with a timeout (matching the `webhookDeliverer` pattern). Credentials are resolved via `CredentialResolver` — the actual type is `func(ctx context.Context, connectionName string) (map[string]string, error)` (two args, not three). The HTTP call is wrapped in `deliverWithRetry` for resilience.

```go
var sendgridBackoff = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second}

// sendGridDeliverer sends email via SendGrid API v3.
// Credentials are resolved from a named connection (api_key from SecretStore;
// from and from_name from Connection.Metadata).
type sendGridDeliverer struct {
    resolver CredentialResolver
    client   *http.Client
}

func newSendGridDeliverer(resolver CredentialResolver) *sendGridDeliverer {
    return &sendGridDeliverer{
        resolver: resolver,
        client:   &http.Client{Timeout: 15 * time.Second},
    }
}

func (d *sendGridDeliverer) Deliver(ctx context.Context, n *notification.Notification, target NotificationDelivery) notification.DeliveryRecord {
    rec := notification.DeliveryRecord{
        Type:   "email",
        Target: target.To,
        SentAt: time.Now().UTC(),
    }
    if target.To == "" {
        rec.Status = "failed"
        rec.Error = "sendgrid: missing 'to' address"
        return rec
    }

    // CredentialResolver takes (ctx, connectionName) — two args.
    creds, err := d.resolver(ctx, target.Connection)
    if err != nil {
        rec.Status = "failed"
        rec.Error = "sendgrid: resolve credentials: " + err.Error()
        return rec
    }

    apiKey   := creds["api_key"]
    from     := creds["from"]
    fromName := creds["from_name"] // may be empty

    if apiKey == "" || from == "" {
        rec.Status = "failed"
        rec.Error = "sendgrid: api_key and from address are required"
        return rec
    }

    subject := n.Summary
    if subject == "" {
        subject = "Huginn notification"
    }

    payload := map[string]any{
        "personalizations": []map[string]any{
            {"to": []map[string]string{{"email": target.To}}},
        },
        "from":    map[string]string{"email": from, "name": fromName},
        "subject": subject,
        "content": []map[string]string{
            {"type": "text/plain", "value": n.Detail},
        },
    }

    body, err := json.Marshal(payload)
    if err != nil {
        rec.Status = "failed"
        rec.Error = "sendgrid: marshal payload: " + err.Error()
        return rec
    }

    err = deliverWithRetry(ctx, sendgridBackoff, func() error {
        req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost,
            "https://api.sendgrid.com/v3/mail/send", bytes.NewReader(body))
        if reqErr != nil {
            return &permanentError{reqErr}
        }
        req.Header.Set("Authorization", "Bearer "+apiKey)
        req.Header.Set("Content-Type", "application/json")

        resp, doErr := d.client.Do(req)
        if doErr != nil {
            return doErr // transient; retry
        }
        defer resp.Body.Close()

        if resp.StatusCode == 202 {
            return nil
        }
        respBody, _ := io.ReadAll(resp.Body)
        if resp.StatusCode >= 400 && resp.StatusCode < 500 {
            // 4xx = permanent (bad key, unverified sender, etc.)
            return &permanentError{fmt.Errorf("sendgrid: client error %d: %s", resp.StatusCode, respBody)}
        }
        return fmt.Errorf("sendgrid: unexpected status %d: %s", resp.StatusCode, respBody)
    })

    if err != nil {
        rec.Status = "failed"
        rec.Error = err.Error()
    } else {
        rec.Status = "sent"
    }
    return rec
}
```

**Register in `NewDelivererRegistry`** in `delivery.go`:

```go
func NewDelivererRegistry(resolver CredentialResolver) *DelivererRegistry {
    return &DelivererRegistry{
        m: map[string]Deliverer{
            "webhook":  newWebhookDeliverer(),
            "email":    newEmailDeliverer(resolver),
            "sendgrid": newSendGridDeliverer(resolver),
        },
    }
}
```

A workflow step uses `type: sendgrid` to route to this deliverer.

---

### 3. Connection Validator

The catalog entry declares `"validation": { "available": true }`. Validators are registered in `buildCredentialValidatorRegistry()` in `internal/server/credential_validators.go` — **not** in `handlers_connections.go`. Follow the existing pattern exactly: register a `ValidatorFunc` that calls a standalone `validateSendGridCredentials` function.

**Registration (add to the `── Communication` block):**

```go
r.Register("sendgrid", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
    return validateSendGridCredentials(ctx, f["api_key"])
}))
```

**Standalone validate function (add below the Communication section):**

```go
func validateSendGridCredentials(ctx context.Context, apiKey string) error {
    if apiKey == "" {
        return errors.New("api_key is required")
    }
    // GET /v3/scopes: lightweight auth check — requires a valid key,
    // returns the list of permission scopes. No side effects.
    req, err := http.NewRequestWithContext(ctx, http.MethodGet,
        "https://api.sendgrid.com/v3/scopes", nil)
    if err != nil {
        return err
    }
    req.Header.Set("Authorization", "Bearer "+apiKey)
    client := safeHTTPClient()
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("sendgrid: validation request failed: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode == 401 || resp.StatusCode == 403 {
        return errors.New("invalid API key")
    }
    if resp.StatusCode != 200 {
        return fmt.Errorf("sendgrid: validation returned %d", resp.StatusCode)
    }
    return nil
}
```

`GET /v3/scopes` is the standard lightweight auth probe for SendGrid — requires a valid key and returns 200 with the key's permission list. No side effects. Prefers this over `/v3/user/credits` which requires account-level permission and may 403 on restricted keys.

---

## File Changes

| File | Change |
|------|--------|
| `internal/connections/catalog/catalog.json` | Add SendGrid entry (3 fields: api_key, from, from_name) |
| `internal/scheduler/delivery.go` | Add `sendgridBackoff`, `sendGridDeliverer` struct + `Deliver` method; register in `NewDelivererRegistry` |
| `internal/server/credential_validators.go` | Register `"sendgrid"` in `buildCredentialValidatorRegistry()`; add `validateSendGridCredentials` function |

No DB migrations. No frontend changes (the connection wizard is driven entirely by the catalog JSON).

---

## Tests

**Backend:**
- `TestSendGridDeliverer_SuccessOn202` — mock HTTP server returns 202, expect `rec.Status == "sent"`
- `TestSendGridDeliverer_FailsOn4xx` — mock returns 400, expect `rec.Status == "failed"` with status in error, no retries (permanentError)
- `TestSendGridDeliverer_RetriesOn5xx` — mock returns 500 twice then 202, expect `rec.Status == "sent"` after retries
- `TestSendGridDeliverer_MissingAPIKey` — empty creds, expect `rec.Status == "failed"` before HTTP call
- `TestSendGridDeliverer_MissingFrom` — empty from, expect `rec.Status == "failed"` before HTTP call
- `TestValidateSendGrid_InvalidKey` — mock returns 401, expect `"invalid API key"` error
- `TestValidateSendGrid_Success` — mock returns 200, expect nil

---

## Success Criteria

- SendGrid appears in the Connections catalog under "communication"
- Wizard shows three fields: API Key (password), From address (email), From name (text)
- Saving a valid key + from address stores api_key in SecretStore, from/from_name in Metadata
- The "Test connection" button calls the validator and shows success/error
- A workflow `sendgrid` step wired to a SendGrid connection sends via the API (not SMTP)
- 5xx responses are retried with backoff; 4xx are treated as permanent failures
- `go build ./...` clean; all new tests pass
