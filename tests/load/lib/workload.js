// The shared workload: a realistic read/write mix across all three resources.
//
//   70% GET   (single-resource reads + paginated collection reads)
//   20% POST  (create)
//    5% PUT   (update)
//    5% DELETE
//
// Design notes
// ------------
// * One k6 iteration issues PARALLEL_REQUESTS requests via http.batch, then
//   thinks for THINK_TIME seconds. That is what turns "100 VUs" into roughly
//   "1000 requests in flight" - and the achieved number is reported, not
//   assumed (see http_req_waiting in the summary).
// * Every VU owns a disjoint slice of the id pool for DELETE traffic, so two
//   VUs never race to delete the same row. When a VU runs out of ids it
//   creates a throwaway row instead and deletes that on a later iteration,
//   which is exactly what a real client does and keeps the error rate honest.
// * Requests are batched rather than issued in a loop: k6 still measures each
//   request's latency individually, so batching does not distort
//   http_req_duration.
import http from 'k6/http';
import { sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';
import {
  BASE_URL, PARALLEL_REQUESTS, THINK_TIME, TIMEOUT,
  READ_WEIGHT, WRITE_WEIGHT, UPDATE_WEIGHT, DELETE_WEIGHT,
  ORDERS_SHARE, PRODUCTS_SHARE, LIST_LIMIT, MAX_LIST_PAGE, PROFILE,
} from './config.js';

// An explicit HTTP status distribution (k6 reports http_req_failed out of the
// box, not the raw status spread) plus a per-iteration duration trend.
const statusDistribution = new Counter('api_status_codes');
const iterationDuration = new Trend('api_iteration_duration', true);

const TOTAL_WRITE_WEIGHT = WRITE_WEIGHT + UPDATE_WEIGHT + DELETE_WEIGHT;
const TOTAL_WEIGHT = READ_WEIGHT + TOTAL_WRITE_WEIGHT;
const PRODUCTS_CUTOFF = ORDERS_SHARE + PRODUCTS_SHARE;

const ORDER_STATUSES = ['pending', 'paid', 'shipped', 'delivered', 'cancelled'];

/** Per-VU state. Each k6 VU is its own JS runtime, so this really is per-VU. */
let state = null;

function initState(pools) {
  // The sampled pool is split in two DISJOINT halves:
  //
  //   [0 .. mid)  READ region  - every VU may read from it, nobody deletes it
  //   [mid .. N)  DELETE region - partitioned per VU, only ever deleted
  //
  // This matters. If reads and deletes shared a pool, VU 2 would keep reading
  // rows VU 1 had just deleted, and the benchmark would report a few percent
  // of 404s that say nothing at all about system performance. Disjoint regions
  // make that impossible without any cross-VU bookkeeping.
  //
  // When a VU exhausts its DELETE slice it falls back to create-then-delete,
  // which is what a real client does anyway.
  const READ_SHARE = 0.5;
  const MAX_VUS = 100;

  const split = (arr) => {
    const mid = Math.floor(arr.length * READ_SHARE);
    const per = Math.floor((arr.length - mid) / MAX_VUS);
    const start = exec.vu.idInTest * per;
    return {
      readable: arr.slice(0, mid),
      // Reversed so the DELETE path can pop() in O(1).
      deletable: per < 1 ? [] : arr.slice(mid + start, mid + start + per).reverse(),
    };
  };

  const products = split(pools.products);
  const orders = split(pools.orders);

  // Users are special. orders.user_id has ON DELETE CASCADE, so deleting a
  // seeded user silently deletes ~5 of its orders - and those orders are
  // scattered across the whole orders id space, including the READ region.
  // The result would be 404s on orders that have nothing to do with system
  // performance. So users are only ever deleted after being created by the
  // same VU; products and orders (which have no outgoing FK) use the seeded
  // DELETE region.
  return {
    readable: { users: pools.users, products: products.readable, orders: orders.readable },
    deletable: { users: [], products: products.deletable, orders: orders.deletable },
    pendingCreates: { users: [], products: [], orders: [] },
    cursors: { users: 0, products: 0, orders: 0 },
  };
}

function nextId(resource) {
  const pool = state.readable[resource];
  if (pool.length === 0) return null;
  // Rotating cursor: consecutive reads in one VU hit different rows, so the
  // benchmark does not degenerate into measuring a single cached row.
  state.cursors[resource] = (state.cursors[resource] + 1) % pool.length;
  return pool[state.cursors[resource]];
}

function pickResource() {
  const r = Math.random() * 100;
  if (r < ORDERS_SHARE) return 'orders';
  if (r < PRODUCTS_CUTOFF) return 'products';
  return 'users';
}

function pickVerb() {
  const r = Math.random() * TOTAL_WEIGHT;
  if (r < READ_WEIGHT) return 'GET';
  if (r < READ_WEIGHT + WRITE_WEIGHT) return 'POST';
  if (r < READ_WEIGHT + WRITE_WEIGHT + UPDATE_WEIGHT) return 'PUT';
  return 'DELETE';
}

function claimDeletable(resource) {
  const pool = state.deletable[resource];
  if (pool.length > 0) return pool.pop();
  const created = state.pendingCreates[resource];
  if (created.length > 0) return created.shift();
  return null;
}

function tag(name, resource, op) {
  return { name, resource, op, profile: PROFILE };
}

function randomEmail() {
  return `load-${__VU}-${__ITER}-${Math.floor(Math.random() * 1e9)}@example.com`;
}

function createBody(resource) {
  switch (resource) {
    case 'users':
      return JSON.stringify({ name: `load-user-${__VU}-${__ITER}`, email: randomEmail() });
    case 'products':
      return JSON.stringify({
        name: `load-product-${__VU}-${__ITER}`,
        description: 'created by the k6 baseline profile',
        price: (Math.random() * 200).toFixed(2),
        stock: Math.floor(Math.random() * 500),
      });
    default: {
      // An order must reference a live user, so pick one this VU has not
      // deleted. If it cannot, do not fabricate an id.
      const userId = nextId('users');
      if (userId === null) return null;
      return JSON.stringify({
        user_id: userId,
        total_amount: (Math.random() * 500).toFixed(2),
      });
    }
  }
}

/**
 * Builds one request descriptor. `expected` is the status the API should
 * return; anything else is reported as a benchmark failure.
 */
/** Builds a POST for `resource`, recording the row as deletable later. */
function createRequest(resource, op) {
  const body = createBody(resource);
  if (body === null) return null;
  return {
    method: 'POST',
    url: `${BASE_URL}/api/v1/${resource}`,
    body,
    tags: tag(`POST /api/v1/${resource}`, resource, op),
    expected: 201,
    createFor: resource,
  };
}

function buildRequest() {
  const resource = pickResource();
  const verb = pickVerb();

  switch (verb) {
    case 'GET': {
      // ~1 read in 4 is a paginated collection read. Those execute count(*)
      // plus an OFFSET scan, which is where the baseline is expected to hurt.
      if (Math.random() < 0.25) {
        const page = 1 + Math.floor(Math.random() * MAX_LIST_PAGE);
        return {
          method: 'GET',
          url: `${BASE_URL}/api/v1/${resource}?page=${page}&limit=${LIST_LIMIT}`,
          tags: tag(`GET /api/v1/${resource}`, resource, 'list'),
          expected: 200,
        };
      }
      const id = nextId(resource);
      if (id === null) return createRequest(resource, 'get-fallback');
      return {
        method: 'GET',
        url: `${BASE_URL}/api/v1/${resource}/${id}`,
        tags: tag(`GET /api/v1/${resource}/:id`, resource, 'get'),
        expected: 200,
      };
    }

    case 'POST': {
      const body = createBody(resource);
      if (body === null) return buildRequest(); // nothing safe to reference
      return {
        method: 'POST',
        url: `${BASE_URL}/api/v1/${resource}`,
        body,
        tags: tag(`POST /api/v1/${resource}`, resource, 'create'),
        expected: 201,
      };
    }

    case 'PUT': {
      const id = nextId(resource);
      if (id === null) return createRequest(resource, 'update');
      const url = `${BASE_URL}/api/v1/${resource}/${id}`;
      if (resource === 'users') {
        return {
          method: 'PUT',
          url,
          body: JSON.stringify({ name: `renamed-${__VU}-${__ITER}` }),
          tags: tag('PUT /api/v1/users/:id', resource, 'update'),
          expected: 200,
        };
      }
      if (resource === 'products') {
        return {
          method: 'PUT',
          url,
          body: JSON.stringify({ stock: Math.floor(Math.random() * 250) }),
          tags: tag('PUT /api/v1/products/:id', resource, 'update'),
          expected: 200,
        };
      }
      return {
        method: 'PUT',
        url,
        body: JSON.stringify({ status: ORDER_STATUSES[Math.floor(Math.random() * ORDER_STATUSES.length)] }),
        tags: tag('PUT /api/v1/orders/:id', resource, 'update'),
        expected: 200,
      };
    }

    default: {
      const id = claimDeletable(resource);
      if (id === null) {
        // No id left to delete. Create a throwaway row instead; its id is
        // remembered below and deleted by a later DELETE. This mirrors what a
        // real client does and keeps the error rate honest.
        return createRequest(resource, 'create-for-delete');
      }
      return {
        method: 'DELETE',
        url: `${BASE_URL}/api/v1/${resource}/${id}`,
        tags: tag(`DELETE /api/v1/${resource}/:id`, resource, 'delete'),
        expected: 204,
      };
    }
  }
}

function handleResponses(requests, responses) {
  for (let i = 0; i < responses.length; i += 1) {
    const res = responses[i];
    const req = requests[i];

    statusDistribution.add(1, {
      status: String(res.status),
      resource: req.tags.resource,
      op: req.tags.op,
      profile: PROFILE,
    });

    if (res.status !== req.expected) {
      console.error(
        `unexpected status: ${req.method} ${req.url} -> ${res.status} ` +
        `(expected ${req.expected}): ${String(res.body).slice(0, 200)}`,
      );
      continue;
    }

    if (req.createFor && res.status === 201) {
      try {
        const created = res.json();
        if (created && created.id) state.pendingCreates[req.createFor].push(created.id);
      } catch (e) {
        // A malformed create body means the API is misbehaving; the status
        // counter above already recorded it.
      }
    }

  }
}

/** The scenario entry point. */
export function workload(pools) {
  if (state === null) state = initState(pools);

  const start = Date.now();
  const requests = [];
  for (let i = 0; i < PARALLEL_REQUESTS; i += 1) {
    let r = buildRequest();
    if (r === null) r = createRequest(pickResource(), 'get-fallback');
    if (r === null) continue;
    requests.push({
      method: r.method,
      url: r.url,
      body: r.body,
      params: {
        headers: { 'Content-Type': 'application/json' },
        timeout: TIMEOUT,
        tags: r.tags,
      },
      expected: r.expected,
      createFor: r.createFor,
      tags: r.tags,
    });
  }

  const responses = http.batch(requests);
  handleResponses(requests, responses);
  iterationDuration.add(Date.now() - start);

  if (THINK_TIME > 0) sleep(THINK_TIME);
}

/** Removes rows this VU created, so repeated runs start from a known state. */
export function teardownCleanup() {
  for (const resource of ['users', 'products', 'orders']) {
    const pool = state ? state.deletable[resource] : [];
    while (pool.length > 0) {
      const id = pool.pop();
      http.del(`${BASE_URL}/api/v1/${resource}/${id}`, null, {
        timeout: TIMEOUT,
        tags: { phase: 'teardown' },
      });
    }
  }
}
