// Đường ghi duy nhất của màn hình (kế hoạch M1 Task 48; ADR-41 khóa chống trùng, ADR-46 retry_later; luật console 4).
//
// - Khóa `Idempotency-Key` sinh khi người dùng bắt đầu thao tác (lần gọi run), giữ nguyên cho mọi lần tự gửi lại.
// - 503 retry_later: chờ Retry-After (giây; thiếu, sai → 1 s; tối đa 10 s) rồi gửi lại cùng khóa, tối đa 3 lần; vẫn
//   bận → câu TEXT_BUSY. Máy chủ không lưu retry_later, nên gửi lại cùng khóa chạy lại từ đầu.
// - 409 request_in_progress: cùng khóa đang chạy ở máy chủ → chờ 1 s gửi lại cùng khóa (tối đa 30 lần, khớp giới hạn
//   30 s của một câu lệnh); quá thì TEXT_BUSY.
// - Lỗi mạng (chưa rõ máy chủ đã làm chưa): chờ 1 s gửi lại cùng khóa, tối đa 3 lần.
// - Lỗi khác (4xx, 5xx khác) không tự gửi lại.
// - Bấm lại khi yêu cầu trước chưa xong → nhận lại đúng lời hứa đang chạy (không gửi thêm).
// - Bấm lại sau lỗi chưa rõ kết quả (mạng, bận, 5xx) với cùng nội dung → dùng lại khóa cũ (máy chủ trả kết quả đã
//   lưu nếu lần trước đã xong); nội dung đã đổi, hoặc lần trước xong/lỗi dứt khoát → khóa mới.
import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import { ApiError, NetworkError, errorMessage } from "./errors";

export const TEXT_BUSY = "Hệ thống đang bận, thử lại sau ít giây";

const MAX_BUSY_RETRIES = 3;
const MAX_NETWORK_RETRIES = 3;
const MAX_IN_PROGRESS_RETRIES = 30;
const DEFAULT_WAIT_MS = 1000;
const MAX_WAIT_MS = 10_000;

export type WriteResult<T> = { ok: true; data: T } | { ok: false; error: unknown };

export interface WriteActionOptions {
  /** Chờ ms rồi trả; phải từ chối khi signal bị hủy. Mặc định setTimeout (test thay bằng bản giả). */
  sleep?: (ms: number, signal: AbortSignal) => Promise<void>;
  /** Sinh khóa mới. Mặc định crypto.randomUUID(). */
  newKey?: () => string;
}

export interface WriteAction<TReq, TRes> {
  /** Bắt đầu thao tác (hoặc nhận lại thao tác đang chạy). Không bao giờ từ chối: lỗi nằm trong kết quả. */
  run(body: TReq): Promise<WriteResult<TRes>>;
  pending: boolean;
  /** Lỗi của lần chạy cuối (ApiError, NetworkError, …); null khi chưa có hoặc đã xong. */
  error: unknown;
  /** Câu tiếng Việt cho lỗi của lần chạy cuối; null khi không có. */
  message: string | null;
  /** Xóa lỗi và câu báo (giữ khóa đang nhớ cho lần bấm lại). */
  reset(): void;
}

/** Lý do hủy của signal (Error), hoặc AbortError khi lý do không phải Error. */
export const abortReason = (signal: AbortSignal): Error =>
  signal.reason instanceof Error ? signal.reason : new DOMException("đã hủy", "AbortError");

/** Chờ ms bằng setTimeout; từ chối (abortReason) khi signal bị hủy. */
export function defaultSleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(abortReason(signal));
      return;
    }
    const t = setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(t);
        reject(abortReason(signal));
      },
      { once: true },
    );
  });
}

const isBusy = (e: unknown) => e instanceof ApiError && e.status === 503 && e.code === "retry_later";
const isInProgress = (e: unknown) => e instanceof ApiError && e.status === 409 && e.code === "request_in_progress";

// Lỗi mà máy chủ có thể đã làm xong, hoặc chưa làm gì nhưng nên gửi lại đúng như cũ: giữ khóa cho lần bấm lại.
const outcomeUnknown = (e: unknown) =>
  e instanceof NetworkError || isInProgress(e) || (e instanceof ApiError && e.status >= 500);

