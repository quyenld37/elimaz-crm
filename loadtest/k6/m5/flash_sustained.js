// FS16: 15.000 suất, 50 lượt mua thành công mỗi giây trong 5 phút; mỗi người mua rồi hoàn tất đơn ngay (đơn new sinh
// sự kiện order.created cho hàng đợi). Đạt: m5report -mode flash_sustained (kế hoạch M5 Task 219).
import exec from 'k6/execution';
import { sleep } from 'k6';
import { TREND_STATS, buy, complete, externalRef, msUntilOpen, summary } from './common.js';

const RATE = parseInt(__ENV.RATE || '50', 10);

export const options = {
  summaryTrendStats: TREND_STATS,
  setupTimeout: '120s',
  scenarios: {
    sustained: {
      executor: 'constant-arrival-rate', exec: 'order', rate: RATE, timeUnit: '1s',
      duration: __ENV.DURATION || '5m', preAllocatedVUs: 100, maxVUs: 1000,
    },
  },
};

export function setup() {
  sleep(Math.max(0, msUntilOpen()) / 1000);
}

export function order() {
  const n = 100000 + exec.scenario.iterationInTest; // mỗi lượt một người mới
  const ref = externalRef('u', n);
  if (buy(n, ref, { retry429: true }) === 'bought') {
    complete(n, ref);
  }
}

export const handleSummary = summary;
