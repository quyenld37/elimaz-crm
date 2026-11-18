// Phần chung của bộ thử tải M0 (kế hoạch M0 Task 22, ADR-42). Tải gồm:
//   - webhook selftest có chữ ký: mỗi lượt một POST /hooks/selftest mang đúng một ping (ingest → tách gói → sự kiện
//     vào → báo hiệu selftest.pong tới 50 client SSE do loadkit sse giữ);
//   - lượt đọc của 50 nhân viên giả: GET /api/v1/me và GET /api/v1/platform/jobs xen kẽ, chia đều cho 50 thẻ.
// Biến môi trường (run.sh đặt): INGEST, API, SECRET, TENANT_KEY, RUN, TOKENS (đường dẫn tuyệt đối tokens.json),
// SUMMARY_OUT, DURATION (ghi đè thời lượng của bài).
import http from 'k6/http';
import crypto from 'k6/crypto';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';

const INGEST = __ENV.INGEST || 'http://127.0.0.1:4191';
const API = __ENV.API || 'http://127.0.0.1:4190';
const SECRET = __ENV.SECRET || '';
const TENANT_KEY = __ENV.TENANT_KEY || 'loadtest';
const RUN = __ENV.RUN || 'r0';
const TOKENS = __ENV.TOKENS ? JSON.parse(open(__ENV.TOKENS)) : [];

// Số nhân viên giả; giới hạn của api là 300 yêu cầu / 60 s mỗi nhân viên (httpapi.RateLimit), nên tốc độ đọc
// mỗi nhân viên phải dưới 5 lượt mỗi giây để 429 không lẫn vào số đo.
export const STAFF = 50;
export const MAX_READ_PER_STAFF = 4;

const webhookMs = new Trend('webhook_ms', true);
const webhookOK = new Counter('webhook_ok');
const webhookBad = new Counter('webhook_bad');
const readMs = new Trend('read_ms', true);
const readOK = new Counter('read_ok');
const readBad = new Counter('read_bad');

// eventId: lt.<run>.<ms gửi>.<VU>.<lượt> — loadkit đọc lại lúc gửi để tính độ trễ tới báo hiệu.
export function eventID(sentMs) {
  return `lt.${RUN}.${sentMs}.${exec.vu.idInTest}.${exec.vu.iterationInScenario}`;
}

export function webhook() {
  const sent = Date.now();
  const body = JSON.stringify({
    events: [{ kind: 'ping', eventId: eventID(sent), tenantKey: TENANT_KEY, sentAt: new Date(sent).toISOString() }],
  });
  const sig = crypto.hmac('sha256', SECRET, body, 'hex');
  const res = http.post(`${INGEST}/hooks/selftest`, body, {
    headers: { 'Content-Type': 'application/json', 'X-Selftest-Signature': `sha256=${sig}` },
    tags: { name: 'webhook' },
    timeout: '10s',
  });
  webhookMs.add(res.timings.duration);
  const ok = res.status === 200;
  webhookOK.add(ok ? 1 : 0);
  webhookBad.add(ok ? 0 : 1);
}

export function read() {
  if (TOKENS.length < STAFF) {
    throw new Error(`cần ${STAFF} thẻ trong TOKENS, có ${TOKENS.length}`);
  }
  // Chia đều theo số lượt của cả bài: lượt i dùng nhân viên i mod 50, chẵn lẻ của vòng chọn /me hay /platform/jobs.
  const i = exec.scenario.iterationInTest;
  const token = TOKENS[i % STAFF].token;
  const path = Math.floor(i / STAFF) % 2 === 0 ? '/api/v1/me' : '/api/v1/platform/jobs?limit=50';
  const res = http.get(`${API}${path}`, {
    headers: { 'Cf-Access-Jwt-Assertion': token },
    tags: { name: path.startsWith('/api/v1/me') ? 'me' : 'jobs' },
    timeout: '10s',
  });
  readMs.add(res.timings.duration);
  const ok = res.status === 200;
  readOK.add(ok ? 1 : 0);
  readBad.add(ok ? 0 : 1);
}

// options dựng hai kịch bản tốc độ đến cố định: webhook `rate` lượt mỗi giây, đọc `readPerStaff` lượt mỗi giây mỗi
// nhân viên, trong `duration` (DURATION ghi đè). Ngưỡng của k6 là các ngưỡng đạt đo được ở phía gửi (04 mục 1); các
// ngưỡng còn lại do loadkit report chấm.
export function options(rate, readPerStaff, duration) {
  if (readPerStaff > MAX_READ_PER_STAFF) {
    throw new Error(`đọc ${readPerStaff} lượt/giây mỗi nhân viên vượt giới hạn tần suất của api`);
  }
  const d = __ENV.DURATION || duration;
  return {
    discardResponseBodies: true,
    summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max', 'count'],
    scenarios: {
      webhooks: {
        executor: 'constant-arrival-rate', exec: 'webhook', rate, timeUnit: '1s', duration: d,
        preAllocatedVUs: Math.max(20, rate), maxVUs: Math.max(100, rate * 5),
      },
      reads: {
        executor: 'constant-arrival-rate', exec: 'read', rate: Math.round(readPerStaff * STAFF), timeUnit: '1s',
        duration: d, preAllocatedVUs: 20, maxVUs: 200,
      },
    },
    thresholds: {
      webhook_ms: ['p(95)<300'],
      webhook_bad: ['count==0'],
      read_ms: ['p(95)<200', 'p(99)<500'],
    },
  };
}

// handleSummary ghi nguyên data.metrics ra SUMMARY_OUT cho loadkit report, và một dòng tóm tắt ra màn hình.
export function handleSummary(data) {
  const m = data.metrics;
  const v = (name, stat) => (m[name] && m[name].values[stat] !== undefined ? m[name].values[stat].toFixed(1) : '-');
  const line = `webhook: ${v('webhook_ok', 'count')} OK, ${v('webhook_bad', 'count')} khác 200, ` +
    `p95 ${v('webhook_ms', 'p(95)')} ms; đọc: ${v('read_ok', 'count')} OK, ${v('read_bad', 'count')} lỗi, ` +
    `p95 ${v('read_ms', 'p(95)')} ms, p99 ${v('read_ms', 'p(99)')} ms\n`;
  const out = { stdout: line };
  if (__ENV.SUMMARY_OUT) {
    out[__ENV.SUMMARY_OUT] = JSON.stringify(m, null, 2);
  }
  return out;
}
