package server

import (
	"net"
	"strings"
	"testing"
)

func TestValidateSubdomain_Valid(t *testing.T) {
	for _, c := range []string{"mycompany", "my-company", "a", "abc123", "my-co-123"} {
		if err := validateSubdomain(c); err != nil {
			t.Errorf("%q should be valid: %v", c, err)
		}
	}
}

func TestValidateSubdomain_Invalid(t *testing.T) {
	for _, c := range []string{"", "my.company", "a/b", "../etc", "my company", "-start", "end-"} {
		if err := validateSubdomain(c); err == nil {
			t.Errorf("%q should be invalid", c)
		}
	}
}

func TestValidateBaseURL_BlocksLoopback(t *testing.T) {
	for _, u := range []string{"http://localhost/evil", "http://127.0.0.1/evil", "https://127.0.0.1/evil"} {
		if err := validateBaseURL(u); err == nil {
			t.Errorf("loopback URL %q should be blocked", u)
		}
	}
}

func TestValidateBaseURL_BlocksLinkLocal(t *testing.T) {
	for _, u := range []string{"https://169.254.169.254/latest/meta-data/", "https://169.254.1.1/"} {
		if err := validateBaseURL(u); err == nil {
			t.Errorf("link-local URL %q should be blocked", u)
		}
	}
}

func TestValidateBaseURL_RequiresHTTPS(t *testing.T) {
	if err := validateBaseURL("http://public.example.com"); err == nil {
		t.Error("http:// should be rejected")
	}
}

func TestValidateBaseURL_AllowsHTTPS(t *testing.T) {
	for _, u := range []string{
		"https://inputs.datadoghq.com",
		"https://my-splunk.example.com:8089",
		"https://grafana.mycompany.com",
	} {
		if err := validateBaseURL(u); err != nil {
			t.Errorf("%q should be valid: %v", u, err)
		}
	}
}

func TestIsBlockedIP_Loopback(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "127.0.0.100", "::1"} {
		if !isBlockedIP(ip) {
			t.Errorf("loopback %q should be blocked", ip)
		}
	}
}

func TestIsBlockedIP_Unspecified(t *testing.T) {
	if !isBlockedIP("0.0.0.0") {
		t.Error("0.0.0.0 should be blocked")
	}
}

func TestIsBlockedIP_IPv4MappedIPv6(t *testing.T) {
	if !isBlockedIP("::ffff:127.0.0.1") {
		t.Error("::ffff:127.0.0.1 (IPv4-mapped loopback) should be blocked")
	}
}

func TestIsBlockedIP_NonIPString(t *testing.T) {
	// Non-IP strings (hostnames, garbage) are not blocked — the hostname path
	// relies on DNS resolution to catch blocked IPs.
	for _, s := range []string{"not-an-ip", "my-splunk.example.com", "999.999.999.999"} {
		if isBlockedIP(s) {
			t.Errorf("non-IP string %q should not be blocked by isBlockedIP", s)
		}
	}
}

func TestIsBlockedIP_RFC1918(t *testing.T) {
	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "172.16.0.1", "172.31.255.255"} {
		if !isBlockedIP(ip) {
			t.Errorf("RFC 1918 address %q should be blocked", ip)
		}
	}
}

func TestIsBlockedIP_LinkLocal(t *testing.T) {
	for _, ip := range []string{"169.254.0.1", "169.254.169.254"} {
		if !isBlockedIP(ip) {
			t.Errorf("link-local %q should be blocked", ip)
		}
	}
}

func TestIsBlockedIP_PublicIP(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "203.0.113.1"} {
		if isBlockedIP(ip) {
			t.Errorf("public IP %q should not be blocked", ip)
		}
	}
}

func TestSafeHTTPClient_BlocksLoopback(t *testing.T) {
	// Start a real HTTP listener on loopback to prove the client refuses to connect.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind loopback listener: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	client := safeHTTPClient()
	_, err = client.Get("http://" + addr)
	if err == nil {
		t.Fatal("expected safeHTTPClient to block loopback connection, got nil error")
	}
	// Accept either "blocked" in the message or a connection refusal due to no server.
	errStr := err.Error()
	if !strings.Contains(errStr, "blocked") && !strings.Contains(errStr, "refused") && !strings.Contains(errStr, "connect") {
		t.Errorf("expected a dial/blocked error, got: %v", err)
	}
}

func TestSafeHTTPClient_IsConstructable(t *testing.T) {
	client := safeHTTPClient()
	if client == nil {
		t.Fatal("safeHTTPClient() returned nil")
	}
}

func TestValidateGitHubUsername_Valid(t *testing.T) {
	for _, c := range []string{"octocat", "my-user", "user123", "a"} {
		if err := validateGitHubUsername(c); err != nil {
			t.Errorf("%q should be valid: %v", c, err)
		}
	}
}

func TestValidateGitHubUsername_Invalid(t *testing.T) {
	for _, c := range []string{"", "--help", "user name", "user/evil"} {
		if err := validateGitHubUsername(c); err == nil {
			t.Errorf("%q should be invalid", c)
		}
	}
}
