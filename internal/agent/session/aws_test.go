package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
)

func TestParseAWSCredentials_ExtractsProfile(t *testing.T) {
	content := `
[default]
aws_access_key_id = DEFAULTKEY
aws_secret_access_key = DEFAULTSECRET

[staging]
aws_access_key_id = STAGINGKEY
aws_secret_access_key = STAGINGSECRET
`
	got, err := session.ParseAWSProfile(content, "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["aws_access_key_id"] != "STAGINGKEY" {
		t.Errorf("expected STAGINGKEY, got %q", got["aws_access_key_id"])
	}
	if got["aws_secret_access_key"] != "STAGINGSECRET" {
		t.Errorf("expected STAGINGSECRET, got %q", got["aws_secret_access_key"])
	}
}

func TestParseAWSCredentials_FallsBackToDefault(t *testing.T) {
	content := `
[default]
aws_access_key_id = DEFAULTKEY
aws_secret_access_key = DEFAULTSECRET
`
	got, err := session.ParseAWSProfile(content, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["aws_access_key_id"] != "DEFAULTKEY" {
		t.Errorf("expected DEFAULTKEY, got %q", got["aws_access_key_id"])
	}
}

func TestParseAWSCredentials_ErrorOnMissingProfile(t *testing.T) {
	content := `
[default]
aws_access_key_id = DEFAULTKEY
aws_secret_access_key = DEFAULTSECRET
`
	_, err := session.ParseAWSProfile(content, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
	}
}

func TestParseAWSCredentials_EmptySection(t *testing.T) {
	content := "[default]\n\n[staging]\n"
	_, err := session.ParseAWSProfile(content, "staging")
	if err == nil {
		t.Fatal("expected error for empty section")
	}
	if !strings.Contains(err.Error(), "no keys") {
		t.Errorf("expected 'no keys' in error, got: %v", err)
	}
}

func TestWriteAWSSession_CreatesCredentialsFile(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	creds := map[string]string{
		"aws_access_key_id":     "TESTKEY",
		"aws_secret_access_key": "TESTSECRET",
	}
	if err := session.WriteAWSSession(sess, creds, "us-east-1"); err != nil {
		t.Fatalf("WriteAWSSession: %v", err)
	}

	credFile := filepath.Join(sess.Dir, ".aws", "credentials")
	b, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "[default]") {
		t.Errorf("expected [default] section, got:\n%s", content)
	}
	if !strings.Contains(content, "aws_access_key_id = TESTKEY") {
		t.Errorf("expected TESTKEY in credentials, got:\n%s", content)
	}

	// Verify file permission is 0600
	info, _ := os.Stat(credFile)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 perms, got %o", info.Mode().Perm())
	}

	// Verify env vars added to session
	hasCredsEnv := false
	hasConfigEnv := false
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "AWS_SHARED_CREDENTIALS_FILE=") {
			hasCredsEnv = true
		}
		if strings.HasPrefix(e, "AWS_CONFIG_FILE=") {
			hasConfigEnv = true
		}
	}
	if !hasCredsEnv {
		t.Error("expected AWS_SHARED_CREDENTIALS_FILE in session env")
	}
	if !hasConfigEnv {
		t.Error("expected AWS_CONFIG_FILE in session env")
	}

	// Verify config file exists with correct content and permissions
	cfgFile := filepath.Join(sess.Dir, ".aws", "config")
	b, err = os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content = string(b)
	if !strings.Contains(content, "[default]") {
		t.Errorf("expected [default] section in config, got:\n%s", content)
	}
	if !strings.Contains(content, "region = us-east-1") {
		t.Errorf("expected region = us-east-1 in config, got:\n%s", content)
	}

	// Verify config file permission is 0600
	info, _ = os.Stat(cfgFile)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected config 0600 perms, got %o", info.Mode().Perm())
	}
}

