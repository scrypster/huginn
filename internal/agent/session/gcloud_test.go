package session_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
)

func TestSetupGCloud_AddsEnvVars(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	session.SetupGCloud(sess, "dev-project")

	hasConfig := false
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "CLOUDSDK_ACTIVE_CONFIG_NAME=dev-project") {
			hasConfig = true
		}
	}
	if !hasConfig {
		t.Error("expected CLOUDSDK_ACTIVE_CONFIG_NAME=dev-project in env")
	}
}

func TestSetupGCloud_EmptyProfile_NoEnvVarAdded(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	session.SetupGCloud(sess, "")

	for _, e := range sess.Env {
		if strings.HasPrefix(e, "CLOUDSDK_ACTIVE_CONFIG_NAME=") {
			t.Errorf("expected no CLOUDSDK_ACTIVE_CONFIG_NAME when configName is empty, got: %s", e)
		}
	}
}

func TestSetupGCloud_DoesNotSetCLOUDSDK_CONFIG(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	session.SetupGCloud(sess, "dev-project")

	// CLOUDSDK_CONFIG is intentionally NOT set — gcloud needs the real config
	// dir to read credentials for the named configuration. See gcloud.go.
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "CLOUDSDK_CONFIG=") {
			t.Errorf("CLOUDSDK_CONFIG should not be set (gcloud needs real config dir), got: %s", e)
		}
	}
}

func TestSetupGCloud_DuplicateCallsDoNotPanic(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	// Call twice — should not panic
	session.SetupGCloud(sess, "profile1")
	session.SetupGCloud(sess, "profile2")

	// CLOUDSDK_ACTIVE_CONFIG_NAME should be present at least once
	found := false
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "CLOUDSDK_ACTIVE_CONFIG_NAME=") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected CLOUDSDK_ACTIVE_CONFIG_NAME in session env after SetupGCloud")
	}
}
