// Package agent — per-step pre-authorised connections (Phase 1.4).
//
// Workflow steps may declare `connections: { github: my-personal-gh, ... }`
// in their YAML. Today the OAuth tool layer resolves connections by an
// `account` argument supplied by the agent at tool-call time. This package
// surfaces the step's pre-auth'd picks to the agent as a system-prompt
// addendum so it knows which `account` value to use, and stashes the map on
// the Go context so future tool-layer changes can read it directly.
//
// Backwards compatible: when no map is present the addendum is empty and
// nothing else changes.
package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// stepConnectionsKey is the context key for the per-step connection map.
type stepConnectionsKey struct{}

// WithStepConnections returns a derived context that carries the per-step
// connection map (provider name → connection account label). Nil/empty maps
// are accepted but are no-ops downstream.
func WithStepConnections(ctx context.Context, conns map[string]string) context.Context {
	if len(conns) == 0 {
		return ctx
	}
	// Defensive copy so the runner can mutate the source map without poisoning
	// the context value.
	cp := make(map[string]string, len(conns))
	for k, v := range conns {
		cp[k] = v
	}
	return context.WithValue(ctx, stepConnectionsKey{}, cp)
}

// StepConnections returns the per-step connection map placed on ctx by
// WithStepConnections, or nil if none was set. The returned map is read-only
// — callers must not mutate it.
func StepConnections(ctx context.Context) map[string]string {
	v, _ := ctx.Value(stepConnectionsKey{}).(map[string]string)
	return v
}

// stepConnectionsAddendum builds a deterministic system-prompt addendum that
// instructs the agent to use the supplied account labels when calling
// integration tools for the listed providers. Returns "" when no map is set.
//
// The output is stable (alphabetical by provider) so the prompt is
// deterministic across runs — important for prompt-cache hits and for
// regression-testing the system prompt.
func stepConnectionsAddendum(ctx context.Context) string {
	conns := StepConnections(ctx)
	if len(conns) == 0 {
		return ""
	}
	providers := make([]string, 0, len(conns))
	for p := range conns {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	var b strings.Builder
	b.WriteString("## Pre-authorised connections for this step\n")
	b.WriteString("Use these account labels when calling tools for the listed providers:\n")
	for _, p := range providers {
		fmt.Fprintf(&b, "- %s → account: %q\n", p, conns[p])
	}
	b.WriteString("\nPass the label via the `account` parameter when a tool exposes it.")
	return b.String()
}
