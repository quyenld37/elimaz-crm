// FS02, FS13: 500 yêu cầu mỗi giây vào catalog/products?ids, availability, flash-sales/current trong 10 phút (chia
// đều ba đường). Đạt: m5report -mode web_read (p95 < 200 ms, p99 < 500 ms, trúng bộ đệm > 95%) (kế hoạch M5 Task 219).
import exec from 'k6/execution';
import { SALE, TREND_STATS, read, summary } from './common.js';

const RATE = parseInt(__ENV.RATE || '500', 10);
const PRODUCTS = SALE.catalogProducts || [];
const VARIANTS = SALE.catalogVariants || [];
const PAGE = 10; // website hỏi theo trang danh mục: 10 mẫu, 10 biến thể mỗi lần

export const options = {
  summaryTrendStats: TREND_STATS,
  scenarios: {
    reads: {
      executor: 'constant-arrival-rate', exec: 'reads', rate: RATE, timeUnit: '1s', duration: __ENV.DURATION || '10m',
      preAllocatedVUs: 100, maxVUs: 1000,
    },
  },
};

// slice: trang thứ k (vòng) của danh sách xs.
function slice(xs, k) {
  const out = [];
  for (let j = 0; j < Math.min(PAGE, xs.length); j++) {
    out.push(xs[(k * PAGE + j) % xs.length]);
  }
  return out.join(',');
}

export function reads() {
  const i = exec.scenario.iterationInTest;
  const k = Math.floor(i / 3);
  switch (i % 3) {
    case 0:
      read(`/api/v1/web/catalog/products?ids=${slice(PRODUCTS, k)}`, 'web_products');
      break;
    case 1:
      read(`/api/v1/web/availability?variantIds=${slice(VARIANTS, k)}`, 'web_availability');
      break;
    default:
      read('/api/v1/web/flash-sales/current', 'web_flash_current');
  }
}

export const handleSummary = summary;
