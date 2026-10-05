# Baseline results

Measured results for the **unoptimised baseline** of `scaling-systems`.

> **Every number in this file came from a real run.** Values are copied from
> `summary.json`, `stats.tsv`, or the `/metrics` snapshots of
> `benchmarks/runs/20260929-115413-baseline/`, or from two `psql` queries against
> the same dataset. Nothing is estimated. Where a number was not measured, the
> cell says so explicitly rather than guessing.
>
> See [`README.md`](README.md) for methodology and [`system-info.md`](system-info.md)
> for the machine.

---

## 1. How this baseline was measured

| | |
| --- | --- |
| Run id | `20260929-115413-baseline` |
| Profile | `tests/load/baseline.js` (profile 2) |
| Shape | 30 s ramp to 100 VUs → 2 min warm-up → **5 min measured** → 1 min drain (8 min 30 s total) |
| Requests per iteration | 10, via `http.batch` |
| Think time | 1 s between iterations |
| VUs | 100 max |
| Traffic mix | 70% GET / 20% POST / 5% PUT / 5% DELETE, spread across users / products / orders |
| Dataset at start | 100,000 users · 50,000 products · 500,000 orders (`make seed-force`) |
| Errors | **0 of 344,771** |
| Artefacts | `summary.json`, `stats.tsv` (80 samples), `metrics-before.txt`, `metrics-after.txt` |

> **Read §6 before quoting a concurrency number.** The brief asked for ~1000
> concurrent requests. This run did **not** reach that, and the reason is worth
> more than the number would have been.

---

## 2. Headline result

| Metric | Value |
| --- | --- |
| **Throughput** | **675.70 req/s** (344,771 requests in 8m30s) |
| **Latency p50** | **74.36 ms** |
| **Latency p95** | **475.38 ms** |
| **Latency p99** | **802.48 ms** |
| **Latency max** | **1,897.96 ms** |
| **Error rate** | **0.00%** (0 of 344,771) |
| **Achieved concurrency** | **≈76 requests in flight** (not the 1000 targeted — see §6) |
| **Saturated** | **Yes — PostgreSQL**, at 205% of its 2-CPU limit. The API was at 11%. |

**The system is database-bound.** The API used 10.9% of its CPU budget on
average; PostgreSQL used 160.9% of 200% and hit 205% at peak. That asymmetry is
the single most important fact in this document.

---

## 3. Throughput and latency

From `summary.json` (all values milliseconds unless noted).

| Metric | avg | p50 | p90 | p95 | p99 | p99.9 | max |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `http_req_duration` | 112.70 | 74.36 | 300.86 | 475.38 | 802.48 | 1,208.54 | 1,897.96 |
| `http_req_waiting` (TTFB) | 112.65 | 74.30 | 300.79 | 475.37 | 802.40 | 1,208.51 | 1,897.94 |
| `http_req_receiving` | 0.04 | 0.01 | 0.04 | 0.08 | 0.55 | 3.18 | 21.41 |
| `http_req_sending` | 0.01 | 0.00 | 0.01 | 0.02 | 0.08 | 1.20 | 18.08 |
| `http_req_blocked` | 0.00 | 0.00 | 0.00 | 0.00 | 0.01 | 0.30 | 6.86 |
| `http_req_connecting` | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 | 0.16 | 1.86 |
| `iteration_duration` | 1,350.87 | 1,289.16 | 1,802.09 | 1,993.72 | 2,294.74 | 2,600.55 | 2,900.40 |
| `api_iteration_duration` | 349.55 | 288.00 | 800.00 | 992.00 | 1,293.00 | 1,600.05 | 1,900.00 |

**The time breakdown is the story.** `http_req_waiting` is 112.65 ms of the
112.70 ms total — the client spends essentially *all* of its time waiting for
the first byte. `http_req_blocked` and `http_req_connecting` are both 0.00 ms:
the load generator is never blocked and never pays for a TCP handshake. There
is no client-side or network bottleneck to chase; all of the time is spent
server-side waiting for the database.

