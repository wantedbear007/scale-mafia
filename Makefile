# ==============================================================================
# scaling-systems - baseline benchmark workflow
#
# The whole point of this file is reproducibility. If a benchmark cannot be
# reproduced with a handful of commands, its numbers are worthless.
#
#   make up && make migrate && make seed && make load-baseline
#
# ==============================================================================

SHELL := /bin/bash
.DEFAULT_GOAL := help

COMPOSE      := docker compose
API          := $(COMPOSE) exec -T api
K6           := $(COMPOSE) --profile load run --rm
RESULTS_DIR  := benchmarks/runs
HOST         := http://localhost:$(or $(API_HOST_PORT),8080)

# Keep the stack description in one place for reproducibility.
export APP_VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# --------------------------------------------------------------------------
# build / run
# --------------------------------------------------------------------------
.PHONY: build
build: ## Build the API image
	$(COMPOSE) build

.PHONY: up
up: ## Start postgres + api (k6 is opt-in, see load-* targets)
	$(COMPOSE) up -d postgres api
	@echo "waiting for the API to become ready..."
	@until curl -fsS $(HOST)/ready >/dev/null 2>&1; do sleep 1; done
	@echo "api ready at $(HOST)"

.PHONY: down
down: ## Stop the stack (keeps the database volume)
	$(COMPOSE) --profile load down

.PHONY: down-all
down-all: ## Stop the stack AND delete the database volume
	$(COMPOSE) --profile load down -v

.PHONY: restart
restart: down up ## Recreate the stack

.PHONY: logs
logs: ## Tail API logs
	$(COMPOSE) logs -f api

.PHONY: ps
ps: ## Show container status and applied resource limits
	@$(COMPOSE) ps
	@echo
	@docker inspect scaling-api scaling-postgres \
		--format '{{.Name}}  cpus={{.HostConfig.NanoCpus}}  memory={{.HostConfig.Memory}}' 2>/dev/null || true

.PHONY: shell
shell: ## Open a shell in the API container (distroless: no shell, so this runs psql on postgres)
	$(COMPOSE) exec postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling}

# --------------------------------------------------------------------------
# database
# --------------------------------------------------------------------------
.PHONY: migrate
migrate: ## Apply migrations (idempotent, safe to re-run)
	$(API) /app/migrate up

.PHONY: migrate-status
migrate-status: ## Show applied / pending migrations
	$(API) /app/migrate status

.PHONY: seed
seed: ## Seed the benchmark dataset (100k users, 50k products, 500k orders)
	$(API) /app/migrate seed

.PHONY: seed-force
seed-force: ## TRUNCATE everything, re-seed, and re-apply migrations
	$(API) /app/migrate seed -force

.PHONY: db-reset
db-reset: ## Drop all tables, re-apply migrations and re-seed
	$(API) /app/migrate reset
	$(API) /app/migrate seed

.PHONY: psql
psql: ## Open a psql session on the benchmark database
	$(COMPOSE) exec postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling}

.PHONY: db-size
db-size: ## Show table sizes and row counts
	@$(COMPOSE) exec -T postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling} -c \
		"SELECT relname AS table, n_live_tup AS rows, pg_size_pretty(pg_total_relation_size(relid)) AS total_size \
		 FROM pg_stat_user_tables ORDER BY pg_total_relation_size(relid) DESC;"

# --------------------------------------------------------------------------
# tests
# --------------------------------------------------------------------------
# NOTE: automated unit/integration tests are intentionally NOT part of the
# baseline phase - the benchmark is the deliverable here. The correctness
# signal for this phase is `make load-smoke` plus the manual checks listed in
# benchmarks/baseline/README.md. These targets exist so the workflow is
# obvious, and they will be filled in later.
.PHONY: test
test: ## Placeholder: no automated tests in the baseline phase
	@echo "No automated tests in the baseline phase. Use 'make load-smoke' for the correctness gate."
	@echo "See benchmarks/baseline/README.md for the manual verification checklist."

.PHONY: fmt
fmt: ## gofmt the tree
	gofmt -l -w .

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: lint
lint: vet ## vet + gofmt check
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@echo "lint clean"

# --------------------------------------------------------------------------
# load testing
# --------------------------------------------------------------------------
# Knobs (all optional):
#   K6_PARALLEL=10     concurrent requests per k6 iteration
#   K6_THINK=1         seconds of think time between iterations (0 = none)
#   K6_OUT=smoke       name of the folder under benchmarks/runs/ for artefacts
K6_PARALLEL ?= 10
K6_THINK    ?= 1
K6_OUT      ?=

.PHONY: load-smoke
load-smoke: export K6_RUN_ID := $(if $(K6_OUT),$(K6_OUT),$(shell date +%Y%m%d-%H%M%S)-smoke)
load-smoke: ## k6 profile 1: smoke (10 VUs, 1 minute)
	@mkdir -p $(RESULTS_DIR)/$(K6_RUN_ID)
	$(K6) -e BASE_URL=http://api:8080 \
	      -e K6_PROFILE=smoke \
	      -e PARALLEL_REQUESTS=$(K6_PARALLEL) \
	      -e THINK_TIME_SECONDS=$(K6_THINK) \
	      -e K6_SUMMARY_EXPORT=/results/runs/$(K6_RUN_ID)/summary.json \
	      k6 run /scripts/smoke.js

