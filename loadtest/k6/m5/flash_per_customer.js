// FS05: một SĐT, 100 yêu cầu song song (mỗi yêu cầu một mã đơn), giới hạn 1 → đúng 1 bought (kế hoạch M5 Task 219).
// 429 (giới hạn 10 lượt/10 s của SĐT) là kết quả cuối, không gửi lại.
import exec from 'k6/execution';
import { sleep } from 'k6';
import { TREND_STATS, buy, externalRef, msUntilOpen, summary } from './common.js';

const PERSON = 4242;

export const options = {
  summaryTrendStats: TREND_STATS,
  setupTimeout: '120s',
  scenarios: {
    burst: { executor: 'per-vu-iterations', exec: 'click', vus: 100, iterations: 1, maxDuration: '2m' },
  },
};

export function setup() {
  sleep(Math.max(0, msUntilOpen()) / 1000);
}

export function click() {
  buy(PERSON, externalRef('p', exec.vu.idInTest));
}

export const handleSummary = summary;
