//go:build linux

package relay

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const systemdUnitName = "huginn-relay.service"

type linuxServiceManager struct {
	dir string
}

func newPlatformServiceManager() (ServiceManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("service: cannot determine home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "systemd", "user")
	return &linuxServiceManager{dir: dir}, nil
}

// NewServiceManagerForDir returns a ServiceManager that installs to dir (for tests).
func NewServiceManagerForDir(dir string) ServiceManager {
	return &linuxServiceManager{dir: dir}
}

func (m *linuxServiceManager) unitPath() string {
	return filepath.Join(m.dir, systemdUnitName)
}

func (m *linuxServiceManager) Install(binaryPath string) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("service: mkdir: %w", err)
	}
	unit := buildUnit(binaryPath)
	if err := os.WriteFile(m.unitPath(), []byte(unit), 0o644); err != nil {
		return fmt.Errorf("service: write unit: %w", err)
	}
	exec.Command("systemctl", "--user", "daemon-reload").Run()               //nolint:errcheck
	exec.Command("systemctl", "--user", "enable", "--now", systemdUnitName).Run() //nolint:errcheck
	return nil
}

func (m *linuxServiceManager) Uninstall() error {
	exec.Command("systemctl", "--user", "disable", "--now", systemdUnitName).Run() //nolint:errcheck
	if err := os.Remove(m.unitPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove unit: %w", err)
	}
	exec.Command("systemctl", "--user", "daemon-reload").Run() //nolint:errcheck
	return nil
}

func (m *linuxServiceManager) IsInstalled() bool {
	_, err := os.Stat(m.unitPath())
	return err == nil
}

func buildUnit(binaryPath string) string {
	return strings.TrimSpace(fmt.Sprintf(`[Unit]
Description=Huginn Satellite Relay
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s relay start
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
`, binaryPath)) + "\n"
}
