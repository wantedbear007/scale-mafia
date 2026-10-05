// Shared setup phase: samples real ids from the running API.
//
// Why not just hard-code 1..100000?
//   * Benchmark runs DELETE rows, so id 4711 may be gone on the second run.
//   * Sampling proves the seed really produced the expected dataset size.
//   * It costs only a handful of setup requests, executed once before the
//     clock starts - setup() time is NOT part of the reported test duration.
import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, ID_POOL_SIZE, SAMPLE_LIMIT, TIMEOUT } from './config.js';

export const RESOURCES = ['users', 'products', 'orders'];

/** Pages a collection endpoint and collects ids until it has `wanted` of them. */
function sampleIds(resource, wanted) {
  const ids = [];
  const maxPages = Math.max(1, Math.ceil(wanted / SAMPLE_LIMIT));

  for (let page = 1; page <= maxPages && ids.length < wanted; page += 1) {
    const res = http.get(`${BASE_URL}/api/v1/${resource}?page=${page}&limit=${SAMPLE_LIMIT}`, {
      timeout: TIMEOUT,
      tags: { phase: 'setup' },
    });
    if (res.status !== 200) {
      throw new Error(`setup: GET /api/v1/${resource}?page=${page} returned ${res.status}: ${res.body}`);
    }

    let payload;
    try {
      payload = res.json();
    } catch (e) {
      throw new Error(`setup: ${resource} response was not JSON: ${res.body.slice(0, 200)}`);
    }

    if (!payload.data || payload.data.length === 0) break;
    for (const row of payload.data) {
      if (row.id !== undefined) ids.push(row.id);
      if (ids.length >= wanted) break;
    }
  }
  return ids;
}

export function setup() {
  const ready = http.get(`${BASE_URL}/ready`, { timeout: TIMEOUT, tags: { phase: 'setup' } });
  if (ready.status !== 200) {
    throw new Error(
      `API is not ready (${BASE_URL}/ready returned ${ready.status}). ` +
      'Run "make up && make migrate && make seed" first.',
    );
  }

  const pools = {};
  for (const resource of RESOURCES) {
    const ids = sampleIds(resource, ID_POOL_SIZE);
    if (ids.length === 0) {
      throw new Error(`setup: found no ${resource}. Seed the database first: make migrate && make seed`);
    }
    pools[resource] = ids;
    console.log(`setup: sampled ${ids.length} ${resource} ids`);
  }
  return pools;
}

export { check };
