package server

import (
	"context"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
)

func inferListedModelCaps(name string, supportsTools bool) modelconfig.ModelInfo {
	info := modelconfig.ModelInfo{Name: name, SupportsTools: supportsTools}
	info.InferCapabilities()
	return info
}

func attachModelCaps(m map[string]any, info modelconfig.ModelInfo) {
	m["supportsTools"] = info.SupportsTools
	m["supportsDelegation"] = info.SupportsDelegation
	m["tier"] = info.Tier
}

// enrichOllamaModels probes /api/show for each tagged model, stamps
// supportsTools / supportsDelegation / tier onto the picker payload, and
// writes successful probes into ModelRegistry.Available.
func enrichOllamaModels(ctx context.Context, s *Server, baseURL string, models []map[string]any) []any {
	names := make([]string, 0, len(models))
	for _, m := range models {
		if name, _ := m["name"].(string); name != "" {
			names = append(names, name)
		}
	}
	probed := backend.ProbeOllamaModels(ctx, baseURL, names)
	byName := make(map[string]modelconfig.ModelInfo, len(probed))
	for _, info := range probed {
		byName[info.Name] = info
	}

	out := make([]any, 0, len(models))
	for _, m := range models {
		name, _ := m["name"].(string)
		if info, ok := byName[name]; ok {
			attachModelCaps(m, info)
		} else if name != "" {
			// Show failed: keep runtime optimistic, but still infer tier so
			// the picker can warn on 7b-class names.
			info := inferListedModelCaps(name, !modelconfig.IsLowTierToolClass(name))
			attachModelCaps(m, info)
		}
		out = append(out, m)
	}

	if s != nil && s.orch != nil {
		if reg := s.orch.ModelRegistry(); reg != nil && len(probed) > 0 {
			reg.ReplaceAvailable(probed)
		}
	}
	return out
}