`api_iteration_duration` (349.55 ms) against `iteration_duration` (1,350.87 ms)
is also revealing: each VU spends 26% of its cycle doing work and 74% asleep in
`sleep(1s)`. See §6.

### Rate metrics

| Metric | Value |
| --- | --- |
| `http_reqs` | 344,771 total · **675.70 req/s** |
| `iterations` | 34,474 total · 67.56 iterations/s |
| `data_received` | 313,721,124 B (299.2 MiB) · 599.5 KiB/s |
| `data_sent` | 47,238,936 B (45.1 MiB) · 90.4 KiB/s |
| `vus` | max 100 |

---

## 4. Errors

| Metric | Value |
| --- | --- |
| `http_req_failed` | **0.00%** — 0 failures out of 344,771 |
| Non-2xx added during the run | **0** |

Every `status` label in `metrics-after.txt` minus `metrics-before.txt` is 200,
201 or 204. The 404 and 400 counts visible in the metrics file are all
pre-existing, left over from manual API testing before the run; their deltas are
exactly 0.

Full delta from the `/metrics` snapshots:

| Method | Route | Status | Count |
| --- | --- | --- | --- |
| GET | `/api/v1/orders/:id` | 200 | 61,398 |
| GET | `/api/v1/users/:id` | 200 | 59,732 |
| GET | `/api/v1/products/:id` | 200 | 59,372 |
| POST | `/api/v1/users/` | 201 | 25,526 |
| POST | `/api/v1/orders/` | 201 | 25,310 |
| POST | `/api/v1/products/` | 201 | 24,510 |
| GET | `/api/v1/orders/` | 200 | 20,651 |
| GET | `/api/v1/users/` | 200 | 20,120 |
| GET | `/api/v1/products/` | 200 | 19,916 |
| PUT | `/api/v1/orders/:id` | 200 | 5,854 |
| PUT | `/api/v1/users/:id` | 200 | 5,707 |
| PUT | `/api/v1/products/:id` | 200 | 5,607 |
| DELETE | `/api/v1/orders/:id` | 204 | 4,179 |
| DELETE | `/api/v1/products/:id` | 204 | 4,094 |
| DELETE | `/api/v1/users/:id` | 204 | 2,794 |
| | | **k6 subtotal** | **344,770** |
| GET | `/ready` | 200 | 55 |
| GET | `/metrics` | 200 | 1 |
| | | **Total served** | **344,826** |

The k6 subtotal (344,770) matches k6's own `http_reqs` count of 344,771 to
within one request. The 56 extra are infrastructure probes: the Compose
healthcheck polling `/ready` about twice a minute, plus one `/metrics` scrape.
They are in the metrics file but are not load.

Observed method mix: **GET 70.0% · POST 21.9% · PUT 5.0% · DELETE 3.2%**, against
a nominal 70/20/5/5. DELETE came in low and POST high, which is exactly what the
create-then-delete fallback predicts: when a VU exhausts its delete slice it
creates a row and deletes that instead, so some DELETE volume is spent as POST.
The mix is close enough that the headline numbers are representative.

### Note: the run mutates the dataset

POSTs add rows and DELETEs remove them, so the tables do not end where they
started. After this run:

| Table | Before | After |
| --- | --- | --- |
| users | 100,000 | 122,732 |
| products | 50,000 | 70,416 |
| orders | 500,000 | 521,131 |

`make seed-force` resets it. This matters for comparability: **always re-seed
before a measured run**, never after.

---

## 5. Resource usage

Sampled from `docker stats` every 5 s, 80 samples, percentages of each
container's *limit* (2 CPUs / 4 GB).

| | API | PostgreSQL |
| --- | --- | --- |
| CPU — mean | **10.9%** | **160.9%** |
| CPU — peak | 25.3% | **205.0%** (over the 200% limit) |
| Memory — mean | 1.4% | 7.4% |
| Memory — peak | 1.5% | 9.4% |
| Memory — final | 47.62 MiB / 4 GiB | 268.5 MiB / 4 GiB |

