package streaming_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/streaming"
)

func TestRunner_TokenAccumulation(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		emit("hello")
		emit(" ")
		emit("world")
		return nil
	})

	var tokens []string
	for tok := range r.TokenCh() {
		tokens = append(tokens, tok)
	}
	if err := <-r.ErrCh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := strings.Join(tokens, "")
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestRunner_ErrorPropagation(t *testing.T) {
	r := streaming.NewRunner()
	want := errors.New("boom")
	r.Start(context.Background(), func(emit func(string)) error {
		emit("partial")
		return want
	})

	for range r.TokenCh() {
	}
	err := <-r.ErrCh()
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
}

func TestRunner_Cancellation(t *testing.T) {
	r := streaming.NewRunner()
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx, func(emit func(string)) error {
		emit("before")
		cancel()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	})

	for range r.TokenCh() {
	}
	err := <-r.ErrCh()
	if err == nil {
		t.Error("expected cancellation error, got nil")
	}
}

func TestRunner_PanicRecovery(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		emit("before panic")
		panic("something went wrong")
	})

	for range r.TokenCh() {
	}
	err := <-r.ErrCh()
	if err == nil {
		t.Error("expected panic recovery error, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected 'panic' in error, got: %v", err)
	}
}

func TestRunner_EmptyStream(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		return nil
	})

	var tokens []string
	for tok := range r.TokenCh() {
		tokens = append(tokens, tok)
	}
	if err := <-r.ErrCh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected no tokens, got %v", tokens)
	}
}
