package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// CloudVaultClient is the interface for pushing memory entries to HuginnCloud
// and fetching them back (for initial bulk sync and incremental sync).
type CloudVaultClient interface {
	// PushEntries sends memory operations to the cloud vault.
	PushEntries(ctx context.Context, machineID string, entries []VaultPushEntry) error
	// FetchAll retrieves a page of all vault entries (bulk sync path).
	// cursor="" fetches from the beginning. Returns (entries, nextCursor, err).
	FetchAll(ctx context.Context, machineID, agentFilter, cursor string) ([]VaultFetchEntry, string, error)
	// FetchSince retrieves a page of vault entries created after sinceMs (incremental sync).
	// sinceComposite should be fmt.Sprintf("%d#", sinceMs).
	FetchSince(ctx context.Context, machineID, sinceComposite, cursor string) ([]VaultFetchEntry, string, error)
}

// VaultPushEntry is a memory operation sent to the cloud vault.
type VaultPushEntry struct {
	Op        string `json:"op"`          // "set" or "delete"
	AgentName string `json:"agent_name"`
	MemoryID  string `json:"memory_id"`
	Vault     string `json:"vault"`
	Concept   string `json:"concept"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"` // unix epoch ms
}

// VaultFetchEntry is a memory entry returned from the cloud vault.
type VaultFetchEntry struct {
	Op        string `json:"op"`
	AgentName string `json:"agent_name"`
	MemoryID  string `json:"memory_id"`
	Vault     string `json:"vault"`
	Concept   string `json:"concept"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

// httpVaultClient implements CloudVaultClient over the HuginnCloud REST API.
type httpVaultClient struct {
	baseURL    string
	httpClient *http.Client
	token      func() string // always-fresh API key getter
}

// NewHTTPVaultClient creates a CloudVaultClient backed by the HuginnCloud API.
// token is called on every request to get the current bearer token.
func NewHTTPVaultClient(baseURL string, token func() string) CloudVaultClient {
	return &httpVaultClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		token: token,
	}
}

func (c *httpVaultClient) PushEntries(ctx context.Context, machineID string, entries []VaultPushEntry) error {
	body, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal entries: %w", err)
	}
	url := c.baseURL + "/api/v1/vault/" + machineID + "/memories"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("push entries: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push entries: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *httpVaultClient) FetchAll(ctx context.Context, machineID, agentFilter, cursor string) ([]VaultFetchEntry, string, error) {
	url := c.baseURL + "/api/v1/vault/" + machineID + "/memories"
	if agentFilter != "" {
		url += "?agent=" + agentFilter
		if cursor != "" {
			url += "&after=" + cursor
		}
	} else if cursor != "" {
		url += "?after=" + cursor
	}
	return c.fetchPage(ctx, url)
}

func (c *httpVaultClient) FetchSince(ctx context.Context, machineID, sinceComposite, cursor string) ([]VaultFetchEntry, string, error) {
	url := c.baseURL + "/api/v1/vault/" + machineID + "/memories?since=" + sinceComposite
	if cursor != "" {
		url += "&after=" + cursor
	}
	return c.fetchPage(ctx, url)
}

func (c *httpVaultClient) fetchPage(ctx context.Context, url string) ([]VaultFetchEntry, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, "", fmt.Errorf("fetch page: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Entries    []VaultFetchEntry `json:"entries"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("decode response: %w", err)
	}
	return result.Entries, result.NextCursor, nil
}

// ---------------------------------------------------------------------------
// Retry helper
// ---------------------------------------------------------------------------

