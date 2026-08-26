package main

import (
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/oneshot"
)

// oneshotRunOpts is the CLI-side input for a one-shot agentic run.
type oneshotRunOpts struct {
	prompt          string
	agentName       string
	model           string
	noTools         bool
	skipPermissions bool
	maxTurns        int
	cwd             string
	bashTimeoutSecs int
	backend         backend.Backend
	models          *modelconfig.Models
}

func newOneShotConfig(opts oneshotRunOpts) oneshot.Config {
	bashTimeout := time.Duration(opts.bashTimeoutSecs) * time.Second
	return oneshot.Config{
		Prompt:          opts.prompt,
		AgentName:       opts.agentName,
		Model:           opts.model,
		NoTools:         opts.noTools,
		SkipPermissions: opts.skipPermissions,
		MaxTurns:        opts.maxTurns,
		CWD:             opts.cwd,
		BashTimeout:     bashTimeout,
		Backend:         opts.backend,
		Models:          opts.models,
	}
}

func oneshotToolNames(calls []oneshot.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		if c.Name != "" {
			names = append(names, c.Name)
		}
	}
	return names
}
