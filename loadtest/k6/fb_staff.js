// Tải nhân viên của bài thử tải M4 (kế hoạch M4 Task 180), chạy cùng fb_live.js: 20 nhân viên đọc danh sách mỗi 20 s
// và mở một hội thoại; 10 nhân viên gửi một tin mỗi 10 s; 5 nhân viên chốt một comment live mỗi 30 s. 50 client SSE do
// loadkit sse giữ. Thẻ: đọc dùng nhân viên 1–20, gửi 21–30, chốt 31–35 (mỗi nhân viên dưới giới hạn 300 yêu cầu / 60 s).
import exec from 'k6/execution';
import { sleep } from 'k6';
import { claimOne, duration, readConversations, sendOne, staffToken, summary } from './fb_common.js';

const d = duration('15m');

export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max', 'count'],
  scenarios: {
    readers: { executor: 'constant-vus', exec: 'reader', vus: 20, duration: d },
    senders: { executor: 'constant-vus', exec: 'sender', vus: 10, duration: d },
    claimers: { executor: 'constant-vus', exec: 'claimer', vus: 5, duration: d },
  },
  thresholds: {
    read_ms: ['p(95)<200', 'p(99)<500'],
  },
};

// every chạy fn rồi ngủ cho đủ chu kỳ period giây (lượt đầu lệch ngẫu nhiên để các nhân viên không bấm cùng lúc).
function every(period, fn) {
  if (exec.vu.iterationInScenario === 0) {
    sleep(Math.random() * period);
  }
  const t0 = Date.now();
  fn();
  sleep(Math.max(0, period - (Date.now() - t0) / 1000));
}

// staff: thẻ của nhân viên offset + (mã VU mod n). Mã VU chung cho mọi kịch bản nên hai VU cùng kịch bản có thể trùng
// nhân viên: vẫn dưới giới hạn tần suất (nặng nhất 4 yêu cầu / 10 s).
function staff(offset, n) {
  return staffToken(offset + ((exec.vu.idInTest - 1) % n));
}

export function reader() {
  every(20, () => readConversations(staff(0, 20)));
}

export function sender() {
  every(10, () => sendOne(staff(20, 10)));
}

export function claimer() {
  every(30, () => claimOne(staff(30, 5)));
}

export function handleSummary(data) {
  return summary(data, ['read_ms', 'read_bad', 'send_api_ms', 'send_ok', 'send_bad', 'claim_ms', 'claim_ok',
    'claim_conflict', 'claim_bad']);
}