.PHONY: load-baseline
load-baseline: export K6_RUN_ID := $(if $(K6_OUT),$(K6_OUT),$(shell date +%Y%m%d-%H%M%S)-baseline)
load-baseline: ## k6 profile 2: baseline (100 VUs, 5 min load, 2 min warm-up)
	@mkdir -p $(RESULTS_DIR)/$(K6_RUN_ID)
	$(K6) -e BASE_URL=http://api:8080 \
	      -e K6_PROFILE=baseline \
	      -e PARALLEL_REQUESTS=$(K6_PARALLEL) \
	      -e THINK_TIME_SECONDS=$(K6_THINK) \
	      -e K6_SUMMARY_EXPORT=/results/runs/$(K6_RUN_ID)/summary.json \
	      k6 run /scripts/baseline.js

.PHONY: load-stress
load-stress: export K6_RUN_ID := $(if $(K6_OUT),$(K6_OUT),$(shell date +%Y%m%d-%H%M%S)-stress)
load-stress: ## k6 profile 3: stress (100 -> 200 -> 300 -> 500 VUs)
	@mkdir -p $(RESULTS_DIR)/$(K6_RUN_ID)
	$(K6) -e BASE_URL=http://api:8080 \
	      -e K6_PROFILE=stress \
	      -e PARALLEL_REQUESTS=$(K6_PARALLEL) \
	      -e THINK_TIME_SECONDS=$(K6_THINK) \
	      -e K6_SUMMARY_EXPORT=/results/runs/$(K6_RUN_ID)/summary.json \
	      k6 run /scripts/stress.js

# --------------------------------------------------------------------------
# observation
# --------------------------------------------------------------------------
.PHONY: stats
stats: ## Live CPU / memory / network / block IO for api and postgres
	@echo "container  cpu%  mem%  mem_usage/limit  net_rx/tx  block_read/write"
	@docker stats --no-stream --format \
		"{{.Name}}  {{.CPUPerc}}  {{.MemPerc}}  {{.MemUsage}}  {{.NetIO}}  {{.BlockIO}}" \
		scaling-api scaling-postgres

.PHONY: watch-stats
watch-stats: ## Continuously refresh container stats
	docker stats --format \
		"{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}\t{{.BlockIO}}\t{{.PIDs}}" \
		scaling-api scaling-postgres

.PHONY: db-stats
db-stats: ## PostgreSQL activity: connections, queries, size, cache hit rate
	@$(COMPOSE) exec -T postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling} -c \
		"SELECT state, count(*) AS connections FROM pg_stat_activity WHERE datname = current_database() GROUP BY state ORDER BY connections DESC;"
	@$(COMPOSE) exec -T postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling} -c \
		"SELECT count(*) FILTER (WHERE state = 'active') AS running_queries, \
		        count(*) FILTER (WHERE wait_event_type IS NOT NULL) AS waiting_queries, \
		        max(now() - query_start) AS longest_running \
		 FROM pg_stat_activity WHERE datname = current_database();"
	@$(COMPOSE) exec -T postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling} -c \
		"SELECT pg_size_pretty(pg_database_size(current_database())) AS database_size;"
	@$(COMPOSE) exec -T postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling} -c \
		"SELECT sum(heap_blks_read) AS heap_blocks_read, sum(heap_blks_hit) AS heap_blocks_hit, \
		        round(100.0 * sum(heap_blks_hit) / NULLIF(sum(heap_blks_hit) + sum(heap_blks_read), 0), 2) AS cache_hit_pct \
		 FROM pg_statio_user_tables;"

.PHONY: db-locks
db-locks: ## Show blocked/locking queries (empty output means no lock contention)
	@$(COMPOSE) exec -T postgres psql -U $${DATABASE_USER:-scaling} -d $${DATABASE_NAME:-scaling} -c \
		"SELECT pid, state, wait_event_type, wait_event, left(query, 120) AS query \
		 FROM pg_stat_activity WHERE wait_event_type = 'Lock';"

.PHONY: metrics
metrics: ## Snapshot the API's Prometheus metrics
	curl -s $(HOST)/metrics

.PHONY: system-info
system-info: ## Record host/container/DB versions into benchmarks/baseline/system-info.md
	./scripts/collect-system-info.sh

.PHONY: api-metrics-snapshot
api-metrics-snapshot: ## Capture /metrics before+after a run (for pool/GC analysis)
	@mkdir -p $(RESULTS_DIR)/$${K6_OUT:-latest}
	curl -s $(HOST)/metrics > $(RESULTS_DIR)/$${K6_OUT:-latest}/metrics-$(shell date +%H%M%S).txt
	@echo "saved to $(RESULTS_DIR)/$${K6_OUT:-latest}/"

# --------------------------------------------------------------------------
# housekeeping
# --------------------------------------------------------------------------
.PHONY: clean
clean: ## Remove local build artefacts
	rm -rf bin/ results/
	go clean -cache -testcache 2>/dev/null || true
