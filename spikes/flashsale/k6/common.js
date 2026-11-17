// Phần dùng chung của ba bài đo k6 cho POST /buy (kế hoạch M0 bản 5, Task 1).
import http from 'k6/http';
import { sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

export const BASE = __ENV.BASE_URL || 'http://127.0.0.1:4190';
export const TENANT = __ENV.TENANT || '00000000-0000-4000-8000-000000000001';
export const ITEM = __ENV.ITEM;
if (!ITEM) {
  throw new Error('thiếu ITEM (in ra bởi: go run . setup -units N)');
}

const results = {
  bought: new Counter('result_bought'),
  sold_out: new Counter('result_sold_out'),
  over_limit: new Counter('result_over_limit'),
};
const retries = new Counter('result_retry_503');
const others = new Counter('result_other');
const gaveUp = new Counter('gave_up');
// Từ lượt bấm tới kết quả cuối, kể cả các lần thử lại.
export const clickToFinal = new Trend('click_to_final', true);

export function uuidFor(n) {
  return '00000000-0000-4000-8000-' + n.toString(16).padStart(12, '0');
}

export function buyRequest(key, customer, qty) {
  return {
    method: 'POST',
    url: `${BASE}/buy`,
    body: JSON.stringify({ item_id: ITEM, customer_id: customer, qty: qty }),
    params: {
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key, 'X-Tenant': TENANT },
      tags: { name: 'buy' },
    },
  };
}

// record ghi một câu trả lời; trả true khi là kết quả cuối, false khi phải thử lại cùng khóa.
export function record(res) {
  if (res.status === 503) {
    retries.add(1);
    return false;
  }
  let result = '';
  try {
    result = res.json('result');
  } catch (_) {
    // body không phải JSON
  }
  if (res.status === 200 && results[result]) {
    results[result].add(1);
  } else {
    others.add(1, { status: String(res.status) });
  }
  return true;
}

// buyUntilFinal gửi một lượt bấm, thử lại với cùng khóa khi nhận 503, chờ after_ms ±20%.
export function buyUntilFinal(key, customer, qty) {
  const req = buyRequest(key, customer, qty);
  const t0 = Date.now();
  for (let i = 0; i < 40; i++) {
    const res = http.post(req.url, req.body, req.params);
    if (record(res)) {
      clickToFinal.add(Date.now() - t0);
      return;
    }
    let after = 500;
    try {
      after = res.json('after_ms') || 500;
    } catch (_) {
      // giữ 500
    }
    sleep((after / 1000) * (0.8 + Math.random() * 0.4));
  }
  gaveUp.add(1);
}

export const summaryTrendStats = ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'];
