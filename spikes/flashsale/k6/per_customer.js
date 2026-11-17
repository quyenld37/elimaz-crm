// Một khách gửi 100 yêu cầu song song, mỗi yêu cầu một khóa. Máy chủ: serve -limit 2.
// Mong đợi: đúng 2 bought, 98 over_limit (verify -limit 2).
import http from 'k6/http';
import { sleep } from 'k6';
import { buyRequest, record, uuidFor, summaryTrendStats } from './common.js';

// batch, batchPerHost: mặc định k6 chỉ gửi 6 yêu cầu song song mỗi máy; nâng lên 100 để đúng "100 song song".
export const options = { vus: 1, iterations: 1, batch: 100, batchPerHost: 100, summaryTrendStats };

export default function () {
  const customer = uuidFor(424242);
  let pending = [];
  for (let i = 0; i < 100; i++) {
    pending.push(buyRequest(`per-customer-${i}`, customer, 1));
  }
  for (let round = 0; round < 40 && pending.length > 0; round++) {
    const responses = http.batch(pending);
    pending = pending.filter((_, i) => !record(responses[i]));
    if (pending.length > 0) {
      sleep(0.5);
    }
  }
  if (pending.length > 0) {
    throw new Error(`${pending.length} yêu cầu vẫn nhận retry sau 40 vòng`);
  }
}
