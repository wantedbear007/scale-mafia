# Benchmark environment

Collected: `2026-09-29T06:33:13Z` (UTC)

## Project

| Item | Value |
| --- | --- |
| Git commit | _(no commits yet)_ |
| Git branch | _(unborn)_ |
| Working tree | dirty |

## Host

| Item | Value |
| --- | --- |
| OS | Darwin 27.0.0 arm64 |
| CPU model | Apple M5 |
| Host logical CPUs | 10 |
| Host RAM | 16.0 GiB |
| Container runtime | 29.4.0 (linux/arm64) |
| Docker Compose | 5.1.2 |

## Docker VM (what the containers actually get)

| Item | Value |
| --- | --- |
| Docker VM CPUs | 10 |
| Docker VM memory | 7.8 GiB |

## Toolchain

| Item | Value |
| --- | --- |
| Go (host) | go version go1.27.0 darwin/arm64 |
| Go (in image) | scaling-systems-api dev (linux/arm64, go1.25.14) |
| PostgreSQL | postgres (PostgreSQL) 18.6 (Debian 18.6-1.pgdg13+2) |
| k6 | k6 v2.3.0 (commit/e088784614, go1.27.1, linux/arm64)  |
| API image | scaling-systems-api:baseline (40.9MB) |

## Container resource limits

| Service | CPUs | Memory | Source |
| --- | --- | --- | --- |
| scaling-api | 2 | 4096 MiB | declared (reservations: 0.5 CPU / 256 MiB) |
| scaling-postgres | 2 | 4096 MiB | declared (reservations: 0.5 CPU / 512 MiB) |
| scaling-k6 | 2 | 2048 MiB | declared (reservations: 0.5 CPU / 256 MiB) |

Live check (only for containers running right now):

- `scaling-api`: 2 CPUs, 4096 MiB
- `scaling-postgres`: 2 CPUs, 4096 MiB

## PostgreSQL settings in effect

```
checkpoint_completion_target = 0.9 = 
effective_cache_size = 524288 = 8kB
fsync = on = 
maintenance_work_mem = 65536 = kB
max_connections = 200 = 
max_parallel_workers_per_gather = 2 = 
max_worker_processes = 8 = 
random_page_cost = 4 = 
shared_buffers = 16384 = 8kB
synchronous_commit = on = 
wal_buffers = 512 = 8kB
wal_level = replica = 
work_mem = 4096 = kB
```

## Dataset size

| Table | Rows | Total size |
| --- | --- | --- |
orders | 521131 | 53 MB
products | 70416 | 13 MB
users | 122732 | 31 MB

## Indexes actually present

```
orders: orders_pkey
products: products_pkey
schema_migrations: schema_migrations_pkey
users: users_email_key
users: users_pkey
```
