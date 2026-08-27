package backend_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func TestTeammateKeyMissSpeech(t *testing.T) {
	got := backend.TeammateKeyMissSpeech("Sam")
	if got != "Sam couldn't get a key for this." {
		t.Fatalf("got %q", got)
	}
	if backend.TeammateKeyMissSpeech("") != "This teammate couldn't get a key for this." {
		t.Fatalf("empty name: %q", backend.TeammateKeyMissSpeech(""))
	}
}

func TestIsKeyMiss_LiveChatWithAgentLeak(t *testing.T) {
	err := errors.New(`chat(Sam): turn 1: chat completion: resolve api key: api key: keyring lookup failed for service "huginn"`)
	if !backend.IsKeyMiss(err) {
		t.Fatal("expected key miss")
	}
	if backend.IsKeyMiss(errors.New("connection refused")) {
		t.Fatal("network error is not a key miss")
	}
	if backend.IsKeyMiss(nil) {
		t.Fatal("nil is not a key miss")
	}
}

func TestPersistKeyMissSpeech_EmptyLeftover(t *testing.T) {
	err := errors.New(`chat completion: resolve api key: api key: keyring lookup failed for service "huginn"`)
	got := backend.PersistKeyMissSpeech("Sam", "", err)
	if got != "Sam couldn't get a key for this." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{"keyring", "api key", "⚠️", "huginn", "resolve"} {
		if strings.Contains(strings.ToLower(got), leak) && leak != "key" {
			if leak == "key" {
				continue
			}
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
	if strings.Contains(got, "keyring") || strings.Contains(got, "api key") || strings.Contains(got, "⚠️") {
		t.Fatalf("raw error leaked: %q", got)
	}
}

func TestPersistKeyMissSpeech_KeepsLeftover(t *testing.T) {
	err := errors.New(`keyring lookup failed`)
	got := backend.PersistKeyMissSpeech("Sam", "Steve isn't in Lab. Sam is.", err)
	if got != "Steve isn't in Lab. Sam is." {
		t.Fatalf("got %q", got)
	}
}

func TestPersistKeyMissSpeech_NonKeyErrEmpty(t *testing.T) {
	got := backend.PersistKeyMissSpeech("Sam", "", errors.New("connection refused"))
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
