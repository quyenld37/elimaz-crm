// @vitest-environment jsdom
// Canh API qua /health/live bằng client sinh (healthLive).
import { waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { watchApiLive } from "./apiHealth";

afterEach(() => {
  vi.unstubAllGlobals();
});

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const ok = () =>
  new Response(JSON.stringify({ status: "ok" }), { status: 200, headers: { "content-type": "application/json" } });

// Hạn của một lần hỏi đủ dài để không bao giờ hết trong test (trừ ca kiểm chính việc hết giờ): máy tải cao thì một lần
// hỏi đã trả lời vẫn có thể chạy quá vài chục ms. Test chờ theo sự kiện (waitFor: số lần hỏi, số lần báo), không chờ
// một quãng giờ cố định (Task 421).
const NEVER_MS = 600_000;

function watch(fetchImpl: (u: string) => Promise<Response>, intervalMs = 40, timeoutMs = NEVER_MS) {
  const seen: boolean[] = [];
  let calls = 0;
  vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
    calls++;
    return fetchImpl(input instanceof Request ? input.url : String(input));
  });
  const stop = watchApiLive({ intervalMs, timeoutMs, onChange: (down) => seen.push(down) });
  return { seen, stop, calls: () => calls };
}

describe("watchApiLive", () => {
  it("200 → báo API sống (false), chỉ báo khi đổi", async () => {
    const w = watch(() => Promise.resolve(ok()));
    // Lần hỏi thứ ba chỉ bắt đầu sau khi hai lần trước đã báo (hoặc không báo vì không đổi).
    await waitFor(() => {
      expect(w.calls()).toBeGreaterThanOrEqual(3);
    });
    w.stop();
    expect(w.seen).toEqual([false]);
  });

  it.each([
    ["lỗi mạng → báo tạm ngừng (true)", () => Promise.reject(new TypeError("Failed to fetch"))],
    ["502 → tạm ngừng", () => Promise.resolve(new Response("", { status: 502 }))],
  ])("%s", async (_name, reply: () => Promise<Response>) => {
    const w = watch(reply);
    await waitFor(() => {
      expect(w.calls()).toBeGreaterThanOrEqual(2);
    });
    w.stop();
    expect(w.seen).toEqual([true]);
  });

  it("không trả lời trong timeoutMs → tạm ngừng", async () => {
    const w = watch((/* treo */) => new Promise<Response>(() => undefined), 1000, 40);
    await waitFor(() => {
      expect(w.seen.length).toBeGreaterThan(0);
    });
    w.stop();
    expect(w.seen).toEqual([true]);
  });

  it("tạm ngừng rồi sống lại → true rồi false", async () => {
    let n = 0;
    const w = watch(() => (n++ === 0 ? Promise.reject(new TypeError("x")) : Promise.resolve(ok())));
    await waitFor(() => {
      expect(w.calls()).toBeGreaterThanOrEqual(3);
    });
    w.stop();
    expect(w.seen).toEqual([true, false]);
  });

  it("stop(): không hỏi thêm", async () => {
    let n = 0;
    const w = watch(() => {
      n++;
      return Promise.resolve(ok());
    });
    await sleep(10);
    w.stop();
    const after = n;
    await sleep(120);
    expect(n).toBe(after);
  });

  it("hỏi đúng /health/live", async () => {
    const urls: string[] = [];
    const w = watch((u) => {
      urls.push(u);
      return Promise.resolve(ok());
    });
    await waitFor(() => {
      expect(urls.length).toBeGreaterThan(0);
    });
    w.stop();
    expect(urls[0]?.endsWith("/health/live")).toBe(true);
  });
});
