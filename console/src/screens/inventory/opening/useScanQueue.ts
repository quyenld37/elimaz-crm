// Hàng đợi quét của Kiểm kho mở sổ (Task 273, HV03, HV05; mockup trạng thái B).
//
// - Mỗi lần quét là một yêu cầu incrementStocktakeLine (+1 hoặc −1) với `Idempotency-Key` RIÊNG sinh lúc quét. Không
//   dùng useWriteAction ở đây: nó nhớ một khóa theo nội dung nên ba lần quét cùng mã sẽ chung một khóa (bị coi là một).
// - Gửi lần lượt từng yêu cầu theo thứ tự quét. Chưa rõ kết quả (không tới máy chủ, 5xx, 503 retry_later, 409
//   request_in_progress) → giữ trong hàng đợi, trạng thái "waiting", gửi lại ĐÚNG khóa cũ sau retryMs hoặc khi trình
//   duyệt báo có mạng lại. Máy chủ lưu kết quả theo khóa nên gửi lại không đếm trùng (ADR-41).
// - Lỗi dứt khoát (4xx khác) → "error" kèm mã và câu, không gửi lại (ambiguous_code giữ danh sách ứng viên).
// - Lần quét chưa có kết luận được giữ cả trong localStorage theo phiên: tải lại trang vẫn gửi lại đúng khóa cũ.
import { useCallback, useEffect, useRef, useState } from "react";
import type { AmbiguousCodeCandidate, StocktakeIncrementResult } from "../../../api";
import { ApiError, NetworkError, errorMessage } from "../../../app/errors";

/** Chờ trước khi gửi lại một lần quét chưa rõ kết quả (ms). */
export const RETRY_MS = 3000;
/** Số lần quét đã lưu còn giữ trong danh sách (quét hàng nghìn mã không làm danh sách phình mãi). */
const KEEP_SAVED = 50;

/** Giữ mọi lần quét chưa xong và tối đa KEEP_SAVED lần đã lưu mới nhất (danh sách mới nhất đứng đầu). */
function trim(items: ScanItem[]): ScanItem[] {
  let saved = 0;
  return items.filter((i) => i.status !== "saved" || ++saved <= KEEP_SAVED);
}

export interface ScanBody {
  stocktakeId: string;
  code: string;
  by: number;
}

export type ScanStatus = "waiting" | "sending" | "saved" | "error";

export interface ScanItem {
  id: string;
  /** `Idempotency-Key` của lần quét này; không đổi cho tới khi có kết luận. */
  key: string;
  code: string;
  by: number;
  status: ScanStatus;
  /** Số lần đã thử gửi (lớn hơn 0 mà còn "waiting" nghĩa là lần gửi vừa rồi chưa tới được máy chủ). */
  attempts: number;
  /** Câu tiếng Việt khi status = error. */
  message: string | null;
  /** Mã lỗi API khi status = error. */
  errorCode: string | null;
  candidates: AmbiguousCodeCandidate[] | null;
  result: StocktakeIncrementResult | null;
}

export interface ScanQueueOptions {
  stocktakeId: string;
  send: (key: string, body: ScanBody) => Promise<StocktakeIncrementResult>;
  retryMs?: number;
  newKey?: () => string;
  /** Gọi sau mỗi lần cộng lưu được (làm mới tiến độ). */
  onSaved?: (item: ScanItem) => void;
  /** Gọi khi máy chủ báo phiên không còn mở (409 stocktake_not_open). */
  onClosed?: () => void;
}

interface Stored {
  key: string;
  code: string;
  by: number;
}

const storeKey = (stocktakeId: string) => `crm.opening.queue.${stocktakeId}`;

/** Đối tượng có trường `field` kiểu `type`. */
function has(x: unknown, field: string, type: "string" | "number"): boolean {
  return (
    typeof x === "object" &&
    x !== null &&
    field in x &&
    typeof Object.getOwnPropertyDescriptor(x, field)?.value === type
  );
}

function isStored(x: unknown): x is Stored {
  return has(x, "key", "string") && has(x, "code", "string") && has(x, "by", "number");
}

function load(stocktakeId: string): Stored[] {
  try {
    const raw = localStorage.getItem(storeKey(stocktakeId));
    const v: unknown = raw === null ? [] : JSON.parse(raw);
    return Array.isArray(v) ? v.filter(isStored) : [];
  } catch {
    return [];
  }
}

function save(stocktakeId: string, items: readonly ScanItem[]) {
  try {
    const keep: Stored[] = items
      .filter((i) => i.status === "waiting" || i.status === "sending")
      .map((i) => ({ key: i.key, code: i.code, by: i.by }));
    if (keep.length === 0) localStorage.removeItem(storeKey(stocktakeId));
    else localStorage.setItem(storeKey(stocktakeId), JSON.stringify(keep));
  } catch {
    // Trình duyệt chặn bộ nhớ: hàng đợi chỉ còn trong trang này.
  }
}

/** Chưa rõ máy chủ đã làm hay chưa, hoặc nên thử lại đúng như cũ. */
function isUnknown(e: unknown): boolean {
  if (e instanceof NetworkError) return true;
  // 429: máy chủ chưa làm gì, chờ rồi gửi lại cùng khóa (mất lần quét là đếm thiếu).
  return (
    e instanceof ApiError &&
    (e.status >= 500 || e.status === 429 || (e.status === 409 && e.code === "request_in_progress"))
  );
}

