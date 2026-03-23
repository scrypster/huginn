package radar

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LayerConfig holds configurable layer mappings loaded from huginn-radar.yaml.
type LayerConfig struct {
	Layers map[string][]string `yaml:"layers"` // layer name → file patterns
}

const radarConfigFile = "huginn-radar.yaml"

// LoadLayerConfig loads layer configuration from huginn-radar.yaml in the
// workspace root. If the file does not exist, it returns nil (callers should
// fall back to defaultLayers). Other errors are returned.
func LoadLayerConfig(workspaceRoot string) (*LayerConfig, error) {
	path := filepath.Join(workspaceRoot, radarConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg LayerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
