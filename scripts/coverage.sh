#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COVERAGE_MIN="${COVERAGE_MIN:-80}"
COVERAGE_ENFORCE="${COVERAGE_ENFORCE:-0}"
COVERAGE_PROFILE="${COVERAGE_PROFILE:-coverage.out}"
COVERAGE_EXCLUDE_REGEX="${COVERAGE_EXCLUDE_REGEX:-/test/e2e}"

export GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache}"
export GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.gomodcache}"

mapfile -t packages < <(go list ./... | grep -v "$COVERAGE_EXCLUDE_REGEX" || true)
if [ "${#packages[@]}" -eq 0 ]; then
  echo "No packages selected for coverage."
  exit 1
fi

echo "Running coverage for ${#packages[@]} packages..."
go test -count=1 -covermode=atomic -coverprofile="$COVERAGE_PROFILE" "${packages[@]}"

total_pct="$(go tool cover -func="$COVERAGE_PROFILE" | awk '/^total:/ {gsub("%","",$3); print $3}')"
if [ -z "$total_pct" ]; then
  echo "Failed to parse coverage total from $COVERAGE_PROFILE"
  exit 1
fi

printf "Total coverage: %.2f%% (target %.2f%%)\n" "$total_pct" "$COVERAGE_MIN"

if [ "$COVERAGE_ENFORCE" = "1" ]; then
  awk -v got="$total_pct" -v min="$COVERAGE_MIN" 'BEGIN { exit(got+0 < min+0 ? 1 : 0) }' || {
    echo "Coverage gate failed: total ${total_pct}% is below ${COVERAGE_MIN}%"
    exit 1
  }
fi
