#!/usr/bin/env bash
set -euo pipefail

out_dir="${1:-docs/plans/benchmark-scorecards}"
timestamp="$(date -u +"%Y%m%d-%H%M%S")"
report_path="${out_dir}/heartbeat-benchmark-${timestamp}.md"
latest_path="${out_dir}/latest-heartbeat-benchmark.md"

echo "==> Running heartbeat harness tests"
go test ./internal/benchmark/heartbeat_harness -count=1

echo "==> Generating internal heartbeat benchmark scorecard"
go run ./internal/benchmark/heartbeat_harness/cmd \
  -output "${report_path}" \
  -enforce-thresholds

cp "${report_path}" "${latest_path}"

echo "==> Internal benchmark scorecards"
echo "    ${report_path}"
echo "    ${latest_path}"
