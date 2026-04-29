#!/usr/bin/env bash
set -euo pipefail

# Enforce coverage floors for critical runtime packages where regressions are
# most likely to impact reliability/security-sensitive behavior.
packages=(
  "./internal/connections/tools"
  "./internal/server"
  "./internal/scheduler"
  "./internal/spaces"
  "./internal/memory"
  "./internal/stats"
  "./internal/proactivity"
)
minimums=(55 62 74 58 54 45 80)

fail=0

for i in "${!packages[@]}"; do
  pkg="${packages[$i]}"
  min="${minimums[$i]}"

  echo "==> Coverage check: ${pkg} (minimum ${min}%)"
  out="$(go test -cover -count=1 "${pkg}" 2>&1)"
  printf '%s\n' "${out}"

  cov="$(printf '%s\n' "${out}" | awk '
    /coverage: / {
      split($0, p, "coverage: ")
      split(p[2], q, "%")
      value = q[1]
    }
    END { print value }
  ')"

  if [[ -z "${cov}" ]]; then
    echo "ERROR: could not parse coverage for ${pkg}"
    fail=1
    continue
  fi

  if awk -v c="${cov}" -v m="${min}" 'BEGIN { exit ((c + 0) >= (m + 0)) ? 0 : 1 }'; then
    echo "PASS: ${pkg} coverage ${cov}% >= ${min}%"
  else
    echo "FAIL: ${pkg} coverage ${cov}% < ${min}%"
    fail=1
  fi
done

exit "${fail}"
