// Lớp realtime của màn hình (spec 04 mục 6, kế hoạch M0 Task 17). SSE chỉ báo hiệu "đối tượng X vừa đổi"; dữ liệu
// luôn lấy lại bằng API thường. Không dùng EventSource vì nó không cho biết 401, chuyển hướng đăng nhập (phiên
// Cloudflare hết hạn) và không canh im lặng: lớp này đọc luồng bằng fetch với redirect: "manual".
//
// - 200: "connected"; khung "resync" và mỗi refetchEveryMs (lưới an toàn 20 s) → onResync(); khung "changed" →
//   onChanged() (sai dạng → onResync() cho chắc).
// - 401, 403, 3xx, opaqueredirect: "session_expired", dừng hẳn (tải lại trang mới đăng nhập lại được).
// - 5xx, mã khác, lỗi mạng, máy chủ đóng luồng, im lặng quá watchdogMs: "reconnecting", nối lại theo backoffMs
//   (hết danh sách thì giữ số cuối; nối được thì bắt đầu lại từ đầu).

export type RealtimeState = "connected" | "reconnecting" | "session_expired";

export interface ChangedEvent {
  topic: string;
  entity: { type: string; id: string };
  scope?: unknown;
}

export interface RealtimeOptions {
  url: string;
  fetchImpl?: typeof fetch;
  onChanged(e: ChangedEvent): void;
  onResync(): void;
  onState(s: RealtimeState): void;
  /** Không nhận byte nào trong khoảng này thì hủy và nối lại. Mặc định 60 s (nhịp tim máy chủ 20 s). */
  watchdogMs?: number;
  /** Thời gian chờ trước mỗi lần nối lại. Mặc định [1000, 2000, 5000, 10000]. */
  backoffMs?: number[];
  /** Khi đang nối, gọi onResync() theo chu kỳ này dù không có báo hiệu. Mặc định 20 s. */
  refetchEveryMs?: number;
}

export interface RealtimeClient {
  close(): void;
}

const DEFAULT_BACKOFF = [1000, 2000, 5000, 10000];

export function createRealtimeClient(opts: RealtimeOptions): RealtimeClient {
  const doFetch = opts.fetchImpl ?? globalThis.fetch.bind(globalThis);
  const watchdogMs = opts.watchdogMs ?? 60_000;
  const backoff = opts.backoffMs !== undefined && opts.backoffMs.length > 0 ? opts.backoffMs : DEFAULT_BACKOFF;
  const refetchEveryMs = opts.refetchEveryMs ?? 20_000;

  let closed = false;
  let attempt = 0;
  let abort: AbortController | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let watchdog: ReturnType<typeof setTimeout> | undefined;
  let refetch: ReturnType<typeof setInterval> | undefined;

  const stopTimers = () => {
    clearTimeout(watchdog);
    clearInterval(refetch);
    watchdog = undefined;
    refetch = undefined;
  };

  // Kết nối `mine` còn là kết nối hiện hành và client chưa đóng (gọi lại của kết nối cũ thì bỏ qua).
  const live = (mine: AbortController) => !closed && mine === abort;

  const emit = (s: RealtimeState) => {
    if (!closed) opts.onState(s);
  };

  const scheduleReconnect = (mine: AbortController) => {
    if (!live(mine)) return;
    stopTimers();
    // Tách kết nối cũ trước khi hủy: lỗi do chính lần hủy này (fetch, reader) không được nối lại lần nữa.
    abort = null;
    mine.abort();
    emit("reconnecting");
    const wait = backoff[Math.min(attempt, backoff.length - 1)] ?? 0;
    attempt++;
    retryTimer = setTimeout(connect, wait);
  };

  const stopForGood = (mine: AbortController) => {
    if (!live(mine)) return;
    stopTimers();
    abort = null;
    mine.abort();
    emit("session_expired");
    closed = true;
  };

  const armWatchdog = (mine: AbortController) => {
    clearTimeout(watchdog);
    watchdog = setTimeout(() => {
      scheduleReconnect(mine);
    }, watchdogMs);
  };

  function connect() {
    if (closed) return;
    const mine = new AbortController();
    abort = mine;
    void run(mine);
  }

  async function run(mine: AbortController) {
    let res: Response;
    // Canh im lặng cả lúc chờ header: cổng vào treo yêu cầu cũng phải nối lại.
    armWatchdog(mine);
    try {
      res = await doFetch(opts.url, {
        method: "GET",
        headers: { Accept: "text/event-stream" },
        redirect: "manual",
        credentials: "same-origin",
        cache: "no-store",
        signal: mine.signal,
      });
    } catch {
      scheduleReconnect(mine);
      return;
    }
    if (!live(mine)) return;
    if (
      res.type === "opaqueredirect" ||
      res.status === 401 ||
      res.status === 403 ||
      (res.status >= 300 && res.status < 400)
    ) {
      stopForGood(mine);
      return;
    }
    if (res.status !== 200 || res.body === null) {
      void res.body?.cancel().catch(() => undefined);
      scheduleReconnect(mine);
      return;
    }

    attempt = 0;
    emit("connected");
    armWatchdog(mine);
    refetch = setInterval(() => {
      if (live(mine)) opts.onResync();
    }, refetchEveryMs);

    const parser = new SSEParser((event, data) => {
      if (!live(mine)) return;
      if (event === "resync") opts.onResync();
      else if (event === "changed") {
        const e = parseChanged(data);
        if (e === null) opts.onResync();
        else opts.onChanged(e);
      }
    });
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done || !live(mine)) break;
        armWatchdog(mine);
        parser.push(value);
      }
    } catch {
      // Bị hủy (watchdog, close) hoặc mạng đứt: xử lý chung ở dưới.
    }
    scheduleReconnect(mine);
  }

  connect();

  return {
    close() {
      closed = true;
      stopTimers();
      clearTimeout(retryTimer);
      abort?.abort();
    },
  };
}

