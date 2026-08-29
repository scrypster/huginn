package lsp

import "os/exec"

// javascript/typescript both map to typescript-language-server, which
// serves .js/.jsx/.mjs/.cjs and .ts/.tsx alike (it's the same server;
// distinct "javascript"/"typescript" keys exist only because init_tools.go
// gates server startup per source-extension group). rust maps to
// rust-analyzer, which covers .rs.
var knownServers = map[string][]string{
	"go":         {"gopls"},
	"typescript": {"typescript-language-server"},
	"javascript": {"typescript-language-server"},
	"rust":       {"rust-analyzer"},
	"python":     {"pylsp", "pyright-langserver"},
	"ruby":       {"ruby-lsp", "solargraph"},
	"php":        {"intelephense", "phpactor"},
}

var defaultArgs = map[string][]string{
	"gopls":                      {"serve"},
	"typescript-language-server": {"--stdio"},
	"rust-analyzer":              {},
	"pylsp":                      {},
	"pyright-langserver":         {"--stdio"},
	"ruby-lsp":                   {},
	"solargraph":                 {"stdio"},
	"intelephense":               {"--stdio"},
	"phpactor":                   {"language-server"},
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
