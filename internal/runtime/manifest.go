package runtime

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed runtime.json
var runtimeJSON []byte

// BinaryEntry describes a platform-specific llama-server binary.
type BinaryEntry struct {
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	ExtractPath string `json:"extract_path"`
	ArchiveType string `json:"archive_type"` // "zip" or "tar.gz"
}

// RuntimeManifest is the parsed runtime.json.
type RuntimeManifest struct {
	Version            int                    `json:"huginn_runtime_version"`
	LlamaServerVersion string                 `json:"llama_server_version"`
	Binaries           map[string]BinaryEntry `json:"binaries"`
}

// LoadManifest parses the embedded runtime manifest.
func LoadManifest() (*RuntimeManifest, error) {
	var m RuntimeManifest
	if err := json.Unmarshal(runtimeJSON, &m); err != nil {
		return nil, fmt.Errorf("runtime manifest: %w", err)
	}
	return &m, nil
}

// BinaryForPlatform returns the entry for the given platform key.
func (m *RuntimeManifest) BinaryForPlatform(platformKey string) (BinaryEntry, bool) {
	e, ok := m.Binaries[platformKey]
	return e, ok
}
