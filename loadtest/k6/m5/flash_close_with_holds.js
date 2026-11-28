// FS17: 200 người mua (giữ suất) ngay khi mở; ở phút 1 nhân viên đóng đợt khi 200 suất đang giữ; sau đó người số chẵn
// hoàn tất, người số lẻ để hết hạn giữ (m5_run.sh chờ hết hạn rồi mới chạy verify-flash). Đạt: m5report -mode
// flash_close_with_holds (kế hoạch M5 Task 219).
import exec from 'k6/execution';
import { sleep } from 'k6';
import { Counter } from 'k6/metrics';
import { SALE, TREND_STATS, buy, complete, externalRef, msUntilOpen, staffPost, summary } from './common.js';

const BUYERS = parseInt(__ENV.BUYERS || '200', 10);
const closeOK = new Counter('close_ok');

export const options = {
  summaryTrendStats: TREND_STATS,
  setupTimeout: '120s',
  scenarios: {
    hold: { executor: 'shared-iterations', exec: 'hold', vus: Math.min(100, BUYERS), iterations: BUYERS, maxDuration: '55s' },
    close: { executor: 'shared-iterations', exec: 'closeSale', vus: 1, iterations: 1, startTime: '60s' },
    finish: {
      executor: 'shared-iterations', exec: 'finish', vus: Math.min(50, BUYERS), iterations: BUYERS, startTime: '70s', maxDuration: '2m',
    },
  },
};

export function setup() {
  sleep(Math.max(0, msUntilOpen()) / 1000);
}

export function hold() {
  const n = 400000 + exec.scenario.iterationInTest;
  buy(n, externalRef('h', n), { retry429: true });
}

export function closeSale() {
  if (staffPost(`/api/v1/flash-sales/${SALE.saleId}/close`, 200)) {
    closeOK.add(1);
  }
}

export function finish() {
  const n = 400000 + exec.scenario.iterationInTest;
  if (n % 2 === 0) {
    complete(n, externalRef('h', n));
  }
}

export const handleSummary = summary;
