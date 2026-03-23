package relay

import (
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// ShellManager owns a single persistent PTY shell. The same PTY survives
// browser reconnects — send shell_start again to reattach.
type ShellManager struct {
	mu      sync.Mutex
	ptmx    *os.File
	cmd     *exec.Cmd
	hub     Hub
	running bool
}

// NewShellManager creates an idle ShellManager. Call Start to open the PTY.
func NewShellManager() *ShellManager {
	return &ShellManager{}
}

// Start opens (or reattaches to) the PTY shell and sends shell_ready to hub.
// If the shell is already running, sends shell_ready immediately without
// restarting the process.
func (sm *ShellManager) Start(hub Hub, cols, rows uint16) {
	sm.mu.Lock()
	sm.hub = hub

	// Check and set sm.running atomically to prevent concurrent shell startups.
	if sm.running {
		sm.mu.Unlock()
		_ = hub.Send("", Message{
			Type: MsgShellReady,
			Payload: map[string]any{
				"cols": float64(cols),
				"rows": float64(rows),
			},
		})
		return
	}

	// Mark running before unlocking so concurrent Start calls reattach.
	// This check-and-set is atomic while holding the lock.
	sm.running = true
	sm.mu.Unlock()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell)
	// Only pass safe env vars to the shell — never inherit API keys or secrets.
	safeEnv := []string{
		"TERM=xterm-256color",
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"LOGNAME=" + os.Getenv("LOGNAME"),
		"SHELL=" + os.Getenv("SHELL"),
		"LANG=" + os.Getenv("LANG"),
		"LC_ALL=" + os.Getenv("LC_ALL"),
	}
	// Include PATH — safe and necessary for basic shell operation.
	if path := os.Getenv("PATH"); path != "" {
		safeEnv = append(safeEnv, "PATH="+path)
	}
	cmd.Env = safeEnv

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		sm.mu.Lock()
		sm.running = false
		sm.mu.Unlock()
		_ = hub.Send("", Message{
			Type:    MsgShellExit,
			Payload: map[string]any{"error": err.Error()},
		})
		return
	}

	// NOTE: If StartWithSize fails after concurrent callers have entered the
	// reattach path (running==true), those callers will have sent shell_ready
	// prematurely. This is a known limitation of the optimistic locking pattern
	// and is acceptable given the single-persistent-shell use case.
	sm.mu.Lock()
	sm.ptmx = ptmx
	sm.cmd = cmd
	sm.mu.Unlock()

	_ = hub.Send("", Message{
		Type: MsgShellReady,
		Payload: map[string]any{
			"cols": float64(cols),
			"rows": float64(rows),
		},
	})

	// NOTE: hub is stored at Start time. If the WebSocket reconnects and a new hub
	// is created, call Start again with the new hub to update the reference.
	go sm.readLoop()
}

// Input base64-decodes data and writes raw bytes to the PTY master.
func (sm *ShellManager) Input(b64data string) {
	sm.mu.Lock()
	ptmx := sm.ptmx
	running := sm.running
	sm.mu.Unlock()

	if !running || ptmx == nil {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(b64data)
	if err != nil {
		slog.Warn("relay: shell input: invalid base64", "err", err)
		return
	}
	_, _ = ptmx.Write(raw)
}

// Resize sends a TIOCSWINSZ ioctl to resize the PTY window.
func (sm *ShellManager) Resize(cols, rows uint16) {
	sm.mu.Lock()
	ptmx := sm.ptmx
	running := sm.running
	sm.mu.Unlock()

	if !running || ptmx == nil {
		return
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Exit kills the shell process and closes the PTY.
// Sends shell_exit to the browser if a hub is registered.
func (sm *ShellManager) Exit() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.exitLocked(nil)
}

// exitLocked tears down the shell. Must be called with sm.mu held.
func (sm *ShellManager) exitLocked(errMsg *string) {
	if !sm.running {
		return
	}
	sm.running = false

	if sm.cmd != nil && sm.cmd.Process != nil {
		c := sm.cmd
		_ = c.Process.Kill()
		go func() { _ = c.Wait() }()
	}
	if sm.ptmx != nil {
		_ = sm.ptmx.Close()
		sm.ptmx = nil
	}
	sm.cmd = nil

	payload := map[string]any{}
	if errMsg != nil {
		payload["error"] = *errMsg
	}
	if sm.hub != nil {
		_ = sm.hub.Send("", Message{Type: MsgShellExit, Payload: payload})
	}
}

// readLoop reads PTY output and sends shell_output messages until EOF or error.
func (sm *ShellManager) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		sm.mu.Lock()
		ptmx := sm.ptmx
		sm.mu.Unlock()
		if ptmx == nil {
			return
		}

		n, err := ptmx.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			sm.mu.Lock()
			hub := sm.hub
			sm.mu.Unlock()
			if hub != nil {
				_ = hub.Send("", Message{
					Type:    MsgShellOutput,
					Payload: map[string]any{"data": encoded},
				})
			}
		}
		if err != nil {
			if err != io.EOF {
				// EIO on Linux when PTY master is closed — treat as EOF
				_ = err
			}
			sm.mu.Lock()
			if sm.running {
				sm.exitLocked(nil)
			}
			sm.mu.Unlock()
			return
		}
	}
}