// withRetry retries op up to 3 times with exponential backoff (100/200/400ms).
// Context cancellation stops the retry loop immediately.
func withRetry(ctx context.Context, op func() error) error {
	backoff := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	var err error
	for i, d := range backoff {
		if err = op(); err == nil {
			return nil
		}
		if i < len(backoff)-1 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}

// ---------------------------------------------------------------------------
// VaultSyncMarker — Pebble-backed sync state
// ---------------------------------------------------------------------------

// VaultSyncMarker tracks whether the initial bulk sync has completed and
// the high-water-mark timestamp for subsequent incremental syncs.
type VaultSyncMarker struct {
	Complete     bool  `json:"complete"`
	LastSyncedAt int64 `json:"last_synced_at"` // unix ms high-water mark
}

// pebbleMarkerStore defines the minimal Pebble interface needed by the vault
// sync logic. Implemented by the storage.Store's KV methods.
type pebbleMarkerStore interface {
	GetBytes(key string) ([]byte, bool)
	SetBytes(key string, value []byte) error
}

const vaultSyncMarkerKey = "vault_sync_marker"

// readSyncMarker reads the vault sync marker from Pebble.
// Returns a zero-value marker if not yet written.
func readSyncMarker(kv pebbleMarkerStore) VaultSyncMarker {
	data, ok := kv.GetBytes(vaultSyncMarkerKey)
	if !ok {
		return VaultSyncMarker{}
	}
	var m VaultSyncMarker
	if err := json.Unmarshal(data, &m); err != nil {
		slog.Warn("vault sync: failed to parse marker", "err", err)
		return VaultSyncMarker{}
	}
	return m
}

// writeSyncMarker persists the vault sync marker to Pebble.
func writeSyncMarker(kv pebbleMarkerStore, m VaultSyncMarker) {
	data, err := json.Marshal(m)
	if err != nil {
		slog.Warn("vault sync: failed to marshal marker", "err", err)
		return
	}
	if err := kv.SetBytes(vaultSyncMarkerKey, data); err != nil {
		slog.Warn("vault sync: failed to write marker", "err", err)
	}
}

// ---------------------------------------------------------------------------
// BulkSync and IncrementalSync
// ---------------------------------------------------------------------------

// BulkSync performs a full paginated fetch of all vault entries for machineID
// and stores them locally. Writes the sync marker on completion.
// Each page fetch is retried up to 3 times on transient errors.
func BulkSync(ctx context.Context, client CloudVaultClient, machineID string, kv pebbleMarkerStore, onEntry func(VaultFetchEntry)) error {
	slog.Info("vault sync: starting bulk sync", "machine_id", machineID)
	var cursor string
	for {
		var entries []VaultFetchEntry
		var nextCursor string
		if err := withRetry(ctx, func() error {
			var err error
			entries, nextCursor, err = client.FetchAll(ctx, machineID, "", cursor)
			return err
		}); err != nil {
			return fmt.Errorf("bulk sync page: %w", err)
		}
		for _, e := range entries {
			onEntry(e)
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	writeSyncMarker(kv, VaultSyncMarker{
		Complete:     true,
		LastSyncedAt: time.Now().UnixMilli(),
	})
	slog.Info("vault sync: bulk sync complete", "machine_id", machineID)
	return nil
}

// IncrementalSync fetches vault entries created after marker.LastSyncedAt
// using the GSI, and updates the high-water mark on completion.
func IncrementalSync(ctx context.Context, client CloudVaultClient, machineID string, marker VaultSyncMarker, kv pebbleMarkerStore, onEntry func(VaultFetchEntry)) error {
	sinceComposite := fmt.Sprintf("%d#", marker.LastSyncedAt)
	slog.Debug("vault sync: incremental sync", "machine_id", machineID, "since_ms", marker.LastSyncedAt)
	var cursor string
	var maxCreatedAt int64 = marker.LastSyncedAt
	for {
		var entries []VaultFetchEntry
		var nextCursor string
		if err := withRetry(ctx, func() error {
			var err error
			entries, nextCursor, err = client.FetchSince(ctx, machineID, sinceComposite, cursor)
			return err
		}); err != nil {
			return fmt.Errorf("incremental sync page: %w", err)
		}
		for _, e := range entries {
			onEntry(e)
			if e.CreatedAt > maxCreatedAt {
				maxCreatedAt = e.CreatedAt
			}
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	// Update high-water mark even if no new entries (LastSyncedAt = now)
	newTs := time.Now().UnixMilli()
	if maxCreatedAt > newTs {
		newTs = maxCreatedAt
	}
	writeSyncMarker(kv, VaultSyncMarker{Complete: true, LastSyncedAt: newTs})
	return nil
}
