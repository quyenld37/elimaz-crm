// Phần chung của bộ thử tải M5 (kế hoạch M5 Task 219 / M5-36, ADR-42): k6 gọi thẳng API web của CRM bằng khóa dịch
// vụ, giống website. Mỗi người ảo (số n) một SĐT giả 09xxxxxxxx và một IP 10.x.y.z; lượt mua gửi lại cùng
// Idempotency-Key khi 503 theo Retry-After (X-Attempt tăng dần, tối đa 10), như website (QĐ21).
// Biến môi trường (loadtest/m5_run.sh đặt): API, KEY (tệp key.json), SALE (tệp sale.json), RUN, TOKENS (tệp thẻ nhân
// viên, chỉ bài đóng đợt), SUMMARY_OUT, DURATION (ghi đè thời lượng để chạy khói).
import http from 'k6/http';
import { sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

export const API = __ENV.API || 'http://127.0.0.1:4290';
export const RUN = __ENV.RUN || 'm5';
const KEY = __ENV.KEY ? JSON.parse(open(__ENV.KEY)).secret : '';
export const SALE = __ENV.SALE ? JSON.parse(open(__ENV.SALE)) : {};
const TOKENS = __ENV.TOKENS ? JSON.parse(open(__ENV.TOKENS)) : [];
const CONSOLE_ORIGIN = 'http://localhost:4192';
const MAX_ATTEMPTS = 10;
const MAX_WAIT_S = 5;

export const TREND_STATS = ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max', 'count'];

const buyMs = new Trend('buy_ms', true);
const results = {
  bought: new Counter('buy_bought'),
  sold_out: new Counter('buy_sold_out'),
  over_limit: new Counter('buy_over_limit'),
  not_open: new Counter('buy_not_open'),
};
const buy429 = new Counter('buy_429');
const buy503 = new Counter('buy_503');
const buyErr = new Counter('buy_err');
// Lượt bấm nhận 503 cả 10 lần gửi: không phải mã lỗi khác (chỉ 503 retry_later), website báo khách thử lại.
const buyGaveUp = new Counter('buy_gave_up');
const arrival = new Trend('arrival_s');
const boughtArrival = new Trend('bought_arrival_s');
const completeMs = new Trend('complete_ms', true);
const completeOK = new Counter('complete_ok');
const completeExpired = new Counter('complete_expired');
const completeErr = new Counter('complete_err');
const readMs = new Trend('read_ms', true);
const readErr = new Counter('read_err');

// phone: SĐT giả của người n (09 + 8 chữ số; không phải số thật của ai trong testdata).
export function phone(n) {
  return `09${String(n % 100000000).padStart(8, '0')}`;
}

// ip: IP riêng của người n (10.0.0.0/8), để giới hạn theo IP không gộp mọi người ảo vào một IP.
export function ip(n) {
  return `10.${(n >> 16) & 255}.${(n >> 8) & 255}.${n & 255}`;
}

export function externalRef(tag, n) {
  return `m5-${RUN}-${tag}-${n}`;
}

function keyHeaders(extra) {
  return Object.assign({ Authorization: `Bearer ${KEY}`, 'Content-Type': 'application/json' }, extra || {});
}

// waitFor: số giây chờ theo Retry-After (thiếu hay sai → 1 s; trần 5 s để bài không treo).
function waitFor(res) {
  const s = parseFloat(res.headers['Retry-After']);
  return Number.isFinite(s) && s > 0 ? Math.min(s, MAX_WAIT_S) : 1;
}

// buy: một lượt bấm mua của người n với mã đơn ref. Trả kết quả cuối ("bought", "sold_out", "over_limit",
// "not_open", "429", "gave_up" (10 lần 503), "err"). opts.retry429 = false: 429 là kết quả cuối (mặc định), true: chờ rồi gửi lại.
export function buy(n, ref, opts) {
  const o = opts || {};
  const first = Date.now();
  if (SALE.startsAt) {
    arrival.add((first - Date.parse(SALE.startsAt)) / 1000);
  }
  const body = JSON.stringify({ externalRef: ref, phone: phone(n), qty: 1, clientIp: ip(n) });
  const url = `${API}/api/v1/web/flash-sales/${SALE.saleId}/items/${SALE.itemId}/buy`;
  const idem = `${ref}-k`;
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    const res = http.post(url, body, {
      headers: keyHeaders({ 'Idempotency-Key': idem, 'X-Attempt': String(attempt), 'Cf-Connecting-Ip': ip(n) }),
      tags: { name: 'flash_buy' },
      timeout: '15s',
    });
    buyMs.add(res.timings.duration);
    if (res.status === 200) {
      let r = '';
      try {
        r = res.json('result');
      } catch (_) {
        r = '';
      }
      if (results[r]) {
        results[r].add(1);
        if (r === 'bought' && SALE.startsAt) {
          boughtArrival.add((first - Date.parse(SALE.startsAt)) / 1000);
        }
        return r;
      }
      buyErr.add(1);
      return 'err';
    }
    if (res.status === 503) {
      buy503.add(1);
      sleep(waitFor(res));
      continue;
    }
    if (res.status === 429) {
      buy429.add(1);
      if (!o.retry429) {
        return '429';
      }
      sleep(waitFor(res));
      continue;
    }
    buyErr.add(1);
    if (__ENV.DEBUG) {
      console.warn(`buy ${res.status} ${res.body}`);
    }
    return 'err';
  }
  buyGaveUp.add(1);
  return 'gave_up';
}