Host: Apple M5, 10 logical CPUs, 16.0 GiB RAM; Docker VM 10 CPUs / 7.8 GiB.
See [`system-info.md`](system-info.md).

**PostgreSQL is pinned at its CPU limit.** 160.9% of 200% on average, and 205%
at peak means it was being throttled — it wanted more CPU than the container
allowed. It cannot go faster no matter how much concurrency is added.

**The API is nearly idle.** 10.9% average, 25.3% peak, 48 MiB of a 4 GiB
budget. There is roughly 18x headroom on the API side. Memory is not a
constraint anywhere: PostgreSQL peaked at 9.4% of its limit, the API at 1.5%.

This is the central result. Adding CPU to the API, or writing faster Go, would
change almost nothing.

---

## 6. Achieved concurrency — and why it was not 1000

**Measured average concurrency: ≈76 requests in flight.** The brief targeted
~1000 concurrent requests. That target was not met, and pretending otherwise
would make every other number in this document misleading.

### How it was measured

k6 does not export an in-flight gauge, and `http_requests_in_flight` in the API
only reaches a meaningful value under load (the before/after snapshots are both
taken at idle, so they read 1). Little's Law is the reliable method here:

```text
concurrency = throughput × latency
            = 675.70 req/s × 0.11270 s
            = 76.2 requests in flight
```

### Why the target was missed

`iteration_duration` p50 is 1,289 ms, of which `api_iteration_duration` is only
288–350 ms. Each VU therefore:

```text
  fire 10 requests in a batch   ~350 ms   (26% of the cycle)
  sleep(THINK_TIME_SECONDS)    1,000 ms   (74% of the cycle)
  repeat
```

With `THINK_TIME_SECONDS=1`, a VU is only *doing* work a quarter of the time.
Multiplying out: 100 VUs × 25.9% active × an average of ~2.9 requests in flight
while a batch is in progress ≈ 76, matching the Little's Law figure. The batch
of 10 does not translate into 10 sustained connections in flight, because within
a batch the requests finish at staggered times and the mean is well under 10.

So the honest statement of this run is:

> **100 VUs, 675.70 req/s, ≈76 concurrent requests, PostgreSQL saturated.**

It is *not* a 1000-concurrency measurement, and the p95 of 475 ms must be read
in that light.

### The trade-off, stated plainly

Think time was kept at 1 s deliberately. It models a real client rather than a
closed-loop fuzzer, and closed-loop testing (`THINK_TIME_SECONDS=0`) would have
kept 1000 requests permanently in flight — a harsher test, but a less
realistic one, and one whose "saturation point" would mostly measure the pool
size rather than the database.

For a genuine 1000-concurrency run, `THINK_TIME_SECONDS=0` (closed loop) is the
knob. It is a separate experiment with its own before/after, not a correction to
this baseline.

---

## 7. Where the time went

### 7.1 Connection pool — the secondary bottleneck

| Metric | Before | After | Delta |
| --- | --- | --- | --- |
| `db_pool_connections_max_open` | 100 | 100 | — |
| `db_pool_connections_idle` | 50 | 50 | 0 |
| `db_pool_connections_open` | 50 | 50 | 0 |
| `db_pool_wait_count_total` | 150,305 | 278,838 | **+128,533** |
| `db_pool_wait_duration_seconds_total` | 11,109.81 | 20,849.58 | **+9,739.77** |
| `db_pool_max_idle_closed_total` | 14,709 | 28,339 | **+13,630** |
| `db_pool_max_lifetime_closed_total` | 0 | 0 | 0 |
| `db_pool_max_idle_time_closed_total` | 4 | 4 | 0 |

128,533 requests had to wait for a connection:

