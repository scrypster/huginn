package threadmgr

import (
	"errors"
	"strings"
	"testing"
)

func TestFinishSummaryForLLMError_KeyMiss(t *testing.T) {
	err := errors.New(`chat completion: resolve api key: api key: keyring lookup failed for service "huginn"`)
	got := finishSummaryForLLMError("Sam", err)
	if got.Status != "error" {
		t.Fatalf("status %q", got.Status)
	}
	if got.Summary != "Sam couldn't get a key for this." {
		t.Fatalf("summary %q", got.Summary)
	}
	if strings.Contains(got.Summary, "LLM API error") || strings.Contains(got.Summary, "keyring") {
		t.Fatalf("raw leak: %q", got.Summary)
	}
}

func TestFinishSummaryForLLMError_OtherError(t *testing.T) {
	err := errors.New("connection refused")
	got := finishSummaryForLLMError("Sam", err)
	if !strings.Contains(got.Summary, "connection refused") {
		t.Fatalf("summary %q", got.Summary)
	}
}
