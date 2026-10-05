#!/usr/bin/env bash
# Collects everything needed to make a benchmark result interpretable later:
# host specs, tool versions, container resource limits and dataset size.
#
#   make system-info
#   ./scripts/collect-system-info.sh > benchmarks/runs/<run>/system-info.md
#
# Nothing here is invented - every value is read from the live system.
set -euo pipefail

OUT="${1:-benchmarks/baseline/system-info.md}"
mkdir -p "$(dirname "$OUT")"

compose() { docker compose "$@" 2>/dev/null || true; }

{
  echo "# Benchmark environment"
  echo
  echo "Collected: \`$(date -u +%Y-%m-%dT%H:%M:%SZ)\` (UTC)"
  echo
  echo "## Project"
  echo
  echo "| Item | Value |"
  echo "| --- | --- |"
  # A repo with no commits yet: `git rev-parse HEAD` prints "HEAD" on stdout
  # *and* fails on stderr, so an `||` fallback alone is not enough.
  if commit=$(git rev-parse --short HEAD 2>/dev/null); then
    echo "| Git commit | \`$commit\` |"
  else
    echo "| Git commit | _(no commits yet)_ |"
  fi
  if branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null); then
    echo "| Git branch | \`$branch\` |"
  else
    echo "| Git branch | _(unborn)_ |"
  fi
  echo "| Working tree | $(if [ -n "$(git status --porcelain 2>/dev/null)" ]; then echo 'dirty'; else echo 'clean'; fi) |"
  echo
  echo "## Host"
  echo
  echo "| Item | Value |"
  echo "| --- | --- |"
  echo "| OS | $(uname -s) $(uname -r) $(uname -m) |"
  echo "| CPU model | $(sysctl -n machdep.cpu.brand_string 2>/dev/null || grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | xargs || echo 'n/a') |"
  echo "| Host logical CPUs | $(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 'n/a') |"
  echo "| Host RAM | $(python3 -c "import sys;print(round(int(sys.argv[1])/1024/1024/1024,1),'GiB')" "$(sysctl -n hw.memsize 2>/dev/null || echo 0)" 2>/dev/null || free -h | awk '/Mem:/{print $2}') |"
  echo "| Container runtime | $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'n/a') ($(docker version --format '{{.Server.Os}}/{{.Server.Arch}}' 2>/dev/null || echo '-')) |"
  echo "| Docker Compose | $(docker compose version --short 2>/dev/null || echo 'n/a') |"
  echo
  echo "## Docker VM (what the containers actually get)"
  echo
  echo "| Item | Value |"
  echo "| --- | --- |"
  echo "| Docker VM CPUs | $(docker info --format '{{.NCPU}}' 2>/dev/null || echo 'n/a') |"
  echo "| Docker VM memory | $(docker info --format '{{.MemTotal}}' 2>/dev/null | awk '{printf "%.1f GiB", $1/1024/1024/1024}' || echo 'n/a') |"
  echo
  echo "## Toolchain"
  echo
  echo "| Item | Value |"
  echo "| --- | --- |"
  echo "| Go (host) | $(go version 2>/dev/null || echo 'n/a') |"
  echo "| Go (in image) | $(compose run --rm --entrypoint /app/server api -version 2>/dev/null || echo 'n/a') |"
  echo "| PostgreSQL | $(compose exec -T postgres postgres --version 2>/dev/null || echo 'n/a') |"
  echo "| k6 | $(compose --profile load run --rm k6 version 2>/dev/null | tr '\n' ' ' || echo 'n/a') |"
  echo "| API image | $(docker images --format '{{.Repository}}:{{.Tag}} ({{.Size}})' scaling-systems-api:baseline 2>/dev/null | head -1 || echo 'n/a') |"
  echo
  echo "## Container resource limits"
  echo
  echo "| Service | CPUs | Memory | Source |"
  echo "| --- | --- | --- | --- |"
  # Go templates have no arithmetic (`div` does not exist), and scaling-k6 only
  # exists while a benchmark is running. So read the *declared* limits from
  # `docker compose config --format json` and cross-check against the live
  # container, reporting which one each number came from.
  compose --profile load config --format json 2>/dev/null | python3 -c '
