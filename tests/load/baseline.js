// PROFILE 2 - BASELINE
//
// This is the run every future optimisation is compared against, so it must
// be boring and reproducible:
//
//   Warm-up : 2 minutes  (JIT-free, but still lets the page cache, the
//                        connection pool and PostgreSQL's shared buffers fill,
//                        and lets autovacuum settle after seeding)
//   Load    : 5 minutes  (this is the measured window)
//   Cooldown: 1 minute
//
//   make load-baseline
//   docker compose --profile load run --rm k6 run /scripts/baseline.js
//
// 100 VUs x PARALLEL_REQUESTS(10) = up to ~1000 requests in flight.
// The number actually achieved is in the report as http_req_waiting; it is
// never assumed.
//
// Reproducibility rules for this profile:
//   * always run it against a freshly seeded dataset (`make seed -f`)
//   * never change more than one thing between comparable runs
//   * record the git commit, the image digest and the resource limits
import { setup as seedSetup } from './lib/seed.js';
import { workload as workloadFn, teardownCleanup as cleanupFn } from './lib/workload.js';
import { buildThresholds } from './lib/thresholds.js';
import { THINK_TIME, PARALLEL_REQUESTS, PROFILE } from './lib/config.js';

export const options = {
  scenarios: {
    baseline: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 100 }, // ramp to target
        { duration: '2m', target: 100 }, // warm-up (not the measured window)
        { duration: '5m', target: 100 }, // measured load window
        { duration: '1m', target: 0 }, // cooldown / drain
      ],
      gracefulRampDown: '15s',
      exec: 'workload',
    },
  },
  // Loose on purpose: this profile measures reality, it does not gate on it.
  thresholds: buildThresholds({
    maxP95: 20000,
    maxP99: 45000,
    maxErrorRate: 0.10,
  }),
  summaryTrendStats: [
    'avg', 'min', 'med', 'p(50)', 'p(75)', 'p(90)', 'p(95)', 'p(99)', 'p(99.9)', 'max',
  ],
};

export function setup() {
  console.log(
    `[${PROFILE}] baseline profile: 100 VUs x ${PARALLEL_REQUESTS} parallel requests, ` +
    `think_time=${THINK_TIME}s, warm-up 2m, load 5m, cooldown 1m`,
  );
  return seedSetup();
}

// k6 looks these up by name: `workload` is the scenario exec,
// `teardown` runs after the test so the dataset is left tidy.
export const workload = workloadFn;
export const teardown = cleanupFn;
