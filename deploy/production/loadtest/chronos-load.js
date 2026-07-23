// k6 load test for the ChronosOS control plane REST API.
//
// Exercises the REAL endpoints (os/server.go):
//   POST /api/schedules          create a schedule
//   GET  /api/sessions           list sessions (read-heavy → replica path)
//   GET  /api/events/stream      SSE stream (long-lived connection)
//   GET  /health/ready           readiness (unauthenticated)
//
// Validates the SLOs from README §2/§13:
//   http_req_duration p99 < 300ms, http_req_failed < 1%.
//
// Usage:
//   k6 run -e BASE_URL=https://chronos.prod.example.com \
//          -e TOKEN=$JWT \
//          -e TARGET_RPS=350 -e DURATION=5m \
//          chronos-load.js
//
// TARGET_RPS is the per-instance rate you are validating (see README §14): run
// against ONE pod (scale deploy to 1 in staging) to find the true per-pod
// ceiling, then feed it back into the replica math and KEDA targetRPS.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8420';
const TOKEN = __ENV.TOKEN || '';
const TARGET_RPS = parseInt(__ENV.TARGET_RPS || '350', 10);
const DURATION = __ENV.DURATION || '5m';

const createLatency = new Trend('chronos_create_schedule_ms', true);
const listLatency = new Trend('chronos_list_sessions_ms', true);
const sseConnRate = new Rate('chronos_sse_connect_ok');

const authHeaders = TOKEN ? { Authorization: `Bearer ${TOKEN}` } : {};
const jsonHeaders = Object.assign({ 'Content-Type': 'application/json' }, authHeaders);

export const options = {
  scenarios: {
    // Steady RPS for the read/write API mix (open model → tests server, not VUs).
    api_mix: {
      executor: 'constant-arrival-rate',
      rate: TARGET_RPS,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.ceil(TARGET_RPS * 0.5),
      maxVUs: TARGET_RPS * 2,
      exec: 'apiMix',
    },
    // A pool of long-lived SSE subscribers held for the whole run.
    sse_subscribers: {
      executor: 'constant-vus',
      vus: parseInt(__ENV.SSE_VUS || '50', 10),
      duration: DURATION,
      exec: 'sseStream',
    },
  },
  thresholds: {
    // Overall SLOs.
    http_req_failed: ['rate<0.01'],
    'http_req_duration{scenario:api_mix}': ['p(99)<300', 'p(95)<150'],
    chronos_create_schedule_ms: ['p(99)<300'],
    chronos_list_sessions_ms: ['p(99)<150'],
    chronos_sse_connect_ok: ['rate>0.99'],
  },
};

// Weighted read/write mix: ~80% reads, ~20% writes (typical control-plane).
export function apiMix() {
  const roll = Math.random();
  if (roll < 0.8) {
    listSessions();
  } else {
    createSchedule();
  }
  sleep(0.1);
}

function listSessions() {
  const res = http.get(`${BASE_URL}/api/sessions?limit=20`, { headers: authHeaders, tags: { op: 'list_sessions' } });
  listLatency.add(res.timings.duration);
  check(res, { 'list sessions 2xx': (r) => r.status >= 200 && r.status < 300 });
}

function createSchedule() {
  const payload = JSON.stringify({
    name: `load-${__VU}-${__ITER}`,
    cron: '*/5 * * * *',
    agent_id: 'loadtest-agent',
    payload: { message: 'k6 load' },
  });
  const res = http.post(`${BASE_URL}/api/schedules`, payload, { headers: jsonHeaders, tags: { op: 'create_schedule' } });
  createLatency.add(res.timings.duration);
  check(res, { 'create schedule 2xx': (r) => r.status >= 200 && r.status < 300 });
}

// Open an SSE stream and hold it. k6 http.get on an SSE endpoint returns when
// the connection is established / times out; we assert connectivity and reopen.
export function sseStream() {
  const res = http.get(`${BASE_URL}/api/events/stream`, {
    headers: Object.assign({ Accept: 'text/event-stream' }, authHeaders),
    timeout: '30s',
    tags: { op: 'sse' },
  });
  sseConnRate.add(res.status === 200);
  check(res, { 'sse connected': (r) => r.status === 200 });
  sleep(1);
}

// Readiness gate before the run proper (fail fast if the target is unhealthy).
export function setup() {
  const res = http.get(`${BASE_URL}/health/ready`);
  if (res.status !== 200) {
    throw new Error(`target not ready: /health/ready => ${res.status}`);
  }
}
