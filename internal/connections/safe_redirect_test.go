package connections

import (
	"net"
	"strings"
	"testing"
)

func TestValidateRedirectURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		envKey  string
		envVal  string
		wantErr bool
		errFrag string
	}{
		{
			name:    "valid https public",
			url:     "https://example.com/oauth/callback",
			wantErr: false,
		},
		{
			name:    "valid http public",
			url:     "http://example.com/callback",
			wantErr: false,
		},
		{
			name:    "non-http scheme ftp blocked",
			url:     "ftp://example.com/callback",
			wantErr: true,
			errFrag: "scheme",
		},
		{
			name:    "javascript scheme blocked",
			url:     "javascript://example.com",
			wantErr: true,
			errFrag: "scheme",
		},
		{
			name:    "loopback 127.0.0.1 blocked",
			url:     "https://127.0.0.1/callback",
			wantErr: true,
			errFrag: "loopback",
		},
		{
			name:    "loopback ::1 blocked",
			url:     "http://[::1]/callback",
			wantErr: true,
			errFrag: "loopback",
		},
		{
			name:    "localhost allowed by default",
			url:     "http://localhost/callback",
			wantErr: false,
		},
		{
			name:    "localhost blocked when env=false",
			url:     "http://localhost/callback",
			envKey:  "HUGINN_ALLOW_LOCALHOST_REDIRECT",
			envVal:  "false",
			wantErr: true,
			errFrag: "localhost",
		},
		{
			name:    "empty host blocked",
			url:     "https:///callback",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envKey != "" {
				t.Setenv(tc.envKey, tc.envVal)
			}
			err := validateRedirectURL(tc.url)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got: %v", tc.url, err)
			}
			if tc.wantErr && tc.errFrag != "" && err != nil && !strings.Contains(err.Error(), tc.errFrag) {
				t.Errorf("expected error to contain %q, got: %v", tc.errFrag, err)
			}
		})
	}
}

// TestValidateRedirectURL_RFC1918 verifies private IP ranges are blocked
// by testing the isRedirectPrivateIP helper directly.
func TestValidateRedirectURL_RFC1918(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.100", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("could not parse IP %q", c.ip)
		}
		got := isRedirectPrivateIP(ip)
		if got != c.blocked {
			t.Errorf("isRedirectPrivateIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}