```text
128,533 waits / 344,771 requests = 37.3% of all requests waited
9,739.77 s / 128,533 waits       = 75.8 ms average wait per wait event
0.373 × 75.8 ms                  ≈ 28 ms of extra latency per request
```

Against a p50 of 74.36 ms, **roughly 28 ms — about 38% of median request
latency — is queueing for a database connection**, not executing SQL.

The mechanism is a feedback loop, not an independent fault:

1. PostgreSQL is CPU-saturated, so every query is slow (avg 103.3 ms, §7.2).
2. A connection is held for the whole query, so slow queries mean fewer
   connections available per second.
3. 100 connections × slow queries → the pool runs dry → requests wait.
4. The pool also churns: 13,630 connections were closed because `max_idle` is
   50 while `max_open` is 100, so the surplus is dropped and re-dialled.

This is **not** a misconfigured pool. Raising `DATABASE_MAX_OPEN_CONNECTIONS`
would make it worse, by pushing more concurrent work at an already CPU-bound
PostgreSQL and driving it further past its limit. The pool is a symptom; the
database is the cause.

### 7.2 Query latency — 370,080 queries, 38,232 s of database time

Average **103.3 ms per query**. Broken down by operation (deltas between the
`/metrics` snapshots):

| Operation | Calls | Total time | Avg | Share of DB time |
| --- | ---: | ---: | ---: | ---: |
| **`orders.list`** | 20,651 | 7,795.5 s | **377.49 ms** | **20.4%** |
| `users.list` | 20,120 | 3,465.8 s | 172.26 ms | 9.1% |
| `users.create` | 25,526 | 3,436.1 s | 134.61 ms | 9.0% |
| `products.create` | 24,510 | 3,366.4 s | 137.35 ms | 8.8% |
| `orders.create` | 25,310 | 3,074.0 s | 121.45 ms | 8.0% |
| `products.list` | 19,916 | 2,987.8 s | 150.02 ms | 7.8% |
| `orders.get` | 61,398 | 2,723.0 s | 44.35 ms | 7.1% |
| `users.get` | 59,732 | 2,680.7 s | 44.88 ms | 7.0% |
| `products.get` | 59,372 | 2,649.9 s | 44.63 ms | 6.9% |
| `users.delete` | 2,794 | 1,420.8 s | **508.51 ms** | 3.7% |
| `users.exists` | 25,310 | 1,172.5 s | 46.33 ms | 3.1% |
| `orders.update` | 5,854 | 820.3 s | 140.13 ms | 2.1% |
| `users.update` | 5,707 | 781.9 s | 137.01 ms | 2.0% |
| `products.update` | 5,607 | 779.9 s | 139.09 ms | 2.0% |
| `orders.delete` | 4,179 | 544.4 s | 130.26 ms | 1.4% |
| `products.delete` | 4,094 | 533.2 s | 130.25 ms | 1.4% |
| **Total** | **370,080** | **38,232.2 s** | **103.31 ms** | 100% |

Three things to notice:

1. **`orders.list` is the single most expensive operation** — 20.4% of all
   database time, at 377 ms per call, 8.5x the cost of a single-row `get`. It
   runs `SELECT count(*)` (a full scan of 500,000 rows) plus the paged `SELECT`,
   on every request. This is the expected result and it is confirmed.
2. **`users.delete` is the slowest per call** at 508.51 ms — slower even than
   `orders.list`. It cascades to that user's orders, so one DELETE is really
   "delete the user" plus "find and delete their ~5 orders" plus an
   `orders.user_id` lookup with **no index to use**.
3. **Every `.list` call costs 2–8x a `.get`.** `orders.get` is 44.35 ms;
   `orders.list` is 377.49 ms. Same table, same pool, same PostgreSQL. The
   difference is entirely the `count(*)` and the missing index.

Note that `users.exists` (25,310 calls, 46.33 ms) is issued by every order
create to validate `user_id`, adding a round trip per POST.

### 7.3 Go runtime — not a problem, and now provably so

