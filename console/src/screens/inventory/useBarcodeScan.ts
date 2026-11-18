// Quét mã của màn Kiểm kho (kế hoạch M1 Task 329; mockup docs/mockups/m1/kho-kiem-kho.html, trạng thái B).
//
// - Nhận chuỗi từ máy quét dạng bàn phím (gõ rồi Enter vào ô quét) hoặc người gõ tay vào cùng ô.
// - Quét cùng mã cộng 1 vào số đếm cục bộ ngay (kể cả khi đang tra mã: các lần quét dồn lại, tra một lần).
// - Gửi recordStocktakeCount với số tuyệt đối sau SAVE_DELAY_MS không đổi thêm số của mã đó; ghi lại dòng đã có thì
//   kèm lineVersion của lần lưu trước. Một yêu cầu một lúc (hàng đợi), qua useWriteAction (khóa chống trùng, tự gửi
//   lại cùng khóa khi máy chủ bận hoặc mạng chập).
// - Lỗi chưa rõ kết quả còn lại → "đang thử lại" sau RETRY_MS với cùng thân. 409 version_conflict (người khác vừa đếm
//   cùng mã, hoặc lần trước đã lưu mà mất câu trả lời) → tải lại phiên: máy chủ đã có đúng số của mình thì coi là đã
//   lưu; khác thì lấy số trên máy chủ và báo ở dòng, không ghi đè số của người khác.
import { useCallback, useEffect, useRef, useState, type ChangeEvent, type KeyboardEvent } from "react";
import type { StocktakeLine, VariantSearchHit } from "../../api";
import { ApiError, NetworkError, errorMessage } from "../../app/errors";
import { useWriteAction, type WriteActionOptions } from "../../app/idempotency";
import { MAX_COUNT } from "./stocktakeRules";

/** Chờ sau lần đổi số cuối của một mã rồi mới gửi (ms). */
export const SAVE_DELAY_MS = 800;
/** Chờ trước khi gửi lại số chưa lưu được (ms). */
export const RETRY_MS = 3000;

export interface SaveBody {
  stocktakeId: string;
  variantId: string;
  counted: number;
  lineVersion?: number;
}

/** pending: chờ gửi; saving: đang gửi; retrying: lỗi chưa rõ, sẽ gửi lại; error: lỗi dứt khoát hoặc xung đột. */
export type SaveStatus = "pending" | "saving" | "saved" | "retrying" | "error";

export interface ScanEntry {
  variantId: string;
  /** Kết quả tra mã trên máy này; null với dòng chỉ có trên máy chủ (máy khác đếm). */
  hit: VariantSearchHit | null;
  counted: number;
  /** Dòng máy chủ trả ở lần lưu (hoặc lần tải) gần nhất. */
  line: StocktakeLine | null;
  status: SaveStatus;
  message: string | null;
}

export interface ScanOptions {
  stocktakeId: string;
  /** Dòng của phiên trên máy chủ (getStocktake). */
  lines: readonly StocktakeLine[];
  /** Tra mã đã quét → biến thể, hoặc null khi không có. Lỗi thì ném. */
  lookup: (code: string) => Promise<VariantSearchHit | null>;
  /** Gửi recordStocktakeCount với khóa chống trùng. */
  save: (key: string, body: SaveBody) => Promise<StocktakeLine>;
  /** Tải lại phiên, trả dòng mới nhất. */
  reload: () => Promise<readonly StocktakeLine[]>;
  delayMs?: number;
  retryMs?: number;
  writeOptions?: WriteActionOptions;
}

export interface BarcodeScan {
  /** Mã vừa đổi gần nhất đứng trước; dòng chỉ có trên máy chủ theo thứ tự máy chủ ở sau. */
  entries: ScanEntry[];
  /** Lần quét thành công gần nhất. */
  last: { code: string; variantId: string } | null;
  /** Mã vừa quét không có trong danh mục. */
  notFound: string | null;
  /** Câu lỗi khi tra mã không được (mạng, quyền). */
  lookupError: string | null;
  scan: (code: string) => void;
  setCount: (variantId: string, n: number) => void;
  add: (variantId: string, d: number) => void;
  /** Gửi ngay mọi số đang chờ và chờ xong; true khi mọi dòng đã lưu. */
  flush: () => Promise<boolean>;
  /** Có số chưa lưu (đang chờ, đang gửi, đang thử lại). */
  busy: boolean;
  /** Gắn vào ô quét. */
  input: {
    value: string;
    onChange: (e: ChangeEvent<HTMLInputElement>) => void;
    onKeyDown: (e: KeyboardEvent<HTMLInputElement>) => void;
  };
}

// Lỗi mà máy chủ có thể đã làm xong, hoặc chưa làm nhưng nên gửi lại đúng như cũ (như useWriteAction).
const outcomeUnknown = (e: unknown) =>
  e instanceof NetworkError ||
  (e instanceof ApiError && (e.status >= 500 || (e.status === 409 && e.code === "request_in_progress")));
