package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// subdomainRE matches a valid DNS label (no dots, slashes, or leading/trailing hyphens).
var subdomainRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)

// githubUsernameRE matches valid GitHub usernames.
var githubUsernameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9\-]{0,37}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)


// validateSubdomain checks that s is a safe DNS label suitable for interpolation
// into a hostname like "{s}.zendesk.com".
func validateSubdomain(s string) error {
	if s == "" {
		return fmt.Errorf("subdomain is required")
	}
	if !subdomainRE.MatchString(s) {
		return fmt.Errorf("subdomain %q contains invalid characters (use letters, numbers, hyphens only)", s)
	}
	return nil
}

// validateBaseURL checks that rawURL is a safe HTTPS URL for use as a provider base URL.
// Blocks loopback, link-local, RFC 1918 addresses, and non-HTTPS schemes.
func validateBaseURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS (got %q)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL must have a non-empty host")
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("URL must not point to localhost")
	}
	if isBlockedIP(host) {
		return fmt.Errorf("URL host %q is a blocked IP address", host)
	}
	// Best-effort DNS lookup to catch hostnames that resolve to blocked IPs
	if ips, err := net.LookupHost(host); err == nil {
		for _, ip := range ips {
			if isBlockedIP(ip) {
				return fmt.Errorf("URL resolves to blocked IP address %q", ip)
			}
		}
	}
	return nil
}

func isBlockedIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false // not an IP literal — hostname callers rely on DNS resolution path
	}
	// Normalize IPv4-mapped IPv6 (e.g. ::ffff:127.0.0.1 → 127.0.0.1).
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// safeHTTPClient returns an *http.Client whose dialer validates every resolved
// IP against isBlockedIP before connecting. This eliminates the TOCTOU window
// between validateBaseURL and the actual HTTP request (DNS rebinding).
func safeHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("safeDialer: invalid addr %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("safeDialer: DNS lookup failed for %q: %w", host, err)
			}
			for _, ip := range ips {
				if isBlockedIP(ip) {
					return nil, fmt.Errorf("safeDialer: resolved IP %s for host %q is blocked", ip, host)
				}
			}
			// Connect using the first non-blocked IP.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}
}

// validateGitHubUsername checks that s is safe to pass to the gh CLI --user flag.
func validateGitHubUsername(s string) error {
	if s == "" {
		return fmt.Errorf("username is required")
	}
	if !githubUsernameRE.MatchString(s) {
		return fmt.Errorf("username %q contains invalid characters (letters, numbers, hyphens only)", s)
	}
	return nil
}
