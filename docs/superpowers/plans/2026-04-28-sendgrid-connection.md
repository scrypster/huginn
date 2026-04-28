# SendGrid Connection Provider — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add SendGrid as a first-class connection provider so workflow steps can deliver email via the SendGrid API instead of SMTP.

**Architecture:** Three files change. The catalog JSON entry drives the UI automatically. A new `sendGridDeliverer` in `delivery.go` follows the existing `webhookDeliverer` pattern (struct with `*http.Client`, wraps send in `deliverWithRetry`, 4xx = permanent). A `validateSendGridCredentials` function is registered in `credential_validators.go` alongside other communication validators.

**Tech Stack:** Go standard library (`net/http`, `encoding/json`). No new dependencies.

---

## File Changes

| File | Change |
|------|--------|
| `internal/connections/catalog/catalog.json` | Insert SendGrid entry after the `discord` entry |
| `internal/scheduler/delivery.go` | Add `sendgridBackoff`, `sendGridDeliverer` struct + `Deliver` method; add `"sendgrid"` to `NewDelivererRegistry` |
| `internal/server/credential_validators.go` | Register `"sendgrid"` in `buildCredentialValidatorRegistry`; add `validateSendGridCredentials` |

---

### Task 1: Catalog Entry

**Files:**
- Modify: `internal/connections/catalog/catalog.json` (insert after the `discord` entry, ~line 760)

- [ ] **Step 1: Find the insertion point**

Open `internal/connections/catalog/catalog.json`. Find the closing `}` of the `discord` entry (the entry with `"id": "discord"` around line 633). Insert the new SendGrid entry immediately after it, before the next `{`.

- [ ] **Step 2: Insert the SendGrid catalog entry**

After the discord entry's closing `},` add:

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
  },
```

- [ ] **Step 3: Verify JSON is valid**

Run:
```bash
python3 -m json.tool internal/connections/catalog/catalog.json > /dev/null && echo "valid"
```
Expected: `valid`

- [ ] **Step 4: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/connections/catalog/catalog.json
git commit -m "feat(connections): add SendGrid catalog entry"
```

---

### Task 2: SendGrid Deliverer

**Files:**
- Modify: `internal/scheduler/delivery.go`
- Modify: `internal/scheduler/delivery_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/scheduler/delivery_test.go`, append after the last existing test:

```go
// ── SendGrid deliverer tests ──────────────────────────────────────────────────

func TestSendGridDeliverer_MissingTo(t *testing.T) {
	d := newSendGridDeliverer(nil)
	rec := d.Deliver(context.Background(), &notification.Notification{}, NotificationDelivery{})
	if rec.Status != "failed" {
		t.Errorf("expected failed, got %q", rec.Status)
	}
	if !strings.Contains(rec.Error, "missing 'to'") {
		t.Errorf("unexpected error: %q", rec.Error)
	}
}

func TestSendGridDeliverer_MissingAPIKey(t *testing.T) {
	resolver := func(ctx context.Context, conn string) (map[string]string, error) {
		return map[string]string{"from": "sender@example.com"}, nil
	}
	d := newSendGridDeliverer(resolver)
	rec := d.Deliver(context.Background(),
		&notification.Notification{Summary: "hi"},
		NotificationDelivery{To: "dst@example.com", Connection: "test-conn"})
	if rec.Status != "failed" {
		t.Errorf("expected failed, got %q", rec.Status)
	}
	if !strings.Contains(rec.Error, "api_key") {
		t.Errorf("unexpected error: %q", rec.Error)
	}
}

func TestSendGridDeliverer_MissingFrom(t *testing.T) {
	resolver := func(ctx context.Context, conn string) (map[string]string, error) {
		return map[string]string{"api_key": "SG.key"}, nil
	}
	d := newSendGridDeliverer(resolver)
	rec := d.Deliver(context.Background(),
		&notification.Notification{Summary: "hi"},
		NotificationDelivery{To: "dst@example.com", Connection: "test-conn"})
	if rec.Status != "failed" {
		t.Errorf("expected failed, got %q", rec.Status)
	}
	if !strings.Contains(rec.Error, "from address") {
		t.Errorf("unexpected error: %q", rec.Error)
	}
}

func TestSendGridDeliverer_SuccessOn202(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer SG.testkey" {
			t.Errorf("expected Bearer auth, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusAccepted) // 202
	}))
	defer srv.Close()

	resolver := func(ctx context.Context, conn string) (map[string]string, error) {
		return map[string]string{
			"api_key": "SG.testkey",
			"from":    "sender@example.com",
		}, nil
	}
	d := newSendGridDeliverer(resolver)
	// Override the sendgrid URL to point at our test server.
	// We do this by swapping the client with one that rewrites the request.
	d.client = &http.Client{
		Transport: rewriteHostTransport{base: http.DefaultTransport, target: srv.URL},
		Timeout:   5 * time.Second,
	}
	rec := d.Deliver(context.Background(),
		&notification.Notification{Summary: "Test", Detail: "body"},
		NotificationDelivery{To: "dst@example.com", Connection: "conn"})
	if rec.Status != "sent" {
		t.Errorf("expected sent, got %q (err=%s)", rec.Status, rec.Error)
	}
}

func TestSendGridDeliverer_PermanentOn4xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest) // 400
	}))
	defer srv.Close()

	resolver := func(ctx context.Context, conn string) (map[string]string, error) {
		return map[string]string{"api_key": "SG.key", "from": "s@ex.com"}, nil
	}
	d := newSendGridDeliverer(resolver)
	d.client = &http.Client{
		Transport: rewriteHostTransport{base: http.DefaultTransport, target: srv.URL},
		Timeout:   5 * time.Second,
	}
	rec := d.Deliver(context.Background(),
		&notification.Notification{Summary: "hi"},
		NotificationDelivery{To: "dst@ex.com", Connection: "c"})
	if rec.Status != "failed" {
		t.Errorf("expected failed, got %q", rec.Status)
	}
	// 4xx should NOT retry — exactly 1 attempt
	if calls != 1 {
		t.Errorf("expected 1 attempt for 4xx, got %d", calls)
	}
}

func TestSendGridDeliverer_RetriesOn5xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			return
		}
		w.WriteHeader(http.StatusAccepted) // 202 on 3rd try
	}))
	defer srv.Close()

	resolver := func(ctx context.Context, conn string) (map[string]string, error) {
		return map[string]string{"api_key": "SG.key", "from": "s@ex.com"}, nil
	}
	d := newSendGridDeliverer(resolver)
	d.client = &http.Client{
		Transport: rewriteHostTransport{base: http.DefaultTransport, target: srv.URL},
		Timeout:   5 * time.Second,
	}
	// Use zero-duration backoff so the test runs fast
	origBackoff := sendgridBackoff
	sendgridBackoff = []time.Duration{0, 0, 0}
	defer func() { sendgridBackoff = origBackoff }()

	rec := d.Deliver(context.Background(),
		&notification.Notification{Summary: "hi"},
		NotificationDelivery{To: "dst@ex.com", Connection: "c"})
	if rec.Status != "sent" {
		t.Errorf("expected sent after retry, got %q (err=%s)", rec.Status, rec.Error)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

// rewriteHostTransport rewrites all requests to point at a test server URL.
type rewriteHostTransport struct {
	base   http.RoundTripper
	target string
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, _ := url.Parse(t.target)
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = target.Scheme
	req2.URL.Host = target.Host
	return t.base.RoundTrip(req2)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/scheduler/... -run TestSendGrid -v 2>&1 | head -30
```
Expected: `FAIL` — `newSendGridDeliverer` not defined yet.