import json, sys
try:
    services = json.load(sys.stdin).get("services", {})
except Exception:
    sys.exit(0)
def mib(n):
    n = int(n or 0)
    return "unlimited" if n <= 0 else "%d MiB" % (n // 1048576)
for name in ("api", "postgres", "k6"):
    svc = services.get(name)
    if not svc:
        print("| scaling-%s | n/a | n/a | not in compose file |" % name)
        continue
    lim = (svc.get("deploy", {}).get("resources", {}).get("limits", {}))
    res = (svc.get("deploy", {}).get("resources", {}).get("reservations", {}))
    cpus = lim.get("cpus", "unlimited")
    print("| scaling-%s | %s | %s | declared (reservations: %s CPU / %s) |"
          % (name, cpus, mib(lim.get("memory")), res.get("cpus", "-"), mib(res.get("memory"))))
' 2>/dev/null || echo "| _(could not read compose config)_ | | | |"
  echo
  echo "Live check (only for containers running right now):"
  echo
  for c in scaling-api scaling-postgres; do
    if docker inspect "$c" >/dev/null 2>&1; then
      nano=$(docker inspect "$c" --format '{{.HostConfig.NanoCpus}}')
      mem=$(docker inspect "$c" --format '{{.HostConfig.Memory}}')
      if [ "${nano:-0}" -gt 0 ]; then cpu="$((nano / 1000000000)) CPUs"; else cpu="unlimited"; fi
      if [ "${mem:-0}" -gt 0 ]; then mib="$((mem / 1048576)) MiB"; else mib="unlimited"; fi
      echo "- \`$c\`: $cpu, $mib"
    else
      echo "- \`$c\`: not running"
    fi
  done
  echo
  echo "## PostgreSQL settings in effect"
  echo
  echo '```'
  compose exec -T postgres psql -U "${DATABASE_USER:-scaling}" -d "${DATABASE_NAME:-scaling}" -At -F ' = ' -c \
    "SELECT name, setting, COALESCE(unit, '') FROM pg_settings
      WHERE name IN ('max_connections','shared_buffers','work_mem','maintenance_work_mem',
                     'effective_cache_size','max_worker_processes','max_parallel_workers_per_gather',
                     'wal_buffers','checkpoint_completion_target','random_page_cost',
                     'synchronous_commit','fsync','wal_level')
      ORDER BY name;" 2>/dev/null || echo "(postgres not running)"
  echo '```'
  echo
  echo "## Dataset size"
  echo
  echo "| Table | Rows | Total size |"
  echo "| --- | --- | --- |"
  # Exact counts. pg_stat_user_tables.n_live_tup is only a planner estimate, and
  # a subquery over pg_class would count catalog rows rather than table rows, so
  # the three known tables are counted explicitly.
  compose exec -T postgres psql -U "${DATABASE_USER:-scaling}" -d "${DATABASE_NAME:-scaling}" -At -F ' | ' -c \
    "SELECT 'orders', count(*), pg_size_pretty(pg_total_relation_size('orders'::regclass)) FROM orders
     UNION ALL
     SELECT 'users', count(*), pg_size_pretty(pg_total_relation_size('users'::regclass)) FROM users
     UNION ALL
     SELECT 'products', count(*), pg_size_pretty(pg_total_relation_size('products'::regclass)) FROM products
     ORDER BY 1;" 2>/dev/null || echo "(postgres not running)"
  echo
  echo "## Indexes actually present"
  echo
  echo '```'
  compose exec -T postgres psql -U "${DATABASE_USER:-scaling}" -d "${DATABASE_NAME:-scaling}" -At -c \
    "SELECT tablename || ': ' || indexname FROM pg_indexes
      WHERE schemaname = 'public' ORDER BY tablename, indexname;" 2>/dev/null || echo "(postgres not running)"
  echo '```'
} > "$OUT"

echo "wrote $OUT"