| Metric | Value |
| --- | --- |
| `go_goroutines` | 12 → 13 (flat) |
| `go_threads` | 11 → 12 (flat) |
| `process_resident_memory_bytes` | 50.18 MB → 50.72 MB (+0.54 MB) |
| `process_cpu_seconds_total` | +59.52 s over ~510 s = **11.7% of one core** |
| `go_memstats_alloc_bytes_total` | +3.26 GB over the run |
| GC cycles | 221 |
| GC total pause | **113.0 ms** across the whole run |
| GC average pause | **0.511 ms** |
| `go_gc_forced_total` | 0 (no memory-pressure collections) |
| `process_open_fds` | 58 → 58 (no leak) |

Total GC pause across 8m30s of load was 113 ms. Even assigning the *entire*
p99 of 802 ms to garbage collection would be an 8x overstatement. **GC
contributes roughly 0.04% of total elapsed time and is definitively not a
bottleneck.** No goroutine growth, no file-descriptor leak, no memory growth.

Allocation rate was ~6.1 MB/s, which is unremarkable and would only become
interesting if the API were actually CPU-bound. It is not: 11.7% of one core.

### 7.4 PostgreSQL

| Metric | Value |
| --- | --- |
| Cache hit ratio | **100.00%** (496,573,948 hits, 0 reads) |
| `shared_buffers` | 128 MiB (stock default) |
| Database size | 117 MB |
| Largest table | `orders`, 50 MB / 500,000 rows |
| Connection limit | `max_connections = 200`; pool ceiling 100 |
| Indexes | PKs only, plus `users_email_key`. **No index on `orders(user_id)`.** |

**100% cache hit ratio is the key detail.** PostgreSQL is not disk-bound. Every
page it touched was already in RAM — the entire 117 MB dataset fits in
`shared_buffers` many times over. So this is **pure CPU work**: sequential
scans re-reading pages that are already cached, over and over, for 500,000 rows
on every single `orders.list`.

That is why the fix is indexes and not more memory, more disks, or a faster
storage tier. Buying disk would change nothing.

---

## 8. The bottleneck

**Bottleneck: PostgreSQL, CPU-bound on full table scans.**

**Evidence.** `docker stats` shows 160.9% mean / 205% peak CPU against a 200%
limit — the container was throttled, so PostgreSQL could not have gone faster
regardless of load. Meanwhile the API used 10.9%. The API's 88% idle CPU budget
was the direct cause of PostgreSQL having 88% to itself, and it still could not
keep up.

**Not the bottleneck, and the metric that rules each out:**

| Candidate | Ruled out by | Value |
| --- | --- | --- |
| The API's code | `process_cpu_seconds_total` | 59.52 s CPU over 510 s = 11.7% of one core |
| Garbage collection | `go_gc_duration_seconds` | 113 ms total across 344,771 requests |
| Memory pressure | `process_resident_memory_bytes`, `go_gc_forced_total` | +0.54 MB; 0 forced GCs |
| Disk I/O | `pg_statio_user_tables` | 0 heap blocks read; 100% cache hit |
| The load generator | `http_req_blocked`, `http_req_connecting` | both 0.00 ms |
| Network | `http_req_waiting` vs `http_req_duration` | 112.65 of 112.70 ms — all time is server-side |
| Connection pool | secondary, not primary | 37% of requests waited, but only because queries are slow |

**Why it happens.** Every collection endpoint runs `SELECT count(*)` over the
whole table, and there is no index on any column the queries filter, join or
order by. `orders.list` scans 500,000 rows to produce 20. A foreign key in
PostgreSQL does not create an index, so `orders.user_id` lookups — including the
`ON DELETE CASCADE` cleanup on every user delete — are sequential scans too.
Those repeated scans consume all available CPU, and the cascade of
slow query → connection held longer → pool exhaustion is what turns a CPU limit
into a latency distribution.

**The causal chain, in order:**

