# Roadmap

What this project will explore, in order. **None of it is implemented yet.**
The baseline in `benchmarks/baseline/` is the reference every phase is measured
against.

The ordering is deliberate: each phase should answer the question the previous
one raised. Skipping ahead means guessing.

---

## How to use this roadmap

Every phase below follows the same discipline (see
[README § Benchmark methodology](README.md#benchmark-methodology)):

```text
BASELINE → MEASURE → IDENTIFY BOTTLENECK → CHANGE ONE THING
         → BENCHMARK AGAIN → COMPARE → KEEP/REVERT → NEXT
```

A phase is only "done" when there is a written experiment with before/after
numbers, an explanation with evidence, and an honest list of trade-offs. If an
optimisation does not measurably help, that is a valid result - and reverting
is a valid outcome.

---

## Phase 1 — Baseline CRUD system ✅

**Status: complete.** Deliberately simple, deliberately unoptimised, fully
reproducible.

- [x] Go + Fiber + PostgreSQL, three resources, full CRUD
- [x] offset pagination with a `count(*)`
- [x] embedded, deterministic migrations
- [x] deterministic seed of 100k / 50k / 500k rows
- [x] structured JSON logs with request correlation
- [x] Prometheus metrics: HTTP, Go runtime, connection pool, query latency
- [x] k6 smoke / baseline / stress profiles
- [x] recorded results in `benchmarks/baseline/results.md`
- [x] `make` targets that reproduce the whole thing

Deliberately left out so the baseline has real bottlenecks: caches, queues,
replicas, sharding, extra indexes, PostgreSQL tuning, horizontal scaling.

**The question this phase answers:** where is the time actually going?

---

## Phase 2 — PostgreSQL analysis

Start here next. The baseline is database-bound by design, so this is where
the first real wins are, and where the most transferable skills are.

- [ ] `EXPLAIN` every list/get query; read the plan, do not guess at it
- [ ] `EXPLAIN (ANALYZE, BUFFERS)` under load - planned vs actual rows,
      shared hit vs read
- [ ] index on `orders(user_id)` (the FK creates no index in PostgreSQL)
- [ ] composite index `(user_id, created_at DESC)` for per-user ordering
- [ ] decide whether the per-request `count(*)` is affordable; if not, make it
      optional, approximate, or move it behind a flag
- [ ] **cursor / keyset pagination** replacing `OFFSET`, and a measurement of
      how much deeper pages cost today
- [ ] identify the N+1 pattern if one is introduced (and resist it)
- [ ] `pg_stat_statements`: the top queries by total time, not by mean
- [ ] connection pool sizing: `DATABASE_MAX_OPEN_CONNECTIONS` vs PostgreSQL
      `max_connections` vs 1,000 concurrent requests
- [ ] `shared_buffers` and cache hit ratio - only after the above

**Key question:** is the bottleneck PostgreSQL's CPU, its I/O, or the number
of round trips?

---

## Phase 3 — Go optimisation

- [ ] allocation profile (`go test -benchmem`, `pprof`) - what allocates most?
- [ ] `encoding/json` vs a faster encoder - and whether it is worth the
      dependency and the readability cost
- [ ] response size: how many bytes go over the wire, and does that matter?
- [ ] heap profile: is anything retained that should not be?
- [ ] GC: `GOGC`, `GOMEMLIMIT`, allocation rate vs pause time from
      `go_gc_duration_seconds`
- [ ] goroutine count vs `GOMAXPROCS` vs the 2-CPU container limit
- [ ] CPU profile of the API under load
- [ ] `pprof` endpoints (debug-only build or loopback-only) and `go tool pprof`
- [ ] middleware cost: is the access logger or the metrics histogram showing up
      in the profile?

**Key question:** how much of the request time is in our code rather than in
PostgreSQL?

---

## Phase 4 — Redis

Only after the uncached read cost is known and recorded.

- [ ] where a cache would help, and where it provably would not
- [ ] cache-aside pattern
- [ ] TTL: what expiry is defensible for each resource?
- [ ] invalidation: write-through vs delete-on-write, and the staleness window
- [ ] hot keys: what happens when one key gets 50% of the traffic?
- [ ] cache stampede: 1,000 concurrent misses on the same cold key
- [ ] hit rate as a first-class metric; a cache you cannot measure is a cache
      you cannot tune
- [ ] the cost of Redis itself: memory, extra network hop, added failure mode

**Key question:** does caching beat optimising the query underneath it? Often
the honest answer is "no", and that is a useful result.

---

## Phase 5 — Database scaling

- [ ] read replicas: replication lag, read-your-writes, and routing
- [ ] `PgBouncer`: transaction pooling vs session pooling, and which queries
      break under transaction pooling (prepared statements, `LISTEN`)
- [ ] table partitioning by time, and whether the index alone made it moot
- [ ] PostgreSQL resource tuning with a measurement for each knob:
      `shared_buffers`, `work_mem`, `effective_cache_size`,
      `max_parallel_workers_per_gather`, checkpoint/WAL settings
- [ ] `EXPLAIN` again after each change
- [ ] connection storms: what happens when the pool churns after a restart?
- [ ] backups, restore time, and what data loss actually costs

**Key question:** what is the cheapest way to buy the next order of magnitude -
a bigger box, a replica, or a better query?

---

## Phase 6 — API scaling

- [ ] run 2+ API replicas; the connection pool multiplies with them - what
      happens to `max_connections`?
- [ ] a load balancer in front: least-connections vs round-robin, and how it
      changes the latency distribution
- [ ] prove the API is genuinely stateless (restart a replica mid-run and
      observe the effect)
- [ ] graceful shutdown: in-flight requests during a rolling deploy
- [ ] health checks that mean something (`/health` vs `/ready`)
- [ ] the container resource limits scaled per replica

**Key question:** does the application scale horizontally, or was the database
always the ceiling?

---

## Phase 7 — Reliability

Only meaningful once the happy path is understood.

- [ ] request timeouts at every layer, and what a timeout should return
- [ ] retries: where they help, where they amplify a failure (retry storms)
- [ ] retry with jitter and a budget, not a fixed sleep
- [ ] circuit breakers around a degraded dependency
- [ ] backpressure: what does the system do when it is asked for more than it
      can serve?
- [ ] rate limiting: token bucket vs leaky bucket, per-client vs global
- [ ] graceful degradation: which endpoints stop working first, and should
      they?
- [ ] chaos experiments: kill PostgreSQL, kill Redis, add 200 ms of latency
- [ ] SLOs and error budgets, and how they would have changed the earlier
      decisions

**Key question:** what does failure look like, and is it survivable?

---

## Phase 8 — Advanced architecture

- [ ] when an outbox is genuinely required, and the cost of dual writes
- [ ] queues: throughput, ordering, delivery guarantees, and dead-letter
      handling
- [ ] asynchronous request processing: which endpoints can return 202?
- [ ] event-driven architecture and the operational cost of tracing a request
      across services
- [ ] eventual consistency: what the user sees, and for how long
- [ ] saga patterns for multi-step writes

**Key question:** what complexity is worth it, and what should stay
synchronous?

---

## Phase 9 — Raw Go

Strip away the framework and see what it was actually costing.

- [ ] rewrite the hot path with `net/http` + `database/sql`
- [ ] compare: cold-start memory, throughput, p99, binary size, build time
- [ ] how much of the baseline latency is Fiber, and how much is PostgreSQL?
- [ ] what does Fiber buy that would be painful to rebuild: routing, body
      parsing, graceful shutdown, static files, the ecosystem?
- [ ] `sqlx` / `pgx` native types vs `database/sql` scanning
- [ ] prepared statement reuse and `QueryRow` overhead

**Key question:** is the framework earning its keep at this scale, or only at
ten times it?

---

## Phase 10 — Extreme load

- [ ] push until it breaks, then find out *which* limit broke first
- [ ] orders of magnitude: 1M / 10M / 100M orders - which optimisations
      stop scaling? (`OFFSET` certainly does; sequential ids are the enemy)
- [ ] the shapes of failure: gradual degradation, cliff edge, or oscillation?
- [ ] recovery: how long after the load stops before the system is healthy?
- [ ] cost per million requests, not just requests per second
- [ ] re-measure the whole roadmap's assumptions at the new scale

**Key question:** which of the earlier wins still hold, and which were specific
to a dataset that fit comfortably in RAM?

---

## Cross-cutting concerns

These apply to every phase rather than to one:

- [ ] **Variance.** Run the baseline 3x before trusting a 10% difference.
- [ ] **One change at a time.** Otherwise attribution is impossible.
- [ ] **Record the trade-offs**, not just the win. Memory, write amplification,
      correctness risk, complexity, migration cost.
- [ ] **Watch for regressions** on the metrics that were not targeted. A 40%
      p99 win that doubles memory usage may be a bad trade.
- [ ] **Keep the baseline runnable.** The whole project is worthless if the
      original cannot be re-measured. `git checkout` the baseline commit and
      run `make up migrate seed load-baseline`.

---

## Anti-goals

Things this project will deliberately not do, even when they seem attractive:

- ❌ Optimising before measuring
- ❌ Changing more than one thing per experiment
- ❌ Hard-coding benchmark numbers
- ❌ Adding a dependency to avoid writing twenty lines of clear code
- ❌ "Best practice" without a measurement attached to it
- ❌ Declaring victory on a single run
