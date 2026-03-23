package scheduler

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// ── isPrivateIP ───────────────────────────────────────────────────────────────

func TestIsPrivateIP_Loopback(t *testing.T) {
	// 127.0.0.1 is NOT in privateRanges; loopback is handled by ip.IsLoopback()
	// in safeWebhookURL separately. isPrivateIP only covers RFC1918/6598/ULA.
	if isPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should not be in privateRanges (loopback handled separately in safeWebhookURL)")
	}
}

func TestIsPrivateIP_RFC1918_10(t *testing.T) {
	if !isPrivateIP(net.ParseIP("10.0.0.1")) {
		t.Error("10.0.0.1 should be private (RFC1918 10/8)")
	}
}

func TestIsPrivateIP_RFC1918_172(t *testing.T) {
	if !isPrivateIP(net.ParseIP("172.16.0.1")) {
		t.Error("172.16.0.1 should be private (RFC1918 172.16/12)")
	}
}

func TestIsPrivateIP_RFC1918_192(t *testing.T) {
	if !isPrivateIP(net.ParseIP("192.168.1.1")) {
		t.Error("192.168.1.1 should be private (RFC1918 192.168/16)")
	}
}

func TestIsPrivateIP_SharedAddressSpace(t *testing.T) {
	if !isPrivateIP(net.ParseIP("100.64.0.1")) {
		t.Error("100.64.0.1 should be private (RFC6598 shared address space)")
	}
}

func TestIsPrivateIP_PublicIP(t *testing.T) {
	if isPrivateIP(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 should NOT be private")
	}
}

func TestIsPrivateIP_IPv6UniqueLocal(t *testing.T) {
	if !isPrivateIP(net.ParseIP("fc00::1")) {
		t.Error("fc00::1 should be private (IPv6 unique local fc00::/7)")
	}
}

func TestIsPrivateIP_IPv6LinkLocal(t *testing.T) {
	if !isPrivateIP(net.ParseIP("fe80::1")) {
		t.Error("fe80::1 should be private (IPv6 link-local fe80::/10)")
	}
}

// ── safeWebhookURL ────────────────────────────────────────────────────────────

func TestSafeWebhookURL_InvalidScheme(t *testing.T) {
	err := safeWebhookURL("ftp://example.com/hook")
	if err == nil {
		t.Fatal("expected error for non-http(s) scheme")
	}
	if !isPermanent(err) {
		t.Error("expected permanent error for invalid scheme")
	}
}

func TestSafeWebhookURL_MalformedURL(t *testing.T) {
	if err := safeWebhookURL("not-a-url"); err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

// ── deliverWithRetry ──────────────────────────────────────────────────────────

func TestDeliverWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	err := deliverWithRetry(context.Background(), []time.Duration{10 * time.Millisecond}, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDeliverWithRetry_RetryOnTransientFailure(t *testing.T) {
	calls := 0
	err := deliverWithRetry(context.Background(), []time.Duration{1 * time.Millisecond, 1 * time.Millisecond}, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDeliverWithRetry_ExhaustedRetries(t *testing.T) {
	calls := 0
	sentinel := errors.New("always fails")
	err := deliverWithRetry(context.Background(), []time.Duration{1 * time.Millisecond, 1 * time.Millisecond}, func() error {
		calls++
		return sentinel
	})
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if calls != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDeliverWithRetry_PermanentErrorNotRetried(t *testing.T) {
	calls := 0
	err := deliverWithRetry(context.Background(), []time.Duration{time.Second, time.Second}, func() error {
		calls++
		return &permanentError{errors.New("permanent")}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 call for permanent error, got %d", calls)
	}
}

