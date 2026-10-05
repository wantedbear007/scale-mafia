#!/usr/bin/env bash
# Samples container CPU / memory / network / block IO to a TSV while a load
# test runs, so the results file can report measured resource usage rather
# than a guess.
#
#   ./scripts/sample-stats.sh benchmarks/runs/<run>/stats.tsv 600 &
#   ...run the load test...
#   kill %1
#
# Columns: unix_ts  container  cpu_pct  mem_pct  mem_usage  net_io  block_io
set -euo pipefail

OUT="${1:-benchmarks/runs/stats.tsv}"
INTERVAL="${2:-5}"
# NOTE: space separated, and must be unquoted below so it word-splits.
CONTAINERS="${CONTAINERS:-scaling-api scaling-postgres}"
DURATION="${DURATION:-0}"

mkdir -p "$(dirname "$OUT")"
echo -e "unix_ts\tcontainer\tcpu_pct\tmem_pct\tmem_usage\tnet_io\tblock_io" > "$OUT"

start=$(date +%s)
while true; do
  now=$(date +%s)
  if [ "$DURATION" -gt 0 ] && [ $((now - start)) -ge "$DURATION" ]; then
    break
  fi
  # shellcheck disable=SC2086
  if ! docker stats --no-stream \
      --format "{{.Name}}\t{{.CPUPerc}}\t{{.MemPerc}}\t{{.MemUsage}}\t{{.NetIO}}\t{{.BlockIO}}" \
      $CONTAINERS 2>/dev/null | while IFS= read -r line; do
    [ -n "$line" ] && echo -e "$now\t$line" >> "$OUT"
  done; then
    echo "warning: docker stats failed for: $CONTAINERS" >&2
  fi
  sleep "$INTERVAL"
done

echo "wrote $OUT ($(wc -l < "$OUT" | tr -d ' ') lines)"
