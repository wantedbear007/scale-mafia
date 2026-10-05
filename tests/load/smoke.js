// PROFILE 1 - SMOKE
//
// Purpose: prove the stack works end to end before spending 8 minutes on a
// real run. 10 VUs for 1 minute.
//
//   make load-smoke
//   docker compose --profile load run --rm k6 run /scripts/smoke.js
//
// Thresholds here are a correctness gate (did anything 5xx?), not a
// performance gate.
import { setup as seedSetup } from './lib/seed.js';
import { workload as workloadFn, teardownCleanup as cleanupFn } from './lib/workload.js';
import { buildThresholds } from './lib/thresholds.js';
import { THINK_TIME, PARALLEL_REQUESTS, PROFILE } from './lib/config.js';

export const options = {
  scenarios: {
    smoke: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 5 },
        { duration: '20s', target: 10 },
        { duration: '30s', target: 10 },
      ],
      gracefulRampDown: '10s',
      exec: 'workload',
    },
  },
  thresholds: buildThresholds({
    maxP95: 5000,
    maxP99: 10000,
    maxErrorRate: 0.05,
  }),
  summaryTrendStats: [
    'avg', 'min', 'med', 'p(50)', 'p(75)', 'p(90)', 'p(95)', 'p(99)', 'p(99.9)', 'max',
  ],
};

export function setup() {
  console.log(`[${PROFILE}] smoke profile: think_time=${THINK_TIME}s parallel_requests=${PARALLEL_REQUESTS}`);
  return seedSetup();
}

// k6 looks these up by name: `workload` is the scenario exec,
// `teardown` runs after the test so the dataset is left tidy.
export const workload = workloadFn;
export const teardown = cleanupFn;
