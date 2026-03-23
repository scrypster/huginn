package tools

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/scrypster/huginn/internal/backend"
)

// ssrfBlockedCIDRs contains additional CIDR ranges blocked beyond what the
// standard net.IP predicates cover (loopback, private, link-local-unicast,
// unspecified). These cover multicast, reserved, CGNAT, and IPv6 multicast /
// link-local ranges that an attacker could exploit for SSRF.
var ssrfBlockedCIDRs = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",      // "This" network (RFC 1122 §3.2.1.3)
		"100.64.0.0/10",  // CGNAT / shared address space (RFC 6598)
		"224.0.0.0/4",    // IPv4 multicast Class D (RFC 1112)
		"240.0.0.0/4",    // IPv4 reserved Class E (RFC 1112)
		"ff00::/8",       // IPv6 multicast (RFC 4291)
		"fe80::/10",      // IPv6 link-local (RFC 4291) — belt-and-suspenders
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil && n != nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

const (
	fetchURLTimeout  = 10 * time.Second
	fetchURLMaxBytes = 512 * 1024 // 512 KB
)

// FetchURLTool retrieves a URL and converts HTML to markdown.
// JS-rendered content is not supported — only static HTML is processed.
type FetchURLTool struct {
	client *http.Client // injectable for testing
}

func (t *FetchURLTool) Name() string { return "fetch_url" }
func (t *FetchURLTool) Description() string {
	return "Fetch a URL and return its content as markdown. " +
		"Note: JavaScript-rendered content is not supported; only static HTML is processed."
}
func (t *FetchURLTool) Permission() PermissionLevel { return PermRead }

func (t *FetchURLTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "fetch_url",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"url"},
				Properties: map[string]backend.ToolProperty{
					"url": {Type: "string", Description: "URL to fetch (http or https only)"},
				},
			},
		},
	}
}

func (t *FetchURLTool) httpClient() *http.Client {
	if t.client != nil {
		return t.client
	}
	return &http.Client{
		Timeout: fetchURLTimeout,
		Transport: &http.Transport{
			DialContext:         ssrfSafeDialContext(),
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// isBlockedIP reports whether ip should be denied for SSRF reasons.
// Blocked ranges:
//   - Loopback (127.0.0.0/8, ::1/128)
//   - RFC-1918 private (10/8, 172.16/12, 192.168/16)
//   - Link-local unicast (169.254.0.0/16, fe80::/10)
//   - Unspecified (0.0.0.0, ::)
//   - "This" network (0.0.0.0/8)
//   - CGNAT / shared address space (100.64.0.0/10, RFC 6598)
//   - IPv4 multicast Class D (224.0.0.0/4)
//   - IPv4 reserved Class E (240.0.0.0/4)
//   - IPv6 multicast (ff00::/8)
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range ssrfBlockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfSafeDialContext returns a DialContext that resolves the hostname and
// rejects any blocked IP before establishing a connection. This eliminates the
// TOCTOU race between a pre-flight DNS check and the actual HTTP connection
// (DNS rebinding): the resolved IP is checked and then used directly for the
// dial, with no second resolution.
func ssrfSafeDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: fetchURLTimeout, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("fetch_url: invalid addr %q: %w", addr, err)
		}
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("fetch_url: dns: %w", err)
		}
		for _, a := range ips {
			ip := net.ParseIP(a)
			if ip != nil && isBlockedIP(ip) {
				return nil, fmt.Errorf("fetch_url: access to private/internal hosts is not allowed")
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("fetch_url: no addresses for host %q", host)
		}
		// Dial the resolved IP directly — no second DNS lookup, closes TOCTOU window.
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
	}
}

// isPrivateHost returns true if hostname resolves to any blocked IP.
// Uses net.SplitHostPort to correctly handle IPv6 bracket notation.
// This is a fast-fail pre-flight check; the authoritative guard is
// ssrfSafeDialContext which runs at dial time.
func isPrivateHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host // no port present — use as-is
	}
	addrs, err := net.LookupHost(h)
	if err != nil {
		return false // can't resolve = not private (dial-time guard will catch it)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip != nil && isBlockedIP(ip) {
			return true
		}
	}
	return false
}

func (t *FetchURLTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	rawURL, ok := args["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return ToolResult{IsError: true, Error: "fetch_url: 'url' argument required"}
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("fetch_url: invalid URL: %v", err)}
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ToolResult{IsError: true, Error: fmt.Sprintf("fetch_url: unsupported scheme %q (only http/https allowed)", parsed.Scheme)}
	}

	// SSRF guard: only for non-test clients
	if t.client == nil && isPrivateHost(parsed.Host) {
		return ToolResult{IsError: true, Error: "fetch_url: access to private/internal hosts is not allowed"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("fetch_url: build request: %v", err)}
	}
	req.Header.Set("User-Agent", "Huginn/1.0 (coding assistant)")

	resp, err := t.httpClient().Do(req)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("fetch_url: http: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("fetch_url: HTTP %d for %s", resp.StatusCode, rawURL),
		}
	}

	limited := io.LimitReader(resp.Body, fetchURLMaxBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("fetch_url: read body: %v", err)}
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		converter := md.NewConverter("", true, nil)
		converted, err := converter.ConvertString(string(body))
		if err != nil {
			return ToolResult{Output: string(body)}
		}
		return ToolResult{Output: converted}
	}

	return ToolResult{Output: string(body)}
}
