//go:build darwin

package relay

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	launchAgentLabel = "com.scrypster.huginn"
	plistName        = "com.scrypster.huginn.plist"
)

type darwinServiceManager struct {
	dir string
}

func newPlatformServiceManager() (ServiceManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("service: cannot determine home dir: %w", err)
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	return &darwinServiceManager{dir: dir}, nil
}

// NewServiceManagerForDir returns a ServiceManager that installs to dir (for tests).
func NewServiceManagerForDir(dir string) ServiceManager {
	return &darwinServiceManager{dir: dir}
}

func (m *darwinServiceManager) plistPath() string {
	return filepath.Join(m.dir, plistName)
}

func (m *darwinServiceManager) Install(binaryPath string) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("service: mkdir: %w", err)
	}
	plist := buildPlist(binaryPath)
	if err := os.WriteFile(m.plistPath(), []byte(plist), 0o644); err != nil {
		return fmt.Errorf("service: write plist: %w", err)
	}
	exec.Command("launchctl", "load", "-w", m.plistPath()).Run() //nolint:errcheck
	return nil
}

func (m *darwinServiceManager) Uninstall() error {
	path := m.plistPath()
	exec.Command("launchctl", "unload", "-w", path).Run() //nolint:errcheck
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove plist: %w", err)
	}
	return nil
}

func (m *darwinServiceManager) IsInstalled() bool {
	_, err := os.Stat(m.plistPath())
	return err == nil
}

func buildPlist(binaryPath string) string {
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
    <string>start</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>5</integer>
  <key>StandardOutPath</key>
  <string>/tmp/huginn-relay.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/huginn-relay.err</string>
</dict>
</plist>`, launchAgentLabel, binaryPath)) + "\n"
}