async function sendWithRetry<TReq, TRes>(
  fn: (key: string, body: TReq) => Promise<TRes>,
  key: string,
  body: TReq,
  sleep: (ms: number, signal: AbortSignal) => Promise<void>,
  signal: AbortSignal,
): Promise<TRes> {
  let busy = 0;
  let network = 0;
  let inProgress = 0;
  for (;;) {
    signal.throwIfAborted();
    try {
      return await fn(key, body);
    } catch (e) {
      let wait: number | null = null;
      if (isBusy(e) && busy < MAX_BUSY_RETRIES) {
        busy++;
        const ra = e instanceof ApiError ? e.retryAfterMs : null;
        wait = Math.min(Math.max(ra ?? DEFAULT_WAIT_MS, 0), MAX_WAIT_MS);
      } else if (isInProgress(e) && inProgress < MAX_IN_PROGRESS_RETRIES) {
        inProgress++;
        wait = DEFAULT_WAIT_MS;
      } else if (e instanceof NetworkError && network < MAX_NETWORK_RETRIES) {
        network++;
        wait = DEFAULT_WAIT_MS;
      }
      if (wait === null) throw e;
      await sleep(wait, signal);
    }
  }
}

interface State {
  pending: boolean;
  error: unknown;
  message: string | null;
}

/** Bộ hủy của component: signal bị hủy ("màn đã đóng") khi component gỡ khỏi màn. */
export function useAbortOnUnmount(): RefObject<AbortController> {
  const abort = useRef<AbortController>(new AbortController());
  useEffect(() => {
    const ac = new AbortController();
    abort.current = ac;
    return () => {
      ac.abort(new DOMException("màn đã đóng", "AbortError"));
    };
  }, []);
  return abort;
}

/**
 * Gửi một yêu cầu ghi với khóa chống trùng và tự gửi lại cùng khóa khi máy chủ bận.
 * `fn` gọi client sinh, ví dụ `(key, body) => unwrap(createProduct({ body, headers: { "Idempotency-Key": key } }))`.
 * Mọi thứ đổi theo thao tác (mã đối tượng trong path, version…) phải nằm trong `body`: khóa được nhớ theo nội dung của
 * `body`, nên lấy mã đối tượng từ props bên trong `fn` có thể dùng lại khóa của thao tác trước.
 */
export function useWriteAction<TReq, TRes>(
  fn: (key: string, body: TReq) => Promise<TRes>,
  opts: WriteActionOptions = {},
): WriteAction<TReq, TRes> {
  const [state, setState] = useState<State>({ pending: false, error: null, message: null });
  const fnRef = useRef(fn);
  const optsRef = useRef(opts);
  const inflight = useRef<Promise<WriteResult<TRes>> | null>(null);
  const remembered = useRef<{ key: string; fingerprint: string | undefined } | null>(null);
  const abort = useAbortOnUnmount();

  useEffect(() => {
    fnRef.current = fn;
    optsRef.current = opts;
  });

  const run = useCallback(
    (body: TReq): Promise<WriteResult<TRes>> => {
      if (inflight.current !== null) return inflight.current;
      // JSON.stringify(undefined) là undefined (thao tác không có thân): vẫn so được.
      const fingerprint: string | undefined = JSON.stringify(body);
      const newKey = optsRef.current.newKey ?? (() => crypto.randomUUID());
      const key =
        remembered.current !== null && remembered.current.fingerprint === fingerprint
          ? remembered.current.key
          : newKey();
      remembered.current = { key, fingerprint };
      setState({ pending: true, error: null, message: null });
      const sleep = optsRef.current.sleep ?? defaultSleep;
      const p = (async (): Promise<WriteResult<TRes>> => {
        try {
          const data = await sendWithRetry(fnRef.current, key, body, sleep, abort.current.signal);
          remembered.current = null;
          setState({ pending: false, error: null, message: null });
          return { ok: true, data };
        } catch (error) {
          if (!outcomeUnknown(error)) remembered.current = null;
          const message = isBusy(error) || isInProgress(error) ? TEXT_BUSY : errorMessage(error);
          setState({ pending: false, error, message });
          return { ok: false, error };
        } finally {
          inflight.current = null;
        }
      })();
      inflight.current = p;
      return p;
    },
    [abort],
  );

  const reset = useCallback(() => {
    setState((s) => ({ ...s, error: null, message: null }));
  }, []);

  return { run, reset, pending: state.pending, error: state.error, message: state.message };
}
