// internal/scheduler/delivery.go
package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/notification"
)

// permanentError wraps an error that should not be retried (e.g. HTTP 4xx).
// deliverWithRetry checks for this type to short-circuit the retry loop.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// isPermanent returns true if err is (or wraps) a permanentError.
func isPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// CredentialResolver looks up connection credentials by name.
// Returns a key-value map (e.g. "host", "port", "username", "password", "from"
// for SMTP; "api_key", "from" for SendGrid).
// If nil, email delivery that references a named connection will fail with a
// clear error rather than silently dropping the message.
type CredentialResolver func(ctx context.Context, connectionName string) (map[string]string, error)

// Deliverer sends a notification to one external target.
// Implementations must be safe for concurrent use.
type Deliverer interface {
	Deliver(ctx context.Context, n *notification.Notification, target NotificationDelivery) notification.DeliveryRecord
}

// jitter adds ±25% random jitter to d to prevent thundering-herd retry storms.
// For example, jitter(2s) returns a value uniformly distributed in [1.5s, 2.5s].
func jitter(d time.Duration) time.Duration {
	delta := float64(d) * 0.25
	// rand.Int63n produces values in [0, 2*delta), which we shift to [-delta, delta).
	offset := time.Duration(rand.Int63n(int64(2*delta+1)) - int64(delta)) //nolint:gosec
	return d + offset
}

// deliverWithRetry calls fn up to len(backoff)+1 times. On transient failure it
// sleeps for the next backoff duration (with ±25% jitter) before retrying.
// Returns the last error, or nil on success. Errors wrapped as permanentError
// are not retried.
func deliverWithRetry(ctx context.Context, backoff []time.Duration, fn func() error) error {
	var err error
	for attempt := 0; attempt <= len(backoff); attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if isPermanent(err) {
			return err
		}
		if attempt < len(backoff) {
			delay := jitter(backoff[attempt])
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return err
}

// ValidateWebhookURLSyntax performs a fast, DNS-free SSRF check suitable for
// save-time validation. It verifies that rawURL uses http/https and does not
// contain a literal private/loopback IP address. Hostnames are NOT resolved —
// the full DNS-resolved check runs at delivery time via safeWebhookURL.
// Returns a non-nil error for scheme violations or literal private IPs.
func ValidateWebhookURLSyntax(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook URL scheme must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil {
		// Not a literal IP — hostname resolution happens at delivery time.
		return nil
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || isPrivateIP(ip) {
		return fmt.Errorf("webhook URL must not target private or loopback IP addresses (SSRF protection)")
	}
	return nil
}

// safeWebhookURL validates that the webhook URL does not point to a loopback,
// link-local, or private IP range (SSRF mitigation). It resolves the hostname
// via DNS and checks every returned address against RFC 1918 private ranges,
// RFC 6598 shared address space, and IPv6 unique-local / link-local ranges.
// Returns a permanentError for policy violations (unsafe scheme, private IP)
// and a transient error for DNS lookup failures.
func safeWebhookURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &permanentError{fmt.Errorf("webhook URL scheme must be http or https, got %q", u.Scheme)}
	}
	host := u.Hostname()
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %q: %w", host, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || isPrivateIP(ip) {
			return &permanentError{fmt.Errorf("webhook URL %q resolves to non-public IP %s (SSRF protection)", rawURL, addr)}
		}
	}
	return nil
}

