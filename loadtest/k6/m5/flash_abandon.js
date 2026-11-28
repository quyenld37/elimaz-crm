// FS06: 500 suất, hạn giữ 5 phút; đợt một 500 người mỗi người mua một suất, người số chẵn hoàn tất, người số lẻ bỏ đi.
// Sau hạn giữ (cộng nhịp hủy đơn nháp 15 s), đợt hai đúng (số người bỏ đi) người mới mua lại: mọi người đợt hai phải
// mua được — một sold_out ở đợt hai là suất "hết oan". Đạt: m5report -mode flash_abandon (kế hoạch M5 Task 219).
import exec from 'k6/execution';
import { sleep } from 'k6';
import { Counter } from 'k6/metrics';
import { SALE, TREND_STATS, buy, complete, externalRef, msUntilOpen, summary } from './common.js';

const SLOTS = SALE.slots || 500;
const ABANDONED = SLOTS - Math.floor((SLOTS + 1) / 2); // người số lẻ trong 0..SLOTS-1
const WAVE2_AT = __ENV.WAVE2_AT || '6m'; // hạn giữ 5 phút + nhịp hủy + lề
const wave2Bought = new Counter('wave2_bought');
const wave2SoldOut = new Counter('wave2_sold_out_early');

export const options = {
  summaryTrendStats: TREND_STATS,
  setupTimeout: '120s',
  scenarios: {
    wave1: {
      executor: 'shared-iterations', exec: 'first', vus: Math.min(100, SLOTS), iterations: SLOTS, maxDuration: '3m',
    },
    wave2: {
      executor: 'shared-iterations', exec: 'second', vus: Math.max(1, Math.min(50, ABANDONED)), iterations: Math.max(1, ABANDONED), startTime: WAVE2_AT,
      maxDuration: '3m',
    },
  },
};

export function setup() {
  sleep(Math.max(0, msUntilOpen()) / 1000);
}

export function first() {
  const n = 200000 + exec.scenario.iterationInTest;
  const ref = externalRef('a1', n);
  if (buy(n, ref, { retry429: true }) === 'bought' && n % 2 === 0) {
    complete(n, ref);
  }
}

export function second() {
  const n = 300000 + exec.scenario.iterationInTest;
  const r = buy(n, externalRef('a2', n), { retry429: true });
  if (r === 'bought') {
    wave2Bought.add(1);
  } else if (r === 'sold_out') {
    wave2SoldOut.add(1);
  }
}

export const handleSummary = summary;