func TestSetupAWS_ExtractsProfileAndRegion(t *testing.T) {
	// Create a fake HOME directory with AWS credentials
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	awsDir := filepath.Join(tmpHome, ".aws")
	if err := os.MkdirAll(awsDir, 0700); err != nil {
		t.Fatalf("mkdir .aws: %v", err)
	}

	// Write fake credentials file
	credsContent := `[default]
aws_access_key_id = DEFAULTKEY
aws_secret_access_key = DEFAULTSECRET

[staging]
aws_access_key_id = STAGINGKEY
aws_secret_access_key = STAGINGSECRET
`
	if err := os.WriteFile(filepath.Join(awsDir, "credentials"), []byte(credsContent), 0600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	// Write fake config file
	configContent := `[default]
region = us-west-2

[profile staging]
region = us-east-1
`
	if err := os.WriteFile(filepath.Join(awsDir, "config"), []byte(configContent), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	if err := session.SetupAWS(sess, "staging"); err != nil {
		t.Fatalf("SetupAWS: %v", err)
	}

	// Check credentials file was written
	credFile := filepath.Join(sess.Dir, ".aws", "credentials")
	b, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read session credentials: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "STAGINGKEY") {
		t.Errorf("expected STAGINGKEY in session credentials, got:\n%s", content)
	}

	// Check region came from config
	cfgFile := filepath.Join(sess.Dir, ".aws", "config")
	cfgBytes, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read session config: %v", err)
	}
	if !strings.Contains(string(cfgBytes), "us-east-1") {
		t.Errorf("expected region us-east-1 in session config, got:\n%s", string(cfgBytes))
	}
}

func TestParseAWSProfile_CaseInsensitiveSection(t *testing.T) {
	content := "[STAGING]\naws_access_key_id = KEY123\naws_secret_access_key = SECRET456\n"
	creds, err := session.ParseAWSProfile(content, "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["aws_access_key_id"] != "KEY123" {
		t.Errorf("expected KEY123, got %q", creds["aws_access_key_id"])
	}
	if creds["aws_secret_access_key"] != "SECRET456" {
		t.Errorf("expected SECRET456, got %q", creds["aws_secret_access_key"])
	}
}

func TestParseAWSProfile_SkipsLineWithoutEquals(t *testing.T) {
	content := "[default]\nno_equals_line\naws_access_key_id = KEY123\naws_secret_access_key = SECRET456\n"
	creds, err := session.ParseAWSProfile(content, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["aws_access_key_id"] != "KEY123" {
		t.Errorf("expected KEY123, got %q", creds["aws_access_key_id"])
	}
	if creds["aws_secret_access_key"] != "SECRET456" {
		t.Errorf("expected SECRET456, got %q", creds["aws_secret_access_key"])
	}
	// Verify that the bad line did not create a key
	if len(creds) != 2 {
		t.Errorf("expected 2 keys, got %d", len(creds))
	}
}

func TestParseAWSProfile_ExplicitDefaultProfile(t *testing.T) {
	content := "[default]\naws_access_key_id = KEY123\naws_secret_access_key = SECRET456\n"
	creds, err := session.ParseAWSProfile(content, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["aws_access_key_id"] != "KEY123" {
		t.Errorf("expected KEY123, got %q", creds["aws_access_key_id"])
	}
	if creds["aws_secret_access_key"] != "SECRET456" {
		t.Errorf("expected SECRET456, got %q", creds["aws_secret_access_key"])
	}
}

func TestWriteAWSSession_EmptyCredentials(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	creds := map[string]string{}
	if err := session.WriteAWSSession(sess, creds, ""); err != nil {
		t.Fatalf("WriteAWSSession with empty creds: %v", err)
	}

	// Verify credentials file was created with [default] section
	credFile := filepath.Join(sess.Dir, ".aws", "credentials")
	b, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "[default]") {
		t.Errorf("expected [default] section in credentials, got:\n%s", content)
	}

	// Verify that only [default] header is present with no key-value pairs
	expected := "[default]\n"
	if content != expected {
		t.Errorf("expected credentials to be %q, got %q", expected, content)
	}

	// Verify file permission is 0600
	info, _ := os.Stat(credFile)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 perms, got %o", info.Mode().Perm())
	}

	// Verify config file was created with [default] section and output = json
	cfgFile := filepath.Join(sess.Dir, ".aws", "config")
	cfgBytes, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgContent := string(cfgBytes)
	if !strings.Contains(cfgContent, "[default]") {
		t.Errorf("expected [default] section in config, got:\n%s", cfgContent)
	}
	if !strings.Contains(cfgContent, "output = json") {
		t.Errorf("expected output = json in config, got:\n%s", cfgContent)
	}
}
