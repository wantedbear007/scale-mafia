# Baseline

This directory holds the **reference measurement** for the whole project.
Every future optimisation is compared against the numbers recorded here.

```
benchmarks/
└── baseline/
    ├── README.md          <- this file: how the baseline is produced
    ├── results.md         <- filled in from a real run
    └── system-info.md     <- filled in by `make system-info`
```

`benchmarks/runs/` holds raw per-run artefacts (k6 `summary.json`, sampled
`docker stats`, `/metrics` snapshots). Those are git-ignored: they are large and
machine specific. The **conclusions** belong in `results.md`, which is
committed.

## Rules for this phase

1. **Do not optimise.** The baseline is supposed to be slow in interesting
   ways. Its job is to expose where the real bottlenecks are.
2. **Do not hard-code numbers.** Every figure in `results.md` comes from a run.
   If a cell is empty, the measurement has not been made yet.
3. **One change at a time.** After this baseline, each experiment changes
   exactly one thing, re-runs the same profile on the same machine, and records
   before / change / after / difference / explanation / trade-offs.

## Producing the baseline

```bash
make up            # postgres + api
make migrate       # create the schema
make seed-force    # TRUNCATE and reload 100k users / 50k products / 500k orders
make load-smoke    # 10 VUs, 1 minute - the correctness gate
make system-info   # record machine + toolchain + DB settings

# resource sampling around the real run (optional but recommended)
RUN=benchmarks/runs/$(date +%Y%m%d-%H%M%S)-baseline
mkdir -p $RUN
./scripts/sample-stats.sh $RUN/stats.tsv 5 &   # stop after the test finishes

make load-baseline K6_OUT=$(basename $RUN)

curl -s localhost:8080/metrics > $RUN/metrics-after.txt
```

The same thing, spelled out:

```bash
make up
make migrate
make seed
make load-baseline
```

## What the profiles are

| Profile | Command | VUs | Duration | Purpose |
| --- | --- | --- | --- | --- |
| Smoke | `make load-smoke` | 10 | 1 min | Correctness gate. Run first. |
| Baseline | `make load-baseline` | 100 | 30 s ramp + 2 min warm-up + 5 min load + 1 min drain | The reference number. |
| Stress | `make load-stress` | 100 → 200 → 300 → 500 | 2 min hold per stage | Find the saturation point. |

Each iteration fires `PARALLEL_REQUESTS` (default 10) requests concurrently and
then thinks for `THINK_TIME_SECONDS` (default 1). So the baseline profile aims
at **100 VUs × 10 = up to ~1000 requests in flight** — and *reports* what it
actually achieved rather than assuming it. See "Concurrency is measured, not
assumed" below.

## Concurrency is measured, not assumed

These three are different things and are easy to confuse:

| Term | Meaning | How it is obtained here |
| --- | --- | --- |
| **VUs** | Virtual users. k6 spins up N of them. It is a count of *loops*, not of open sockets. | `ramping-vus` executor |
| **RPS** | Requests completed per second. | `http_reqs` rate in the k6 summary |
| **Concurrency** | Requests in flight at one instant. ≈ RPS × latency (Little's Law). | k6's `http_req_waiting` **and** the API's own `http_requests_in_flight` |

100 VUs sending one request at a time can never exceed ~100 requests in flight,
and usually achieve less because each VU spends most of its time in `sleep()`.
That is why the workload issues requests in parallel batches.

When reading a result, check that these two agree:

```bash
# what k6 thought was in flight
jq '.metrics.http_req_waiting' benchmarks/runs/<run>/summary.json

# what the API actually had open, sampled during the run
grep http_requests_in_flight benchmarks/runs/<run>/metrics-after.txt
```

If k6's `http_req_waiting` climbs but the API's `http_requests_in_flight`
does not, the requests are queueing **outside** the application (TCP accept
queue, or the k6 container itself is out of CPU).

## Reading the latency numbers

| Percentile | Meaning | What it tells you |
| --- | --- | --- |
| p50 | Half the requests were faster | The "typical" experience under load |
| p90 | 90% were faster | Early tail; first sign of contention |
| p95 | 95% were faster | The usual target for an SLA |
| p99 | 99% were faster | The worst 1%; the first thing users actually complain about |
| max | The single slowest request | Outliers, timeouts, GC pauses, connection-pool waits |

A healthy system has p50 ≈ p95. A system past its knee has p95 far above p50:
requests are queueing behind a saturated resource. That gap is the signal worth
chasing.

## Manual verification checklist

There are no automated tests in the baseline phase (the benchmark is the
deliverable). Before recording a run, confirm the system is correct:

```bash
# liveness and readiness
curl -s localhost:8080/health
curl -s localhost:8080/ready

# users CRUD
curl -s -X POST localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada Lovelace","email":"ada@example.com"}'          # 201
curl -s 'localhost:8080/api/v1/users?page=1&limit=2'              # paginated envelope
curl -s localhost:8080/api/v1/users/1                             # 200
curl -s -X PUT localhost:8080/api/v1/users/1 \
  -H 'Content-Type: application/json' -d '{"name":"Ada L"}'       # 200
curl -s -X DELETE localhost:8080/api/v1/users/1 -o /dev/null -w '%{http_code}\n'  # 204

# products and orders CRUD
curl -s -X POST localhost:8080/api/v1/products \
  -H 'Content-Type: application/json' \
  -d '{"name":"Keyboard","price":"49.99","stock":10}'             # 201
curl -s -X POST localhost:8080/api/v1/orders \
  -H 'Content-Type: application/json' \
  -d '{"user_id":1,"total_amount":"99.98"}'                       # 201

# error handling
curl -s localhost:8080/api/v1/users/999999999                      # 404 user_not_found
curl -s -X POST localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' -d '{"name":"","email":"x"}' # 400 validation_error
curl -s -X POST localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"dup","email":"ada@example.com"}'                    # 409 duplicate_user
curl -s -X POST localhost:8080/api/v1/orders \
  -H 'Content-Type: application/json' \
  -d '{"user_id":999999999,"total_amount":"1.00"}'                # 400 invalid_foreign_key
curl -s -X PUT localhost:8080/api/v1/orders/1 \
  -H 'Content-Type: application/json' -d '{"status":"teleported"}' # 400 validation_error
curl -s 'localhost:8080/api/v1/users?page=0&limit=20'              # 400 invalid_page
curl -s 'localhost:8080/api/v1/users?page=1&limit=99999'          # 400 invalid_limit

# metrics endpoint
curl -s localhost:8080/metrics | grep -E '^(http_requests_total|db_pool_|go_goroutines)'
```

If `make load-smoke` reports `http_req_failed: 0.00%` and the checklist above
behaves as commented, the baseline is valid.

## Experiment report template

Copy this for every optimisation. **Do not skip the trade-offs section** - it
is the part that stops the next person from repeating a bad idea.

```markdown
## Experiment NNN: <one-sentence change>

**Before**  (commit <sha>)
| Metric | Value |
| --- | --- |
| RPS | |
| p50 / p95 / p99 / max | |
| Error rate | |
| API CPU / RAM | |
| Postgres CPU / RAM | |
| DB pool in use / max, wait count | |

**Change**
<exactly what changed, and the file(s) touched>

**After**   (commit <sha>, same machine, same profile, same seed)
| Metric | Value |
| --- | --- |
| RPS | |
| p50 / p95 / p99 / max | |
| Error rate | |
| API CPU / RAM | |
| Postgres CPU / RAM | |
| DB pool in use / max, wait count | |

**Difference**
<deltas, and which of them are outside run-to-run noise>

**Explanation**
<the mechanism. Cite EXPLAIN output, pprof profiles, or metric series.>

**Trade-offs**
<what got worse: memory, write amplification, correctness risk, complexity,
 time-to-migrate. What would make this a bad idea at 10x the data?>

**Keep / Revert**
<decision, and the metric you will watch to catch a regression later>
```
