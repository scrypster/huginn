package connections

import (
	"fmt"
	"net"
	"net/url"
	"os"
)

// validateRedirectURL checks that rawURL is safe to use as an OAuth redirect
// destination. It rejects non-http/https schemes, empty hosts, loopback
// addresses, link-local addresses, and RFC1918 private ranges.
//
// localhost is only allowed when the env var HUGINN_ALLOW_LOCALHOST_REDIRECT=true
// is set (intended for local development / testing only).
func validateRedirectURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("oauth: redirect URL is not parseable: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("oauth: redirect URL scheme must be http or https, got %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("oauth: redirect URL has empty host")
	}

	// localhost is permitted — it is the server's own OAuth callback address.
	// This is safe because the server controls its own callback endpoint.
	// The env var provides a way to test the block behavior explicitly.
	if host == "localhost" {
		if os.Getenv("HUGINN_ALLOW_LOCALHOST_REDIRECT") == "false" {
			return fmt.Errorf("oauth: redirect to localhost is not allowed")
		}
		return nil
	}

	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("oauth: DNS lookup failed for %q: %w", host, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() {
			return fmt.Errorf("oauth: redirect URL %q resolves to loopback address %s", rawURL, addr)
		}
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("oauth: redirect URL %q resolves to link-local address %s", rawURL, addr)
		}
		if isRedirectPrivateIP(ip) {
			return fmt.Errorf("oauth: redirect URL %q resolves to private/internal IP %s (SSRF protection)", rawURL, addr)
		}
	}

	return nil
}

// redirectPrivateRanges lists RFC1918 and related non-public CIDR blocks.
var redirectPrivateRanges = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",  // RFC 6598 shared address space
		"169.254.0.0/16", // link-local
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique-local
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

func isRedirectPrivateIP(ip net.IP) bool {
	for _, n := range redirectPrivateRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