// complete: hoàn tất đơn nháp ref của người n (COD, địa chỉ giả). Trả "ok", "expired" hay "err".
export function complete(n, ref) {
  const body = JSON.stringify({
    phone: phone(n),
    // Tỉnh, xã thử do loadkit m5key chèn (T01, T01001, chung với m2seed: seedAdminUnits): đơn hoàn tất cần địa chỉ đủ hai cấp.
    shipAddress: {
      name: 'Khách thử tải', phone: phone(n), line: `Số ${n} đường Thử Tải`, provinceCode: 'T01', wardCode: 'T01001',
    },
    shippingFee: 30000,
  });
  const url = `${API}/api/v1/web/flash-orders/${encodeURIComponent(ref)}/complete`;
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    const res = http.post(url, body, {
      headers: keyHeaders({ 'Idempotency-Key': `${ref}-c`, 'Cf-Connecting-Ip': ip(n) }),
      tags: { name: 'flash_complete' },
      timeout: '15s',
    });
    completeMs.add(res.timings.duration);
    if (res.status === 200) {
      completeOK.add(1);
      return 'ok';
    }
    if (res.status === 503) {
      sleep(waitFor(res));
      continue;
    }
    if (res.status === 422 && String(res.body).includes('hold_expired')) {
      completeExpired.add(1);
      return 'expired';
    }
    if (__ENV.DEBUG) {
      console.warn(`complete ${res.status} ${res.body}`);
    }
    completeErr.add(1);
    return 'err';
  }
  completeErr.add(1);
  return 'err';
}

// read: một lượt đọc của website (GET path bằng khóa dịch vụ).
export function read(path, name) {
  const res = http.get(`${API}${path}`, { headers: keyHeaders(), tags: { name }, timeout: '10s' });
  readMs.add(res.timings.duration);
  if (res.status !== 200 && res.status !== 304) {
    readErr.add(1);
    if (__ENV.DEBUG) {
      console.warn(`read ${name} ${res.status} ${res.body}`);
    }
  }
}

// staffPost: thao tác của nhân viên (thẻ Access thứ nhất của TOKENS), như loadkit apiClient.
export function staffPost(path, want) {
  const res = http.post(`${API}${path}`, null, {
    headers: {
      'Cf-Access-Jwt-Assertion': TOKENS[0].token, Origin: CONSOLE_ORIGIN, 'Content-Type': 'application/json',
      'Idempotency-Key': `${RUN}-${path.replace(/[^A-Za-z0-9_-]/g, '_')}`.slice(-128),
    },
    tags: { name: 'staff' },
  });
  if (res.status !== want) {
    console.error(`${path} → ${res.status} ${res.body}`);
  }
  return res.status === want;
}

// msUntilOpen: số mili giây tới giờ mở của đợt (âm khi đã mở).
export function msUntilOpen() {
  return Date.parse(SALE.startsAt) - Date.now();
}

// summary ghi nguyên data.metrics ra SUMMARY_OUT cho loadkit m5report, và một dòng tóm tắt ra màn hình.
export function summary(data) {
  const m = data.metrics;
  const v = (name, stat) => (m[name] && m[name].values[stat] !== undefined ? m[name].values[stat].toFixed(1) : '0');
  const line = `mua: ${v('buy_bought', 'count')} bought, ${v('buy_sold_out', 'count')} sold_out, ` +
    `${v('buy_over_limit', 'count')} over_limit, ${v('buy_429', 'count')} lần 429, ${v('buy_503', 'count')} lần 503, ` +
    `${v('buy_gave_up', 'count')} hết lượt gửi lại, ${v('buy_err', 'count')} lỗi; hoàn tất ${v('complete_ok', 'count')}; đọc p95 ${v('read_ms', 'p(95)')} ms, ` +
    `${v('read_err', 'count')} lỗi\n`;
  const out = { stdout: line };
  if (__ENV.SUMMARY_OUT) {
    out[__ENV.SUMMARY_OUT] = JSON.stringify(m, null, 2);
  }
  return out;
}
