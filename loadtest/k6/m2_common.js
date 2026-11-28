// Phần chung của bộ thử tải M2 (kế hoạch M2 Task 88, M2-31): m2-search.js (tìm khách trên 1 triệu khách) và
// m2-orders.js (lên đơn 10×). m2_run.sh đặt biến: API, TOKENS (tokens.json của loadkit access), SAMPLE (m2-sample.json
// của tools/seedperf: công thức SĐT, mẫu mã khách, mã, tên), M2_FIXTURE (m2.json của loadkit m2seed: biến thể, mã giảm
// giá), SUMMARY_OUT, DURATION. Dùng lại thẻ, Origin, tóm tắt của bộ M4 (fb_common.js).
import exec from 'k6/execution';
import { staffToken, TOKENS } from './fb_common.js';

export const SAMPLE = __ENV.SAMPLE ? JSON.parse(open(__ENV.SAMPLE)) : { phones: 1, customerIds: [], codes: [], names: [] };
export const M2 = __ENV.M2_FIXTURE ? JSON.parse(open(__ENV.M2_FIXTURE)) : { variants: [], lowVariants: [], coupons: [] };

// phoneDigits: 9 chữ số sau +84 của SĐT giả thứ k — đúng công thức của tools/seedperf (fakePhone): "90" + 7 chữ số của
// (k·phoneMul + phoneAdd) mod phoneSpace. SĐT thứ k < SAMPLE.phones thuộc một khách đã nạp; k ≥ 2.000.000 là số mới.
export function phoneDigits(k) {
  const n = (k * SAMPLE.phoneMul + SAMPLE.phoneAdd) % SAMPLE.phoneSpace;
  return SAMPLE.phonePrefix.slice(3) + String(n).padStart(7, '0');
}

// token: nhân viên theo lượt (xoay vòng 50 nhân viên: mỗi người dưới giới hạn 300 yêu cầu / 60 s của api).
export function token() {
  return staffToken(exec.scenario.iterationInTest % TOKENS.length);
}

export function randInt(n) {
  return Math.floor(Math.random() * n);
}
