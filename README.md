# scaling-systems

A deliberately **unoptimised** CRUD backend plus a reproducible benchmark
harness. This is phase 1 of a long-term learning project about how to design,
measure, optimise and scale backend systems.

> **This is the baseline. Do not optimise it.**
> There is no cache, no queue, no replica, no index that was not forced on us,
> and no clever Go. That is intentional: the value of the first benchmark is
> that it exposes where the real bottlenecks are, and that is only true if the
> starting point is naive. Every optimisation later in the project is measured
> against the numbers recorded in [`benchmarks/baseline/results.md`](benchmarks/baseline/results.md).

---

## Contents

- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Full execution walkthrough](#full-execution-walkthrough)
    - 0 Prerequisites · 1 Setup · 2 Migrate · 3 Seed · 4 Try the API · 5–7 Load tests · 8 Monitoring · 9 Teardown · 10 Troubleshooting
- [Project structure](#project-structure)
- [Database schema](#database-schema)
- [API](#api)
- [Pagination](#pagination)
- [Configuration](#configuration)
- [Metrics](#metrics)
- [Logging](#logging)
- [Running the benchmarks](#running-the-benchmarks)
- [Understanding the numbers](#understanding-the-numbers)
- [Monitoring resources](#monitoring-resources)
- [Benchmark methodology](#benchmark-methodology)
- [Deliberate omissions](#deliberate-omissions)

---

## Architecture

```text
              k6 (separate container, own CPU/memory budget)
               |  HTTP, read-heavy mix across 3 resources
               v
      +---------------------+
      |  Fiber HTTP API     |   2 CPUs / 4 GB
      |  handler -> service |   1 replica, no load balancer
      |      -> repository  |
      +----------+----------+
                 |  database/sql + pgx, pool of 100
                 v
      +---------------------+
      |  PostgreSQL 18.6    |   2 CPUs / 4 GB
      |  stock config       |   max_connections = 200
      +---------------------+

      Prometheus metrics on the API at GET /metrics
      Structured JSON logs on the API's stdout
```

Request flow is exactly four hops, and nothing hides a fifth:

```text
HTTP -> Fiber handler -> service -> repository -> PostgreSQL
```

`handler` parses and shapes HTTP. `service` holds validation and the "no rows
means 404" rule. `repository` is the only layer that contains SQL. There is no
unit-of-work, no interface-per-layer-for-its-own-sake, and no event bus.

---

## Quick start

Requirements: Docker with Compose v2+ (`docker compose`, **not** the legacy
`docker-compose` v1), and ~10 GB of memory available to your Docker VM.

```bash
cp .env.example .env     # optional; the defaults already work
make up                  # start postgres + api, wait for /ready
make migrate             # create the schema (idempotent)
make seed                # load 100k users / 50k products / 500k orders (~3 s)
make load-smoke          # 10 VUs, 1 minute - proves everything works
make load-baseline       # 100 VUs, 5 minute measured window
```

`make help` lists every target.

The first `make up` builds the image from scratch (about a minute on a laptop);
later runs reuse the cache.

### Where to look once it is running

| URL | What |
| --- | --- |
| <http://localhost:8080/health> | liveness - never touches the database |
| <http://localhost:8080/ready> | readiness - pings PostgreSQL |
| <http://localhost:8080/metrics> | Prometheus metrics |
| <http://localhost:8080/api/v1/users?page=1&limit=20> | a paginated collection |

---

## Full execution walkthrough

The end-to-end flow, from a clean checkout to a recorded baseline. Every step
below was performed and verified while writing these docs; timings are real
off this machine.

### 0. Prerequisites

- Docker with Compose **v2** (`docker compose`, not the legacy v1). Check:
  `docker compose version` → `Docker Compose version v2.x.x`.
- `make` (ships with Xcode Command Line Tools on macOS).
- ~10 GB of memory available to the Docker VM. On Docker Desktop / OrbStack,
  give the VM at least 8 GB; this project's containers reserve 2 × 4 GB
  (`api`, `postgres`) plus 2 GB for `k6`.
- Nothing else CPU- or disk-heavy running. A second benchmark running at the
  same time invalidates every number.

```bash
cd scaling-systems
make help        # every target that follows, one screen
```

### 1. One-time setup

```bash
cp .env.example .env     # optional; the defaults already work
make up                  # builds the image the first time, then starts postgres + api
```

`make up` is two things at once: it builds `scaling-systems-api:baseline`
(about a minute from scratch, cached afterwards) and then starts PostgreSQL and
the API in detached mode. The shell blocks until the API answers `/ready`, so
the next step is safe to run immediately after.

| Wait - how long? | `make up` returns when `/ready` answers |
| --- | --- |
| Confirm containers are up and healthy | `make ps` → both `(healthy)` |
| API logs | `make logs` (tail) |

The API exposes a `HEALTHCHECK` that pings `/ready` about twice a minute, so a
`healthy` status in `make ps` already proves the server can reach PostgreSQL.

### 2. Migrate

```bash
make migrate         # apply migrations (idempotent, safe to re-run)
make migrate-status  # show applied / pending
```

`make migrate` runs the `migrate` binary, which embeds the SQL in
`migrations/`, applies it in version order inside one transaction per migration,
and records it in `schema_migrations`. The second run applies nothing and exits
with a message like `no pending migrations`.

First run output (abridged; the migration binary writes JSON to stdout):

```text
{"time":"...","level":"INFO","msg":"migrations_applied","applied":[1],"skipped":[],"elapsed":"..."}
```

An already-up-to-date run instead says:

```text
{"time":"...","level":"INFO","msg":"migrations_up_to_date","skipped":[1],"elapsed":"..."}
```

### 3. Seed the benchmark dataset

```bash
make seed          # top up to 100k / 50k / 500k; no-op if already at target
make seed-force    # TRUNCATE ... RESTART IDENTITY, then reload - use before a measured run
```

`make seed` takes about **3 seconds**. The seeder logs one
`seed_complete` line on the API's stdout (its own logs, via `make logs`):

```json
{"time":"...","level":"INFO","msg":"seed_complete","service":"api","env":"development",
 "users":100000,"products":50000,"orders":500000,"elapsed":"2.553277114s"}
```

Verify:

```bash
make db-size          # row counts + on-disk sizes per table
make psql             # interactive shell if you prefer SQL
```

> **Rule for measured runs:** run `make seed-force`, never `make seed`. A load
> test POSTs and DELETEs, so it mutates the tables (one baseline run takes
> users 100k → 122k, orders 500k → 521k). `seed` only tops up missing rows and
> would not bring earlier runs back to exactly the same ids. `seed-force`
> does. Deterministic dataset = comparable runs.

### 4. Exercise the API by hand

The server is now live at <http://localhost:8080>.

```bash
# liveness / readiness
curl -s localhost:8080/health        # {"status":"ok","time":"2026-09-29T07:52:55Z"}
curl -s localhost:8080/ready         # {"database":"ok","status":"ok"}

# create a user (201, plus an X-Request-Id header you can trace in the logs)
curl -s -X POST localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice","email":"alice@example.com"}' -i | head -6

# read it back
curl -s localhost:8080/api/v1/users/1

# the only valid inputs the API enforces
curl -s -X POST localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' -d '{"name":"x"}' | jq .error.message
  # "validation failed: email: is required and must not be empty"

# 404 shape (resource-specific: users -> user_not_found)
curl -s localhost:8080/api/v1/users/99999999 | jq .error.code     # "user_not_found"

# pagination envelope on a collection
curl -s 'localhost:8080/api/v1/orders?page=1&limit=20' | jq '.pagination'

# Prometheus metrics
curl -s localhost:8080/metrics | grep -E 'http_requests_total|db_pool_connections'
```

HTTP semantics: `201` create, `200` read/update/list, `204` delete, `400`
validation, `404` unknown id, `409` duplicate email, `503` readiness when the DB
is down. Every error body carries a `request_id` that also appears in the
log line for that request (`X-Request-Id` response header).

### 5. Load test: smoke (correctness gate)

```bash
make load-smoke        # 10 VUs, ~1 minute
```

This is the gate. It exercises every endpoint with the real workload mix and
**must report `http_req_failed: 0.00%`**. 0.00% is a baseline requirement, not a
nice-to-have: if smoke shows errors, the workload itself is broken (e.g. reads
racing deletes), and any subsequent benchmark is garbage. Fix the workload
before measuring.

The smoke profile is 10 s ramp to 5 → 20 s at 10 → 30 s holding 10, then drain.
Since `PARALLEL_REQUESTS=10` and `THINK_TIME_SECONDS=1`, it fires roughly
`iteration_duration ≈ 1s` and completes ~450 iterations in a minute.

On success the run dir appears with a `summary.json`:

```bash
ls benchmarks/runs/*-smoke/
# summary.json
```

### 6. Load test: baseline (the recorded measurement)

```bash
make seed-force       # always re-seed before a measured run
make load-baseline    # 100 VUs, 8m30s
```

Shorthand, if you want a named artifact folder:

```bash
make seed-force
make load-baseline K6_OUT=my-baseline
ls benchmarks/runs/my-baseline/
```

What happens in those 8m30s:

| Segment | Duration | What it is for |
| --- | ---: | --- |
| Ramp | 30 s | bring 100 VUs online without a cold-start cliff |
| Warm-up | 2 m | let caches and the connection pool stabilise |
| **Measured window** | **5 m** | the numbers you quote come from here |
| Cooldown | 1 m | drain graciously, watch the system recover |

Each VU fires 10 requests in a batch (`http.batch`), sleeps 1 s, repeats - the
70/20/5/5 GET/POST/PUT/DELETE mix across users, products and orders.

While it runs, in another terminal (see [Monitoring resources](#monitoring-resources)):

```bash
make watch-stats      # api + postgres CPU/RAM, refreshing
make db-stats         # connections, running/waiting queries, cache hit ratio
make metrics          # /metrics snapshot
```

When it finishes, the recorded result is waiting:

```bash
ls benchmarks/runs/*-baseline/
# metrics-before.txt  metrics-after.txt  stats.tsv  summary.json
```

```bash
# headline numbers in one line
jq '{rps: .metrics.http_reqs.values.rate,
     p50: .metrics.http_req_duration.values["p(50)"],
     p95: .metrics.http_req_duration.values["p(95)"],
     p99: .metrics.http_req_duration.values["p(99)"],
     failed: .metrics.http_req_failed.value}' \
  benchmarks/runs/*-baseline/summary.json
```

The recorded baseline for this repo lives in
[`benchmarks/baseline/results.md`](benchmarks/baseline/results.md) - the
current figures are **675.70 req/s, p50 74.36 ms, p95 475.38 ms, p99 802.48 ms,
0 errors**, with PostgreSQL saturated at ~160% CPU and the API at ~11%.

### 7. Load test: stress (find the knee, expected to break)

```bash
make seed-force
make load-stress       # 100 -> 200 -> 300 -> 500 VUs, 2m each, ~10m30s total
```

The stress profile is the destroyer test. Each stage ramps 30 s to the next
VU level and holds 2 m. The system should degrade progressively and the run is
expected to exceed the baseline thresholds near the top end. The point is to
find **which** thing breaks first (this baseline: PostgreSQL CPU), and to get
the shape of the degradation curve - gradual, cliff, or oscillating.

The summary land in `benchmarks/runs/<timestamp>-stress/`. `http_req_failed`
will NOT be 0% here, and that is the finding, not a failure of the test.

### 8. Monitoring during a run

```bash
make stats                # one-shot docker stats for api + postgres
make watch-stats          # same, refreshing every second
make db-stats             # connections, running/waiting queries, DB size, cache hit %
make db-locks             # blocked queries; empty output = no lock contention
make db-size              # per-table rows + sizes
make metrics              # snapshot /metrics to a file
make psql                 # interactive session
```

Each baseline run also records its own `stats.tsv` (docker stats every 5 s) and
`metrics-before/after.txt`, so you can reconstruct resource usage after the
fact without remembering to watch it live.

### 9. Tear down

```bash
make down        # stop postgres + api, KEEP the database volume
make down-all    # stop AND delete the database volume (next start is empty)
make restart     # stop + start without losing data
```

Volume notes:

- `make down` then `make up` → same data is still there (the volume survives).
- `make down-all` → next start needs `make migrate` (and `make seed-force`)
  again. This is the "wipe everything" switch.
- The API image is unchanged by these; `make build` rebuilds it.

### 10. Troubleshooting

| Symptom | Most likely cause | Fix |
| --- | --- | --- |
| `make up` hangs at "waiting for API" | `/ready` is 503 - API cannot reach PostgreSQL | `docker compose logs api postgres`, then `make restart` |
| `make migrate` reports it can't connect | postgres not up yet | The `make up` gate usually prevents this; give it a few seconds, or `make restart` |
| `http_req_failed` > 0 in smoke | Workload bug (reads racing deletes), not the system | `make load-smoke` again; if it persists, check the failures in the k6 console output |
| `too many clients` / "connection limit exceeded" | eventually a *good* result - the pool hammered `max_connections` | Read the pool metrics: `curl -s localhost:8080/metrics \| grep db_pool_wait` |
| Numbers look identical between two profiles | You forgot to re-seed; `seed` tops up, it does not reset | `make seed-force` before every run |
| Port `8080` or `5432` busy | Another stack on the same ports | `docker compose down`, or change `APP_PORT` / `DATABASE_PORT` in `.env` |
| Image build is slow | First build (no cache) | It caches afterwards; `make build` re-runs it intentionally |

### 11. Reproduce the recorded baseline exactly

```bash
make up
make migrate
make seed-force
make load-smoke       # 0% errors
make load-baseline
```

- Same host as [`benchmarks/baseline/system-info.md`](benchmarks/baseline/system-info.md).
- Nothing else CPU-heavy running.
- Compare your run to [`benchmarks/baseline/results.md`](benchmarks/baseline/results.md).
  Remember: under 3% difference on the headline metrics is run-to-run noise.

---

## Project structure

```text
scaling-system/
├── cmd/
│   ├── server/main.go          # the API; also its own health check (no shell needed)
│   └── migrate/main.go         # migrations + seeding
├── internal/
│   ├── apierr/                 # one error type -> consistent JSON + HTTP status
│   ├── config/                 # every env var, with documented defaults
│   ├── database/               # pool setup + Prometheus pool collector
│   ├── handlers/               # HTTP layer
│   ├── migrate/                # migration runner (embedded SQL, advisory lock)
│   ├── middleware/             # request id, logging, metrics, recovery, errors
│   ├── models/                 # entities and API payloads
│   ├── observability/          # slog setup + metric definitions
│   ├── repositories/           # the only SQL in the project
│   ├── seeder/                 # deterministic batched benchmark data
│   ├── server/                 # dependency wiring + routes
│   ├── services/               # business rules
│   └── validation/             # hand-rolled input validation
├── migrations/
│   ├── 0001_init.sql           # schema (embedded into the binary)
│   └── embed.go
├── tests/load/
│   ├── lib/{config,seed,workload,thresholds}.js
│   ├── smoke.js                # profile 1: 10 VUs, 1 min
│   ├── baseline.js             # profile 2: 100 VUs, 5 min load
│   └── stress.js               # profile 3: 100 -> 200 -> 300 -> 500 VUs
├── benchmarks/
│   ├── baseline/{README,results,system-info}.md
│   └── runs/                   # raw per-run artefacts (git-ignored)
├── scripts/{collect-system-info,sample-stats}.sh
├── Dockerfile                  # multi-stage: golang builder -> distroless runtime
├── docker-compose.yml
├── Makefile
└── .env.example
```

Three dependencies, all stable and mainstream:

| Dependency | Version | Why |
| --- | --- | --- |
| `gofiber/fiber/v2` | v2.52.15 | the web framework under test |
| `jackc/pgx/v5` (via `stdlib`) | v5.11.0 | the PostgreSQL driver; `stdlib` keeps us on `database/sql` so Phase 9 can compare against raw Go |
| `prometheus/client_golang` | v1.24.1 | metrics, and the Go runtime/process collectors come free with it |

Structured logging is `log/slog` from the standard library, so there is no
logging dependency. Validation and pagination are hand-rolled for the same
reason: fewer moving parts to read.

---

## Database schema

```text
users                        products
┌──────────────────────┐     ┌──────────────────────┐
│ id          BIGINT PK │     │ id          BIGINT PK │
│ name        TEXT      │     │ name        TEXT      │
│ email       TEXT  UQ  │     │ description TEXT      │
│ created_at  TIMESTAMPTZ   │ price       NUMERIC   │
│ updated_at  TIMESTAMPTZ   │ stock       INTEGER    │
└──────────┬───────────┘     │ created_at  TIMESTAMPTZ
           │                 │ updated_at  TIMESTAMPTZ
           │ 1               └──────────────────────┘
           │
           │ N
┌──────────┴───────────┐
│ id           BIGINT PK│   status CHECK IN
│ user_id     BIGINT FK │     (pending, paid, shipped,
│ status      TEXT      │      delivered, cancelled)
│ total_amount NUMERIC  │   ON DELETE CASCADE
│ created_at  TIMESTAMPTZ
│ updated_at  TIMESTAMPTZ
└──────────────────────┘
```

Also enforced by the database, not just by Go:

- `users.email` is `UNIQUE` (case-insensitive in practice, because the
  application lower-cases before insert)
- `products.price >= 0`, `products.stock >= 0`
- `orders.total_amount >= 0`, `orders.status` in the allowed set
- `name` must be non-blank and at most 255 characters
- `ON DELETE CASCADE` from `orders` to `users`

### Indexes - and what is missing on purpose

The **only** indexes are the ones PostgreSQL creates for us:

```text
orders_pkey
products_pkey
users_pkey
users_email_key      (backs the UNIQUE constraint)
```

Deliberately absent, each a future experiment:

| Missing index | Why we are leaving it out |
| --- | --- |
| `orders(user_id)` | a foreign key does **not** create an index in PostgreSQL. Listing a user's orders is a `Seq Scan` today. |
| composite `(user_id, created_at DESC)` | needed for keyset pagination later |
| any index to support `ORDER BY` in the list endpoints | the PK is the only ordering support we have |
| `products(name)` | the moment search arrives |
| a partial or covering index for the list queries | the `count(*)` on every collection request is a full table scan - a genuine, measurable baseline cost |

Each of these is a hypothesis to test in Phase 2, not a to-do for now.

### Migrations

Migrations are plain SQL, embedded into the binary, applied in version order,
one transaction each, tracked in `schema_migrations`:

```bash
make migrate         # apply
make migrate-status  # show applied / pending
make db-reset        # drop everything, re-apply, re-seed
```

They are deterministic and re-runnable. An already-applied migration whose
contents changed is **rejected** (checksum mismatch) rather than silently
ignored - append a new file instead.

### Seeding

```bash
make seed          # top up to the target counts, idempotent
make seed-force    # TRUNCATE ... RESTART IDENTITY, then reload
```

| Table | Rows | Approx. on-disk size |
| --- | --- | --- |
| users | 100,000 | ~18 MB |
| products | 50,000 | ~7 MB |
| orders | 500,000 | ~49 MB |

Seeding uses batched multi-row `INSERT`s (5,000 rows per statement by default,
`SEED_BATCH_SIZE`) through the same driver as the application, and takes about
**3 seconds**. `COPY` would be faster, but keeping the seeder on the same code
path as the app is worth more than shaving two seconds off a one-off step.

The dataset is **deterministic**: fixed RNG seeds, explicit ids, so a reseed
produces the same rows and ids 1..N always exist. That is what lets the load
test address real rows, and what makes two runs comparable. `ANALYZE` is run at
the end so the planner is not working from stale statistics.

---

## API

All endpoints live under `/api/v1` and speak JSON.

### Users

| Method | Path | Success | Notes |
| --- | --- | --- | --- |
| `POST` | `/api/v1/users` | `201` | `{name, email}`; `409` if the email exists |
| `GET` | `/api/v1/users` | `200` | `?page=&limit=`, returns a paginated envelope |
| `GET` | `/api/v1/users/:id` | `200` | `404` if unknown |
| `PUT` | `/api/v1/users/:id` | `200` | partial update; at least one field required |
| `DELETE` | `/api/v1/users/:id` | `204` | cascades to that user's orders |

### Products

| Method | Path | Success | Notes |
| --- | --- | --- | --- |
| `POST` | `/api/v1/products` | `201` | `{name, description?, price, stock}` |
| `GET` | `/api/v1/products` | `200` | `?page=&limit=` |
| `GET` | `/api/v1/products/:id` | `200` | |
| `PUT` | `/api/v1/products/:id` | `200` | partial update |
| `DELETE` | `/api/v1/products/:id` | `204` | |

### Orders

| Method | Path | Success | Notes |
| --- | --- | --- | --- |
| `POST` | `/api/v1/orders` | `201` | `{user_id, status?, total_amount}`; `400` if the user does not exist |
| `GET` | `/api/v1/orders` | `200` | `?page=&limit=` |
| `GET` | `/api/v1/orders/:id` | `200` | |
| `PUT` | `/api/v1/orders/:id` | `200` | `{status?, total_amount?}` |
| `DELETE` | `/api/v1/orders/:id` | `204` | |

### Health and observability

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | liveness. Does **not** touch the database, so a DB blip cannot cause a restart loop. |
| `GET` | `/ready` | readiness. Pings PostgreSQL; `503` when unreachable. Used by Compose and the container `HEALTHCHECK`. |
| `GET` | `/metrics` | Prometheus exposition. |

### Error format

Every error, including unmatched routes, uses the same envelope:

```json
{
  "error": {
    "code": "validation_error",
    "message": "validation failed: email: must be a valid email address",
    "request_id": "9a3f1c0b7d2e4f5a8b6c",
    "details": [{ "field": "email", "message": "must be a valid email address" }]
  }
}
```

| Status | When |
| --- | --- |
| `400` | malformed JSON, failed validation, unknown `page`/`limit`, violated FK |
| `404` | unknown id, or an unmatched route |
| `409` | unique constraint violation (duplicate email) |
| `413` | body over `APP_BODY_LIMIT_BYTES` (1 MB) |
| `500` | unexpected server error - the cause is logged, never returned |
| `503` | `/ready` only, when PostgreSQL is unreachable |

Internal details (SQL text, hostnames, stack traces) never reach the client;
the `request_id` in the response is the way to find the corresponding log line.

---

## Pagination

Offset pagination, on purpose. Cursor/keyset pagination is a Phase 2
experiment, and offset pagination is the thing worth measuring first.

```text
GET /api/v1/orders?page=1&limit=20
```

```json
{
  "data": [ ... ],
  "pagination": { "page": 1, "limit": 20, "total": 500000, "total_pages": 25000, "has_next": true }
}
```

| Rule | Value |
| --- | --- |
| default | `page=1`, `limit=20` |
| minimum `limit` | 1 |
| maximum `limit` | 1000 (`400 invalid_limit` above that) |
| ordering | `ORDER BY id` - stable, backed only by the primary key |

Every collection request runs **two** statements: `SELECT count(*)` and the
paged `SELECT`. The `count(*)` is a full table scan on every single call. On
500,000 orders that is real, measurable cost, and it is one of the first
things Phase 2 should attack.

---

## Configuration

Everything is an environment variable with a working default; see
[`.env.example`](.env.example).

### Application

| Variable | Default | Meaning |
| --- | --- | --- |
| `APP_ENV` | `development` | label attached to logs and metrics |
| `APP_HOST` | `0.0.0.0` | listen address |
| `APP_PORT` | `8080` | listen port |
| `APP_READ_TIMEOUT` | `15s` | request read timeout |
| `APP_WRITE_TIMEOUT` | `30s` | response write timeout |
| `APP_IDLE_TIMEOUT` | `60s` | keep-alive idle timeout |
| `APP_SHUTDOWN_TIMEOUT` | `20s` | graceful shutdown budget |
| `APP_BODY_LIMIT_BYTES` | `1048576` | max request body (1 MB) |
| `APP_TRUST_PROXY` | `false` | honour `X-Forwarded-*`. Off by default: trusting it would let a client spoof its IP in the logs. |

### Database and connection pool

| Variable | Default | Meaning |
| --- | --- | --- |
| `DATABASE_HOST` | `localhost` | |
| `DATABASE_PORT` | `5432` | |
| `DATABASE_NAME` | `scaling` | |
| `DATABASE_USER` | `scaling` | |
| `DATABASE_PASSWORD` | `scaling` | |
| `DATABASE_SSLMODE` | `disable` | fine inside the Compose network |
| `DATABASE_MAX_OPEN_CONNECTIONS` | `100` | hard ceiling on the pool |
| `DATABASE_MAX_IDLE_CONNECTIONS` | `50` | connections kept warm |
| `DATABASE_CONN_MAX_LIFETIME` | `30m` | recycle connections (spreads server-side cost, clears DNS) |
| `DATABASE_CONN_MAX_IDLE_TIME` | `5m` | drop genuinely unused connections |
| `DATABASE_CONNECT_TIMEOUT` | `10s` | per-attempt dial timeout |
| `DATABASE_WAIT_TIMEOUT` | `60s` | how long startup retries before giving up |
| `DATABASE_URL` | *(empty)* | full DSN; overrides all of the above when set |

**Why 100 / 50 / 30m?** These are ordinary, widely used starting values, not a
tuned configuration. Go's own defaults are *unlimited* open connections, which
would simply hand PostgreSQL its `max_connections` limit and produce a confusing
"too many clients" wall instead of a latency curve. 100 open connections is
enough headroom to be a real bottleneck under the 1,000-concurrency baseline
rather than a hard failure. Tuning them is a Phase 2/3 experiment.

### Seeding

| Variable | Default |
| --- | --- |
| `SEED_USERS` | `100000` |
| `SEED_PRODUCTS` | `50000` |
| `SEED_ORDERS` | `500000` |
| `SEED_BATCH_SIZE` | `5000` |
| `SEED_FORCE` | `false` |

### Logging

| Variable | Default | Meaning |
| --- | --- | --- |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `json` | `json` or `text` |

---

## Metrics

`GET /metrics` exposes Prometheus text format. Labels are deliberately
low-cardinality: `route` is the **registered route pattern**
(`/api/v1/users/:id`), never the raw path, so a scan of arbitrary URLs cannot
explode the metric.

### HTTP

| Metric | Type | Labels |
| --- | --- | --- |
| `http_requests_total` | counter | `method`, `route`, `status` |
| `http_request_duration_seconds` | histogram | `method`, `route` |
| `http_requests_in_flight` | gauge | - |
| `http_request_size_bytes` | histogram | `method`, `route` |
| `http_response_size_bytes` | histogram | `method`, `route`, `status` |

`http_requests_total` broken down by `status` is the status distribution;
`http_requests_in_flight` is the server-side view of concurrency and is the
number to compare against k6's `http_req_waiting`.

### Database

| Metric | Type |
| --- | --- |
| `db_pool_connections_open` | gauge |
| `db_pool_connections_in_use` | gauge (this is "active connections") |
| `db_pool_connections_idle` | gauge |
| `db_pool_connections_max_open` | gauge |
| `db_pool_wait_count_total` | counter - **pool exhaustion signal** |
| `db_pool_wait_duration_seconds_total` | counter |
| `db_pool_max_idle_closed_total` | counter |
| `db_pool_max_lifetime_closed_total` | counter |
| `db_pool_max_idle_time_closed_total` | counter |
| `db_query_duration_seconds` | histogram, label `operation` (e.g. `orders.list`) |
| `db_queries_total` | counter, labels `operation`, `outcome` |

### Go runtime and process

Registered by `client_golang`, so they are free:

| Metric | What it tells you |
| --- | --- |
| `go_goroutines` | goroutine count; a leak or blocked pool shows up here |
| `go_memstats_alloc_bytes` | live heap |
| `go_memstats_alloc_bytes_total` | cumulative allocation - the driver of GC frequency |
| `go_memstats_heap_alloc_bytes`, `go_memstats_heap_sys_bytes` | heap in use vs. obtained from the OS |
| `go_gc_duration_seconds` | **GC pause time**, summarised |
| `go_gc_forced_total` | forced collections (a sign of memory pressure) |
| `go_threads` | OS threads |
| `process_cpu_seconds_total` | CPU actually consumed by the process |
| `process_resident_memory_bytes` | RSS |
| `app_info` | Go version, GOOS/GOARCH, `GOMAXPROCS`, `runtime.NumCPU`, build version |

A few useful one-liners:

```bash
# effective concurrency
curl -s localhost:8080/metrics | grep http_requests_in_flight

# pool pressure: is wait_count climbing?
curl -s localhost:8080/metrics | grep db_pool_

# slowest operations
curl -s localhost:8080/metrics \
  | awk '/^db_query_duration_seconds_bucket/ {split($1,a,"\""); print a[2], $2}' \
  | sort -k2 -n | tail -10

# GC
curl -s localhost:8080/metrics | grep -E 'go_gc_duration_seconds_(sum|count)'
```

---

## Logging

One JSON object per line on stdout, from `log/slog`:

```json
{"time":"2026-09-29T06:13:41.123Z","level":"INFO","msg":"request","service":"api",
 "env":"development","method":"GET","route":"/api/v1/orders/:id","path":"/api/v1/orders/84",
 "status":200,"duration_ms":2.91,"bytes":142,"ip":"172.20.0.5",
 "user_agent":"Grafana k6/2.3.0","request_id":"2574f139b1c65d82e02b2fae"}
```

Every request can be traced by `request_id`, which is:

- generated (12 random bytes, hex) if the client did not send a usable one
- echoed back in the `X-Request-Id` response header
- accepted from an inbound `X-Request-Id` (sanitised: max 128 chars, `[A-Za-z0-9._:-]`) so a trace can span services
- present in every error body and every log line for that request

Levels: `INFO` for success, `WARN` for 4xx, `ERROR` for 5xx and panics
(panics include a stack trace in the log, never in the response).

**What is deliberately not logged:** request bodies, response bodies, headers
that can carry credentials, the database password (the DSN is logged redacted).
The user agent is truncated to 128 characters so a client cannot flood the log
or inject newlines.

### A fasthttp gotcha worth knowing

Fiber sits on fasthttp, which reuses request buffers. Strings returned by
`c.Method()` and `c.Path()` point into memory that gets overwritten on the next
request, so keeping them in a Prometheus label corrupts the label. The first
version of this baseline had a metric literally named
`method="GETE"`. `internal/middleware/fasthttp.go` maps the method onto
constants and copies the path; if you add middleware, do the same.

---

## Running the benchmarks

### Prerequisites

```bash
make up
make migrate
make seed-force     # a fresh, deterministic dataset
make load-smoke     # correctness gate: must report 0% errors
```

### Profiles

| Profile | Command | Shape |
| --- | --- | --- |
| 1 - Smoke | `make load-smoke` | 10 VUs, 1 minute |
| 2 - Baseline | `make load-baseline` | 30 s ramp to 100, **2 min warm-up**, **5 min measured**, 1 min drain |
| 3 - Stress | `make load-stress` | 100 → 200 → 300 → 500 VUs, 2 min each |

The stress profile is expected to fall over. Its job is to find the knee.

### Knobs

```bash
make load-baseline K6_PARALLEL=10 K6_THINK=1 K6_OUT=my-run
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `PARALLEL_REQUESTS` | `10` | concurrent requests per iteration - this is what creates concurrency |
| `THINK_TIME_SECONDS` | `1` | sleep between iterations; `0` = closed loop |
| `ID_POOL_SIZE` | `5000` | ids sampled per resource during setup |
| `MAX_LIST_PAGE` | `5` | how deep list pagination is exercised |
| `K6_OUT` | timestamp | output folder under `benchmarks/runs/` |

### The workload

70% GET / 20% POST / 5% PUT / 5% DELETE, spread across all three resources
(~34% orders, ~33% products, ~33% users). One request in four is a paginated
collection read rather than a single-row read, because `count(*)` plus `OFFSET`
is where the baseline is expected to hurt most.

One k6 iteration fires `PARALLEL_REQUESTS` requests with `http.batch` and then
sleeps. A batch is **one** iteration but `PARALLEL_REQUESTS` **requests**, which
is exactly how 100 VUs produce up to ~1000 requests in flight.

Two subtleties the workload handles, both of which would otherwise pollute the
measurement with fake errors:

1. The sampled id pool is split into a **read region** and a **delete region**.
   If they overlapped, VUs would read rows other VUs had just deleted and the
   benchmark would report a few percent of 404s that say nothing about
   performance.
2. `orders.user_id` is `ON DELETE CASCADE`, so deleting a *seeded* user would
   silently delete its orders, including ones inside the read region. Users are
   therefore only ever deleted after the same VU created them - which is what a
   real client does anyway.

### Artefacts

Each run writes to `benchmarks/runs/<timestamp>-<profile>/`:

| File | What |
| --- | --- |
| `summary.json` | the full k6 summary (all percentiles, every metric) |
| `stats.tsv` | sampled `docker stats` for api and postgres, every 5 s |
| `metrics-before.txt`, `metrics-after.txt` | `/metrics` scraped around the run |

### The full reproducible sequence

```bash
make up
make migrate
make seed-force
make load-smoke
make load-baseline
make stats            # resource usage right after the run
make db-stats         # PostgreSQL connections, queries, size, cache hit rate
```

---

## Understanding the numbers

### VUs vs RPS vs concurrency

These three get confused constantly, and confusing them is how people end up
with benchmarks that claim to be "1000 concurrent" while running 100 sequential
loops.

| Term | Definition | Where it comes from |
| --- | --- | --- |
| **VUs** | Virtual users. k6 creates N of them. It is a count of *loops*, not of sockets. | `ramping-vus` executor; `vus` in the summary |
| **RPS** | Requests completed per second. | `http_reqs` rate in the summary |
| **Concurrency** | Requests in flight at one instant. | Little's Law: `concurrency ≈ RPS × latency` |

Why 100 VUs is not 1000 concurrent requests: with one sequential request per
iteration, 100 VUs can never have more than ~100 requests open - and fewer in
practice, because each VU spends most of its time in `sleep()`. To really
reach ~1000 in flight you need either far more VUs or parallel requests per
iteration. This project does the latter (`http.batch`, 10 per iteration), and
then **measures** the result instead of assuming it.

Two independent counters, which should agree:

```bash
# k6's own in-flight counter
jq '.metrics.http_req_waiting | {avg: .values.avg, max: .values.max}' \
   benchmarks/runs/<run>/summary.json

# the API's view
curl -s localhost:8080/metrics | grep http_requests_in_flight
```

- both around 1000 → the target was genuinely reached
- k6 high, API low → requests are queueing outside the app (TCP accept queue,
  or the k6 container is out of CPU)
- k6 low, API high → the app is the constraint, which is the interesting case

### Latency percentiles

| Percentile | Reading | What it usually means |
| --- | --- | --- |
| p50 | median | the typical experience under this load |
| p90 | 90% faster | the early tail; the first hint of contention |
| p95 | 95% faster | the usual SLA target |
| p99 | 99% faster | the worst 1%; what users actually complain about |
| max | the slowest single request | timeouts, GC pauses, pool waits, lock waits |

Compare p95 to p50:

- **p95 ≈ p50** → healthy. Requests are independent; the system is keeping up.
- **p95 ≫ p50** → requests are queueing behind a saturated resource. The gap
  *is* the bottleneck. Find it in `db_pool_wait_count_total`,
  `db_query_duration_seconds`, `go_gc_duration_seconds` and
  `docker stats`.
- **max is an outlier of an order of magnitude** → look for individual stalls:
  connection-pool waits, a checkpoint, autovacuum, or a GC pause.

### What to collect for each run

| Group | Metrics |
| --- | --- |
| Throughput | `http_reqs` rate (RPS), `iterations`, `vus` |
| Latency | `http_req_duration` p50/p90/p95/p99/max |
| Errors | `http_req_failed`, status distribution from `api_status_codes` |
| Concurrency | k6 `http_req_waiting`, API `http_requests_in_flight` |
| Network | `data_received`, `data_sent` |
| API CPU / RAM | `docker stats`, `process_cpu_seconds_total` |
| Postgres CPU / RAM | `docker stats`, `pg_stat_activity` |
| DB pool | `db_pool_connections_in_use`, `db_pool_wait_count_total` |
| Go runtime | `go_goroutines`, `go_gc_duration_seconds`, `process_resident_memory_bytes` |

The template for all of it is in
[`benchmarks/baseline/results.md`](benchmarks/baseline/results.md).

---

## Monitoring resources

### Containers

```bash
docker stats                                    # everything, live
make stats                                      # api + postgres, one-shot
make watch-stats                                # continuous, compact
make ps                                         # status + the applied CPU/memory limits
```

### PostgreSQL

```bash
make db-stats      # connections by state, running/waiting queries, DB size, cache hit ratio
make db-locks      # blocked queries (empty output = no lock contention)
make db-size       # per-table row counts and sizes
```

Or interactively:

```bash
make psql
```

Useful queries to keep in your back pocket:

```sql
-- who is connected right now
SELECT state, count(*) FROM pg_stat_activity
 WHERE datname = current_database() GROUP BY state;

-- the longest running query
SELECT pid, now() - query_start AS runtime, state, wait_event_type, wait_event, left(query, 120)
  FROM pg_stat_activity
 WHERE state <> 'idle' ORDER BY runtime DESC LIMIT 10;

-- total size of the biggest tables
SELECT relname, pg_size_pretty(pg_total_relation_size(relid))
  FROM pg_stat_user_tables ORDER BY pg_total_relation_size(relid) DESC;

-- cache hit ratio: how much of the traffic is actually served from RAM
SELECT sum(heap_blks_hit) * 100.0 / NULLIF(sum(heap_blks_hit) + sum(heap_blks_read), 0)
  FROM pg_statio_user_tables;
```

Anything slower than 2 s is logged by PostgreSQL itself, because the Compose
file sets `log_min_duration_statement=2000`:

```bash
docker compose logs postgres | grep duration
```

### Prometheus / Grafana

Deliberately **not** part of the baseline. `GET /metrics` is a plain
Prometheus endpoint, so if you want to scrape it:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: scaling-api
    static_configs:
      - targets: ['host.docker.internal:8080']
```

Do that as a side experiment, not as a prerequisite for measuring. The whole
point of the baseline is that `docker stats` plus `pg_stat_activity` plus a k6
summary are enough to find the first bottleneck. A Grafana stack is a Phase 7
concern, and adding it now would add noise to every measurement.

---

## Benchmark methodology

Every optimisation in this project follows the same loop. Skipping a step
produces a number nobody can trust.

```text
   BASELINE
      ↓
   MEASURE              make load-baseline, same machine, same seed
      ↓
   IDENTIFY BOTTLENECK  one component, with evidence
      ↓
   CHANGE ONE THING     exactly one
      ↓
   BENCHMARK AGAIN      identical profile, identical dataset
      ↓
   COMPARE              before / after / difference
      ↓
   KEEP / REVERT        and record why, including the trade-offs
      ↓
   NEXT OPTIMIZATION
```

Rules that make the numbers mean something:

1. **One change at a time.** Two changes and you cannot attribute the result.
2. **Same machine, same dataset, same profile.** Re-seed with
   `make seed-force`; the seeder is deterministic, so the data is identical.
3. **Change one configuration value, not a whole file.** Pool size, then
   `shared_buffers`, then an index - not all three.
4. **Record the commit** for every before and after.
5. **Keep the raw artefacts.** `summary.json`, `stats.tsv` and the `/metrics`
   snapshots; the prose summary is what you read, the artefacts are what you
   fall back on when someone asks "are you sure?".
6. **Watch run-to-run variance.** Run the baseline three times before trusting
   a 10% difference.
7. **Write the trade-offs.** Memory, write amplification, correctness risk,
   complexity, migration cost. And: what would make this decision wrong at 10x
   the data?

Every experiment uses the report template in
[`benchmarks/baseline/README.md`](benchmarks/baseline/README.md):

```text
Before   -> p95 = X ms, RPS = Y
Change   -> <one sentence, plus the file>
After    -> p95 = A ms, RPS = B
Difference -> the deltas, and which are outside noise
Explanation -> the mechanism, with evidence
Trade-offs  -> what got worse, and when this stops being a good idea
```

The numbers in that template are placeholders, not results. Results go in
`benchmarks/baseline/results.md` and only ever come from a real run.

---

## Deliberate omissions

Things a production system would have, that are **intentionally absent** from
this baseline. Each is a later phase, not a bug:

| Missing | Why |
| --- | --- |
| Redis / any cache | Phase 4. First we need to see how slow uncached reads actually are. |
| Message queue (RabbitMQ/Kafka) | Phase 8. |
| Read replicas | Phase 5. One writer is enough until we know reads dominate. |
| Sharding / partitioning | Phase 5. |
| Cursor pagination | Phase 2, once offset pagination's cost is measured. |
| Indexes beyond PK and `UNIQUE(email)` | Phase 2, one `EXPLAIN` at a time. |
| PostgreSQL performance tuning | `shared_buffers` is still 128 MB. Phase 5, after we know what is actually slow. |
| Multiple API replicas / load balancer | Phase 6. |
| Rate limiting, circuit breakers, retries | Phase 7. |
| Response compression | It would change `data_sent` and CPU cost, muddying every comparison. |
| Automated unit/integration tests | Out of scope for the baseline; `make load-smoke` is the correctness gate. See `benchmarks/baseline/README.md`. |
| Cursor-based connection pool (`pgxpool`) | `database/sql` is used on purpose so Phase 9 can compare it against raw `net/http` + `database/sql`. |

The roadmap for turning each of these into an experiment is in
[`ROADMAP.md`](ROADMAP.md).
