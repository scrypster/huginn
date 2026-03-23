package pricing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// PricingEntry holds per-model costs in USD per 1M tokens.
type PricingEntry struct {
	PromptPer1M     float64 `json:"prompt"`
	CompletionPer1M float64 `json:"completion"`
}

// DefaultTable is the built-in pricing reference (USD per 1M tokens).
var DefaultTable = map[string]PricingEntry{
	// Anthropic
	"claude-opus-4-6":            {3.00, 15.00},
	"claude-sonnet-4-6":          {3.00, 15.00},
	"claude-3-5-sonnet-20241022": {3.00, 15.00},
	"claude-3-5-haiku-20241022":  {0.80, 4.00},
	"claude-3-opus-20240229":     {15.00, 75.00},
	"claude-3-haiku-20240307":    {0.25, 1.25},
	// OpenAI
	"gpt-4o":        {2.50, 10.00},
	"gpt-4o-mini":   {0.15, 0.60},
	"gpt-4-turbo":   {10.00, 30.00},
	"gpt-4":         {30.00, 60.00},
	"gpt-3.5-turbo": {0.50, 1.50},
	"o1":            {15.00, 60.00},
	"o1-mini":       {3.00, 12.00},
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

// MonthlyKey returns the KV key for monthly cost tracking.
func MonthlyKey(t time.Time) string {
	return fmt.Sprintf("stats:cost:%d-%02d", t.Year(), t.Month())
}
