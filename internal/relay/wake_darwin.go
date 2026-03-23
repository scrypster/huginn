//go:build darwin

package relay

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <IOKit/IOMessage.h>
#include <CoreFoundation/CoreFoundation.h>

extern int wakeCB_go(void);

static IONotificationPortRef gNotifyPort;
static io_object_t            gPowerNotifier;

static void powerCallback(void *refcon, io_service_t service, natural_t messageType, void *messageArgument) {
    if (messageType == kIOMessageSystemHasPoweredOn) {
        wakeCB_go();
    }
    // IOAllowPowerChange must be called for all power messages (including
    // kIOMessageSystemWillSleep and kIOMessageCanSystemSleep) to allow the
    // system to proceed. It is a no-op for kIOMessageSystemHasPoweredOn.
    IOAllowPowerChange(*(io_connect_t*)refcon, (long)messageArgument);
}

static io_connect_t registerPowerCallback(void) {
    io_connect_t root_port = 0;
    gNotifyPort = IONotificationPortCreate(kIOMainPortDefault);
    CFRunLoopAddSource(CFRunLoopGetCurrent(),
        IONotificationPortGetRunLoopSource(gNotifyPort),
        kCFRunLoopDefaultMode);
    IORegisterForSystemPower(&root_port, &gNotifyPort, powerCallback, &gPowerNotifier);
    return root_port;
}
*/
import "C"

import (
	"context"
	"runtime"
	"sync"
)

var (
	wakeMu   sync.Mutex
	wakeChs  []chan struct{}
	wakeOnce sync.Once
)

//export wakeCB_go
func wakeCB_go() C.int {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	for _, ch := range wakeChs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return 0
}

// WakeNotifier delivers a signal on machine wake from sleep.
type WakeNotifier struct {
	ch chan struct{}
}

// NewWakeNotifier creates and registers an IOKit power notification listener.
// Call once at startup. The returned notifier must not be garbage collected.
func NewWakeNotifier() *WakeNotifier {
	ch := make(chan struct{}, 1)
	wakeMu.Lock()
	wakeChs = append(wakeChs, ch)
	wakeMu.Unlock()
	wakeOnce.Do(func() {
		started := make(chan struct{})
		go func() {
			runtime.LockOSThread()
			C.registerPowerCallback()
			close(started)
			C.CFRunLoopRun() // blocks; delivers IOKit notifications on this OS thread
		}()
		<-started // wait for registration before returning
	})
	return &WakeNotifier{ch: ch}
}

// Watch returns a channel that receives a value on each system wake event.
// The channel is closed when ctx is cancelled.
func (w *WakeNotifier) Watch(ctx context.Context) <-chan struct{} {
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.ch:
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	return out
}
