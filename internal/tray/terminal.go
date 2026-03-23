package tray

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// TerminalApp represents a detected terminal emulator on this machine.
type TerminalApp struct {
	Name    string // display name, e.g. "Warp"
	AppPath string // macOS .app path or binary path
}

// darwinTerminalCandidates is the ordered list of terminals we detect on macOS.
// The first match is used as the default when no preference is set.
var darwinTerminalCandidates = []TerminalApp{
	{Name: "Warp", AppPath: "/Applications/Warp.app"},
	{Name: "iTerm2", AppPath: "/Applications/iTerm.app"},
	{Name: "Ghostty", AppPath: "/Applications/Ghostty.app"},
	{Name: "Alacritty", AppPath: "/Applications/Alacritty.app"},
	{Name: "Kitty", AppPath: "/Applications/kitty.app"},
	{Name: "Hyper", AppPath: "/Applications/Hyper.app"},
	{Name: "Tabby", AppPath: "/Applications/Tabby.app"},
	// Terminal.app is always available — keep it last as the fallback.
	{Name: "Terminal", AppPath: "/System/Applications/Utilities/Terminal.app"},
}

// DetectTerminals returns the list of terminal apps installed on this machine,
// in preference order. Always includes at least Terminal.app on macOS.
func DetectTerminals() []TerminalApp {
	switch runtime.GOOS {
	case "darwin":
		var found []TerminalApp
		for _, t := range darwinTerminalCandidates {
			if _, err := os.Stat(t.AppPath); err == nil {
				found = append(found, t)
			}
		}
		if len(found) == 0 {
			// Absolute fallback — Terminal.app is always present.
			found = append(found, TerminalApp{Name: "Terminal", AppPath: "/System/Applications/Utilities/Terminal.app"})
		}
		return found
	default:
		return nil
	}
}

// openTerminalFn is the function used by OpenTerminal.
// Override in tests to prevent launching real terminal apps.
var openTerminalFn = openTerminalDefault

// openURLFn is the function used by openURL.
// Override in tests to prevent opening a real browser.
var openURLFn = openURLDefault

// notifyFn is the function used by Notify.
// Override in tests to prevent sending real OS notifications.
var notifyFn = notifyDefault

// OpenTerminal opens the named terminal app on macOS using `open -a`.
// appName is the display name from DetectTerminals (e.g. "Warp", "iTerm2").
// Opens a new window without running any command.
func OpenTerminal(appName string) error {
	return openTerminalFn(appName)
}

func openTerminalDefault(appName string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-a", appName).Start()
	case "linux":
		return openTerminalLinux()
	case "windows":
		return openTerminalWindows()
	default:
		return fmt.Errorf("tray: OpenTerminal not supported on %s", runtime.GOOS)
	}
}

func openTerminalLinux() error {
	candidates := []string{"warp-terminal", "x-terminal-emulator", "gnome-terminal", "konsole", "xterm"}
	if term := os.Getenv("TERMINAL"); term != "" {
		candidates = append([]string{term}, candidates...)
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return exec.Command(path).Start()
		}
	}
	return fmt.Errorf("tray: no terminal emulator found; set $TERMINAL")
}

func openTerminalWindows() error {
	// Prefer Windows Terminal, fall back to cmd.exe.
	if _, err := exec.LookPath("wt.exe"); err == nil {
		return exec.Command("wt.exe").Start()
	}
	return exec.Command("cmd.exe", "/C", "start", "cmd").Start()
}

// OpenURL opens a URL in the system's default browser.
func OpenURL(url string) error {
	return openURLFn(url)
}

// openURL is the internal alias used within the tray package.
func openURL(url string) error {
	return openURLFn(url)
}

func openURLDefault(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("cmd", "/C", "start", url).Start()
	default:
		return fmt.Errorf("tray: openURL not supported on %s", runtime.GOOS)
	}
}

// Notify sends a system notification with title and message.
// Uses platform-native notification mechanisms. Non-fatal on failure.
func Notify(title, message string) error {
	return notifyFn(title, message)
}

func notifyDefault(title, message string) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, message, title)
		return exec.Command("osascript", "-e", script).Start()
	case "linux":
		if path, err := exec.LookPath("notify-send"); err == nil {
			return exec.Command(path, title, message).Start()
		}
		return fmt.Errorf("tray: notify-send not found")
	case "windows":
		ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show(%q, %q)`, message, title)
		return exec.Command("powershell", "-Command", ps).Start()
	default:
		return fmt.Errorf("tray: Notify not supported on %s", runtime.GOOS)
	}
}
