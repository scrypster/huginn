package runtime

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const pidFile = "llama.pid" // relative to huginn config dir

// WritePIDFile writes the child PID and port to the given path.
func WritePIDFile(path string, pid, port int) error {
	content := fmt.Sprintf("%d %d\n", pid, port)
	return os.WriteFile(path, []byte(content), 0600)
}

// ReadPIDFile reads the PID and port from the given path.
// Returns 0, 0, nil if the file does not exist.
func ReadPIDFile(path string) (pid, port int, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(strings.TrimSpace(string(data)))
	if len(parts) != 2 {
		return 0, 0, nil // corrupt, ignore
	}
	pid, _ = strconv.Atoi(parts[0])
	port, _ = strconv.Atoi(parts[1])
	return pid, port, nil
}

// CleanupZombie checks if a previous llama-server process is still alive.
// If alive, kills it. Removes the PID file either way.
func CleanupZombie(pidFilePath string) {
	pid, _, err := ReadPIDFile(pidFilePath)
	if err != nil || pid == 0 {
		if err := os.Remove(pidFilePath); err != nil && !os.IsNotExist(err) {
			// Log or handle error: file exists but couldn't be removed
			fmt.Fprintf(os.Stderr, "warning: failed to remove PID file %q: %v\n", pidFilePath, err)
		}
		return
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		proc.Signal(syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		proc.Kill()
	}
	if err := os.Remove(pidFilePath); err != nil && !os.IsNotExist(err) {
		// Log or handle error: file exists but couldn't be removed
		fmt.Fprintf(os.Stderr, "warning: failed to remove PID file %q: %v\n", pidFilePath, err)
	}
}

// IsProcessAlive returns true if the process with the given PID is alive.
func IsProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
