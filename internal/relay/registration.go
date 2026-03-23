package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"
)

const (
	// defaultCloudAppURL is the web frontend for user login and machine approval.
	defaultCloudAppURL = "https://app.huginncloud.com"
	// defaultCloudAPIURL is the backend API used for device code flows.
	defaultCloudAPIURL = "https://api.huginncloud.com"

	pollInterval        = 3 * time.Second
	registrationTimeout = 5 * time.Minute

	// HuginnCloudBaseURL is the default base URL for the HuginnCloud web app.
	HuginnCloudBaseURL = defaultCloudAppURL

	// OAuthBrokerURL is the base URL for the HuginnCloud OAuth broker service.
	OAuthBrokerURL = "https://oauth.huginncloud.com"
)

var (
	// ErrDeviceCodeDenied is returned when the user denies the device code.
	ErrDeviceCodeDenied = errors.New("device code denied by user")
	// ErrDeviceCodePending is returned when the device code is still pending.
	ErrDeviceCodePending = errors.New("pending")
	// ErrDeviceCodeExpired is returned when the device code has expired (410).
	ErrDeviceCodeExpired = errors.New("device code expired — run 'huginn connect' to try again")
)

// Registrar handles the huginn connect flow.
type Registrar struct {
	// appURL is the web frontend URL used to open the browser registration page.
	appURL string
	// apiURL is the backend API URL used for device code start/poll requests.
	apiURL     string
	machineID  string
	tokenStore TokenStorer
	// OpenBrowserFn is called to open the registration URL in a browser.
	// If nil, the platform default (open / xdg-open) is used.
	// Tests should set this to a no-op to prevent real browser windows.
	OpenBrowserFn func(rawURL string) error
	// PollInterval overrides the default device-code polling interval.
	// Zero means use the package default (3s). Tests set this to a small value.
	PollInterval time.Duration
}

// NewRegistrar creates a Registrar backed by the OS keyring token store.
// baseURL overrides the app URL (for testing/custom deployments); pass "" for defaults.
func NewRegistrar(baseURL string) *Registrar {
	appURL := defaultCloudAppURL
	apiURL := defaultCloudAPIURL
	if baseURL != "" {
		appURL = baseURL
		apiURL = baseURL
	}
	return &Registrar{
		machineID:  GetMachineID(),
		tokenStore: NewTokenStore(),
		appURL:     appURL,
		apiURL:     apiURL,
	}
}

// NewRegistrarWithStore creates a Registrar with a custom TokenStorer (for testing).
func NewRegistrarWithStore(baseURL string, store TokenStorer) *Registrar {
	appURL := defaultCloudAppURL
	apiURL := defaultCloudAPIURL
	if baseURL != "" {
		appURL = baseURL
		apiURL = baseURL
	}
	return &Registrar{
		machineID:  GetMachineID(),
		tokenStore: store,
		appURL:     appURL,
		apiURL:     apiURL,
	}
}

// RegisterResult is returned by Register on success.
type RegisterResult struct {
	APIKey    string
	MachineID string
}

// Register opens the browser and waits for the user to approve the connection.
// Falls back to Device Code flow if browser cannot be opened or callback times out.
func (r *Registrar) Register(ctx context.Context, machineName string) (*RegisterResult, error) {
	if machineName == "" {
		machineName, _ = os.Hostname()
	}

	// Try browser flow first
	result, err := r.browserFlow(ctx, machineName)
	if err == nil {
		// Save the API key to the token store
		if saveErr := r.tokenStore.Save(result.APIKey); saveErr != nil {
			return nil, fmt.Errorf("registration: save token: %w", saveErr)
		}
		fmt.Printf("Machine '%s' registered to HuginnCloud.\n", result.MachineID)
		return result, nil
	}
	slog.Warn("browser flow failed, falling back to device code", "err", err)

	result, err = r.deviceCodeFlow(ctx)
	if err != nil {
		return nil, err
	}
	// Save the API key to the token store
	if saveErr := r.tokenStore.Save(result.APIKey); saveErr != nil {
		return nil, fmt.Errorf("registration: save token: %w", saveErr)
	}
	slog.Info("relay: machine registered to HuginnCloud", "machine_id", result.MachineID)
	return result, nil
}

