#!/usr/bin/env bash
set -euo pipefail

echo "==> Proactivity quality gate (deterministic policy + scheduler wiring)"
go test ./internal/proactivity ./internal/scheduler \
  -run 'TestHeartbeatDeliveryGate|TestPolicy_|TestDispatchNotification_AgentDM_ProactivityGateSuppresses|TestMakeWorkflowRunner_WithProactivityGate_E2E' \
  -count=1