function isCandidate(x: unknown): x is AmbiguousCodeCandidate {
  return has(x, "variantId", "string") && has(x, "sellerId", "string") && has(x, "sku", "string");
}

function candidatesOf(e: unknown): AmbiguousCodeCandidate[] | null {
  if (!(e instanceof ApiError) || e.code !== "ambiguous_code") return null;
  const c = e.details.candidates;
  return Array.isArray(c) ? c.filter(isCandidate) : null;
}

export interface ScanQueue {
  /** Mới nhất đứng đầu. */
  items: ScanItem[];
  /** Số lần quét chưa gửi xong (đang chờ hoặc đang gửi). */
  waiting: number;
  /** Có lần quét gửi chưa tới được máy chủ (mất mạng, máy chủ bận): màn báo "chờ gửi". */
  offline: boolean;
  enqueue: (code: string, by: number) => void;
  /** Bỏ một dòng lỗi khỏi danh sách. */
  dismiss: (id: string) => void;
}

export function useScanQueue(opts: ScanQueueOptions): ScanQueue {
  const { stocktakeId } = opts;
  const optsRef = useRef(opts);
  useEffect(() => {
    optsRef.current = opts;
  });
  const seq = useRef(0);
  const [items, setItems] = useState<ScanItem[]>(() =>
    load(stocktakeId).map((s, i) => ({
      id: `restored-${String(i)}`,
      key: s.key,
      code: s.code,
      by: s.by,
      status: "waiting" as const,
      attempts: 0,
      message: null,
      errorCode: null,
      candidates: null,
      result: null,
    })),
  );
  // Bản mới nhất của danh sách cho vòng gửi (đọc đồng bộ, không phụ thuộc lần vẽ).
  const live = useRef(items);
  const running = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const alive = useRef(true);
  const isAlive = () => alive.current;

  const update = useCallback(
    (fn: (cur: ScanItem[]) => ScanItem[]) => {
      live.current = trim(fn(live.current));
      save(stocktakeId, live.current);
      setItems(live.current);
    },
    [stocktakeId],
  );
  const patch = useCallback(
    (id: string, p: Partial<ScanItem>) => {
      update((cur) => cur.map((i) => (i.id === id ? { ...i, ...p } : i)));
    },
    [update],
  );

  const pump = useCallback(async () => {
    if (running.current) return;
    running.current = true;
    try {
      for (;;) {
        // Cũ nhất chưa gửi xong đi trước: giữ thứ tự quét.
        const next = [...live.current].reverse().find((i) => i.status === "waiting");
        if (next === undefined || !isAlive()) return;
        patch(next.id, { status: "sending", attempts: next.attempts + 1 });
        try {
          const result = await optsRef.current.send(next.key, { stocktakeId, code: next.code, by: next.by });
          if (!isAlive()) return;
          patch(next.id, { status: "saved", result });
          optsRef.current.onSaved?.({ ...next, status: "saved", result });
        } catch (e) {
          if (!isAlive()) return;
          if (isUnknown(e)) {
            patch(next.id, { status: "waiting" });
            const wait = optsRef.current.retryMs ?? RETRY_MS;
            timer.current = setTimeout(() => {
              timer.current = null;
              void pump();
            }, wait);
            return;
          }
          const errorCode = e instanceof ApiError ? e.code : null;
          patch(next.id, {
            status: "error",
            message: errorMessage(e),
            errorCode,
            candidates: candidatesOf(e),
          });
          if (errorCode === "stocktake_not_open") optsRef.current.onClosed?.();
        }
      }
    } finally {
      running.current = false;
    }
  }, [patch, stocktakeId]);

  useEffect(() => {
    alive.current = true;
    const online = () => {
      if (timer.current !== null) {
        clearTimeout(timer.current);
        timer.current = null;
      }
      void pump();
    };
    window.addEventListener("online", online);
    // Lần quét còn lại từ lần tải trước (localStorage) gửi lại ngay, cùng khóa.
    void pump();
    return () => {
      alive.current = false;
      window.removeEventListener("online", online);
      if (timer.current !== null) clearTimeout(timer.current);
    };
  }, [pump]);

  const enqueue = useCallback(
    (code: string, by: number) => {
      const key = (optsRef.current.newKey ?? (() => crypto.randomUUID()))();
      const item: ScanItem = {
        id: `r${String(++seq.current)}`,
        key,
        code,
        by,
        status: "waiting",
        attempts: 0,
        message: null,
        errorCode: null,
        candidates: null,
        result: null,
      };
      update((cur) => [item, ...cur]);
      void pump();
    },
    [pump, update],
  );

  const dismiss = useCallback(
    (id: string) => {
      update((cur) => cur.filter((i) => i.id !== id));
    },
    [update],
  );

  const waiting = items.filter((i) => i.status === "waiting" || i.status === "sending").length;
  const offline = items.some((i) => i.status === "waiting" && i.attempts > 0);
  return { items, waiting, offline, enqueue, dismiss };
}
