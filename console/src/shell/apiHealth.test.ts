// @vitest-environment jsdom
// Canh API qua /health/live bằng client sinh (healthLive).
import { afterEach, describe, expect, it, vi } from "vitest";
import { watchApiLive } from "./apiHealth";

afterEach(() => {
  vi.unstubAllGlobals();
});

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const ok = () =>
  new Response(JSON.stringify({ status: "ok" }), { status: 200, headers: { "content-type": "application/json" } });

function watch(fetchImpl: (u: string) => Promise<Response>, intervalMs = 40, timeoutMs = 60) {
  const seen: boolean[] = [];
  vi.stubGlobal("fetch", (input: RequestInfo | URL) => fetchImpl(input instanceof Request ? input.url : String(input)));
  const stop = watchApiLive({ intervalMs, timeoutMs, onChange: (down) => seen.push(down) });
  return { seen, stop };
}

describe("watchApiLive", () => {
  it("200 → báo API sống (false), chỉ báo khi đổi", async () => {
    const w = watch(() => Promise.resolve(ok()));
    await sleep(150);
    w.stop();
    expect(w.seen).toEqual([false]);
  });

  it("lỗi mạng → báo tạm ngừng (true)", async () => {
    const w = watch(() => Promise.reject(new TypeError("Failed to fetch")));
    await sleep(30);
    w.stop();
    expect(w.seen).toEqual([true]);
  });

  it("502 → tạm ngừng", async () => {
    const w = watch(() => Promise.resolve(new Response("", { status: 502 })));
    await sleep(30);
    w.stop();
    expect(w.seen).toEqual([true]);
  });

  it("không trả lời trong timeoutMs → tạm ngừng", async () => {
    const w = watch((/* treo */) => new Promise<Response>(() => undefined), 1000, 40);
    await sleep(120);
    w.stop();
    expect(w.seen).toEqual([true]);
  });

  it("tạm ngừng rồi sống lại → true rồi false", async () => {
    let n = 0;
    const w = watch(() => (n++ === 0 ? Promise.reject(new TypeError("x")) : Promise.resolve(ok())));
    await sleep(120);
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
    await sleep(20);
    w.stop();
    expect(urls[0]?.endsWith("/health/live")).toBe(true);
  });
});
