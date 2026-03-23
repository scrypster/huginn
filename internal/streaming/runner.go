// Package streaming provides a testable goroutine-and-channel runner
// for token-streaming LLM calls. It has zero bubbletea dependencies.
package streaming

import (
	"context"
	"fmt"
)

// WorkFn is the function the caller provides. It receives an emit callback
// to send tokens and returns an error (or nil) when done.
type WorkFn func(emit func(string)) error

// Runner manages a single in-flight streaming operation.
// Create one per stream: r := NewRunner(); r.Start(ctx, fn).
type Runner struct {
	tokenCh chan string
	errCh   chan error
}

// NewRunner allocates a Runner with buffered channels.
func NewRunner() *Runner {
	return &Runner{
		tokenCh: make(chan string, 256),
		errCh:   make(chan error, 1),
	}
}

// Start launches fn in a goroutine. Tokens are sent via emit → TokenCh().
// The error (or nil) is sent to ErrCh() after TokenCh() is closed.
// Panics inside fn are caught and converted to errors.
func (r *Runner) Start(ctx context.Context, fn WorkFn) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				close(r.tokenCh)
				r.errCh <- fmt.Errorf("internal panic: %v", rec)
				return
			}
		}()
		err := fn(func(token string) {
			select {
			case r.tokenCh <- token:
			case <-ctx.Done():
			}
		})
		close(r.tokenCh)
		r.errCh <- err
	}()
}

// TokenCh returns the read-only token channel. Range over it until closed.
func (r *Runner) TokenCh() <-chan string { return r.tokenCh }

// ErrCh returns the error channel. Read exactly once after TokenCh is closed.
func (r *Runner) ErrCh() <-chan error { return r.errCh }