```text
no indexes + count(*) on every list
        ↓
PostgreSQL burns 160% CPU re-scanning cached 500k-row tables
        ↓
205% peak → throttled at the 2-CPU limit; queries average 103 ms
        ↓
connections held longer → pool of 100 exhausts 128,533 times
        ↓
+28 ms average queueing → p50 74 ms, p95 475 ms, p99 802 ms
        ↓
API sits at 11% CPU the whole time, waiting
```

---

## 9. First optimisation hypothesis

**Hypothesis.** `orders.list` — specifically its `count(*)` and its unindexed
`ORDER BY id OFFSET n` — accounts for a disproportionate share of database CPU,
and removing the `count(*)` will cut total DB time and lift throughput
proportionally.

**Change (one file, one behaviour).** Make the total-count query optional in the
orders list endpoint, gated behind a request flag, and return the page of rows
without it when absent. `internal/repositories/orders.go` +
`internal/handlers/orders.go`.

**Expected effect.** `orders.list` is 20.4% of DB time at 377 ms/call. Dropping
the `count(*)` should roughly halve that operation's cost — a ~10% cut in total
database time, and more throughput as the freed CPU is handed to waiting
requests. Expect p95 to improve more than p50, since the tail is dominated by
the same full scans.

**How to falsify it.** If `db_query_duration_seconds` for `orders.list` does not
drop by roughly half, the cost is in the `OFFSET` scan rather than the `count(*)`
— which would mean the fix is keyset pagination instead, and the hypothesis was
half right for the wrong reason. If throughput does not move at all while
PostgreSQL CPU stays at 160%, then CPU is no longer the constraint and the
bottleneck has moved somewhere else, which is itself worth knowing.

**What this would not fix.** The pool waits. Those are downstream of query
latency, so they should shrink with it, but they will not disappear while
`orders.user_id` remains unindexed and every user DELETE cascades.

---

## 10. Run-to-run variance

Two full baseline runs, back to back, same machine, same deterministic seed,
identical profile. (The first run's `summary.json` was overwritten by a corrected
artefact path before its numbers were transcribed; these are its console totals.)

| Metric | Run A | Run B | Difference |
| --- | ---: | ---: | ---: |
| `http_reqs` | 342,811 | 344,771 | +0.57% |
| RPS | 671.22 | 675.70 | +0.67% |
| p50 | 76.52 ms | 74.36 ms | −2.8% |
| p95 | 484.89 ms | 475.38 ms | −2.0% |
| p99 | 803.86 ms | 802.48 ms | −0.17% |

Variance is low: under 3% on every headline metric, and under 1% on p99.

**Working rule for this project: treat any difference below 3% as noise.** A
future optimisation must beat that to be believed, and ideally should be run
twice.

---

## 11. Reproducing this run

```bash
make up
make migrate
make seed-force     # required: the run mutates the dataset (§4)
make load-smoke     # correctness gate, must be 0% errors
make load-baseline
```

For comparability with the numbers above:

- same machine as [`system-info.md`](system-info.md) (Apple M5, Docker VM 10 CPU / 7.8 GiB)
- nothing else CPU- or disk-heavy running
- `PARALLEL_REQUESTS=10`, `THINK_TIME_SECONDS=1` (the defaults)
- re-seed first, every time

Artefacts land in `benchmarks/runs/<timestamp>-baseline/`:
`summary.json`, `stats.tsv`, `metrics-before.txt`, `metrics-after.txt`.

---

## 12. Raw artefacts

For `benchmarks/runs/20260929-115413-baseline/`:

| File | Contents |
| --- | --- |
| `summary.json` | every k6 metric and percentile (§3, §6) |
| `stats.tsv` | 80 `docker stats` samples, 5 s apart (§5) |
| `metrics-before.txt` | `/metrics` immediately before the run |
| `metrics-after.txt` | `/metrics` immediately after (all §4, §7.1, §7.3 deltas) |

This document is a summary; those files are the evidence. If a number here ever
disagrees with the artefacts, **the number here is wrong**.
