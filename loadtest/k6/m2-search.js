// Tìm khách dưới tải (kế hoạch M2 Task 88): 20 lượt mỗi giây trong 5 phút qua GET /api/v1/customers?q=, trộn đuôi SĐT
// 4–6 số (60%), SĐT đủ (20%), tên (15%: nửa họ tên đủ, nửa hai chữ cuối), mã khách (5%). Ngưỡng: p95 < 300 ms (DH02),
// lỗi < 0,1%. SĐT đủ và mã luôn có khách (đếm search_miss); loadkit m2report chấm.
import http from 'k6/http';
import { Counter, Trend } from 'k6/metrics';
import { auth, duration, pick, summary, API } from './fb_common.js';
import { phoneDigits, randInt, SAMPLE, token } from './m2_common.js';

const d = duration('5m');

export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max', 'count'],
  scenarios: {
    search: { executor: 'constant-arrival-rate', rate: 20, timeUnit: '1s', duration: d, preAllocatedVUs: 20, maxVUs: 200 },
  },
  thresholds: { search_ms: ['p(95)<300'] },
};

const all = new Trend('search_ms', true);
const byKind = {
  tail: new Trend('search_tail_ms', true),
  phone: new Trend('search_phone_ms', true),
  name: new Trend('search_name_ms', true),
  code: new Trend('search_code_ms', true),
};
const req = new Counter('search_req');
const bad = new Counter('search_bad');
const exact = new Counter('search_exact');
const miss = new Counter('search_miss');

function query() {
  const p = randInt(100);
  if (p < 60) {
    const digits = phoneDigits(randInt(SAMPLE.phones));
    return ['tail', digits.slice(-(4 + randInt(3)))];
  }
  if (p < 80) {
    return ['phone', '0' + phoneDigits(randInt(SAMPLE.phones))];
  }
  if (p < 95) {
    const name = pick(SAMPLE.names);
    return ['name', randInt(2) === 0 ? name : name.split(' ').slice(-2).join(' ')];
  }
  return ['code', pick(SAMPLE.codes)];
}

export default function () {
  const [kind, q] = query();
  const res = http.get(`${API}/api/v1/customers?q=${encodeURIComponent(q)}&limit=20`, {
    headers: auth(token()), tags: { name: 'search', kind }, timeout: '10s',
  });
  all.add(res.timings.duration);
  byKind[kind].add(res.timings.duration);
  req.add(1);
  if (res.status !== 200) {
    bad.add(1, { status: String(res.status) });
    return;
  }
  if (kind === 'phone' || kind === 'code') {
    exact.add(1);
    let n = 0;
    try {
      n = res.json('items').length;
    } catch (e) {
      n = 0;
    }
    if (n === 0) {
      miss.add(1, { kind });
    }
  }
}

export function handleSummary(data) {
  return summary(data, ['search_ms', 'search_tail_ms', 'search_phone_ms', 'search_name_ms', 'search_code_ms', 'search_req',
    'search_bad', 'search_exact', 'search_miss']);
}
