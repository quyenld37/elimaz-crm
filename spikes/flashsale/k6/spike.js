// Flash sale: 500 suất, 20.000 khách khác nhau, tới 1.000 lượt bấm mỗi giây trong 60 s, qty 1,
// thử lại với cùng khóa khi nhận "retry". Máy chủ: serve -limit 1. Nạp: setup -units 500.
// Không dựng 20.000 VU cùng lúc (máy 16 GB không đủ); k6 tự cấp VU theo tốc độ bấm
// (task-01, "Quyết định thay chủ shop" 7).
import exec from 'k6/execution';
import { buyUntilFinal, uuidFor, summaryTrendStats } from './common.js';

const CUSTOMERS = 20000;

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-arrival-rate',
      // START_RATE=1000: bắt đầu ngay ở 1.000 lượt/giây, để 500 suất bị tranh ở tốc độ đỉnh (lần chạy tham khảo thêm).
      startRate: Number(__ENV.START_RATE || 0),
      timeUnit: '1s',
      preAllocatedVUs: 500,
      maxVUs: 3000,
      stages: [
        { target: 1000, duration: '10s' },
        { target: 1000, duration: '50s' },
      ],
    },
  },
  summaryTrendStats,
};

export default function () {
  const n = exec.scenario.iterationInTest;
  buyUntilFinal(`spike-${n}`, uuidFor(n % CUSTOMERS), 1);
}
