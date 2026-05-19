#!/usr/bin/env bash
# Convenience runner for the Base extension runtime benchmark suite.
#
# Output is captured to bench-results.txt next to this script. Pass
# any extra args to override; common variants:
#
#   ./run.sh                  # default tags, full suite
#   ./run.sh -count=1         # single run for quick smoke
#
# We deliberately set -benchtime=2s to keep total wall-clock around
# 3-5 minutes for all 5 runtimes x serial+parallel x 3 counts.

set -euo pipefail
cd "$(dirname "$0")"

OUT=bench-results.txt
: > "$OUT"

run() {
  local label="$1"; shift
  echo "=== ${label} ===" | tee -a "$OUT"
  # Memory tests run once (-count=1) — they print MEMORY lines that are
  # the per-runtime totals; running them 3x just dilutes the signal AND
  # tickles v8go's known cgo teardown bug. Benchmarks run 3x for
  # variance reading.
  echo "--- TestMemory (count=1) ---" | tee -a "$OUT"
  go test "$@" -count=1 -v -run=^TestMemory ./... 2>&1 | tee -a "$OUT"
  echo "--- Benchmarks (count=3, benchtime=2s) ---" | tee -a "$OUT"
  go test "$@" -bench=. -benchmem -benchtime=2s -count=3 -run=^$ ./... 2>&1 | tee -a "$OUT"
  echo "" | tee -a "$OUT"
}

run "default tags"
run "with v8vm" -tags v8vm

echo "Results: $OUT"
