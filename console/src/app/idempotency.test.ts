// @vitest-environment jsdom
// useWriteAction (kế hoạch M1 Task 48, ADR-41, ADR-46): khóa sinh khi bắt đầu thao tác, giữ nguyên khi tự gửi lại.
// Chạy qua client sinh thật (createStaff) với fetch giả đóng vai máy chủ.
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createStaff } from "../api";
import type { CreateStaffRequest, StaffMember } from "../api";
import { ApiError, ERROR_MESSAGES, NetworkError, unwrap } from "./errors";
import { TEXT_BUSY, useWriteAction, type WriteResult } from "./idempotency";

afterEach(() => {
  vi.unstubAllGlobals();
});

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json", ...headers } });

const member: StaffMember = {
  id: "0195f3b2-0000-7000-8000-000000000001",
  email: "lan@elimaz.vn",
  displayName: "Lan",
  status: "active",
  version: 1,
  createdAt: "2026-11-18T00:00:00Z",
  grants: [],
};
const ok = () => json(201, member);
const busy = (retryAfter = "1") => json(503, { error: "retry_later", request_id: "r" }, { "retry-after": retryAfter });
const inProgress = () => json(409, { error: "request_in_progress", request_id: "r" });

interface Seen {
  key: string | null;
  body: string;
}

// Máy chủ giả: trả lần lượt các câu trả lời trong `replies` (hết thì lặp câu cuối), ghi lại khóa và thân.
function server(replies: (() => Response | Promise<Response>)[]) {
  const seen: Seen[] = [];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    const req = input instanceof Request ? input : new Request(input);
    seen.push({ key: req.headers.get("Idempotency-Key"), body: await req.text() });
    const reply = replies[Math.min(seen.length - 1, replies.length - 1)];
    if (reply === undefined) throw new Error("thiếu câu trả lời");
    return reply();
  });
  return seen;
}

const send = (key: string, body: CreateStaffRequest) =>
  unwrap(createStaff({ body, headers: { "Idempotency-Key": key } }));

// Chờ giả: ghi lại thời gian chờ, không chờ thật.
function fakeSleep() {
  const waits: number[] = [];
  const sleep = (ms: number) => {
    waits.push(ms);
    return Promise.resolve();
  };
  return { waits, sleep };
}

function setup(replies: (() => Response | Promise<Response>)[]) {
  const seen = server(replies);
  const s = fakeSleep();
  let n = 0;
  const hook = renderHook(() => useWriteAction(send, { sleep: s.sleep, newKey: () => `key-${String(++n)}` }));
  return { seen, waits: s.waits, hook };
}

const body: CreateStaffRequest = { email: "lan@elimaz.vn", displayName: "Lan" };

async function run(hook: ReturnType<typeof setup>["hook"], b: CreateStaffRequest = body) {
  let r: WriteResult<StaffMember> | undefined;
  await act(async () => {
    r = await hook.result.current.run(b);
  });
  if (r === undefined) throw new Error("run không trả kết quả");
  return r;
}

