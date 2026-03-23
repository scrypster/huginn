package mcp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	closed bool
	done   chan struct{} // closed by Close() to unblock Receive goroutines
	wg     sync.WaitGroup
}

func NewStdioTransport(ctx context.Context, command string, args []string, env []string) (*StdioTransport, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", command, err)
	}
	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		done:   make(chan struct{}),
	}, nil
}

func (t *StdioTransport) Send(_ context.Context, msg []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("transport is closed")
	}
	_, err := t.stdin.Write(append(msg, '\n'))
	return err
}

func (t *StdioTransport) Receive(ctx context.Context) ([]byte, error) {
	// Early-out: don't spawn a goroutine if the context is already cancelled.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				err = fmt.Errorf("mcp: server disconnected: %w", err)
			}
			ch <- result{nil, err}
			return
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		ch <- result{line, nil}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.done:
		return nil, io.EOF
	case r := <-ch:
		return r.data, r.err
	}
}

func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	// Close stdin first to signal the subprocess that input is done.
	t.stdin.Close()
	// Close the done channel so any select in Receive returns immediately.
	close(t.done)
	// Send SIGTERM for graceful shutdown, then SIGKILL after 2s.
	// Killing the process unblocks any ReadBytes() calls in Receive goroutines.
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Signal(syscall.SIGTERM)
		timer := time.AfterFunc(2*time.Second, func() {
			t.cmd.Process.Kill()
		})
		// Wait for all Receive goroutines to exit (ReadBytes unblocked by process death).
		t.wg.Wait()
		timer.Stop()
	}
	// Reap the child process to avoid a zombie.
	t.cmd.Wait()
	return nil
}

var _ Transport = (*StdioTransport)(nil)
