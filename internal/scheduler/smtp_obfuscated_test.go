package scheduler

// smtp_obfuscated_test.go — unit tests for NotificationDelivery.SMTPPassObfuscated
// and the SMTPPassDeprecated flag behaviour.

import (
	"testing"
)

// TestSMTPPassObfuscated_FieldSet verifies that when SMTPPass is non-empty,
// SMTPPassObfuscated returns "[REDACTED]" so that the password is never written
// to logs in plain text.
func TestSMTPPassObfuscated_FieldSet(t *testing.T) {
	d := NotificationDelivery{
		Type:     "email",
		SMTPPass: "super-secret-password",
	}
	got := d.SMTPPassObfuscated()
	if got != "[REDACTED]" {
		t.Errorf("SMTPPassObfuscated() = %q, want %q", got, "[REDACTED]")
	}
}

// TestSMTPPassObfuscated_FieldNotSet verifies that when SMTPPass is empty,
// SMTPPassObfuscated returns "" (not "[REDACTED]") so callers can distinguish
// between "password present but hidden" and "no password configured".
func TestSMTPPassObfuscated_FieldNotSet(t *testing.T) {
	d := NotificationDelivery{
		Type: "email",
		// SMTPPass intentionally left empty
	}
	got := d.SMTPPassObfuscated()
	if got != "" {
		t.Errorf("SMTPPassObfuscated() = %q, want empty string", got)
	}
}

// TestSMTPPassObfuscated_WhitespaceOnlyPassword verifies that a whitespace-only
// password is still considered "set" and returns "[REDACTED]".  A single space
// is a valid (if poor) password value.
func TestSMTPPassObfuscated_WhitespaceOnlyPassword(t *testing.T) {
	d := NotificationDelivery{SMTPPass: " "}
	got := d.SMTPPassObfuscated()
	if got != "[REDACTED]" {
		t.Errorf("SMTPPassObfuscated() = %q, want %q for whitespace-only password", got, "[REDACTED]")
	}
}

// TestSMTPPassDeprecated_FlagDefault verifies that SMTPPassDeprecated defaults
// to false when a NotificationDelivery is constructed without explicit validation.
// The flag is only set to true by the validation layer at workflow-save time.
func TestSMTPPassDeprecated_FlagDefault(t *testing.T) {
	d := NotificationDelivery{
		Type:     "email",
		SMTPPass: "some-pass",
	}
	if d.SMTPPassDeprecated {
		t.Error("SMTPPassDeprecated must default to false before validation")
	}
}

// TestSMTPPassDeprecated_FlagCanBeSetExplicitly verifies that the flag can be
// set to true (simulating what validation does when it detects inline SMTP
// credentials without a named Connection).
func TestSMTPPassDeprecated_FlagCanBeSetExplicitly(t *testing.T) {
	d := NotificationDelivery{
		Type:               "email",
		SMTPPass:           "old-password",
		SMTPPassDeprecated: true,
	}
	if !d.SMTPPassDeprecated {
		t.Error("SMTPPassDeprecated should be true when set explicitly")
	}
	// Deprecation flag does not affect the obfuscation logic.
	if d.SMTPPassObfuscated() != "[REDACTED]" {
		t.Errorf("SMTPPassObfuscated() = %q, want [REDACTED]", d.SMTPPassObfuscated())
	}
}

// TestSMTPPassDeprecated_FlagFalseWithNoPass verifies that the flag remains
// false when no password is set (absence of credentials is not a deprecation).
func TestSMTPPassDeprecated_FlagFalseWithNoPass(t *testing.T) {
	d := NotificationDelivery{
		Type:       "email",
		Connection: "my-smtp-connection",
	}
	if d.SMTPPassDeprecated {
		t.Error("SMTPPassDeprecated should be false when using a named Connection")
	}
	if d.SMTPPassObfuscated() != "" {
		t.Errorf("SMTPPassObfuscated() = %q, want empty", d.SMTPPassObfuscated())
	}
}
