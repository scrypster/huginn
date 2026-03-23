//go:build windows

package relay

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	scheduledTaskName          = "HuginnRelay"
	scheduledTaskNameSentinel  = "HuginnRelaySentinel"
)

type windowsServiceManager struct {
	taskName string
}

func newPlatformServiceManager() (ServiceManager, error) {
	return &windowsServiceManager{taskName: scheduledTaskName}, nil
}

// NewServiceManagerForDir ignores the dir argument on Windows (used only for testing on other platforms).
func NewServiceManagerForDir(_ string) ServiceManager {
	return &windowsServiceManager{taskName: scheduledTaskName}
}

func NewSentinelServiceManager() (ServiceManager, error) {
	return &windowsServiceManager{taskName: scheduledTaskNameSentinel}, nil
}

func (m *windowsServiceManager) Install(binaryPath string) error {
	subcommand := "start"
	if strings.Contains(m.taskName, "Sentinel") {
		subcommand = "sentinel"
	}
	args := []string{
		"/Create", "/F",
		"/TN", m.taskName,
		"/TR", fmt.Sprintf(`"%s" relay %s`, binaryPath, subcommand),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
	}
	out, err := exec.Command("schtasks", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("windows: schtasks create failed: %w\n%s", err, out)
	}
	return nil
}

func (m *windowsServiceManager) Uninstall() error {
	out, err := exec.Command("schtasks", "/Delete", "/F", "/TN", m.taskName).CombinedOutput()
	if err != nil {
		// Task not found is not an error during uninstall
		if strings.Contains(string(out), "cannot find") || strings.Contains(string(out), "does not exist") {
			return nil
		}
		return fmt.Errorf("windows: schtasks delete failed: %w\n%s", err, out)
	}
	return nil
}

func (m *windowsServiceManager) IsInstalled() bool {
	out, err := exec.Command("schtasks", "/Query", "/TN", m.taskName).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), m.taskName)
}
