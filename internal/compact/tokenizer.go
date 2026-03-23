package compact

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	"github.com/scrypster/huginn/internal/backend"
)

//go:embed embed/cl100k_base.tiktoken
var cl100kBPE []byte

var (
	tke     *tiktoken.Tiktoken
	tkeOnce sync.Once
	tkeErr  error
)

func initTokenizer() {
	tkeOnce.Do(func() {
		// Register the embedded BPE data so tiktoken-go uses it instead of fetching from network
		tiktoken.SetBpeLoader(embeddedLoader{})
		tke, tkeErr = tiktoken.GetEncoding("cl100k_base")
	})
}

// embeddedLoader provides the BPE data from the embedded file.
type embeddedLoader struct{}

func (embeddedLoader) LoadTiktokenBpe(tiktokenBpeFile string) (map[string]int, error) {
	// Parse the .tiktoken file format: "<base64-token> <rank>\n" per line
	result := make(map[string]int)
	lines := strings.Split(strings.TrimSpace(string(cl100kBPE)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		// Decode the base64-encoded token to get the actual bytes
		token, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			continue
		}
		// The rank is the second part
		rank, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		result[string(token)] = rank
	}
	return result, nil
}

// EstimateTokens counts tokens in a message slice using cl100k_base tiktoken encoding.
// Falls back to len/4 if the tokenizer is unavailable.
func EstimateTokens(messages []backend.Message) int {
	if len(messages) == 0 {
		return 0
	}
	initTokenizer()
	if tkeErr != nil || tke == nil {
		return EstimateTokensFallback(messages)
	}

	total := 0
	for _, m := range messages {
		// Per-message overhead (ChatML format): ~4 tokens
		total += 4
		total += len(tke.Encode(m.Role, nil, nil))
		total += len(tke.Encode(m.Content, nil, nil))
		// Tool calls: serialize to JSON and count
		if len(m.ToolCalls) > 0 {
			if b, err := json.Marshal(m.ToolCalls); err == nil {
				total += len(tke.Encode(string(b), nil, nil))
			}
		}
	}
	// Reply primer: 2 tokens
	total += 2
	return total
}

// EstimateTokensFallback returns len/4 token estimate (legacy approximation).
func EstimateTokensFallback(messages []backend.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
		total += len(m.Role) / 4
	}
	return total
}