// browserFlow: open browser to /connect, start local HTTP server, wait for callback.
func (r *Registrar) browserFlow(ctx context.Context, machineName string) (*RegisterResult, error) {
	// Guard against already-cancelled contexts before starting.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Pick a random port for the callback server
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("find free port: %w", err)
	}

	resultCh := make(chan *RegisterResult, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			fmt.Fprintf(w, "<html><body><h2>Connection denied.</h2><p>You can close this window.</p></body></html>")
			errCh <- fmt.Errorf("user denied: %s", errParam)
			go srv.Shutdown(context.Background()) //nolint:errcheck
			return
		}
		apiKey := q.Get("api_key")
		machineID := q.Get("machine_id")
		if apiKey == "" || machineID == "" {
			http.Error(w, "missing api_key or machine_id", http.StatusBadRequest)
			errCh <- errors.New("callback missing required params")
			return
		}
		fmt.Fprintf(w, "<html><body><h2>Connected!</h2><p>You can close this window.</p></body></html>")
		resultCh <- &RegisterResult{APIKey: apiKey, MachineID: machineID}
		go srv.Shutdown(context.Background()) //nolint:errcheck
	})

	// Start the server
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("callback server: %w", err)
		}
	}()

	// Build the connect URL
	cbURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	connectURL := fmt.Sprintf("%s/connect?cb=%s&name=%s&machine_id=%s",
		r.appURL,
		url.QueryEscape(cbURL),
		url.QueryEscape(machineName),
		url.QueryEscape(r.machineID),
	)

	slog.Info("opening browser for registration", "url", connectURL)
	openFn := r.OpenBrowserFn
	if openFn == nil {
		openFn = openBrowser
	}
	if err := openFn(connectURL); err != nil {
		srv.Shutdown(context.Background()) //nolint:errcheck
		return nil, fmt.Errorf("open browser: %w", err)
	}

	fmt.Printf("Waiting for HuginnCloud authorization...\n(Or visit: %s)\n", connectURL) // User must see this URL

	// Wait for result
	timeout := time.After(registrationTimeout)
	select {
	case <-ctx.Done():
		srv.Shutdown(context.Background()) //nolint:errcheck
		return nil, ctx.Err()
	case <-timeout:
		srv.Shutdown(context.Background()) //nolint:errcheck
		return nil, fmt.Errorf("registration timed out after %s", registrationTimeout)
	case err := <-errCh:
		return nil, err
	case result := <-resultCh:
		return result, nil
	}
}

// deviceStartResponse is the JSON body returned by GET /api/device/start.
type deviceStartResponse struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}

// startDeviceCode calls GET /api/device/start to obtain a server-issued device code.
// Falls back to a locally-generated code if the endpoint is unreachable so that
// the flow degrades gracefully in offline / test environments.
func (r *Registrar) startDeviceCode(ctx context.Context) (string, error) {
	startURL := fmt.Sprintf("%s/api/device/start", r.apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, startURL, nil)
	if err != nil {
		return generateDeviceCode(), nil // fallback: local code
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return generateDeviceCode(), nil // server unreachable — use local code
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return generateDeviceCode(), nil // non-200 — fall back gracefully
	}

	var body deviceStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Code == "" {
		return generateDeviceCode(), nil
	}
	return body.Code, nil
}

