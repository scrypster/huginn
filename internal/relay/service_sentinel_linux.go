//go:build linux

package relay

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	sentinelSystemdUnit = "huginn-sentinel.service"
	systemdSystemDir    = "/etc/systemd/system"
)

type linuxSentinelServiceManager struct{}

func NewSentinelServiceManager() (ServiceManager, error) {
	return &linuxSentinelServiceManager{}, nil
}

func (m *linuxSentinelServiceManager) unitPath() string {
	return systemdSystemDir + "/" + sentinelSystemdUnit
}

func (m *linuxSentinelServiceManager) Install(binaryPath string) error {
	unit := buildSentinelUnit(binaryPath)
	if err := os.WriteFile(m.unitPath(), []byte(unit), 0o644); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("sentinel install requires root — run with sudo: %w", err)
		}
		return fmt.Errorf("sentinel: write unit: %w", err)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("sentinel: systemctl daemon-reload failed: %w\n%s", err, out)
	}
	if out, err := exec.Command("systemctl", "enable", "--now", sentinelSystemdUnit).CombinedOutput(); err != nil {
		return fmt.Errorf("sentinel: systemctl enable failed: %w\n%s", err, out)
	}
	return nil
}

func (m *linuxSentinelServiceManager) Uninstall() error {
	exec.Command("systemctl", "disable", "--now", sentinelSystemdUnit).Run() //nolint:errcheck
	if err := os.Remove(m.unitPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sentinel: remove unit: %w", err)
	}
	exec.Command("systemctl", "daemon-reload").Run() //nolint:errcheck
	return nil
}

func (m *linuxSentinelServiceManager) IsInstalled() bool {
	_, err := os.Stat(m.unitPath())
	return err == nil
}

func buildSentinelUnit(binaryPath string) string {
	return strings.TrimSpace(fmt.Sprintf(`[Unit]
Description=Huginn Sentinel (presence-only, no code execution)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=%s relay sentinel
Restart=on-failure
RestartSec=10s
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
`, binaryPath)) + "\n"
}
