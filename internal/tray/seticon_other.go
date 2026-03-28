//go:build !windows

package tray

import "fyne.io/systray"

// setTrayIcon sets the tray icon. On non-Windows platforms PNG bytes are
// passed directly to systray (macOS and Linux both handle PNG natively).
func setTrayIcon(b []byte) { systray.SetIcon(b) }
