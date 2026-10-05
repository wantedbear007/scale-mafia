// Shared configuration for every k6 profile.
//
// Everything is environment driven so the same code runs the smoke, baseline
// and stress profiles, and so individual knobs can be tweaked without editing
// the scripts (see `make load-baseline K6_PARALLEL=10`).
//
// ---------------------------------------------------------------------------
// A note on VUs, RPS and concurrency (the distinction that trips everyone up)
// ---------------------------------------------------------------------------
//   * VU         = a virtual user. k6 gives you N of them. That is a count of
//                  loops, not a count of open connections.
//   * RPS        = requests completed per second. Derived from how fast each
//                  loop iterates, not something you configure directly.
//   * Concurrency= requests in flight at the same instant. This is what
//                  Little's Law ties together: concurrency ~= RPS x latency.
//
// With one sequential request per iteration, 100 VUs can never exceed ~100
// requests in flight, and the real number is usually lower because each VU is
// sitting in sleep(). To actually reach a target concurrency of, say, 1000
// in-flight requests with 100 VUs, each iteration must issue PARALLEL_REQUESTS
// requests concurrently - k6's http.batch does exactly that, and a batch
// counts as ONE iteration but PARALLEL_REQUESTS requests.
//
// So achieved concurrency is measured, never assumed: the profile reports
// http_req_waiting (k6's own in-flight counter) and the API reports
// http_requests_in_flight. If the two disagree, the load generator or the
// application is the limiting factor.

/** Static defaults. Every one of these can be overridden with an env var. */
export const CONFIG = {
  baseUrl: 'http://localhost:8080',
  requestTimeout: '30s',
  parallelRequests: 10,
  thinkTimeSeconds: 1,
  readWeight: 70,
  writeWeight: 20,
  updateWeight: 5,
  deleteWeight: 5,
  ordersShare: 34,
  productsShare: 33,
  listLimit: 20,
  maxListPage: 5,
  idPoolSize: 5000,
  sampleLimit: 500,
};

export function envStr(name, def) {
  const v = __ENV[name];
  return v === undefined || v === '' ? def : v;
}

export function envInt(name, def) {
  const v = __ENV[name];
  if (v === undefined || v === '') return def;
  const n = parseInt(v, 10);
  if (Number.isNaN(n)) throw new Error(`${name}="${v}" is not an integer`);
  return n;
}

export function envBool(name, def) {
  const v = __ENV[name];
  if (v === undefined || v === '') return def;
  return v === '1' || String(v).toLowerCase() === 'true';
}

export const BASE_URL = envStr('BASE_URL', CONFIG.baseUrl);

/** Concurrent requests issued per iteration (drives in-flight concurrency). */
export const PARALLEL_REQUESTS = envInt('PARALLEL_REQUESTS', CONFIG.parallelRequests);

/** Think time between iterations, in seconds. 0 means "as fast as possible". */
export const THINK_TIME = envInt('THINK_TIME_SECONDS', CONFIG.thinkTimeSeconds);

/** Read:write mix. Only the ratio between the weights matters. */
export const READ_WEIGHT = envInt('READ_WEIGHT', CONFIG.readWeight);
export const WRITE_WEIGHT = envInt('WRITE_WEIGHT', CONFIG.writeWeight);
export const UPDATE_WEIGHT = envInt('UPDATE_WEIGHT', CONFIG.updateWeight);
export const DELETE_WEIGHT = envInt('DELETE_WEIGHT', CONFIG.deleteWeight);

/** Split of traffic between the three resources (percent). */
export const ORDERS_SHARE = envInt('ORDERS_SHARE', CONFIG.ordersShare);
export const PRODUCTS_SHARE = envInt('PRODUCTS_SHARE', CONFIG.productsShare);

/** limit= used for collection (list) requests. */
export const LIST_LIMIT = envInt('LIST_LIMIT', CONFIG.listLimit);

/** How many ids the setup phase samples per resource. */
export const ID_POOL_SIZE = envInt('ID_POOL_SIZE', CONFIG.idPoolSize);
export const SAMPLE_LIMIT = envInt('SAMPLE_LIMIT', CONFIG.sampleLimit);

/** Page spread for list requests: exercising deep offsets is intentional. */
export const MAX_LIST_PAGE = envInt('MAX_LIST_PAGE', CONFIG.maxListPage);

export const TIMEOUT = envStr('REQUEST_TIMEOUT', CONFIG.requestTimeout);

/** Profile name, used to tag custom metrics so runs are distinguishable. */
export const PROFILE = envStr('K6_PROFILE', 'baseline');