describe("useWriteAction", () => {
  it("503 hai lần rồi 201 → 3 yêu cầu cùng một khóa, chờ theo Retry-After", async () => {
    const { seen, waits, hook } = setup([() => busy("1"), () => busy("2"), ok]);
    const r = await run(hook);
    expect(r).toEqual({ ok: true, data: member });
    expect(seen).toHaveLength(3);
    expect(new Set(seen.map((s) => s.key))).toEqual(new Set(["key-1"]));
    expect(waits).toEqual([1000, 2000]);
    expect(hook.result.current.message).toBeNull();
  });

  it("503 bốn lần → tổng 4 yêu cầu (1 + 3 lần gửi lại), hiện câu Hệ thống đang bận", async () => {
    const { seen, hook } = setup([busy]);
    const r = await run(hook);
    expect(r.ok).toBe(false);
    expect(seen).toHaveLength(4);
    expect(new Set(seen.map((s) => s.key)).size).toBe(1);
    expect(TEXT_BUSY).toBe("Hệ thống đang bận, thử lại sau ít giây");
    expect(hook.result.current.message).toBe(TEXT_BUSY);
  });

  it("422 → không gửi lại, câu tiếng Việt của mã", async () => {
    const { seen, hook } = setup([() => json(422, { error: "invalid_input", request_id: "r" })]);
    const r = await run(hook);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toBeInstanceOf(ApiError);
    expect(seen).toHaveLength(1);
    expect(hook.result.current.message).toBe(ERROR_MESSAGES.invalid_input);
  });

  it("bấm hai lần liền → 1 yêu cầu, hai lần bấm nhận cùng kết quả", async () => {
    let release: (r: Response) => void = () => undefined;
    const gate = new Promise<Response>((res) => (release = res));
    const { seen, hook } = setup([() => gate]);
    let a: Promise<WriteResult<StaffMember>> | undefined;
    let b: Promise<WriteResult<StaffMember>> | undefined;
    act(() => {
      a = hook.result.current.run(body);
      b = hook.result.current.run(body);
    });
    await waitFor(() => {
      expect(hook.result.current.pending).toBe(true);
    });
    await act(async () => {
      release(ok());
      await a;
    });
    expect(seen).toHaveLength(1);
    expect(await a).toEqual(await b);
    expect(hook.result.current.pending).toBe(false);
  });

  it("409 request_in_progress → chờ 1 s, gửi lại cùng khóa", async () => {
    const { seen, waits, hook } = setup([inProgress, inProgress, ok]);
    const r = await run(hook);
    expect(r.ok).toBe(true);
    expect(seen.map((s) => s.key)).toEqual(["key-1", "key-1", "key-1"]);
    expect(waits).toEqual([1000, 1000]);
  });

  it("409 request_in_progress mãi → dừng sau 30 lần gửi lại, câu bận", async () => {
    const { seen, hook } = setup([inProgress]);
    const r = await run(hook);
    expect(r.ok).toBe(false);
    expect(seen).toHaveLength(31);
    expect(hook.result.current.message).toBe(TEXT_BUSY);
  });

  it("Retry-After thiếu, sai hay quá lớn → chờ 1 s hoặc tối đa 10 s", async () => {
    const { waits, hook } = setup([() => busy(""), () => busy("abc"), () => busy("3600"), ok]);
    await run(hook);
    expect(waits).toEqual([1000, 1000, 10_000]);
  });

  it("lỗi mạng rồi 201 → gửi lại cùng khóa sau 1 s", async () => {
    const { seen, waits, hook } = setup([() => Promise.reject(new TypeError("Failed to fetch")), ok]);
    const r = await run(hook);
    expect(r.ok).toBe(true);
    expect(seen.map((s) => s.key)).toEqual(["key-1", "key-1"]);
    expect(waits).toEqual([1000]);
  });

  it("lỗi mạng mãi → 4 yêu cầu, câu mất kết nối; bấm lại cùng nội dung dùng lại khóa cũ", async () => {
    const { seen, hook } = setup([() => Promise.reject(new TypeError("Failed to fetch"))]);
    const r = await run(hook);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toBeInstanceOf(NetworkError);
    expect(seen).toHaveLength(4);
    await run(hook);
    expect(new Set(seen.map((s) => s.key))).toEqual(new Set(["key-1"]));
  });

  it("bấm lại sau khi bận mà đã sửa nội dung → khóa mới (tránh idempotency_key_reused)", async () => {
    const { seen, hook } = setup([busy]);
    await run(hook);
    await run(hook, { ...body, displayName: "Lan Nguyễn" });
    expect(new Set(seen.map((s) => s.key))).toEqual(new Set(["key-1", "key-2"]));
  });

  it("sau khi xong hoặc lỗi 4xx dứt khoát → lần bấm sau là thao tác mới, khóa mới", async () => {
    const { seen, hook } = setup([ok, () => json(409, { error: "email_taken", request_id: "r" }), ok]);
    await run(hook);
    await run(hook);
    await run(hook);
    expect(seen.map((s) => s.key)).toEqual(["key-1", "key-2", "key-3"]);
  });

  it("201 nhưng thân đọc hỏng → gửi lại cùng khóa (máy chủ trả lại kết quả đã lưu), không tạo khóa mới", async () => {
    const { seen, hook } = setup([
      () => new Response('{"id":', { status: 201, headers: { "content-type": "application/json" } }),
      ok,
    ]);
    const r = await run(hook);
    expect(r).toEqual({ ok: true, data: member });
    expect(seen.map((s) => s.key)).toEqual(["key-1", "key-1"]);
  });

  it("401, 403, 429 không tự gửi lại", async () => {
    for (const [status, code] of [
      [401, "unauthenticated"],
      [403, "forbidden"],
      [429, "rate_limited"],
    ] as const) {
      const { seen, hook } = setup([() => json(status, { error: code, request_id: "r" })]);
      await run(hook);
      expect(seen, code).toHaveLength(1);
      hook.unmount();
    }
  });

  it("đóng màn giữa lúc chờ gửi lại → không gửi thêm", async () => {
    const seen = server([busy]);
    let wake: () => void = () => undefined;
    const sleep = (_ms: number, signal: AbortSignal) =>
      new Promise<void>((res, rej) => {
        wake = res;
        signal.addEventListener("abort", () => {
          rej(new DOMException("aborted", "AbortError"));
        });
      });
    const hook = renderHook(() => useWriteAction(send, { sleep }));
    let p: Promise<WriteResult<StaffMember>> | undefined;
    act(() => {
      p = hook.result.current.run(body);
    });
    await waitFor(() => {
      expect(seen).toHaveLength(1);
    });
    hook.unmount();
    wake();
    const r = await p;
    expect(r?.ok).toBe(false);
    expect(seen).toHaveLength(1);
  });

  it("reset() xóa câu báo", async () => {
    const { hook } = setup([busy]);
    await run(hook);
    act(() => {
      hook.result.current.reset();
    });
    expect(hook.result.current.message).toBeNull();
  });
});
