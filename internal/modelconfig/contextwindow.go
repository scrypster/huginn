package modelconfig

import "strings"

// contextWindows maps known model IDs to their context window size in tokens.
var contextWindows = map[string]int{
	"claude-opus-4-6":   200000,
	"claude-sonnet-4-6": 200000,
	"claude-haiku-4-5":  200000,
	"gpt-4o":            128000,
	"gpt-4o-mini":       128000,
	"gpt-4-turbo":       128000,
	"gpt-3.5-turbo":     16385,
	"o1":                200000,
	"o1-mini":           128000,
	"o3":                200000,
	"o3-mini":           200000,
	"qwen2.5-coder:32b": 32768,
	"qwen2.5-coder:14b": 32768,
	"deepseek-r1:14b":   65536,
	"deepseek-r1:32b":   65536,
	"llama3.3:70b":      131072,
	"codellama:34b":     16384,
	"mistral:7b":        32768,
	"gemma2:27b":        8192,
}

const defaultContextWindow = 8192

// maxOutputTokens maps known model IDs to their maximum output token limit.
var maxOutputTokens = map[string]int{
	"claude-opus-4-6":   32768,
	"claude-sonnet-4-6": 65536,
	"claude-haiku-4-5":  8096,
	"gpt-4o":            16384,
	"gpt-4o-mini":       16384,
	"gpt-4-turbo":       4096,
	"gpt-3.5-turbo":     4096,
	"o1":                32768,
	"o1-mini":           65536,
	"o3":                100000,
	"o3-mini":           65536,
}

const defaultMaxOutputTokens = 8096

// MaxOutputTokensForModel returns the maximum output tokens for the named model.
// Falls back to 8096 if unknown.
func MaxOutputTokensForModel(modelID string) int {
	if n, ok := maxOutputTokens[modelID]; ok {
		return n
	}
	for key, n := range maxOutputTokens {
		if modelID != "" && strings.HasPrefix(modelID, key) {
			return n
		}
	}
	return defaultMaxOutputTokens
}

// DefaultContextWindow returns the default context window size.
func DefaultContextWindow() int {
	return defaultContextWindow
}

// ContextWindowForModel returns the context window size in tokens for modelID.
// Resolution: 1) exact match, 2) prefix match (key is prefix of modelID),
// 3) default 8192.
func ContextWindowForModel(modelID string) int {
	if cw, ok := contextWindows[modelID]; ok {
		return cw
	}
	for key, cw := range contextWindows {
		if modelID != "" && strings.HasPrefix(modelID, key) {
			return cw
		}
	}
	return defaultContextWindow
}
