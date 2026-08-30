package agents

import (
	"log/slog"
	"strings"
)

// legacyAutoVaultName reproduces the old hire-flow's auto-generated vault name
// (see internal/tools/create_agent.go's hireVaultName), so this package can
// detect AgentDefs whose VaultName was auto-assigned by that old scheme and
// was never customized by a user. Kept as an independent copy rather than an
// import from internal/tools to avoid a cross-package dependency for a single
// string helper; the two must stay in sync (see hireVaultName comment).
func legacyAutoVaultName(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' {
			sb.WriteRune('-')
		}
	}
	n := strings.Trim(sb.String(), "-")
	if n == "" {
		n = "teammate"
	}
	return n + "-huginn"
}

// MigrateLegacyVaultNames rewrites any AgentDef in cfg whose VaultName exactly
// matches the old hire-flow auto-slug for its own name (e.g. "steve-huginn"
// for an agent named "Steve") to the canonical
// "huginn:agent:<username>:<name>" scheme, and persists the change to disk.
//
// Only VaultNames that exactly equal legacyAutoVaultName(def.Name) are
// touched — any other non-empty VaultName is treated as user-customized and
// left alone. An empty VaultName is already canonical-by-fallback (see
// AgentDef.ResolvedVaultName) and is also left alone.
//
// This migration only rewrites the *pointer* stored in agents.json/yaml. It
// does NOT rename, copy, or merge anything inside MuninnDB itself: the
// codebase has no vault-rename/export/import primitive (checked
// internal/memory, internal/agent/mcp_vault.go, and the muninn_* MCP tool
// set), and a live Muninn connection is not available during startup
// migration anyway. Any memories the agent already wrote under the old
// "<slug>-huginn" vault stay there, untouched and unreachable from the
// agent's new canonical vault — the agent effectively starts fresh under the
// new name. This is safe (no data loss, no silent overwrite) but is a real
// gap: an operator who wants that old memory preserved must export/merge it
// by hand. The WARN logged below names the old vault for exactly that reason.
//
// Idempotent: once VaultName has been rewritten to the canonical form it no
// longer matches legacyAutoVaultName, so a second call is a no-op for that
// agent. Returns the names of the agents that were migrated (for logging/tests).
func MigrateLegacyVaultNames(baseDir string, cfg *AgentsConfig, username string) []string {
	if cfg == nil {
		return nil
	}
	var migrated []string
	for i := range cfg.Agents {
		def := &cfg.Agents[i]
		if def.VaultName == "" {
			continue // already canonical-by-fallback; nothing to do
		}
		if def.VaultName != legacyAutoVaultName(def.Name) {
			continue // custom/user-set VaultName; never touched by migration
		}
		oldVault := def.VaultName
		canonical := ResolveAgentVaultName(def.Name, username)
		if canonical == oldVault {
			continue // already canonical (shouldn't happen given the slug suffix differs, but stay idempotent)
		}
		def.VaultName = canonical
		if err := SaveAgent(baseDir, *def); err != nil {
			slog.Warn("agents: vault name migration failed to persist", "agent", def.Name, "old_vault", oldVault, "new_vault", canonical, "err", err)
			def.VaultName = oldVault // roll back in-memory change since the write failed
			continue
		}
		slog.Info("agents: migrated legacy vault name", "agent", def.Name, "old_vault", oldVault, "new_vault", canonical)
		slog.Warn("agents: old vault left in place in MuninnDB; not renamed/merged automatically", "agent", def.Name, "old_vault", oldVault)
		migrated = append(migrated, def.Name)
	}
	return migrated
}

// MigrateLegacyVaultNamesDefault runs MigrateLegacyVaultNames against the
// default ~/.huginn base directory. Intended for the one startup call site;
// tests should call MigrateLegacyVaultNames directly with a temp baseDir.
func MigrateLegacyVaultNamesDefault(cfg *AgentsConfig, username string) []string {
	baseDir, err := huginnBaseDir()
	if err != nil {
		slog.Warn("agents: vault name migration skipped, could not resolve base dir", "err", err)
		return nil
	}
	return MigrateLegacyVaultNames(baseDir, cfg, username)
}