// privateRanges are the RFC1918 + related non-public IPv4 CIDR blocks.
var privateRanges = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",  // shared address space (RFC 6598)
		"169.254.0.0/16", // link-local
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	var nets []*net.IPNet
	for _, c := range cidrs {
		_, n, _ := net.ParseCIDR(c)
		if n != nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func isPrivateIP(ip net.IP) bool {
	for _, n := range privateRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// --- Webhook deliverer ---

var webhookBackoff = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}

// webhookDeliverer POSTs a JSON notification payload to a URL.
// It validates the target URL against SSRF rules before sending and retries
// transient server errors (5xx) with exponential backoff. Client errors (4xx)
// are treated as permanent and not retried.
type webhookDeliverer struct {
	client *http.Client
}

func newWebhookDeliverer() *webhookDeliverer {
	return &webhookDeliverer{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// NewWebhookDeliverer returns an exported Deliverer backed by the webhook
// implementation. Callers outside the scheduler package (e.g. the retry
// handler) use this to re-deliver without needing access to unexported types.
func NewWebhookDeliverer() Deliverer {
	return newWebhookDeliverer()
}

func (d *webhookDeliverer) Deliver(ctx context.Context, n *notification.Notification, target NotificationDelivery) notification.DeliveryRecord {
	rec := notification.DeliveryRecord{
		Type:   "webhook",
		Target: target.To,
		SentAt: time.Now().UTC(),
	}
	if target.To == "" {
		rec.Status = "failed"
		rec.Error = "webhook: missing 'to' URL"
		return rec
	}

	// SSRF protection: reject loopback, link-local, and private IP ranges.
	if err := safeWebhookURL(target.To); err != nil {
		rec.Status = "failed"
		rec.Error = err.Error()
		return rec
	}

	payload := map[string]any{
		"id":          n.ID,
		"title":       n.Summary,
		"description": n.Detail,
		"severity":    string(n.Severity),
		"run_id":      n.RunID,
		"workflow_id": n.WorkflowID,
		"created_at":  n.CreatedAt.Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		rec.Status = "failed"
		rec.Error = fmt.Sprintf("webhook: marshal: %v", err)
		return rec
	}

	err = deliverWithRetry(ctx, webhookBackoff, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, target.To, bytes.NewReader(body))
		if reqErr != nil {
			return &permanentError{reqErr}
		}
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := d.client.Do(req)
		if doErr != nil {
			return doErr // transient; retry
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return fmt.Errorf("webhook: server error %d", resp.StatusCode) // transient; retry
		}
		if resp.StatusCode >= 400 {
			// 4xx = client error (bad URL, auth, payload); retrying won't help.
			return &permanentError{fmt.Errorf("webhook: client error %d", resp.StatusCode)}
		}
		return nil
	})

	if err != nil {
		rec.Status = "failed"
		rec.Error = err.Error()
	} else {
		rec.Status = "sent"
	}
	return rec
}

// --- Email (SMTP) deliverer ---

var emailBackoff = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second}

// emailDeliverer sends notification summaries via SMTP.
// Credentials are resolved either from a named connection (via CredentialResolver)
// or directly from the inline SMTP fields on NotificationDelivery. Retries
// transient SMTP failures with exponential backoff (2s, 8s, 30s).
type emailDeliverer struct {
	resolver CredentialResolver // may be nil
}

func newEmailDeliverer(resolver CredentialResolver) *emailDeliverer {
	return &emailDeliverer{resolver: resolver}
}

// smtpCreds holds the resolved SMTP credentials for a single email send.
// Populated either from a named connection via CredentialResolver or from
// inline NotificationDelivery fields.
type smtpCreds struct {
	Host     string // e.g. "smtp.gmail.com"
	Port     string // e.g. "587"
	Username string
	Password string
	From     string // sender address
}

func (d *emailDeliverer) Deliver(ctx context.Context, n *notification.Notification, target NotificationDelivery) notification.DeliveryRecord {
	rec := notification.DeliveryRecord{
		Type:   "email",
		Target: target.To,
		SentAt: time.Now().UTC(),
	}
	if target.To == "" {
		rec.Status = "failed"
		rec.Error = "email: missing 'to' address"
		return rec
	}

	creds, err := d.resolveCreds(ctx, target)
	if err != nil {
		rec.Status = "failed"
		rec.Error = fmt.Sprintf("email: credentials: %v", err)
		return rec
	}

	subject := fmt.Sprintf("[Huginn] %s", n.Summary)
	body := buildEmailBody(n)

	err = deliverWithRetry(ctx, emailBackoff, func() error {
		return sendSMTP(creds, target.To, subject, body)
	})

	if err != nil {
		rec.Status = "failed"
		rec.Error = err.Error()
		slog.Warn("scheduler: email delivery failed", "to", target.To, "err", err)
	} else {
		rec.Status = "sent"
	}
	return rec
}

