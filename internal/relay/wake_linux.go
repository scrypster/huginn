//go:build linux

package relay

import (
	"context"
	"log/slog"

	"github.com/godbus/dbus/v5"
)

// WakeNotifier listens to org.freedesktop.login1.Manager.PrepareForSleep
// via D-Bus to detect machine resume from suspend.
type WakeNotifier struct{}

func NewWakeNotifier() *WakeNotifier { return &WakeNotifier{} }

// Watch returns a channel that fires on each system resume event.
// The channel is closed when ctx is cancelled.
func (w *WakeNotifier) Watch(ctx context.Context) <-chan struct{} {
	out := make(chan struct{}, 1)

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		slog.Warn("relay: cannot connect to D-Bus system bus, wake detection disabled", "err", err)
		go func() { <-ctx.Done(); close(out) }()
		return out
	}

	// Match PrepareForSleep signal: arg0=false means we are waking up.
	if err := conn.BusObject().Call(
		"org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.freedesktop.login1.Manager',member='PrepareForSleep'",
	).Err; err != nil {
		slog.Warn("relay: cannot add D-Bus match rule for PrepareForSleep, wake detection disabled", "err", err)
		conn.Close()
		go func() { <-ctx.Done(); close(out) }()
		return out
	}

	ch := make(chan *dbus.Signal, 10)
	conn.Signal(ch)

	go func() {
		defer close(out)
		defer conn.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-ch:
				if !ok {
					return
				}
				if sig == nil || len(sig.Body) == 0 {
					continue
				}
				// PrepareForSleep(active bool): active=false means wake.
				if active, ok := sig.Body[0].(bool); ok && !active {
					select {
					case out <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	return out
}
