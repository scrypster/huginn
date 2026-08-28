package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/logger"
)

//go:embed provider_catalog.json
var bundledProviderCatalogJSON []byte

// ProviderModelEntry describes a single cloud API model.
type ProviderModelEntry struct {
	DisplayName   string   `json:"display_name"`
	Aliases       []string `json:"aliases,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	MaxOutput     int      `json:"max_output_tokens,omitempty"`
	SupportsTools bool     `json:"supports_tools,omitempty"`
	Tier          string   `json:"tier,omitempty"` // "high", "medium", "low"
	Deprecated    bool     `json:"deprecated,omitempty"`
	ReplacedBy    string   `json:"replaced_by,omitempty"`

	// InputCostPerMTok and OutputCostPerMTok are USD per 1M tokens, verified
	// against internal/pricing (the single source of pricing truth — see
	// table.go). Zero means unpriced/unknown, not free.
	InputCostPerMTok  float64 `json:"input_cost_per_mtok,omitempty"`
	OutputCostPerMTok float64 `json:"output_cost_per_mtok,omitempty"`

	// Strengths is a short list of what this model is good at, surfaced to
	// the CoS when choosing a model for a one-off specialist (spawn_specialist).
	Strengths []string `json:"strengths,omitempty"`
}

type providerCatalogFile struct {
	CatalogVersion string                                   `json:"catalog_version"`
	Providers      map[string]map[string]ProviderModelEntry `json:"providers"` // provider → modelID → info
}

// ProviderCatalog resolves cloud provider model IDs and friendly aliases.
// The zero value is safe (empty catalog). Use GlobalProviderCatalog() for the singleton.
type ProviderCatalog struct {
	mu      sync.RWMutex
	byAlias map[string]map[string]string             // provider → alias → real model ID
	byID    map[string]map[string]ProviderModelEntry // provider → modelID → info
	version string
}

// DeprecationWarning is returned when a model ID in use is deprecated.
type DeprecationWarning struct {
	Provider   string
	ModelID    string
	ReplacedBy string
}

var (
	globalProviderCatalog     = &ProviderCatalog{}
	globalProviderCatalogOnce sync.Once
)

// GlobalProviderCatalog returns the package-level singleton ProviderCatalog.
// It is loaded from the bundled JSON on first access, with any locally-cached
// CDN overlay applied on top.
func GlobalProviderCatalog() *ProviderCatalog {
	globalProviderCatalogOnce.Do(func() {
		if err := globalProviderCatalog.load(bundledProviderCatalogJSON); err != nil {
			logger.Warn("provider catalog: failed to load bundled catalog", "err", err)
		}
		// Overlay cached CDN file if present and readable.
		if p := defaultProviderCatalogPath(); p != "" {
			if data, err := os.ReadFile(p); err == nil {
				if err2 := globalProviderCatalog.overlay(data); err2 != nil {
					logger.Warn("provider catalog: ignoring malformed CDN overlay", "path", p, "err", err2)
				}
			}
		}
	})
	return globalProviderCatalog
}

// ResetGlobalProviderCatalog resets the singleton for testing.
func ResetGlobalProviderCatalog() {
	globalProviderCatalog = &ProviderCatalog{}
	globalProviderCatalogOnce = sync.Once{}
}

// load replaces the catalog contents with the parsed JSON.
func (c *ProviderCatalog) load(data []byte) error {
	var f providerCatalogFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse provider catalog: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byAlias = make(map[string]map[string]string)
	c.byID = make(map[string]map[string]ProviderModelEntry)
	c.version = f.CatalogVersion
	c.applyLocked(f)
	return nil
}

// overlay merges catalog entries from data on top of existing entries.
// Existing entries are preserved for any provider/model not in data.
func (c *ProviderCatalog) overlay(data []byte) error {
	var f providerCatalogFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse provider catalog overlay: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byAlias == nil {
		c.byAlias = make(map[string]map[string]string)
		c.byID = make(map[string]map[string]ProviderModelEntry)
	}
	c.applyLocked(f)
	return nil
}

// applyLocked ingests a parsed catalog file. Must hold c.mu write lock.
func (c *ProviderCatalog) applyLocked(f providerCatalogFile) {
	for provider, models := range f.Providers {
		p := strings.ToLower(provider)
		if c.byID[p] == nil {
			c.byID[p] = make(map[string]ProviderModelEntry)
		}
		if c.byAlias[p] == nil {
			c.byAlias[p] = make(map[string]string)
		}
		for modelID, entry := range models {
			c.byID[p][modelID] = entry
			// Register all aliases → real model ID.
			for _, alias := range entry.Aliases {
				c.byAlias[p][strings.ToLower(alias)] = modelID
			}
			// The model ID itself is always its own alias.
			c.byAlias[p][strings.ToLower(modelID)] = modelID
		}
	}
}

// Resolve converts a provider+modelID (or friendly alias) to the canonical API model ID.
// If provider is empty, all providers are searched in insertion order.
// Returns the input unchanged when no match is found — the caller's original ID is preserved.
func (c *ProviderCatalog) Resolve(provider, modelID string) string {
	if modelID == "" {
		return modelID
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := strings.ToLower(modelID)
	if provider != "" {
		p := strings.ToLower(provider)
		if aliases, ok := c.byAlias[p]; ok {
			if canonical, ok := aliases[key]; ok {
				return canonical
			}
		}
		// Provider specified but model not found — return as-is.
		return modelID
	}
	// No provider: search all providers.
	for _, aliases := range c.byAlias {
		if canonical, ok := aliases[key]; ok {
			return canonical
		}
	}
	return modelID
}

// Info returns the ProviderModelEntry for a (provider, modelID) pair.
// provider may be empty to search all providers.
// Returns nil when not found.
func (c *ProviderCatalog) Info(provider, modelID string) *ProviderModelEntry {
	if modelID == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	// First resolve aliases to the canonical ID.
	key := strings.ToLower(modelID)
	canonical := modelID

	if provider != "" {
		p := strings.ToLower(provider)
		if aliases, ok := c.byAlias[p]; ok {
			if id, ok := aliases[key]; ok {
				canonical = id
			}
		}
		if m, ok := c.byID[strings.ToLower(provider)]; ok {
			if entry, ok := m[canonical]; ok {
				cp := entry
				return &cp
			}
		}
		return nil
	}

	for p, aliases := range c.byAlias {
		if id, ok := aliases[key]; ok {
			if entry, ok := c.byID[p][id]; ok {
				cp := entry
				return &cp
			}
		}
	}
	return nil
}

// AvailableModelsBlock renders a compact, deterministic reference of
// non-deprecated cloud models with their verified per-MTok cost and
// strengths, for injection into the CoS's system prompt beside the team
// roster (see agents.AppendTeamRoster) — it is what spawn_specialist's
// model-choice reasoning is grounded against. Providers and models within
// each provider are sorted alphabetically for a stable prompt. Returns ""
// when the catalog has no priced models.
func (c *ProviderCatalog) AvailableModelsBlock() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	providers := make([]string, 0, len(c.byID))
	for p := range c.byID {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	var b strings.Builder
	b.WriteString("Available models (for spawn_specialist model choice):\n")
	any := false
	for _, p := range providers {
		models := c.byID[p]
		ids := make([]string, 0, len(models))
		for id := range models {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			entry := models[id]
			if entry.Deprecated {
				continue
			}
			if entry.InputCostPerMTok == 0 && entry.OutputCostPerMTok == 0 {
				continue // unpriced — omit rather than imply free
			}
			any = true
			fmt.Fprintf(&b, "- %s (%s): $%.2f/$%.2f per MTok in/out",
				id, entry.DisplayName, entry.InputCostPerMTok, entry.OutputCostPerMTok)
			if len(entry.Strengths) > 0 {
				fmt.Fprintf(&b, " — %s", strings.Join(entry.Strengths, ", "))
			}
			b.WriteString("\n")
		}
	}
	if !any {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// CheckDeprecations returns a warning for each (provider, modelID) pair that is
// deprecated in the catalog. Pairs not found in the catalog are silently ignored.
func (c *ProviderCatalog) CheckDeprecations(pairs []struct{ Provider, ModelID string }) []DeprecationWarning {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var warnings []DeprecationWarning
	for _, pair := range pairs {
		p := strings.ToLower(pair.Provider)
		m, ok := c.byID[p]
		if !ok {
			continue
		}
		entry, ok := m[pair.ModelID]
		if !ok {
			continue
		}
		if entry.Deprecated {
			warnings = append(warnings, DeprecationWarning{
				Provider:   pair.Provider,
				ModelID:    pair.ModelID,
				ReplacedBy: entry.ReplacedBy,
			})
		}
	}
	return warnings
}

// Version returns the catalog version string (e.g. "2026-03-19").
func (c *ProviderCatalog) Version() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// defaultProviderCatalogPath returns the path to the locally-cached CDN catalog file.
// Returns "" if the home directory cannot be determined.
func defaultProviderCatalogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".huginn", "provider_catalog.json")
}

// defaultProviderCatalogStampPath is the last-attempt stamp. Written on
// non-200 CDN responses so the 7-day freshness gate still applies when the
// catalog cache itself was not updated. The bundled catalog is unchanged.
func defaultProviderCatalogStampPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".huginn", "provider_catalog.last_attempt")
}

func providerCatalogFreshEnough(catalogPath, stampPath string, maxAge time.Duration) bool {
	for _, p := range []string{stampPath, catalogPath} {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) < maxAge {
			return true
		}
	}
	return false
}

func writeProviderCatalogAttemptStamp(stampPath string, status int) {
	if stampPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(stampPath), 0755); err != nil {
		return
	}
	payload, err := json.Marshal(struct {
		AttemptedAt time.Time `json:"attempted_at"`
		Status      int       `json:"status"`
	}{AttemptedAt: time.Now().UTC(), Status: status})
	if err != nil {
		return
	}
	_ = atomicWriteFile(stampPath, payload, 0644)
}

// TryRefreshProviderCatalog fetches the CDN catalog if the local cache (or the
// last-attempt stamp) is older than maxAge, or missing. Runs non-blocking in a
// background goroutine. Call once at startup from main.go.
//
// A non-200 from the CDN does not replace the bundled/cached catalog. It only
// writes a last-attempt stamp so subsequent startServer calls skip the GET
// until maxAge elapses (no WARN on every boot).
func TryRefreshProviderCatalog(cdnURL string, maxAge time.Duration) {
	go refreshProviderCatalog(cdnURL, maxAge)
}

func refreshProviderCatalog(cdnURL string, maxAge time.Duration) {
	path := defaultProviderCatalogPath()
	if path == "" {
		return
	}
	stampPath := defaultProviderCatalogStampPath()
	if providerCatalogFreshEnough(path, stampPath, maxAge) {
		return
	}

	logger.Info("provider catalog: checking for updates", "url", cdnURL)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(cdnURL)
	if err != nil {
		logger.Warn("provider catalog: CDN fetch failed", "url", cdnURL, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Warn("provider catalog: CDN returned non-200", "status", resp.StatusCode)
		writeProviderCatalogAttemptStamp(stampPath, resp.StatusCode)
		return
	}

	var buf []byte
	buf = make([]byte, 0, 64*1024)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
		if len(buf) > 512*1024 {
			logger.Warn("provider catalog: CDN response too large, ignoring")
			return
		}
	}

	// Validate JSON before saving.
	var check providerCatalogFile
	if err := json.Unmarshal(buf, &check); err != nil {
		logger.Warn("provider catalog: CDN returned invalid JSON", "err", err)
		return
	}

	if err := atomicWriteFile(path, buf, 0644); err != nil {
		logger.Warn("provider catalog: failed to save CDN catalog", "err", err)
		return
	}

	// Apply to the live singleton.
	if err := GlobalProviderCatalog().overlay(buf); err != nil {
		logger.Warn("provider catalog: failed to apply CDN update", "err", err)
		return
	}
	logger.Info("provider catalog: updated from CDN", "version", check.CatalogVersion)
}
