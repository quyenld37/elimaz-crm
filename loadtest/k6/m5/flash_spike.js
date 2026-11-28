// FS03: 20.000 người ảo, 500 suất, lên 1.000 lượt bấm mua mỗi giây trong 10 s rồi giữ 60 s (kế hoạch M5 Task 219).
// Người ảo là số n của lượt bấm (lượt i → người i mod 20.000); k6 dùng ramping-arrival-rate (lượt bấm mỗi giây)
// thay cho 20.000 VU thật để máy chạy k6 không thành bên chạm trần trước. Đạt: m5report -mode flash_spike.
import exec from 'k6/execution';
import { sleep } from 'k6';
import { TREND_STATS, buy, externalRef, msUntilOpen, summary } from './common.js';

const PEOPLE = 20000;
const PEAK = parseInt(__ENV.PEAK || '1000', 10);
const HOLD = __ENV.DURATION || '60s';

export const options = {
  summaryTrendStats: TREND_STATS,
  setupTimeout: '120s',
  scenarios: {
    spike: {
      executor: 'ramping-arrival-rate', exec: 'click', startRate: 0, timeUnit: '1s',
      preAllocatedVUs: 1000, maxVUs: 6000,
      stages: [{ target: PEAK, duration: '10s' }, { target: PEAK, duration: HOLD }],
    },
  },
};

export function setup() {
  sleep(Math.max(0, msUntilOpen()) / 1000); // bấm từ đúng giờ mở
}

export function click() {
  const i = exec.scenario.iterationInTest;
  buy(i % PEOPLE, externalRef('s', i));
}

export const handleSummary = summary;