// deviceCodeFlow: display a code, poll for completion.
func (r *Registrar) deviceCodeFlow(ctx context.Context) (*RegisterResult, error) {
	// Obtain a device code from the server (falls back to local generation on error).
	code, err := r.startDeviceCode(ctx)
	if err != nil {
		return nil, fmt.Errorf("device code start: %w", err)
	}
	fmt.Printf("\nOpen %s/device and enter code: %s\n\n", r.appURL, code) // User must see this device code and URL

	// Poll for completion
	pollURL := fmt.Sprintf("%s/api/device/poll?code=%s&machine_id=%s", r.apiURL, code, r.machineID)
	interval := r.PollInterval
	if interval <= 0 {
		interval = pollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	timeout := time.After(registrationTimeout)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("device code flow timed out")
		case <-ticker.C:
			result, err := r.pollDeviceCode(ctx, pollURL)
			if err != nil {
				// Fatal errors: stop immediately.
				if errors.Is(err, ErrDeviceCodeDenied) || errors.Is(err, ErrDeviceCodeExpired) {
					return nil, err
				}
				continue // transient: pending or network error, keep polling
			}
			return result, nil
		}
	}
}

type devicePollResponse struct {
	Status    string `json:"status"` // "pending" | "approved" | "denied"
	APIKey    string `json:"api_key"`
	MachineID string `json:"machine_id"`
}

func (r *Registrar) pollDeviceCode(ctx context.Context, pollURL string) (*RegisterResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusGone: // 410 — code expired
		fmt.Fprintln(os.Stderr, "Error: device code has expired. Run 'huginn connect' to start over.")
		return nil, ErrDeviceCodeExpired
	case http.StatusTooManyRequests: // 429 — rate limited; caller retries after back-off
		return nil, fmt.Errorf("rate limited (429)")
	default:
		return nil, fmt.Errorf("poll returned %d", resp.StatusCode)
	}

	var result devicePollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	switch result.Status {
	case "approved":
		return &RegisterResult{APIKey: result.APIKey, MachineID: result.MachineID}, nil
	case "denied":
		return nil, ErrDeviceCodeDenied
	default:
		return nil, ErrDeviceCodePending
	}
}

// RegisterWithToken stores a pre-provisioned fleet token without browser flow.
// token is a machine JWT issued by HuginnCloud (via fleet registration API).
// machineName is the display name for this machine in HuginnCloud.
// This is the MDM/fleet deployment path: the token is delivered via config
// profile or configuration management; the user never sees a browser.
func (r *Registrar) RegisterWithToken(token, machineName string) (*RegisterResult, error) {
	if token == "" {
		return nil, fmt.Errorf("registration: fleet token cannot be empty")
	}
	if machineName == "" {
		machineName, _ = os.Hostname()
	}
	if err := r.tokenStore.Save(token); err != nil {
		return nil, fmt.Errorf("registration: save token: %w", err)
	}
	result := &RegisterResult{
		APIKey:    token,
		MachineID: r.machineID,
	}
	slog.Info("relay: registered via fleet token", "machine_id", r.machineID, "name", machineName)
	slog.Info("relay: machine registered via fleet token", "machine_id", result.MachineID)
	return result, nil
}

// Unregister removes the stored machine token.
func (r *Registrar) Unregister() error { return r.tokenStore.Clear() }

// Status returns whether this machine is registered and its machine ID.
func (r *Registrar) Status() (registered bool, machineID string) {
	return r.tokenStore.IsRegistered(), r.machineID
}

// DeliverCode is kept for backward compatibility with the server callback interface.
// In the new API key flow, registration is handled via the local callback server,
// so this is a no-op.
func (r *Registrar) DeliverCode(code string) {
	slog.Debug("relay: DeliverCode called (no-op in API key flow)", "code_len", len(code))
}

// openBrowser opens the default browser on the current platform.
func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", rawURL)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}

// freePort returns an available TCP port on localhost.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// generateDeviceCode returns a human-friendly random code like "ABC-123".
func generateDeviceCode() string {
	letters := "ABCDEFGHJKLMNPQRSTUVWXYZ"
	digits := "0123456789"
	l := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		return string(b)
	}
	d := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = digits[rand.Intn(len(digits))]
		}
		return string(b)
	}
	return l(3) + "-" + d(3)
}
