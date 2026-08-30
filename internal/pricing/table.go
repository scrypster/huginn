package pricing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// PricingEntry holds per-model costs in USD per 1M tokens.
type PricingEntry struct {
	PromptPer1M     float64 `json:"prompt"`
	CompletionPer1M float64 `json:"completion"`
}

// DefaultTable is the built-in pricing reference (USD per 1M tokens).
//
// Verified 2026-08-28 against the official Anthropic and OpenAI pricing
// pages (see per-block citations below). Rows marked "historical,
// unverified" are models the vendor pages no longer publish; the
// verification does NOT cover them. This is the single source of
// pricing truth for the codebase — internal/threadmgr/cost.go looks up
// prices through Lookup() in this package rather than keeping its own
// table (previously it had a private substring table that disagreed with
// this one on several entries, notably claude-opus-4-6 which this table
// had copy-pasted from claude-sonnet-4-6's $3/$15 instead of Opus's real
// $5/$25).
var DefaultTable = map[string]PricingEntry{
	// Anthropic — https://platform.claude.com/docs/en/about-claude/pricing
	// (fetched 2026-08-28; "Base Input Tokens" / "Output Tokens" columns).
	"claude-opus-4-6":           {5.00, 25.00}, // was mis-copied from sonnet (3.00/15.00) — fixed
	"claude-opus-4-5":           {5.00, 25.00},
	"claude-sonnet-4-6":         {3.00, 15.00},
	"claude-sonnet-4-5":         {3.00, 15.00},
	"claude-haiku-4-5-20251001": {1.00, 5.00},
	"claude-haiku-4-5":          {1.00, 5.00},
	"claude-3-5-haiku-20241022": {0.80, 4.00}, // verified; page marks it retired outside Bedrock/Vertex
	// The three rows below are LEGACY and could NOT be reverified on
	// 2026-08-28: the current Anthropic pricing page no longer lists Claude
	// 3 Opus, Claude 3 Haiku, or Claude 3.5 Sonnet at all. They are retained
	// so historical cost records still resolve, but the "verified" note at
	// the top of this table does not cover them.
	"claude-3-5-sonnet-20241022": {3.00, 15.00},  // historical, unverified
	"claude-3-opus-20240229":     {15.00, 75.00}, // historical, unverified
	"claude-3-haiku-20240307":    {0.25, 1.25},   // historical, unverified
	// OpenAI — https://developers.openai.com/api/docs/pricing (fetched
	// 2026-08-28, Standard tier). o1-mini is no longer listed on current
	// docs (retired); its $3.00/$12.00 entry is kept as a historical
	// reference for existing cost data and not reverified here.
	"gpt-4o":        {2.50, 10.00},
	"gpt-4o-mini":   {0.15, 0.60},
	"gpt-4-turbo":   {10.00, 30.00},
	"gpt-4":         {30.00, 60.00},
	"gpt-3.5-turbo": {0.50, 1.50},
	"o1":            {15.00, 60.00},
	"o1-mini":       {3.00, 12.00}, // historical, unverified — see note above
	"o3":            {2.00, 8.00},
	"o3-mini":       {1.10, 4.40},
}

// LoadTable loads the pricing table, merging user overrides from overridePath.
// Missing or empty overridePath returns a copy of DefaultTable.
func LoadTable(overridePath string) (map[string]PricingEntry, error) {
	tbl := make(map[string]PricingEntry, len(DefaultTable))
	for k, v := range DefaultTable {
		tbl[k] = v
	}
	if overridePath == "" {
		return tbl, nil
	}
	data, err := os.ReadFile(overridePath)
	if errors.Is(err, os.ErrNotExist) {
		return tbl, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pricing: read override %q: %w", overridePath, err)
	}
	var overrides map[string]PricingEntry
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, fmt.Errorf("pricing: parse override %q: %w", overridePath, err)
	}
	for k, v := range overrides {
		tbl[k] = v
	}
	return tbl, nil
}

// CalculateCost returns USD cost for given token counts. Returns 0 for unknown models.
func CalculateCost(table map[string]PricingEntry, model string, promptTokens, completionTokens int) float64 {
	entry, ok := table[model]
	if !ok {
		return 0
	}
	return float64(promptTokens)/1_000_000*entry.PromptPer1M +
		float64(completionTokens)/1_000_000*entry.CompletionPer1M
}

// IsCloudModel returns true if the model appears in the pricing table.
func IsCloudModel(table map[string]PricingEntry, model string) bool {
	_, ok := table[model]
	return ok
}

// Lookup resolves a model ID to a pricing entry, falling back to the
// longest table key that appears as a WORD-BOUNDARY match within model when
// there is no exact match (e.g. a dated/regional variant like
// "claude-opus-4-6-us" or "claude-sonnet-4-6-20260615" still resolves
// against "claude-opus-4-6" / "claude-sonnet-4-6"). This replaces the
// ad-hoc substring table that used to live in internal/threadmgr/cost.go —
// that package now calls Lookup against this table instead of keeping its
// own, so there is exactly one place pricing numbers are verified and
// corrected.
//
// The match is boundary-aware, not a bare strings.Contains: the matched key
// must be flanked by non-alphanumeric characters (or the start/end of the
// string) on both sides. This matters most for the short 2-char OpenAI keys
// ("o1", "o3") — a bare Contains would mis-price any model whose name merely
// contains those two characters mid-token (e.g. a hypothetical
// "gpto1-custom" or "foo3bar" model), pricing it as an OpenAI o1/o3 reasoning
// model it isn't. "chat-o1-preview" still matches "o1" (flanked by '-' on
// both sides); "gpto1-custom" does not (flanked by 't' on the left).
//
// Returns ok=false for models with no exact or boundary-match (e.g. local
// Ollama models).
func Lookup(table map[string]PricingEntry, model string) (PricingEntry, bool) {
	if entry, ok := table[model]; ok {
		return entry, true
	}
	lower := strings.ToLower(model)
	bestKey := ""
	var best PricingEntry
	found := false
	for key, entry := range table {
		if key == "" {
			continue
		}
		lowerKey := strings.ToLower(key)
		if hasWordBoundaryMatch(lower, lowerKey) && len(key) > len(bestKey) {
			bestKey = key
			best = entry
			found = true
		}
	}
	return best, found
}

// isAlnumByte reports whether b is an ASCII letter or digit. Model IDs are
// ASCII, so a byte-level check is sufficient and avoids rune decoding.
func isAlnumByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// hasWordBoundaryMatch reports whether key occurs in s at a position flanked
// by non-alphanumeric characters (or the start/end of s) on both sides —
// i.e. key appears as its own token, not merely as a run of characters
// inside a longer alphanumeric token. Scans all occurrences of key in s
// since an early, non-boundary occurrence must not shadow a later
// boundary-matching one.
func hasWordBoundaryMatch(s, key string) bool {
	if key == "" {
		return false
	}
	searchFrom := 0
	for {
		idx := strings.Index(s[searchFrom:], key)
		if idx == -1 {
			return false
		}
		start := searchFrom + idx
		end := start + len(key)
		beforeOK := start == 0 || !isAlnumByte(s[start-1])
		afterOK := end == len(s) || !isAlnumByte(s[end])
		if beforeOK && afterOK {
			return true
		}
		searchFrom = start + 1
		if searchFrom >= len(s) {
			return false
		}
	}
}

// MonthlyKey returns the KV key for monthly cost tracking.
func MonthlyKey(t time.Time) string {
	return fmt.Sprintf("stats:cost:%d-%02d", t.Year(), t.Month())
}
