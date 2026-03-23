package relay

import "errors"

// ErrServiceUnsupported is returned on platforms without auto-start support.
var ErrServiceUnsupported = errors.New("relay: background service not supported on this platform")

// ServiceManager installs and uninstalls the huginn relay as a background service.
type ServiceManager interface {
	// Install writes the service descriptor and loads/enables the service.
	Install(binaryPath string) error
	// Uninstall stops, unloads, and removes the service descriptor.
	Uninstall() error
	// IsInstalled reports whether the service descriptor file exists.
	IsInstalled() bool
}

// NewServiceManager returns the platform-appropriate ServiceManager.
func NewServiceManager() (ServiceManager, error) {
	return newPlatformServiceManager()
}
