// Lên đơn 10× (kế hoạch M2 Task 88; tải 10× của 04 mục 1): 2 đơn mỗi giây trong 5 phút cộng một đợt 20 đơn mỗi giây
// trong 30 giây, 30% dùng mã giảm giá, 10% tranh 5 biến thể ít hàng; song song danh sách đơn trang đầu 10 lượt mỗi
// giây. Ngưỡng: createOrder p95 < 400 ms, listOrders p95 < 200 ms. Từ chối nghiệp vụ (409 out_of_stock, 422 mã
// hết lượt/không hợp lệ) không phải lỗi; lỗi là mã khác. Không bán vượt, used_count, deadlock, tx_retry_exhausted:
// loadkit m2stats sau bài.
import http from 'k6/http';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';
import { auth, duration, pick, summary, uuid4, API } from './fb_common.js';
import { M2, phoneDigits, randInt, SAMPLE, token } from './m2_common.js';

const d = duration('5m');
// BURST_AT: lúc bắt đầu đợt 20 đơn/giây (mặc định giữa bài 5 phút); chạy khói ngắn thì m2_run.sh đặt 0s.
const burstAt = __ENV.BURST_AT || '2m';
const burstFor = __ENV.BURST_FOR || '30s';

export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max', 'count'],
  scenarios: {
    steady: { executor: 'constant-arrival-rate', exec: 'create', rate: 2, timeUnit: '1s', duration: d, preAllocatedVUs: 10, maxVUs: 100 },
    burst: {
      executor: 'constant-arrival-rate', exec: 'create', rate: 20, timeUnit: '1s', duration: burstFor, startTime: burstAt,
      preAllocatedVUs: 40, maxVUs: 300,
    },
    list: { executor: 'constant-arrival-rate', exec: 'list', rate: 10, timeUnit: '1s', duration: d, preAllocatedVUs: 10, maxVUs: 100 },
  },
  thresholds: { create_ms: ['p(95)<400'], list_ms: ['p(95)<200'] },
};

const createMs = new Trend('create_ms', true);
const listMs = new Trend('list_ms', true);
const createReq = new Counter('create_req');
const createOK = new Counter('create_ok');
const createOOS = new Counter('create_oos');
const createCoupon = new Counter('create_coupon_rejected');
const createBad = new Counter('create_bad');
const listReq = new Counter('list_req');
const listBad = new Counter('list_bad');

let badLogged = 0; // mỗi VU

// newPhoneIndex: SĐT mới của lượt (ngoài dải đã nạp, không trùng giữa hai kịch bản lên đơn).
function newPhoneIndex() {
  const base = exec.scenario.name === 'burst' ? 4000000 : 2000000;
  return base + exec.scenario.iterationInTest;
}

function body() {
  const phone = '0' + phoneDigits(newPhoneIndex());
  const b = {
    intent: 'confirmed', shippingFee: 30000,
    shipAddress: {
      name: 'Khách thử tải', phone, line: 'Số 1 đường Thử', provinceCode: M2.provinceCode, wardCode: M2.wardCode,
    },
  };
  if (randInt(10) === 0) {
    b.items = [{ variantId: pick(M2.lowVariants), qty: 1 }];
  } else {
    const first = randInt(M2.variants.length);
    b.items = [{ variantId: M2.variants[first], qty: 1 + randInt(2) }];
    if (randInt(2) === 0) {
      b.items.push({ variantId: M2.variants[(first + 1) % M2.variants.length], qty: 1 });
    }
  }
  if (randInt(2) === 0 && SAMPLE.customerIds.length > 0) {
    b.customerId = pick(SAMPLE.customerIds);
  } else {
    b.customer = { fullName: `Khách thử tải ${newPhoneIndex()}`, phone, phoneIsCustomers: true };
  }
  if (randInt(10) < 3) {
    b.couponCode = pick(M2.coupons).code;
  }
  return b;
}

export function create() {
  const res = http.post(`${API}/api/v1/orders`, JSON.stringify(body()), {
    headers: auth(token(), { 'Content-Type': 'application/json', 'Idempotency-Key': uuid4() }),
    tags: { name: 'create_order' }, timeout: '10s',
  });
  createMs.add(res.timings.duration);
  createReq.add(1);
  let code = '';
  try {
    code = res.json('error') || '';
  } catch (e) {
    code = '';
  }
  if (res.status === 201) {
    createOK.add(1);
  } else if (res.status === 409 && (code === 'out_of_stock' || code === 'insufficient_stock')) {
    createOOS.add(1);
  } else if (res.status === 422 && code.startsWith('coupon_')) {
    createCoupon.add(1);
  } else {
    createBad.add(1, { status: String(res.status), code });
    if (badLogged < 5) { // vài dòng đầu vào k6-orders.log (mã lỗi, request_id: không có dữ liệu khách)
      badLogged++;
      console.error(`createOrder ${res.status} ${res.body}`);
    }
  }
}

export function list() {
  const res = http.get(`${API}/api/v1/orders?limit=50`, { headers: auth(token()), tags: { name: 'list_orders' }, timeout: '10s' });
  listMs.add(res.timings.duration);
  listReq.add(1);
  if (res.status !== 200) {
    listBad.add(1, { status: String(res.status) });
  }
}

export function handleSummary(data) {
  return summary(data, ['create_ms', 'list_ms', 'create_req', 'create_ok', 'create_oos', 'create_coupon_rejected',
    'create_bad', 'list_req', 'list_bad']);
}