func (d *emailDeliverer) resolveCreds(ctx context.Context, target NotificationDelivery) (smtpCreds, error) {
	if target.Connection != "" && d.resolver != nil {
		kvs, err := d.resolver(ctx, target.Connection)
		if err != nil {
			// DO NOT include credential values in this error message.
			return smtpCreds{}, fmt.Errorf("resolve connection %q: %w", target.Connection, err)
		}
		port := kvs["port"]
		if port == "" {
			port = "587"
		}
		from := kvs["from"]
		if from == "" {
			from = kvs["username"]
		}
		return smtpCreds{
			Host:     kvs["host"],
			Port:     port,
			Username: kvs["username"],
			Password: kvs["password"],
			From:     from,
		}, nil
	}
	// No connection: check for inline fields in NotificationDelivery.
	if target.SMTPHost == "" {
		return smtpCreds{}, fmt.Errorf("no connection or smtp_host configured for email delivery; " +
			"set 'connection' to reference a huginn SMTP/Gmail connection or configure 'smtp_host' inline")
	}
	// Deprecation warning: inline SMTPPass without a named connection stores
	// plaintext credentials on disk. Log using the obfuscated helper so the
	// password is never written to log output.
	if target.SMTPPass != "" && target.Connection == "" {
		slog.Warn("scheduler: inline SMTPPass is deprecated and will be removed; use a named connection instead",
			"smtp_host", target.SMTPHost,
			"smtp_pass", target.SMTPPassObfuscated())
	}
	port := target.SMTPPort
	if port == "" {
		port = "587"
	}
	from := target.SMTPFrom
	if from == "" {
		from = target.SMTPUser
	}
	return smtpCreds{
		Host:     target.SMTPHost,
		Port:     port,
		Username: target.SMTPUser,
		Password: target.SMTPPass,
		From:     from,
	}, nil
}

func sendSMTP(creds smtpCreds, to, subject, body string) error {
	addr := creds.Host + ":" + creds.Port
	auth := smtp.PlainAuth("", creds.Username, creds.Password, creds.Host)
	msg := buildMIMEMessage(creds.From, to, subject, body)
	return smtp.SendMail(addr, auth, creds.From, []string{to}, []byte(msg))
}

func buildMIMEMessage(from, to, subject, body string) string {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}

func buildEmailBody(n *notification.Notification) string {
	var sb strings.Builder
	sb.WriteString(n.Summary)
	sb.WriteString("\n\n")
	if n.Detail != "" {
		sb.WriteString(n.Detail)
		sb.WriteString("\n\n")
	}
	sb.WriteString(fmt.Sprintf("Workflow: %s\n", n.WorkflowID))
	sb.WriteString(fmt.Sprintf("Run ID: %s\n", n.RunID))
	sb.WriteString(fmt.Sprintf("Severity: %s\n", string(n.Severity)))
	sb.WriteString(fmt.Sprintf("Time: %s\n", n.CreatedAt.Format(time.RFC3339)))
	return sb.String()
}

// --- Deliverer registry ---

// DelivererRegistry maps notification delivery type → Deliverer implementation.
// Built once via NewDelivererRegistry; safe for concurrent reads (never mutated after init).
type DelivererRegistry struct {
	m map[string]Deliverer
}

// NewDelivererRegistry creates a registry pre-populated with webhook and email deliverers.
// resolver may be nil; email delivery that references a named connection will fail with a
// clear error rather than silently dropping the message.
func NewDelivererRegistry(resolver CredentialResolver) *DelivererRegistry {
	return &DelivererRegistry{
		m: map[string]Deliverer{
			"webhook": newWebhookDeliverer(),
			"email":   newEmailDeliverer(resolver),
		},
	}
}

// get returns the Deliverer for the given type, or nil if unknown.
func (r *DelivererRegistry) get(kind string) Deliverer {
	return r.m[kind]
}
