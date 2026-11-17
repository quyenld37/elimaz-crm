// Bán đều: 50 lượt mua mỗi giây trong 5 phút trên 15.000 suất, mỗi lượt một khách mới, qty 1.
// Máy chủ: serve -limit 1. Nạp: setup -units 15000. Mong đợi: 15.000 bought, verify đạt.
import exec from 'k6/execution';
import { buyUntilFinal, uuidFor, summaryTrendStats } from './common.js';

export const options = {
  scenarios: {
    sustained: {
      executor: 'constant-arrival-rate',
      rate: 50,
      timeUnit: '1s',
      duration: '5m',
      preAllocatedVUs: 100,
      maxVUs: 500,
    },
  },
  summaryTrendStats,
};

export default function () {
  const n = exec.scenario.iterationInTest;
  buyUntilFinal(`sustained-${n}`, uuidFor(1000000 + n), 1);
}
