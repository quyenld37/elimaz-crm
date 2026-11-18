// Canh API bằng GET /health/live qua client sinh (luật console 2). Không trả lời trong timeoutMs, lỗi mạng hoặc mã
// khác 2xx → "tạm ngừng"; khung vẫn chạy và hiện băng báo (ADR-34, mục Kiểm bằng).
import { healthLive } from "../api";

export interface ApiLiveOptions {
  /** Khoảng giữa hai lần hỏi. Mặc định 10 s. */
  intervalMs?: number;
  /** Chờ tối đa cho một lần hỏi. Mặc định 5 s. */
  timeoutMs?: number;
  /** Gọi khi trạng thái đổi (lần đầu luôn gọi): true = API tạm ngừng. */
  onChange(down: boolean): void;
}

export function watchApiLive(opts: ApiLiveOptions): () => void {
  const intervalMs = opts.intervalMs ?? 10_000;
  const timeoutMs = opts.timeoutMs ?? 5_000;
  let stopped = false;
  let last: boolean | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let inflight: AbortController | null = null;

  const report = (down: boolean) => {
    if (stopped || down === last) return;
    last = down;
    opts.onChange(down);
  };

  const check = async () => {
    const ac = new AbortController();
    inflight = ac;
    let t: ReturnType<typeof setTimeout> | undefined;
    // Không dựa vào việc fetch tôn trọng signal: hết giờ là "tạm ngừng" dù yêu cầu còn treo.
    const timedOut = new Promise<never>((_, reject) => {
      t = setTimeout(() => {
        ac.abort();
        reject(new Error("health/live quá giờ"));
      }, timeoutMs);
    });
    let down: boolean;
    try {
      const { response } = await Promise.race([healthLive({ signal: ac.signal, cache: "no-store" }), timedOut]);
      down = response?.ok !== true;
    } catch {
      down = true;
    } finally {
      clearTimeout(t);
      inflight = null;
    }
    report(down);
    if (!stopped) timer = setTimeout(() => void check(), intervalMs);
  };

  void check();
  return () => {
    stopped = true;
    clearTimeout(timer);
    inflight?.abort();
  };
}
