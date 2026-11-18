// Test lớp realtime trên máy chủ HTTP giả (node:http) và fetch thật của Node (kế hoạch M0 Task 17, spec 04 mục 6).
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createRealtimeClient, type ChangedEvent, type RealtimeState } from "./client";

type Handler = (req: IncomingMessage, res: ServerResponse) => void;

let server: Server;
let url: string;
let handlers: Handler[];
let requests: number;
let openResponses: ServerResponse[];
let clients: { close(): void }[];

// Yêu cầu thứ n dùng handlers[n]; hết danh sách thì dùng handler cuối.
beforeEach(async () => {
  handlers = [];
  requests = 0;
  openResponses = [];
  clients = [];
  server = createServer((req, res) => {
    const h = handlers[Math.min(requests, handlers.length - 1)];
    requests++;
    openResponses.push(res);
    if (h === undefined) {
      res.writeHead(500).end();
      return;
    }
    h(req, res);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  url = `http://127.0.0.1:${String((server.address() as AddressInfo).port)}/api/v1/stream`;
});

afterEach(async () => {
  for (const c of clients) c.close();
  for (const r of openResponses) r.destroy();
  server.closeAllConnections();
  await new Promise<void>((resolve) =>
    server.close(() => {
      resolve();
    }),
  );
});

const status =
  (code: number, headers: Record<string, string> = {}): Handler =>
  (_req, res) => {
    res.writeHead(code, headers).end();
  };

// Mở luồng SSE, gửi các khung trong frames rồi giữ kết nối mở (im lặng).
const stream =
  (...frames: string[]): Handler =>
  (_req, res) => {
    res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-store" });
    for (const f of frames) res.write(f);
  };

const RESYNC = "event: resync\ndata: {}\n\n";

function start(over: Partial<Parameters<typeof createRealtimeClient>[0]> = {}) {
  const states: RealtimeState[] = [];
  const changed: ChangedEvent[] = [];
  let resyncs = 0;
  const c = createRealtimeClient({
    url,
    onChanged: (e) => changed.push(e),
    onResync: () => {
      resyncs++;
    },
    onState: (s) => states.push(s),
    backoffMs: [20, 40],
    ...over,
  });
  clients.push(c);
  return { client: c, states, changed, resyncs: () => resyncs };
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function until(cond: () => boolean, timeoutMs = 2000) {
  const end = Date.now() + timeoutMs;
  while (!cond()) {
    if (Date.now() > end) throw new Error("hết giờ chờ điều kiện");
    await sleep(5);
  }
}

describe("createRealtimeClient", () => {
  it("200 rồi im lặng: sau watchdogMs thì reconnecting và mở yêu cầu thứ hai", async () => {
    handlers = [stream(RESYNC)];
    const t = start({ watchdogMs: 200 });
    await until(() => t.states.includes("connected"));
    await until(() => t.states.length >= 3, 1500);
    await sleep(50);
    expect(t.states).toEqual(["connected", "reconnecting", "connected"]);
    expect(requests).toBe(2);
  });

  it("máy chủ không trả header nào: sau watchdogMs thì reconnecting và mở yêu cầu thứ hai", async () => {
    handlers = [() => undefined, stream(RESYNC)];
    const t = start({ watchdogMs: 150 });
    await until(() => t.states.includes("connected"), 1500);
    expect(t.states).toEqual(["reconnecting", "connected"]);
    expect(requests).toBe(2);
  });

  it("nhịp tim giữ kết nối: không nối lại khi có byte trước watchdogMs", async () => {
    handlers = [
      (req, res) => {
        stream(RESYNC)(req, res);
        const iv = setInterval(() => res.write(": ping\n\n"), 50);
        res.on("close", () => {
          clearInterval(iv);
        });
      },
    ];
    const t = start({ watchdogMs: 200 });
    await sleep(600);
    expect(requests).toBe(1);
    expect(t.states).toEqual(["connected"]);
  });

  it("502 hai lần rồi 200: reconnecting, reconnecting, connected và onResync một lần", async () => {
    handlers = [status(502), status(502), stream(RESYNC)];
    const t = start();
    await until(() => t.states.includes("connected"));
    await sleep(150);
    expect(t.states).toEqual(["reconnecting", "reconnecting", "connected"]);
    expect(t.resyncs()).toBe(1);
    expect(requests).toBe(3);
  });

  it("302 (chuyển hướng đăng nhập): session_expired, không yêu cầu thêm trong 500 ms", async () => {
    handlers = [status(302, { location: "https://login.example/" }), stream(RESYNC)];
    const t = start();
    await until(() => t.states.length > 0);
    await sleep(500);
    expect(t.states).toEqual(["session_expired"]);
    expect(requests).toBe(1);
  });

  it.each([401, 403])("%i: session_expired, dừng hẳn", async (code) => {
    handlers = [status(code), stream(RESYNC)];
    const t = start();
    await until(() => t.states.length > 0);
    await sleep(300);
    expect(t.states).toEqual(["session_expired"]);
    expect(requests).toBe(1);
  });

  it("opaqueredirect của trình duyệt (redirect: 'manual'): session_expired", async () => {
    const calls: RequestInit[] = [];
    const fakeFetch: typeof fetch = (_input, init) => {
      calls.push(init ?? {});
      return Promise.resolve(
        // Trình duyệt trả Response kiểu opaqueredirect, status 0; Node không dựng được kiểu này.
        { type: "opaqueredirect", status: 0, ok: false, body: null, headers: new Headers() } as unknown as Response,
      );
    };
    const t = start({ fetchImpl: fakeFetch });
    await until(() => t.states.length > 0);
    await sleep(200);
    expect(t.states).toEqual(["session_expired"]);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.redirect).toBe("manual");
  });

  it("refetchEveryMs: 100, luồng chỉ có nhịp tim: onResync ít nhất 2 lần trong 350 ms", async () => {
    handlers = [
      (req, res) => {
        stream()(req, res);
        const iv = setInterval(() => res.write(": ping\n\n"), 40);
        res.on("close", () => {
          clearInterval(iv);
        });
      },
    ];
    const t = start({ refetchEveryMs: 100 });
    await until(() => t.states.includes("connected"));
    await sleep(350);
    expect(t.resyncs()).toBeGreaterThanOrEqual(2);
  });

  it("không tải lại định kỳ khi chưa nối được", async () => {
    handlers = [status(502)];
    const t = start({ refetchEveryMs: 50, backoffMs: [100] });
    await sleep(400);
    expect(t.resyncs()).toBe(0);
    expect(t.states.every((s) => s === "reconnecting")).toBe(true);
  });

  it("đọc khung changed bị cắt giữa chừng, xuống dòng CRLF, data nhiều dòng", async () => {
    handlers = [
      (req, res) => {
        stream(RESYNC)(req, res);
        const frame =
          'event: changed\r\ndata: {"topic":"t.hello",\r\ndata: "entity":{"type":"conversation","id":"c1"},"scope":{"kind":"page","id":"p1"}}\r\n\r\n';
        res.write(frame.slice(0, 17));
        setTimeout(() => res.write(frame.slice(17)), 30);
      },
    ];
    const t = start();
    await until(() => t.changed.length === 1);
    expect(t.changed[0]).toEqual({
      topic: "t.hello",
      entity: { type: "conversation", id: "c1" },
      scope: { kind: "page", id: "p1" },
    });
  });

  it("khung changed sai dạng: không gọi onChanged, gọi onResync (tải lại cho chắc)", async () => {
    handlers = [stream(RESYNC, 'event: changed\ndata: {"topic":1}\n\n', "event: changed\ndata: không phải json\n\n")];
    const t = start();
    await until(() => t.resyncs() >= 3);
    expect(t.changed).toEqual([]);
  });

  it("máy chủ đóng luồng: reconnecting rồi nối lại", async () => {
    handlers = [
      (req, res) => {
        stream(RESYNC)(req, res);
        setTimeout(() => res.end(), 30);
      },
      stream(RESYNC),
    ];
    const t = start();
    await until(() => requests >= 2 && t.states.length >= 3);
    expect(t.states).toEqual(["connected", "reconnecting", "connected"]);
  });

  it("lỗi mạng (máy chủ không nghe): reconnecting, lùi dần theo backoffMs", async () => {
    const dead = url;
    await new Promise<void>((resolve) =>
      server.close(() => {
        resolve();
      }),
    );
    const t = start({ url: dead, backoffMs: [30, 60] });
    await until(() => t.states.length >= 2, 1500);
    expect(t.states.slice(0, 2)).toEqual(["reconnecting", "reconnecting"]);
    server = createServer(() => undefined);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  });

  it("close(): hủy kết nối, không yêu cầu, không báo trạng thái nào nữa", async () => {
    handlers = [stream(RESYNC)];
    const t = start({ watchdogMs: 100, refetchEveryMs: 30 });
    await until(() => t.states.includes("connected"));
    t.client.close();
    const n = t.resyncs();
    await sleep(300);
    expect(requests).toBe(1);
    expect(t.states).toEqual(["connected"]);
    expect(t.resyncs()).toBe(n);
  });

  it("gửi Accept: text/event-stream và không theo chuyển hướng", async () => {
    let accept = "";
    handlers = [
      (req, res) => {
        accept = req.headers.accept ?? "";
        stream(RESYNC)(req, res);
      },
    ];
    const t = start();
    await until(() => t.states.includes("connected"));
    expect(accept).toBe("text/event-stream");
  });
});
