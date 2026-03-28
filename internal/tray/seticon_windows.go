//go:build windows

package tray

import "fyne.io/systray"

// iconDefaultICO, iconRunningICO, iconCloudICO hold pre-computed ICO-wrapped
// versions of the embedded PNG icons. They are initialised once at startup so
// the pngToICO conversion is never repeated on each icon update call.
var (
	iconDefaultICO []byte
	iconRunningICO []byte
	iconCloudICO   []byte
)

func init() {
	iconDefaultICO = pngToICO(iconDefault, 32, 32)
	iconRunningICO = pngToICO(iconRunning, 32, 32)
	iconCloudICO = pngToICO(iconCloud, 32, 32)
}

// setTrayIcon sets the tray icon on Windows. The embedded icons are PNG bytes,
// but Win32 LoadImage (IMAGE_ICON | LR_LOADFROMFILE) requires ICO format.
// We use pre-computed ICO-wrapped bytes to avoid repeated allocation.
func setTrayIcon(b []byte) {
	switch {
	case &b[0] == &iconDefault[0]:
		systray.SetIcon(iconDefaultICO)
	case &b[0] == &iconRunning[0]:
		systray.SetIcon(iconRunningICO)
	case &b[0] == &iconCloud[0]:
		systray.SetIcon(iconCloudICO)
	default:
		// Fallback for any caller that passes an ad-hoc byte slice.
		systray.SetIcon(pngToICO(b, 32, 32))
	}
}

// pngToICO wraps raw PNG bytes in a single-entry ICO container.
// PNG-in-ICO is natively supported since Windows Vista (MSDN: "ICO (Windows Icon)").
//
// ICO file layout:
//
//	ICONDIR       6 bytes  — file header (reserved, type=1, count=1)
//	ICONDIRENTRY 16 bytes  — image descriptor (width, height, offset, size)
//	PNG data      N bytes  — the original PNG stream, verbatim
//
// The width and height arguments must match the actual PNG image dimensions.
// For the 32×32 icons embedded in icon.go pass w=32, h=32.
// Use 0 only when the image is 256×256 or larger (the ICO "256px" sentinel).
func pngToICO(png []byte, w, h int) []byte {
	const (
		headerSize   = 6
		dirEntrySize = 16
	)
	dataOffset := uint32(headerSize + dirEntrySize)
	size := uint32(len(png))

	ico := make([]byte, int(dataOffset)+len(png))

	// ICONDIR
	ico[0], ico[1] = 0, 0 // reserved
	ico[2], ico[3] = 1, 0 // type = 1 (icon, not cursor)
	ico[4], ico[5] = 1, 0 // image count = 1

	// ICONDIRENTRY
	ico[6] = byte(w)  // width  in pixels (0 = 256)
	ico[7] = byte(h)  // height in pixels (0 = 256)
	ico[8] = 0        // colorCount (0 = no palette / true-colour)
	ico[9] = 0        // reserved
	ico[10], ico[11] = 1, 0  // planes = 1
	ico[12], ico[13] = 32, 0 // bitCount = 32 (RGBA)
	ico[14] = byte(size)
	ico[15] = byte(size >> 8)
	ico[16] = byte(size >> 16)
	ico[17] = byte(size >> 24)
	ico[18] = byte(dataOffset)
	ico[19] = byte(dataOffset >> 8)
	ico[20] = byte(dataOffset >> 16)
	ico[21] = byte(dataOffset >> 24)

	copy(ico[dataOffset:], png)
	return ico
}
