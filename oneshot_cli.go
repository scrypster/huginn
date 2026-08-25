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
