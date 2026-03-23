package tools

import (
	"net"
	"testing"
)

// TestIsBlockedIP_ExtendedRanges verifies that isBlockedIP correctly rejects
// all SSRF-relevant address ranges beyond the standard stdlib predicates.
func TestIsBlockedIP_ExtendedRanges(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		addr    string
		blocked bool
	}{
		// ---- Already-covered by stdlib (regression) ----
		{"loopback_v4", "127.0.0.1", true},
		{"loopback_v6", "::1", true},
		{"rfc1918_10", "10.1.2.3", true},
		{"rfc1918_172", "172.31.0.1", true},
		{"rfc1918_192", "192.168.0.1", true},
		{"link_local_unicast_v4", "169.254.10.5", true},
		{"unspecified_v4", "0.0.0.0", true},
		{"unspecified_v6", "::", true},

		// ---- Newly-blocked ranges ----
		// "This" network 0.0.0.0/8
		{"this_network_0_1", "0.1.2.3", true},
		{"this_network_0_255", "0.255.255.255", true},

		// CGNAT / shared address space 100.64.0.0/10 (RFC 6598)
		{"cgnat_low", "100.64.0.1", true},
		{"cgnat_mid", "100.100.50.25", true},
		{"cgnat_high", "100.127.255.254", true},

		// IPv4 multicast 224.0.0.0/4 (Class D)
		{"multicast_v4_low", "224.0.0.1", true},
		{"multicast_v4_all_hosts", "224.0.0.2", true},
		{"multicast_v4_mid", "239.0.0.1", true},
		{"multicast_v4_high", "239.255.255.255", true},

		// IPv4 reserved 240.0.0.0/4 (Class E)
		{"reserved_v4_low", "240.0.0.1", true},
		{"reserved_v4_mid", "248.0.0.1", true},
		{"reserved_v4_broadcast", "255.255.255.255", true},

		// IPv6 multicast ff00::/8
		{"multicast_v6_all_nodes", "ff02::1", true},
		{"multicast_v6_all_routers", "ff02::2", true},
		{"multicast_v6_high", "ffff::1", true},

		// IPv6 link-local fe80::/10
		{"link_local_v6", "fe80::1", true},
		{"link_local_v6_2", "fe80::dead:beef", true},

		// ---- Public addresses (must NOT be blocked) ----
		{"public_v4_cloudflare_dns", "1.1.1.1", false},
		{"public_v4_google_dns", "8.8.8.8", false},
		{"public_v4_example", "93.184.216.34", false},
		{"public_v6_google", "2001:4860:4860::8888", false},
		// 100.63.x — just below CGNAT range
		{"public_v4_below_cgnat", "100.63.255.255", false},
		// 100.128.x — just above CGNAT range
		{"public_v4_above_cgnat", "100.128.0.1", false},
		// 223.x — just below multicast
		{"public_v4_below_multicast", "223.255.255.255", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tc.addr)
			if ip == nil {
				t.Fatalf("failed to parse test IP %q", tc.addr)
			}
			got := isBlockedIP(ip)
			if got != tc.blocked {
				t.Errorf("isBlockedIP(%q) = %v, want %v", tc.addr, got, tc.blocked)
			}
		})
	}
}

// TestFetchURL_SSRF_MulticastBlocked verifies that the FetchURLTool rejects
// IPv4 multicast addresses via the pre-flight guard.
func TestFetchURL_SSRF_MulticastBlocked(t *testing.T) {
	t.Parallel()

	// 224.0.0.1 is "All Hosts" multicast — never a legitimate web server.
	// We do not need the port to be open; the SSRF guard fires before dial.
	tool := &FetchURLTool{}
	result := tool.Execute(nil, map[string]any{ //nolint:staticcheck // context unused by guard path
		"url": "http://224.0.0.1:80/",
	})
	// The guard may reject via isPrivateHost (pre-flight DNS / IP parse) or
	// ssrfSafeDialContext; either way IsError must be true.
	if !result.IsError {
		t.Error("expected SSRF block for multicast address 224.0.0.1, got success")
	}
}

// TestFetchURL_SSRF_CGNATBlocked verifies that CGNAT (100.64.0.0/10) is blocked.
func TestFetchURL_SSRF_CGNATBlocked(t *testing.T) {
	t.Parallel()

	tool := &FetchURLTool{}
	result := tool.Execute(nil, map[string]any{ //nolint:staticcheck
		"url": "http://100.64.0.1:80/",
	})
	if !result.IsError {
		t.Error("expected SSRF block for CGNAT address 100.64.0.1, got success")
	}
}
