// Tải khách 10× ca live (kế hoạch M4 Task 180; 04 mục 1: đỉnh 100 sự kiện mỗi giây, 15 phút): 70% comment live (một
// nửa qua webhook feed, một nửa qua luồng SSE của máy giả, COMMENT_BOTH_PCT % qua cả hai), 30% tin hộp thư
// (INBOX_DUP_PCT % gửi trùng cùng lúc, HT03). Chạy cùng fb_staff.js (fb_run.sh).
import { duration, inbox, liveComment, summary } from './fb_common.js';

const RATE = parseInt(__ENV.RATE || '100', 10);
const BOTH = parseFloat(__ENV.COMMENT_BOTH_PCT || '10');
const DUP = parseFloat(__ENV.INBOX_DUP_PCT || '5');
const d = duration('15m');

export const options = {
  discardResponseBodies: true,
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max', 'count'],
  scenarios: {
    comments: {
      executor: 'constant-arrival-rate', exec: 'comments', rate: Math.round(RATE * 0.7), timeUnit: '1s', duration: d,
      preAllocatedVUs: 50, maxVUs: 500,
    },
    inbox: {
      executor: 'constant-arrival-rate', exec: 'messages', rate: Math.round(RATE * 0.3), timeUnit: '1s', duration: d,
      preAllocatedVUs: 20, maxVUs: 300,
    },
  },
  thresholds: {
    webhook_ms: ['p(95)<300'],
    webhook_bad: ['count==0'],
    graph_push_bad: ['count==0'],
  },
};

export function comments() {
  liveComment(BOTH);
}

export function messages() {
  inbox(DUP);
}

export function handleSummary(data) {
  return summary(data, ['webhook_ok', 'webhook_bad', 'webhook_ms', 'inbox_sent', 'inbox_dup', 'comment_sent', 'comment_both']);
}