- [ ] **Step 3: Add the deliverer to `delivery.go`**

In `internal/scheduler/delivery.go`, after the `emailDeliverer` section and before the `--- Deliverer registry ---` comment, add:

```go
// --- SendGrid deliverer ---

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

Also add `"net/url"` to the test file imports if not already present (for `rewriteHostTransport`).

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/scheduler/... -run TestSendGrid -v
```
Expected: all 6 `TestSendGrid*` tests PASS.

- [ ] **Step 5: Register in `NewDelivererRegistry`**

In `delivery.go`, update `NewDelivererRegistry` (currently ends around line 420):

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

- [ ] **Step 6: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 7: Run full scheduler tests**

```bash
go test ./internal/scheduler/... -v 2>&1 | tail -20
```
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/scheduler/delivery.go internal/scheduler/delivery_test.go
git commit -m "feat(scheduler): add SendGrid deliverer with retry and 4xx permanent-error"
```

---

### Task 3: Credential Validator

**Files:**
- Modify: `internal/server/credential_validators.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/server/credential_validators_test.go` (create the file if it doesn't exist — check with `ls internal/server/credential_validators_test.go`):

```go
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateSendGridCredentials_MissingKey(t *testing.T) {
	err := validateSendGridCredentials(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
	if err.Error() != "api_key is required" {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateSendGridCredentials_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/scopes" {
			t.Errorf("expected /v3/scopes, got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized) // 401
	}))
	defer srv.Close()

	// Temporarily point sendgrid API base at our test server via an env var
	// or by passing the URL directly. We'll use a test-only exported var.
	origURL := sendgridScopesURL
	sendgridScopesURL = srv.URL + "/v3/scopes"
	defer func() { sendgridScopesURL = origURL }()

	err := validateSendGridCredentials(context.Background(), "SG.badkey")
	if err == nil || err.Error() != "invalid API key" {
		t.Errorf("expected 'invalid API key', got %v", err)
	}
}

func TestValidateSendGridCredentials_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origURL := sendgridScopesURL
	sendgridScopesURL = srv.URL + "/v3/scopes"
	defer func() { sendgridScopesURL = origURL }()

	err := validateSendGridCredentials(context.Background(), "SG.goodkey")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/server/... -run TestValidateSendGrid -v 2>&1 | head -20
```
Expected: FAIL — `validateSendGridCredentials` and `sendgridScopesURL` not defined.

- [ ] **Step 3: Add the validator to `credential_validators.go`**

First, add a package-level variable for testability immediately before the `buildCredentialValidatorRegistry` function:

```go
// sendgridScopesURL is the endpoint used to validate SendGrid API keys.
// Overridable in tests.
var sendgridScopesURL = "https://api.sendgrid.com/v3/scopes"
```

Then, inside `buildCredentialValidatorRegistry`, in the `── Communication ─` block (after the `discord` registration, ~line 43):

```go
r.Register("sendgrid", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
	return validateSendGridCredentials(ctx, f["api_key"])
}))
```

Then add the standalone function at the end of the communication validator functions section:

```go
func validateSendGridCredentials(ctx context.Context, apiKey string) error {
	if apiKey == "" {
		return errors.New("api_key is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sendgridScopesURL, nil)
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

Add `"errors"` to the imports in `credential_validators.go` if not already present.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/... -run TestValidateSendGrid -v
```
Expected: all 3 `TestValidateSendGrid*` tests PASS.

- [ ] **Step 5: Build and full test run**

```bash
go build ./... && go test ./internal/server/... 2>&1 | tail -10
```
Expected: no build errors, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/server/credential_validators.go internal/server/credential_validators_test.go
git commit -m "feat(connections): add SendGrid credential validator (GET /v3/scopes)"
```
