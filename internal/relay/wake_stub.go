//go:build !darwin && !linux

package relay

import "context"

// WakeNotifier is a no-op on platforms without sleep/wake support.
type WakeNotifier struct{}

func NewWakeNotifier() *WakeNotifier { return &WakeNotifier{} }

// Watch returns a channel that never fires on unsupported platforms.
func (w *WakeNotifier) Watch(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	go func() { <-ctx.Done(); close(ch) }()
	return ch // never fires, closes on ctx cancel
}
