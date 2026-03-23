package lsp

import "os/exec"

var knownServers = map[string][]string{
	"go":         {"gopls"},
	"typescript": {"typescript-language-server"},
	"javascript": {"typescript-language-server"},
	"rust":       {"rust-analyzer"},
	"python":     {"pylsp", "pyright-langserver"},
}

var defaultArgs = map[string][]string{
	"gopls":                      {"serve"},
	"typescript-language-server": {"--stdio"},
	"rust-analyzer":              {},
	"pylsp":                      {},
	"pyright-langserver":         {"--stdio"},
}

func Detect(lang string) ServerConfig {
	candidates, ok := knownServers[lang]
	if !ok {
		return ServerConfig{}
	}
	for _, bin := range candidates {
		if path, err := exec.LookPath(bin); err == nil {
			return ServerConfig{Command: path, Args: defaultArgs[bin]}
		}
	}
	return ServerConfig{}
}

func SupportedLanguages() []string {
	langs := make([]string, 0, len(knownServers))
	for lang := range knownServers {
		langs = append(langs, lang)
	}
	return langs
}
