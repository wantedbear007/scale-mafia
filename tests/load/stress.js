// PROFILE 3 - STRESS
//
// The goal is to FIND the saturation point, not to pass it. Stages climb
// 100 -> 200 -> 300 -> 500 VUs with a short hold at each level so the shape
// of the degradation is visible in the time series.
//
//   make load-stress
//   docker compose --profile load run --rm k6 run /scripts/stress.js
//
// How to read the result:
//   * Watch `vus` against `http_req_duration` and `http_req_waiting`.
//   * When latency starts climbing faster than throughput grows, the system
//     has passed its knee. That VU count is the current capacity.
//   * When `http_req_waiting` stops rising while `http_reqs` flattens, the
//     bottleneck is a resource that cannot be queued any further (usually the
//     database connection pool, the API's 2-CPU budget, or PostgreSQL itself).
//   * If error rates spike, check whether they are pool-exhaustion timeouts
//     (API logs) or "too many clients" from PostgreSQL.
//   * 500 VUs x 10 parallel requests = up to 5000 requests in flight. If that
//     is far beyond what the box can serve, expect long tails, not failures.
import { setup as seedSetup } from './lib/seed.js';
import { workload as workloadFn, teardownCleanup as cleanupFn } from './lib/workload.js';
import { buildThresholds } from './lib/thresholds.js';
import { THINK_TIME, PARALLEL_REQUESTS, PROFILE } from './lib/config.js';

const HOLD = '2m';

export const options = {
  scenarios: {
    stress: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 100 },
        { duration: HOLD, target: 100 },

        { duration: '30s', target: 200 },
        { duration: HOLD, target: 200 },

        { duration: '30s', target: 300 },
        { duration: HOLD, target: 300 },

        { duration: '30s', target: 500 },
        { duration: HOLD, target: 500 },

        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '20s',
      exec: 'workload',
    },
  },
  // Intentionally very loose. A stress profile is expected to fall over; if
  // these thresholds fail, the useful data is still in the summary.
  thresholds: buildThresholds({
    maxP95: 60000,
    maxP99: 120000,
    maxErrorRate: 0.50,
  }),
  summaryTrendStats: [
    'avg', 'min', 'med', 'p(50)', 'p(75)', 'p(90)', 'p(95)', 'p(99)', 'p(99.9)', 'max',
  ],
};

export function setup() {
  console.log(
    `[${PROFILE}] stress profile: 100->200->300->500 VUs, ${PARALLEL_REQUESTS} parallel requests, ` +
    `think_time=${THINK_TIME}s, ${HOLD} hold per stage`,
  );
  return seedSetup();
}

// k6 looks these up by name: `workload` is the scenario exec,
// `teardown` runs after the test so the dataset is left tidy.
export const workload = workloadFn;
export const teardown = cleanupFn;
