package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
)

func TestBuildAndSetup_NoToolbelt_NoShims(t *testing.T) {
	// Empty toolbelt — session should still be created cleanly
	sess, err := session.BuildAndSetup(nil)
	if err != nil {
		t.Fatalf("BuildAndSetup: %v", err)
	}
	defer sess.Teardown()

	if sess.Dir == "" {
		t.Error("expected non-empty session dir")
	}
	// Env should have HOME set
	hasHome := false
	for _, e := range sess.Env {
		if len(e) > 5 && e[:5] == "HOME=" {
			hasHome = true
		}
	}
	if !hasHome {
		t.Error("expected HOME= in session env")
	}
}

func TestBuildAndSetup_UnknownProvider(t *testing.T) {
	// Unknown providers should be silently skipped without error
	sess, err := session.BuildAndSetup([]session.ToolbeltEntry{
		{Provider: "unknown_xyz", Profile: ""},
	})
	if err != nil {
		t.Fatalf("unexpected error for unknown provider: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	sess.Teardown()
}

func TestBuildAndSetup_KubectlProvider_NoCredentialSetupNoShim(t *testing.T) {
	// kubectl is in knownCLITools → toolbeltBinaries["kubectl"] = true → no shim installed
	// No credential setup case exists for kubectl in BuildAndSetup.
	// This test confirms the session is returned without error and kubectl is NOT shimmed.
	toolbelt := []session.ToolbeltEntry{{Provider: "kubectl"}}
	sess, err := session.BuildAndSetup(toolbelt)
	if err != nil {
		t.Fatalf("BuildAndSetup: %v", err)
	}
	defer sess.Teardown()

	// kubectl is in knownCLITools → toolbeltBinaries["kubectl"] = true → no shim installed
	// No credential setup case exists for kubectl → this is documented behavior
	shimPath := filepath.Join(sess.Dir, "bin", "kubectl")
	if _, err := os.Stat(shimPath); err == nil {
		t.Error("expected no kubectl shim (kubectl is in toolbelt → allowed binary)")
	}
}

func TestBuildAndSetup_AWS_GracefulFailureOnMissingProfile(t *testing.T) {
	// SetupAWS reads ~/.aws/credentials via os.UserHomeDir(). A nonexistent profile
	// causes ParseAWSProfile to return an error, which BuildAndSetup logs as a warning
	// and continues — the session must still be returned without error.
	toolbelt := []session.ToolbeltEntry{
		{Provider: "aws", Profile: "nonexistent-profile-xyz"},
	}
	sess, err := session.BuildAndSetup(toolbelt)
	if err != nil {
		t.Fatalf("BuildAndSetup should not return an error on missing AWS profile: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	defer sess.Teardown()
}

func TestBuildAndSetup_GithubCLI_BinaryNotFound_Skips(t *testing.T) {
	// When "gh" is not on PATH, exec.LookPath("gh") fails and SetupGH is never called.
	// BuildAndSetup must return a valid session without error.
	t.Setenv("PATH", t.TempDir())

	toolbelt := []session.ToolbeltEntry{
		{Provider: "github_cli", Profile: ""},
	}
	sess, err := session.BuildAndSetup(toolbelt)
	if err != nil {
		t.Fatalf("BuildAndSetup: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	defer sess.Teardown()
}

func TestBuildAndSetup_GCloud_SetsConfigEnvVar(t *testing.T) {
	// SetupGCloud appends CLOUDSDK_ACTIVE_CONFIG_NAME=<profile> to sess.Env when
	// a non-empty profile is provided. Verify that the env var is present.
	toolbelt := []session.ToolbeltEntry{
		{Provider: "gcloud", Profile: "my-test-config"},
	}
	sess, err := session.BuildAndSetup(toolbelt)
	if err != nil {
		t.Fatalf("BuildAndSetup: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	defer sess.Teardown()

	const want = "CLOUDSDK_ACTIVE_CONFIG_NAME=my-test-config"
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "CLOUDSDK_ACTIVE_CONFIG_NAME=") {
			if e == want {
				return // pass
			}
			t.Fatalf("CLOUDSDK_ACTIVE_CONFIG_NAME present but wrong value: got %q, want %q", e, want)
		}
	}
	t.Errorf("expected %q in sess.Env, got: %v", want, sess.Env)
}