const isConflict = (e: unknown) => e instanceof ApiError && e.status === 409 && e.code === "version_conflict";

const clamp = (n: number) => Math.min(Math.max(Math.trunc(n), 0), MAX_COUNT);

// Đọc qua hàm: trạng thái đổi trong lúc chờ (await), TypeScript không thấy.
const statusOf = (e: ScanEntry): SaveStatus => e.status;

/** Dòng đã lưu mà máy chủ có bản mới hơn (máy khác vừa đếm cùng mã): lấy số và version của máy chủ. */
function withServer(e: ScanEntry, server: StocktakeLine | undefined) {
  if (server !== undefined && e.status === "saved" && (e.line === null || server.version > e.line.version)) {
    e.line = server;
    e.counted = server.counted;
  }
}

export const conflictMessage = (n: number) =>
  `Người khác vừa đếm mã này: trên máy chủ là ${String(n)}. Sửa lại số nếu cần.`;

export function useBarcodeScan(opts: ScanOptions): BarcodeScan {
  const optsRef = useRef(opts);
  useEffect(() => {
    optsRef.current = opts;
  });
  const write = useWriteAction((key: string, body: SaveBody) => optsRef.current.save(key, body), opts.writeOptions);
  const runRef = useRef((b: SaveBody) => write.run(b));
  useEffect(() => {
    runRef.current = (b) => write.run(b);
  });

  const entries = useRef(new Map<string, ScanEntry>());
  const order = useRef<string[]>([]);
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const queue = useRef<string[]>([]);
  const running = useRef(false);
  const idle = useRef<(() => void)[]>([]);
  const codes = useRef(new Map<string, string>());
  const lookups = useRef(new Map<string, number>());
  const typed = useRef("");

  const [, setTick] = useState(0);
  const rerender = useCallback(() => {
    setTick((t) => t + 1);
  }, []);
  const [last, setLast] = useState<BarcodeScan["last"]>(null);
  const [notFound, setNotFound] = useState<string | null>(null);
  const [lookupError, setLookupError] = useState<string | null>(null);
  const [value, setValue] = useState("");

  // Rời màn (đổi trang, mở menu khác) khi còn số chờ gửi hoặc chờ thử lại: gửi ngay một lần, không chờ kết quả, để
  // không mất số vừa quét. Hàng đợi của useWriteAction đã bị hủy cùng màn nên gọi save thẳng với khóa mới.
  useEffect(() => {
    const ts = timers.current;
    const es = entries.current;
    return () => {
      for (const t of ts.values()) clearTimeout(t);
      ts.clear();
      const o = optsRef.current;
      const newKey = o.writeOptions?.newKey ?? (() => crypto.randomUUID());
      for (const e of es.values()) {
        if (e.status !== "pending" && e.status !== "retrying") continue;
        const body: SaveBody = {
          stocktakeId: o.stocktakeId,
          variantId: e.variantId,
          counted: e.counted,
          ...(e.line === null ? {} : { lineVersion: e.line.version }),
        };
        o.save(newKey(), body).catch(() => undefined);
      }
    };
  }, []);

  const settle = useCallback(() => {
    if (running.current || queue.current.length > 0) return;
    const ws = idle.current;
    idle.current = [];
    for (const w of ws) w();
  }, []);

  const pump = useCallback(async () => {
    if (running.current) return;
    running.current = true;
    for (;;) {
      const v = queue.current.shift();
      if (v === undefined) break;
      const e = entries.current.get(v);
      if (e === undefined || (e.status !== "pending" && e.status !== "retrying")) continue;
      const body: SaveBody = {
        stocktakeId: optsRef.current.stocktakeId,
        variantId: v,
        counted: e.counted,
        ...(e.line === null ? {} : { lineVersion: e.line.version }),
      };
      e.status = "saving";
      rerender();
      const r = await runRef.current(body);
      // Người dùng đổi số trong lúc gửi → e.status đã về pending (có hẹn giờ gửi lại): chỉ cập nhật version.
      const still = statusOf(e) === "saving";
      if (r.ok) {
        e.line = r.data;
        if (still) {
          e.status = "saved";
          e.message = null;
        }
      } else if (isConflict(r.error)) {
        let fresh: StocktakeLine | undefined;
        try {
          fresh = (await optsRef.current.reload()).find((l) => l.variantId === v);
        } catch {
          fresh = undefined;
        }
        if (fresh !== undefined) e.line = fresh;
        if (statusOf(e) === "saving") {
          if (fresh?.counted === body.counted) {
            e.status = "saved";
            e.message = null;
          } else if (fresh !== undefined) {
            e.counted = fresh.counted;
            e.status = "error";
            e.message = conflictMessage(fresh.counted);
          } else {
            e.status = "error";
            e.message = errorMessage(r.error);
          }
        }
      } else if (outcomeUnknown(r.error)) {
        if (still) {
          e.status = "retrying";
          e.message = null;
          timers.current.set(
            v,
            setTimeout(() => {
              timers.current.delete(v);
              queue.current.push(v);
              void pump();
            }, optsRef.current.retryMs ?? RETRY_MS),
          );
        }
      } else if (still) {
        e.status = "error";
        e.message = errorMessage(r.error);
      }
      rerender();
    }
    running.current = false;
    settle();
  }, [rerender, settle]);

  const enqueue = useCallback(
    (v: string) => {
      const t = timers.current.get(v);
      if (t !== undefined) clearTimeout(t);
      timers.current.delete(v);
      if (!queue.current.includes(v)) queue.current.push(v);
      void pump();
    },
    [pump],
  );

  const change = useCallback(
    (v: string, f: (n: number) => number, hit?: VariantSearchHit) => {
      const server = optsRef.current.lines.find((l) => l.variantId === v);
      let e = entries.current.get(v);
      if (e === undefined) {
        e = {
          variantId: v,
          hit: null,
          counted: server?.counted ?? 0,
          line: server ?? null,
          status: "saved",
          message: null,
        };
        entries.current.set(v, e);
      } else {
        withServer(e, server);
      }
      if (hit !== undefined) e.hit = hit;
      e.counted = clamp(f(e.counted));
      e.status = "pending";
      e.message = null;
      order.current = [v, ...order.current.filter((x) => x !== v)];
      const t = timers.current.get(v);
      if (t !== undefined) clearTimeout(t);
      timers.current.set(
        v,
        setTimeout(() => {
          enqueue(v);
        }, optsRef.current.delayMs ?? SAVE_DELAY_MS),
      );
      rerender();
    },
    [enqueue, rerender],
  );

  const scan = useCallback(
    (raw: string) => {
      const code = raw.trim();
      if (code === "") return;
      const key = code.toUpperCase();
      setLookupError(null);
      const known = codes.current.get(key);
      if (known !== undefined) {
        change(known, (n) => n + 1);
        setLast({ code, variantId: known });
        setNotFound(null);
        return;
      }
      const waiting = lookups.current.get(key);
      if (waiting !== undefined) {
        lookups.current.set(key, waiting + 1);
        return;
      }
      lookups.current.set(key, 1);
      optsRef.current.lookup(code).then(
        (hit) => {
          const n = lookups.current.get(key) ?? 1;
          lookups.current.delete(key);
          if (hit === null) {
            setNotFound(code);
            return;
          }
          codes.current.set(key, hit.variantId);
          change(hit.variantId, (c) => c + n, hit);
          setLast({ code, variantId: hit.variantId });
          setNotFound(null);
        },
        (err: unknown) => {
          lookups.current.delete(key);
          setLookupError(errorMessage(err));
        },
      );
    },
    [change],
  );

  const setCount = useCallback(
    (v: string, n: number) => {
      change(v, () => n);
    },
    [change],
  );
  const add = useCallback(
    (v: string, d: number) => {
      change(v, (n) => n + d);
    },
    [change],
  );

  const flush = useCallback(async () => {
    for (const v of [...timers.current.keys()]) enqueue(v);
    if (running.current || queue.current.length > 0) {
      await new Promise<void>((resolve) => {
        idle.current.push(resolve);
      });
    }
    return [...entries.current.values()].every((e) => e.status === "saved");
  }, [enqueue]);

  // Mã đã đổi trên máy này (mới nhất trước), rồi dòng chỉ có trên máy chủ (máy khác đếm) theo thứ tự máy chủ.
  const local = order.current.flatMap((v) => {
    const e = entries.current.get(v);
    if (e === undefined) return [];
    const copy = { ...e };
    withServer(
      copy,
      opts.lines.find((l) => l.variantId === v),
    );
    return [copy];
  });
  const all: ScanEntry[] = [
    ...local,
    ...opts.lines
      .filter((l) => !entries.current.has(l.variantId))
      .map((l) => ({
        variantId: l.variantId,
        hit: null,
        counted: l.counted,
        line: l,
        status: "saved" as const,
        message: null,
      })),
  ];

  return {
    entries: all,
    last,
    notFound,
    lookupError,
    scan,
    setCount,
    add,
    flush,
    busy: all.some((e) => e.status !== "saved" && e.status !== "error"),
    input: {
      value,
      onChange: (e) => {
        typed.current = e.target.value;
        setValue(e.target.value);
      },
      onKeyDown: (e) => {
        if (e.key !== "Enter") return;
        e.preventDefault();
        scan(typed.current);
        typed.current = "";
        setValue("");
      },
    },
  };
}
