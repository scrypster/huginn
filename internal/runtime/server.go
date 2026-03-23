package runtime

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Manager owns the llama-server subprocess lifecycle.
type Manager struct {
	huginnDir string
	platform  Platform
	manifest  *RuntimeManifest
	cmd       *exec.Cmd
	port      int
	modelPath string
	stderrBuf bytes.Buffer
}

// NewManager creates a Manager.
func NewManager(huginnDir string) (*Manager, error) {
	m, err := LoadManifest()
	if err != nil {
		return nil, err
	}
	return &Manager{
		huginnDir: huginnDir,
		platform:  Detect(),
		manifest:  m,
	}, nil
}

// BinaryPath returns the expected path of the llama-server binary.
func (m *Manager) BinaryPath() string {
	return filepath.Join(m.huginnDir, "bin",
		"llama-server-"+m.manifest.LlamaServerVersion,
		"llama-server")
}

// IsInstalled returns true if the llama-server binary exists on disk.
func (m *Manager) IsInstalled() bool {
	_, err := os.Stat(m.BinaryPath())
	return err == nil
}

// Download fetches and extracts the llama-server binary for the current platform.
// onProgress is called with bytes downloaded and total.
func (m *Manager) Download(ctx context.Context, onProgress func(downloaded, total int64)) error {
	entry, ok := m.manifest.BinaryForPlatform(m.platform.Key())
	if !ok {
		return fmt.Errorf("no llama-server binary available for platform %q", m.platform.Key())
	}

	binDir := filepath.Dir(m.BinaryPath())
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}

	// Determine archive extension based on type
	archiveExt := ".zip"
	if entry.ArchiveType == "tar.gz" {
		archiveExt = ".tar.gz"
	}
	archivePath := filepath.Join(binDir, "llama-server"+archiveExt)
	if err := downloadFile(ctx, entry.URL, archivePath, onProgress); err != nil {
		return fmt.Errorf("download runtime: %w", err)
	}

	// Extract based on archive type
	switch entry.ArchiveType {
	case "tar.gz":
		if err := extractTarGz(archivePath, entry.ExtractPath, m.BinaryPath()); err != nil {
			return fmt.Errorf("extract runtime: %w", err)
		}
	default: // zip
		if err := extractZip(archivePath, entry.ExtractPath, m.BinaryPath()); err != nil {
			return fmt.Errorf("extract runtime: %w", err)
		}
	}

	if err := os.Chmod(m.BinaryPath(), 0755); err != nil {
		return err
	}

	os.Remove(archivePath) // clean up archive
	return nil
}

// Start launches llama-server with the given model file on the given port.
func (m *Manager) Start(modelPath string, port int) error {
	m.port = port
	m.modelPath = modelPath
	m.cmd = exec.Command(m.BinaryPath(),
		"--model", modelPath,
		"--port", fmt.Sprintf("%d", port),
		"--ctx-size", "4096",
		"--parallel", "1",
	)
	m.cmd.Stdout = nil
	m.stderrBuf.Reset()
	m.cmd.Stderr = &m.stderrBuf
	return m.cmd.Start()
}

// WaitForReady polls /health until the server responds or ctx expires.
// It also detects process crashes via ProcessState so it fails fast instead of
// waiting the full 30-second timeout.
func (m *Manager) WaitForReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	url := fmt.Sprintf("http://localhost:%d/health", m.port)
	hc := &http.Client{Timeout: 2 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("llama-server failed to start within 30s: %s", m.stderrBuf.String())
		case <-ticker.C:
			// Check if the process has already exited.
			if m.cmd.ProcessState != nil {
				return fmt.Errorf("llama-server exited with status: %v\n%s",
					m.cmd.ProcessState, m.stderrBuf.String())
			}
			resp, err := hc.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// Endpoint returns the base URL of the running llama-server.
func (m *Manager) Endpoint() string {
	return fmt.Sprintf("http://localhost:%d", m.port)
}

// Shutdown stops the llama-server process gracefully.
func (m *Manager) Shutdown() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	_ = m.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- m.cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return m.cmd.Process.Kill()
	}
}

// Port returns the port the manager's llama-server is listening on.
func (m *Manager) Port() int {
	return m.port
}

// Cmd returns the underlying exec.Cmd for the llama-server process.
func (m *Manager) Cmd() *exec.Cmd {
	return m.cmd
}

// FindFreePort asks the OS for a free TCP port on the loopback interface.
// It binds to "127.0.0.1:0" (never ":0") to avoid exposing the ephemeral
// bind to all network interfaces.
func FindFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// --- helpers ---

func downloadFile(ctx context.Context, url, dest string, onProgress func(int64, int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			downloaded += int64(n)
			if onProgress != nil {
				onProgress(downloaded, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func extractZip(archivePath, extractPath, finalPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != extractPath {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(finalPath)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, rc)
		return err
	}
	return fmt.Errorf("extract_path %q not found in archive", extractPath)
}

func extractTarGz(archivePath, extractPath, finalPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Name != extractPath {
			continue
		}
		out, err := os.Create(finalPath)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, tr)
		return err
	}
	return fmt.Errorf("extract_path %q not found in archive", extractPath)
}
