// Phần chung của bộ thử tải M4 (kế hoạch M4 Task 180, lặp lại HT17 cho ca live, ADR-42). fb_live.js tạo tải khách
// (tin hộp thư, comment live qua hai đường), fb_staff.js tạo tải nhân viên (đọc, gửi, chốt). fb_run.sh đặt biến:
//   INGEST, API (gốc ingest, api), GRAPH (gốc máy Meta giả tools/fakegraph), SECRET (META_APP_SECRET, ký webhook),
//   RUN (mã lượt), TOKENS (tokens.json của loadkit access), FIXTURE (fb.json của loadkit fbseed), PSIDS (số PSID),
//   SUMMARY_OUT, DURATION (ghi đè thời lượng).
// Mã sự kiện mang lúc gửi để loadkit tính độ trễ tới báo hiệu (cùng một máy nên cùng đồng hồ với máy giả):
//   tin hộp thư  m_<run>_<ms gửi>_<VU>_<lượt>     (mid)
//   comment live lc_<run>_<ms gửi>_<VU>_<lượt>    (comment_id; cùng mã ở cả hai đường khi gửi trùng)
//   tin nhân viên "lt <run> <ms bấm> <VU> <lượt>"  (nội dung; máy giả ghi lúc nhận Send API)
import http from 'k6/http';
import crypto from 'k6/crypto';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';

export const INGEST = __ENV.INGEST || 'http://127.0.0.1:4291';
export const API = __ENV.API || 'http://127.0.0.1:4290';
export const GRAPH = __ENV.GRAPH || 'http://127.0.0.1:4293';
const SECRET = __ENV.SECRET || '';
export const RUN = __ENV.RUN || 'r0';
export const TOKENS = __ENV.TOKENS ? JSON.parse(open(__ENV.TOKENS)) : [];
export const FIXTURE = __ENV.FIXTURE ? JSON.parse(open(__ENV.FIXTURE)) : { pages: [], variants: [] };
const PSIDS = parseInt(__ENV.PSIDS || '2000', 10);
// psidBase: PSID thứ i là psidBase + i (tools/fakegraph, FAKEGRAPH_PSIDS có hồ sơ cho 1…PSIDS).
const PSID_BASE = 7000000000000000;

export function duration(d) {
  return __ENV.DURATION || d;
}