// SSEParser đọc khung SSE theo chuẩn WHATWG: dòng kết thúc bằng \n, \r\n hoặc \r; dòng trống kết thúc một khung;
// dòng bắt đầu bằng ":" là chú thích (nhịp tim); nhiều dòng data nối bằng \n.
class SSEParser {
  private buf = "";
  private event = "";
  private data: string[] = [];
  private pendingCR = false;

  constructor(private readonly dispatch: (event: string, data: string) => void) {}

  push(chunk: string) {
    let text = chunk;
    if (this.pendingCR && text.startsWith("\n")) text = text.slice(1);
    this.pendingCR = false;
    this.buf += text;
    for (;;) {
      const m = /\r\n|\r|\n/.exec(this.buf);
      if (m === null) break;
      if (m[0] === "\r" && m.index === this.buf.length - 1) this.pendingCR = true;
      const line = this.buf.slice(0, m.index);
      this.buf = this.buf.slice(m.index + m[0].length);
      this.line(line);
    }
  }

  private line(line: string) {
    if (line === "") {
      if (this.data.length > 0 || this.event !== "") this.dispatch(this.event || "message", this.data.join("\n"));
      this.event = "";
      this.data = [];
      return;
    }
    if (line.startsWith(":")) return;
    const i = line.indexOf(":");
    const field = i < 0 ? line : line.slice(0, i);
    let value = i < 0 ? "" : line.slice(i + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "event") this.event = value;
    else if (field === "data") this.data.push(value);
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

// parseChanged kiểm dạng khung changed mà không ép kiểu: {"topic": string, "entity": {"type","id": string}, "scope"?}.
function parseChanged(data: string): ChangedEvent | null {
  let v: unknown;
  try {
    v = JSON.parse(data);
  } catch {
    return null;
  }
  if (!isRecord(v) || typeof v.topic !== "string" || !isRecord(v.entity)) return null;
  const { type, id } = v.entity;
  if (typeof type !== "string" || typeof id !== "string") return null;
  const e: ChangedEvent = { topic: v.topic, entity: { type, id } };
  if (v.scope !== undefined) e.scope = v.scope;
  return e;
}
