// Bài thử tải webhook hãng của M3 (kế hoạch M3 Task M3-48, ADR-42): 10× ca hiện tại — 2.000 đơn/ngày × khoảng 6 tin
// hãng mỗi vận đơn, dồn đỉnh 20 tin/giây trong 15 phút vào /hooks/fake/<route>. Lượt i của bài đổi vận đơn i mod N
// (N vận đơn của carrier.json) sang bước floor(i / N) của STEPS ở hãng giả (trang quản trị, không gửi webhook), rồi tự
// gửi một webhook có chữ ký tới ingest — đo thời gian ingest trả lời. Mỗi vận đơn đi đủ 6 bước khi số lượt = 6 × N.
// eventId = lt.<run>.<tracking>.<lần đổi ở hãng giả> để carrier_run.sh nối webhook với sự kiện vận đơn CRM ghi.
// Biến môi trường (carrier_run.sh đặt): INGEST, FAKECARRIER, SECRET (bí mật webhook của hãng giả), RUN, FIXTURE (đường
// dẫn tuyệt đối carrier.json), RATE (tin/giây, mặc định 20), DURATION (mặc định 15m), SUMMARY_OUT.
import http from 'k6/http';
import crypto from 'k6/crypto';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';

const INGEST = __ENV.INGEST || 'http://127.0.0.1:4291';
const FAKECARRIER = __ENV.FAKECARRIER || 'http://127.0.0.1:4294';
const SECRET = __ENV.SECRET || '';
const RUN = __ENV.RUN || 'c0';
const FIXTURE = JSON.parse(open(__ENV.FIXTURE));
const RATE = parseInt(__ENV.RATE || '20', 10);
const DURATION = __ENV.DURATION || '15m';

// STEPS: trạng thái thô của hãng giả theo thứ tự; lần đổi thứ k (0-based) có số lần đổi k + 2 ở hãng giả (lần 1 là
// created lúc tạo kiện), khóa sự kiện CRM ghi là "<trạng thái>:<k + 2>".
export const STEPS = ['picked_up', 'in_transit', 'out_for_delivery', 'delivery_failed', 'out_for_delivery', 'delivered'];

const webhookMs = new Trend('webhook_ms', true);
const webhookOK = new Counter('webhook_ok');
const webhookBad = new Counter('webhook_bad');
const advanceBad = new Counter('advance_bad');

export const options = {
  scenarios: {
    carrier: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 40,
      maxVUs: 200,
    },
  },
  thresholds: {
    webhook_ms: ['p(95)<300'],
    webhook_bad: ['count==0'],
    advance_bad: ['count==0'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max', 'count'],
};

export default function () {
  const n = FIXTURE.trackings.length;
  const i = exec.scenario.iterationInTest;
  const step = Math.floor(i / n);
  if (step >= STEPS.length) {
    return; // bài dài hơn 6 × N lượt: vận đơn đã đi hết các bước
  }
  const tracking = FIXTURE.trackings[i % n];
  const status = STEPS[step];
  const adv = http.post(`${FAKECARRIER}/admin/parcels/${tracking}/advance`, JSON.stringify({ to: status }), {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'advance' },
    timeout: '10s',
  });
  if (adv.status !== 200) {
    advanceBad.add(1);
    return;
  }
  const sent = Date.now();
  const body = JSON.stringify({
    events: [{
      eventId: `lt.${RUN}.${tracking}.${step + 2}`,
      accountKey: 'fake-1',
      tracking,
      status,
      sentAt: new Date(sent).toISOString(),
    }],
  });
  const sig = crypto.hmac('sha256', SECRET, body, 'hex');
  const res = http.post(`${INGEST}/hooks/fake/${FIXTURE.routeKey}`, body, {
    headers: { 'Content-Type': 'application/json', 'X-Fake-Signature': `sha256=${sig}` },
    tags: { name: 'webhook' },
    timeout: '10s',
  });
  webhookMs.add(res.timings.duration);
  const ok = res.status === 200;
  webhookOK.add(ok ? 1 : 0);
  webhookBad.add(ok ? 0 : 1);
}

export function handleSummary(data) {
  const out = {};
  if (__ENV.SUMMARY_OUT) {
    out[__ENV.SUMMARY_OUT] = JSON.stringify(data);
  }
  out.stdout = `webhook p95 ${data.metrics.webhook_ms ? data.metrics.webhook_ms.values['p(95)'] : 'n/a'} ms\n`;
  return out;
}