export function pick(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

export function psid() {
  return String(PSID_BASE + 1 + Math.floor(Math.random() * PSIDS));
}

export function eventID(prefix, sentMs) {
  return `${prefix}_${RUN}_${sentMs}_${exec.vu.idInTest}_${exec.vu.iterationInScenario}`;
}

// uuid4 cho Idempotency-Key (không cần ngẫu nhiên mật mã).
export function uuid4() {
  const h = '0123456789abcdef';
  let s = '';
  for (let i = 0; i < 36; i++) {
    if (i === 8 || i === 13 || i === 18 || i === 23) {
      s += '-';
    } else if (i === 14) {
      s += '4';
    } else if (i === 19) {
      s += h[8 + Math.floor(Math.random() * 4)];
    } else {
      s += h[Math.floor(Math.random() * 16)];
    }
  }
  return s;
}

// ---- Tải khách: webhook có chữ ký, comment qua luồng SSE của máy giả.

const webhookMs = new Trend('webhook_ms', true);
const webhookOK = new Counter('webhook_ok');
const webhookBad = new Counter('webhook_bad');
const graphPushBad = new Counter('graph_push_bad');
export const inboxSent = new Counter('inbox_sent'); // tin hộp thư khác nhau đã được ingest nhận (200)
export const inboxDup = new Counter('inbox_dup'); // lần gửi trùng cùng lúc (HT03)
export const commentSent = new Counter('comment_sent'); // comment live khác nhau đã giao (ingest 200 hay máy giả 200)
export const commentBoth = new Counter('comment_both'); // comment gửi cả hai đường

function signed(body) {
  return {
    headers: {
      'Content-Type': 'application/json',
      'X-Hub-Signature-256': `sha256=${crypto.hmac('sha256', SECRET, body, 'hex')}`,
    },
    tags: { name: 'webhook' },
    timeout: '10s',
  };
}

function track(res) {
  webhookMs.add(res.timings.duration);
  const ok = res.status === 200;
  webhookOK.add(ok ? 1 : 0);
  webhookBad.add(ok ? 0 : 1);
  return ok;
}

export function inboxBody(page, from, mid, sentMs, text) {
  return JSON.stringify({
    object: 'page',
    entry: [{
      id: page, time: sentMs,
      messaging: [{ sender: { id: from }, recipient: { id: page }, timestamp: sentMs, message: { mid, text } }],
    }],
  });
}

export function feedBody(page, video, from, name, id, sentMs, text) {
  return JSON.stringify({
    object: 'page',
    entry: [{
      id: page, time: sentMs,
      changes: [{
        field: 'feed',
        value: {
          item: 'comment', verb: 'add', comment_id: id, post_id: video, parent_id: video,
          from: { id: from, name }, message: text, created_time: Math.floor(sentMs / 1000),
        },
      }],
    }],
  });
}

// inbox: một tin khách vào hộp thư của một Page ngẫu nhiên; dupPct % số lượt gửi cùng payload hai lần cùng lúc.
export function inbox(dupPct) {
  const page = pick(FIXTURE.pages).pageId;
  const sent = Date.now();
  const mid = eventID('m', sent);
  const body = inboxBody(page, psid(), mid, sent, `Shop ơi còn hàng không ${mid}`);
  const url = `${INGEST}/hooks/facebook`;
  let ok;
  if (Math.random() * 100 < dupPct) {
    const rs = http.batch([['POST', url, body, signed(body)], ['POST', url, body, signed(body)]]);
    ok = rs.map(track).some((x) => x);
    inboxDup.add(1);
  } else {
    ok = track(http.post(url, body, signed(body)));
  }
  if (ok) {
    inboxSent.add(1);
  }
}

// liveComment: một comment khách dưới phiên live của một Page ngẫu nhiên; lượt chẵn qua webhook feed, lẻ qua luồng
// SSE của máy giả, bothPct % qua cả hai (cùng mã, cùng nội dung: CRM phải ghi một lần).
export function liveComment(bothPct) {
  const p = pick(FIXTURE.pages);
  const sent = Date.now();
  const id = eventID('lc', sent);
  const from = psid();
  const name = `Khách ${from.slice(-4)}`;
  const text = `Chốt ${Math.floor(Math.random() * 20) + 1} ${id}`;
  const both = Math.random() * 100 < bothPct;
  const viaWebhook = exec.vu.iterationInScenario % 2 === 0;
  let ok = false;
  if (both || viaWebhook) {
    const body = feedBody(p.pageId, p.videoId, from, name, id, sent, text);
    ok = track(http.post(`${INGEST}/hooks/facebook`, body, signed(body))) || ok;
  }
  if (both || !viaWebhook) {
    const res = http.post(`${GRAPH}/_fake/live-comment`,
      JSON.stringify({ videoId: p.videoId, id, fromId: from, fromName: name, message: text }),
      { headers: { 'Content-Type': 'application/json' }, tags: { name: 'graph_push' }, timeout: '10s' });
    if (res.status === 200) {
      ok = true;
    } else {
      graphPushBad.add(1);
    }
  }
  if (both) {
    commentBoth.add(1);
  }
  if (ok) {
    commentSent.add(1);
  }
}

// ---- Tải nhân viên.

const readMs = new Trend('read_ms', true);
const readBad = new Counter('read_bad');
const sendMs = new Trend('send_api_ms', true);
const sendOK = new Counter('send_ok');
const sendBad = new Counter('send_bad');
const claimMs = new Trend('claim_ms', true);
const claimOK = new Counter('claim_ok');
const claimConflict = new Counter('claim_conflict'); // 409 nghiệp vụ: đã chốt, khách có nháp, hết hàng
const claimBad = new Counter('claim_bad');

// ORIGIN: CONSOLE_ORIGINS của api trong bài thử (fb_lib.sh); yêu cầu ghi phải mang Origin của màn hình.
const ORIGIN = __ENV.ORIGIN || 'http://localhost:4192';

export function auth(token, extra) {
  return Object.assign({ 'Cf-Access-Jwt-Assertion': token, Origin: ORIGIN }, extra || {});
}

export function staffToken(i) {
  if (TOKENS.length <= i) {
    throw new Error(`cần ít nhất ${i + 1} thẻ trong TOKENS, có ${TOKENS.length}`);
  }
  return TOKENS[i].token;
}

function getJSON(token, path, name) {
  const res = http.get(`${API}${path}`, { headers: auth(token), tags: { name }, timeout: '10s' });
  readMs.add(res.timings.duration);
  if (res.status !== 200) {
    readBad.add(1);
    return null;
  }
  try {
    return res.json();
  } catch (e) {
    readBad.add(1);
    return null;
  }
}

// readConversations: danh sách hội thoại rồi mở một hội thoại (chi tiết và tin).
export function readConversations(token) {
  const page = getJSON(token, '/api/v1/conversations?limit=50', 'list_conversations');
  if (!page || !page.items || page.items.length === 0) {
    return;
  }
  const c = pick(page.items.slice(0, 10));
  getJSON(token, `/api/v1/conversations/${c.id}`, 'get_conversation');
  getJSON(token, `/api/v1/conversations/${c.id}/messages?limit=50`, 'list_messages');
}

// sendOne: chọn một hội thoại hộp thư mới và gửi một tin (khóa chống trùng mới mỗi lần bấm).
export function sendOne(token) {
  const page = getJSON(token, '/api/v1/conversations?kind=inbox&limit=20', 'list_inbox');
  if (!page || !page.items || page.items.length === 0) {
    return;
  }
  const c = pick(page.items);
  const sent = Date.now();
  const text = `lt ${RUN} ${sent} ${exec.vu.idInTest} ${exec.vu.iterationInScenario}`;
  const res = http.post(`${API}/api/v1/conversations/${c.id}/messages`, JSON.stringify({ text }), {
    headers: auth(token, { 'Content-Type': 'application/json', 'Idempotency-Key': uuid4() }),
    tags: { name: 'send_message' }, timeout: '10s',
  });
  sendMs.add(res.timings.duration);
  if (res.status === 201) {
    sendOK.add(1);
  } else {
    sendBad.add(1, { status: String(res.status) });
  }
}

// claimOne: mở danh sách comment chưa xử lý của một phiên live và chốt một comment (một biến thể ngẫu nhiên, 1 cái).
export function claimOne(token) {
  const p = pick(FIXTURE.pages);
  const page = getJSON(token, `/api/v1/live-sessions/${p.sessionId}/comments?limit=50&unhandledOnly=true`, 'live_comments');
  if (!page || !page.items) {
    return;
  }
  const free = page.items.filter((c) => !c.claim);
  if (free.length === 0) {
    return;
  }
  const c = pick(free);
  const body = JSON.stringify({ items: [{ variantId: pick(FIXTURE.variants), qty: 1 }], mode: 'new', askAddress: true });
  const res = http.post(`${API}/api/v1/live-comments/${c.messageId}/claim`, body, {
    headers: auth(token, { 'Content-Type': 'application/json', 'Idempotency-Key': uuid4() }),
    tags: { name: 'claim' }, timeout: '10s',
  });
  claimMs.add(res.timings.duration);
  if (res.status === 201) {
    claimOK.add(1);
  } else if (res.status === 409) {
    claimConflict.add(1);
  } else {
    claimBad.add(1, { status: String(res.status) });
  }
}

// ---- Tóm tắt.

// summary ghi nguyên data.metrics ra SUMMARY_OUT cho loadkit report, và một dòng tóm tắt ra màn hình.
export function summary(data, names) {
  const m = data.metrics;
  const v = (name, stat) => (m[name] && m[name].values[stat] !== undefined ? m[name].values[stat].toFixed(1) : '-');
  const line = names.map((n) => (n.endsWith('_ms') ? `${n} p95 ${v(n, 'p(95)')} ms` : `${n} ${v(n, 'count')}`)).join('; ');
  const out = { stdout: `${line}\n` };
  if (__ENV.SUMMARY_OUT) {
    out[__ENV.SUMMARY_OUT] = JSON.stringify(m, null, 2);
  }
  return out;
}
