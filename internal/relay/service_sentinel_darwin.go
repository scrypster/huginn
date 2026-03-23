//go:build darwin

package relay

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	sentinelDaemonLabel = "com.scrypster.huginn.sentinel"
	sentinelPlistName   = "com.scrypster.huginn.sentinel.plist"
	launchDaemonsDir    = "/Library/LaunchDaemons"
)

type darwinSentinelServiceManager struct{}

// NewSentinelServiceManager returns a ServiceManager for the sentinel LaunchDaemon.
func NewSentinelServiceManager() (ServiceManager, error) {
	return &darwinSentinelServiceManager{}, nil
}

func (m *darwinSentinelServiceManager) plistPath() string {
	return launchDaemonsDir + "/" + sentinelPlistName
}

func (m *darwinSentinelServiceManager) Install(binaryPath string) error {
	plist := buildSentinelPlist(binaryPath)
	if err := os.WriteFile(m.plistPath(), []byte(plist), 0o644); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("sentinel install requires root — run with sudo: %w", err)
		}
		return fmt.Errorf("sentinel: write plist: %w", err)
	}
	if out, err := exec.Command("launchctl", "load", "-w", m.plistPath()).CombinedOutput(); err != nil {
		return fmt.Errorf("sentinel: launchctl load failed: %w\n%s", err, out)
	}
	return nil
}

func (m *darwinSentinelServiceManager) Uninstall() error {
	exec.Command("launchctl", "unload", "-w", m.plistPath()).Run() //nolint:errcheck
	if err := os.Remove(m.plistPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sentinel: remove plist: %w", err)
	}
	return nil
}

func (m *darwinSentinelServiceManager) IsInstalled() bool {
	_, err := os.Stat(m.plistPath())
	return err == nil
}

func buildSentinelPlist(binaryPath string) string {
	return strings.TrimSpace(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>relay</string>
    <string>sentinel</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>/var/log/huginn-sentinel.log</string>
  <key>StandardErrorPath</key>
  <string>/var/log/huginn-sentinel.err</string>
</dict>
</plist>`, sentinelDaemonLabel, binaryPath)) + "\n"
}
