// Shared thresholds for every profile.
//
// These are deliberately LOOSE. The baseline is a measurement instrument, not
// a pass/fail gate: a run that "fails" a threshold still produced the numbers
// we need. The thresholds exist to catch catastrophic regressions (a crashed
// container, a 100% error rate), not to enforce an SLO nobody has agreed to.
//
// A real optimisation experiment should tighten them, and record the change
// alongside the numbers it was based on.
export function buildThresholds({ maxP95, maxErrorRate, maxP99 }) {
  return {
    // Latency of every HTTP request, regardless of method or route.
    http_req_duration: [`p(95)<${maxP95}`, `p(99)<${maxP99}`],
    // A non-2xx/3xx response. Deliberately generous.
    http_req_failed: [`rate<${maxErrorRate}`],
    // A single sanity floor so a "fast" run that did almost no work is obvious.
    http_reqs: ['count>100'],
  };
}

export function printGuidance() {
  console.log(`
--------------------------------------------------------------------------
Concurrency, measured (not assumed)
  VUs          : k6 virtual users (loops), NOT open connections
  RPS          : completed requests per second
  Concurrency  : requests in flight at one instant ~= RPS x latency
  To achieve ~1000 in flight with 100 VUs, each iteration fires
  PARALLEL_REQUESTS concurrent requests (http.batch). Check the reported
  http_req_waiting p95/max below, and the API's own http_requests_in_flight.
--------------------------------------------------------------------------
`);
}
