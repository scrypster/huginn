# Huginn Hardening Program (Confidence Uplift)

Date: 2026-04-28  
Owner: Engineering  
Goal: Raise whole-system dependability from ~8.8/10 to 9.5+/10 with enforced quality gates.

---

## Current Baseline (evidence-backed)

- Backend: `go test -race ./...` passes.
- Backend total coverage: ~64.7% statements.
- Frontend unit tests: 1264/1264 pass.
- Frontend e2e tests: 103/103 pass.
- Frontend coverage: 63.29% statements / 49.84% branches (branch gate currently below desired floor).

---

## Program Structure

Each batch is PR-sized, independently shippable, and includes verification before merge.

### Batch 1 — Confidence Gates + Test Hygiene (Now)

Objective:
- Turn reliability expectations into enforced CI checks where we already have baseline confidence.
- Remove test warning noise that hides real regressions.

Scope:
- Add CI package-level Go coverage floor enforcement for:
  - `internal/connections/tools` >= 55%
  - `internal/server` >= 62%
  - `internal/scheduler` >= 74%
  - `internal/spaces` >= 58%
  - `internal/memory` >= 54%
  - `internal/stats` >= 45%
- Reduce Vue test warnings by providing shared injections in global test setup.
- Guard composable lifecycle hook registration in tests (`getCurrentScope` pattern).

Target confidence delta:
- Connections/tools: +0.2
- Server/API: +0.2
- Frontend unit reliability signal quality: +0.3
- Overall: +0.2

Success criteria:
- CI fails when any covered package regresses below floor.
- Targeted frontend suites run without previous warning noise classes.

### Batch 2 — Frontend Branch Coverage Uplift (Critical)

Objective:
- Raise branch coverage to meet hardened threshold with real tests (not exclusions).

Scope:
- Add branch-path tests for:
  - `AgentsView`
  - `ChatView`
  - `ConnectionsView`
  - `SettingsView`
  - `ModelsView`
  - `useApi`
  - `useSpaceTimeline`
- Keep coverage thresholds unchanged; raise actual measured branch coverage.

Target confidence delta:
- Frontend unit layer: +0.6
- Overall: +0.3

Success criteria:
- `npm run test:coverage` passes with branches >= 63%.
- No threshold lowering.

### Batch 3 — Security Assurance Gates

Objective:
- Add continuous, machine-verifiable vulnerability checks.

Scope:
- Add Go vulnerability scan in CI (`govulncheck` installation + run).
- Add npm dependency audit gate for production dependencies.
- Add weekly scheduled security scan workflow.

Target confidence delta:
- Security assurance process: +1.0
- Overall: +0.2

Success criteria:
- CI blocks merges on high/critical dependency vulns.
- Security scans run on PR and schedule.

### Batch 4 — Reliability Stress + Flake Resistance

Objective:
- Detect concurrency, timing, and retry-edge regressions before release.

Scope:
- Add repeated/stress runs for critical packages:
  - `internal/server`
  - `internal/spaces`
  - `internal/threadmgr`
  - `internal/connections/tools`
- Add deterministic fake clock where needed for polling/retry paths.
- Add flake triage dashboard (recent failures by test).

Target confidence delta:
- Runtime reliability: +0.4
- Overall: +0.2

Success criteria:
- Stress runs stable over N>=20 cycles on critical suites.
- No open flaky tests in critical paths.

### Batch 5 — Performance Hardening Guardrails

Objective:
- Prevent UX regressions from excessive bundle growth and slow cold-start.

Scope:
- Split large frontend chunks (`route-level dynamic import`).
- Add bundle budget check to CI with fail threshold.
- Add perf smoke checks for key screens.

Target confidence delta:
- Frontend production dependability: +0.3
- Overall: +0.1

Success criteria:
- No chunk > budget threshold without explicit override.
- Key page load metrics tracked and stable.

---

## Confidence Targets (post-program)

- Agent orchestration: 9.4+
- Server/API: 9.3+
- Connections/integrations: 9.3+
- Spaces/channels/DM: 9.2+
- Memory subsystem: 9.2+
- Frontend unit reliability: 9.3+
- Frontend e2e reliability: 9.2+
- CI/CD and release process: 9.4+
- Security assurance process: 9.2+
- Overall system confidence: 9.5+

