// Lấy một báo cáo (Task 239, M6-16; Interfaces của kế hoạch; BC05, Q8, Q21).
//
// - GET báo cáo (tại chỗ). Trả `run_in_background` (hết chỗ hay quá 30 s) → tự tạo ĐÚNG MỘT lần chạy nền (POST …/runs
//   qua useWriteAction: khóa chống trùng sinh lúc bấm "Xem", giữ khi tự thử lại), rồi chờ báo hiệu `report.run_finished`
//   của đúng lần chạy đó (khóa truy vấn bắt đầu bằng chủ đề đó → onRealtimeChanged tải lại; resync 20 s là lưới an
//   toàn) và hiện kết quả như xem tại chỗ.
// - Không dùng truy vấn của TanStack cho GET: resync 20 s của khung tải lại mọi truy vấn đang mở, mà báo cáo nặng không
//   được tải lại mỗi 20 s; tự tải lại theo `refreshMs` (Tổng quan: 60 s, chỉ khi tab đang hiện) thì do hook này lo.
// - 503 retry_later: hiện câu bận, tự thử lại sau Retry-After (tối đa 10 s, ba lần).
import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  createSalesReportRun,
  getSalesReportRun,
  type ReportKey,
  type ReportParams,
  type ReportRun,
  type RunInBackground,
} from "../../api";
import { ApiError, errorMessage, unwrap } from "../../app/errors";
import { TEXT_BUSY, useWriteAction, type WriteActionOptions } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";

/** Chủ đề realtime khi một lần chạy nền xong (máy chủ: Task 235). */
export const TOPIC_RUN_FINISHED = "report.run_finished";

export const TEXT_TOO_HEAVY = "Báo cáo quá nặng, thu hẹp khoảng thời gian.";
export const TEXT_RUN_BUSY = "Máy đang bận, báo cáo chưa chạy được. Thử lại sau ít phút.";
export const TEXT_RUN_ERROR = "Báo cáo chạy lỗi. Thử lại sau ít phút.";
export const TEXT_RUN_FORBIDDEN = "Bạn không còn quyền xem báo cáo này.";
export const TEXT_RUN_EXPIRED = "Kết quả đã hết hạn. Bấm Tải lại để chạy lại.";
export const TEXT_BAD_RESULT = "Kết quả báo cáo không đọc được. Bấm Tải lại để chạy lại.";

/** Cặp gọi API của một họ báo cáo (bán hàng: Task 239; tồn kho: Task 241 thêm cặp của họ inventory). */
export interface RunApi {
  create: (
    key: string,
    body: { report: ReportKey; format: "json" | "xlsx"; params: ReportParams },
  ) => Promise<ReportRun>;
  get: (id: string) => Promise<ReportRun>;
}

export const salesRunApi: RunApi = {
  create: (key, body) => unwrap(createSalesReportRun({ body, headers: { "Idempotency-Key": key } })),
  get: (id) => unwrap(getSalesReportRun({ path: { id } })),
};

export type ReportPhase = "loading" | "ready" | "error" | "background" | "failed";

export interface ReportOptions<T> {
  report: ReportKey;
  params: ReportParams;
  /** GET báo cáo; trả bản ready hay run_in_background. */
  load: () => Promise<T | RunInBackground>;
  /** Kiểm kết quả của lần chạy nền (ReportRun.result); sai dạng → null. */
  parse: (v: unknown) => T | null;
  /** Tự tải lại sau chừng này ms khi tab đang hiện (bỏ trống: không tự tải lại). */
  refreshMs?: number;
  /** Tắt khi kỳ chưa hợp lệ: không gọi API. */
  enabled?: boolean;
  runApi?: RunApi;
  sleep?: WriteActionOptions["sleep"];
}

export interface ReportState<T> {
  phase: ReportPhase;
  data: T | null;
  /** Câu tiếng Việt cho phase error, failed và lỗi tạo lần chạy nền. */
  message: string | null;
  updatedAt: Date | null;
  /** Lần chạy nền đã tạo (có thì màn mời xem ở Tệp xuất). */
  runId: string | null;
  /** Tải lại từ đầu (hiện "Đang tải"). */
  reload: () => void;
  /** Gửi lại việc tạo lần chạy nền sau lỗi mạng: cùng khóa chống trùng. */
  retryRun: () => void;
}

interface Loaded<T> {
  /** Khóa tham số lúc tải: khác khóa hiện tại → coi như đang tải lại. */
  forKey: string;
  kind: "ready" | "error" | "background";
  data: T | null;
  message: string | null;
  updatedAt: Date | null;
  runId: string | null;
}

const MAX_BUSY_RETRIES = 3;
const DEFAULT_BUSY_WAIT_MS = 5000;
const MAX_BUSY_WAIT_MS = 10_000;

const isBackground = (v: { status: string }): v is RunInBackground => v.status === "run_in_background";

export function failedText(reason: ReportRun["reason"]): string {
  switch (reason) {
    case "too_heavy":
      return TEXT_TOO_HEAVY;
    case "busy":
      return TEXT_RUN_BUSY;
    case "forbidden":
      return TEXT_RUN_FORBIDDEN;
    default:
      return TEXT_RUN_ERROR;
  }
}

interface Outcome<T> {
  phase: "ready" | "error" | "failed";
  data: T | null;
  message: string | null;
  updatedAt: Date | null;
}

/** Kết quả của lần chạy nền đã tạo (runId) theo trạng thái của nó; chưa xong → null (vẫn chờ). */
function runOutcome<T>(
  r: ReportRun | undefined,
  runId: string | null,
  parse: (v: unknown) => T | null,
): Outcome<T> | null {
  if (r === undefined) return null;
  if (r.id !== runId) return null;
  switch (r.status) {
    case "ready": {
      const data = r.result === undefined ? null : parse(r.result);
      return data === null
        ? { phase: "error", data: null, message: TEXT_BAD_RESULT, updatedAt: null }
        : { phase: "ready", data, message: null, updatedAt: new Date(r.finishedAt ?? r.createdAt) };
    }
    case "failed":
      return { phase: "failed", data: null, message: failedText(r.reason), updatedAt: null };
    case "expired":
      return { phase: "error", data: null, message: TEXT_RUN_EXPIRED, updatedAt: null };
    default:
      return null;
  }
}

export function useReport<T extends { status: string }>(opts: ReportOptions<T>): ReportState<T> {
  const { params, refreshMs } = opts;
  const enabled = opts.enabled ?? true;
  const runApi = opts.runApi ?? salesRunApi;
  const paramsKey = JSON.stringify(params);

  const latest = useRef(opts);
  useEffect(() => {
    latest.current = opts;
  });

  const [loaded, setLoaded] = useState<Loaded<T> | null>(null);
  const generation = useRef(0);
  const busyRetries = useRef(0);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const loadedRef = useRef(loaded);
  useEffect(() => {
    loadedRef.current = loaded;
  });
  // Hàm tải mới nhất cho bộ hẹn giờ thử lại (hàm tự gọi lại chính nó qua ref, không qua tên).
  const fetchRef = useRef<(silent: boolean) => Promise<void>>(() => Promise.resolve());
  const invalidate = () => {
    generation.current++;
  };

  const write = useWriteAction(
    (key: string, body: { report: ReportKey; format: "json" | "xlsx"; params: ReportParams }) =>
      runApi.create(key, body),
    opts.sleep === undefined ? {} : { sleep: opts.sleep },
  );
  const writeRef = useRef(write);
  useEffect(() => {
    writeRef.current = write;
  });

  const startRun = useCallback((forKey: string, gen: number) => {
    void writeRef.current
      .run({ report: latest.current.report, format: "json", params: latest.current.params })
      .then((r) => {
        if (!r.ok || gen !== generation.current) return;
        setLoaded((cur) => (cur !== null && cur.forKey === forKey ? { ...cur, runId: r.data.id } : cur));
      });
  }, []);

  const fetchNow = useCallback(
    async (silent: boolean) => {
      const gen = ++generation.current;
      clearTimeout(retryTimer.current);
      try {
        const r = await latest.current.load();
        if (gen !== generation.current) return;
        if (isBackground(r)) {
          if (silent) return; // tự tải lại không đẩy báo cáo đang hiện sang chạy nền
          busyRetries.current = 0;
          setLoaded({ forKey: paramsKey, kind: "background", data: null, message: null, updatedAt: null, runId: null });
          startRun(paramsKey, gen);
          return;
        }
        busyRetries.current = 0;
        setLoaded({ forKey: paramsKey, kind: "ready", data: r, message: null, updatedAt: new Date(), runId: null });
      } catch (e) {
        if (gen !== generation.current) return;
        if (silent) return; // lỗi khi tự tải lại: giữ số đang hiện
        const busy = e instanceof ApiError && e.status === 503 && e.code === "retry_later";
        setLoaded({
          forKey: paramsKey,
          kind: "error",
          data: null,
          message: busy ? TEXT_BUSY : errorMessage(e),
          updatedAt: null,
          runId: null,
        });
        if (busy && busyRetries.current < MAX_BUSY_RETRIES) {
          busyRetries.current++;
          const wait = Math.min(e.retryAfterMs ?? DEFAULT_BUSY_WAIT_MS, MAX_BUSY_WAIT_MS);
          retryTimer.current = setTimeout(() => {
            void fetchRef.current(false);
          }, wait);
        }
      }
    },
    [paramsKey, startRun],
  );

  useEffect(() => {
    fetchRef.current = fetchNow;
  }, [fetchNow]);

  // Đổi tham số (hay bật lên): tải lại. Số của tham số cũ không hiện: phase suy ra từ forKey.
  useEffect(() => {
    if (!enabled) return;
    busyRetries.current = 0;
    // Hẹn ngay sau lượt vẽ này (không gọi hàm tải đồng bộ trong effect: nó đặt state, React cảnh báo dồn lượt vẽ).
    const start = setTimeout(() => {
      void fetchNow(false);
    }, 0);
    return () => {
      clearTimeout(start);
      invalidate();
      clearTimeout(retryTimer.current);
    };
  }, [enabled, fetchNow]);

  // Tự tải lại khi tab đang hiện; tab ẩn thì bỏ nhịp, hiện lại mà đã quá hạn thì tải ngay.
  useEffect(() => {
    if (!enabled || refreshMs === undefined) return;
    const due = () => document.visibilityState === "visible" && loadedRef.current?.kind === "ready";
    const timer = setInterval(() => {
      if (due()) void fetchNow(true);
    }, refreshMs);
    const onVisible = () => {
      const at = loadedRef.current?.updatedAt;
      if (due() && at != null && Date.now() - at.getTime() >= refreshMs) void fetchNow(true);
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [enabled, refreshMs, fetchNow]);

  const current = loaded !== null && loaded.forKey === paramsKey ? loaded : null;
  const runId = current?.kind === "background" ? current.runId : null;
  const run = useQuery({
    queryKey: queryKey(TOPIC_RUN_FINISHED, "run", runId),
    queryFn: () => runApi.get(runId ?? ""),
    enabled: runId !== null,
  });

  const runResult = runOutcome(run.data, runId, opts.parse);

  // Đang chờ lần chạy nền đã tạo thì "Tải lại" không tạo thêm lần chạy (mỗi bộ tham số chỉ một lần chạy nền cho tới khi
  // nó xong, hỏng hay hết hạn); lúc đó tải lại bình thường.
  const waiting = current?.kind === "background" && runId !== null && runResult === null && !run.isError;
  const reload = () => {
    if (waiting) return;
    setLoaded(null);
    void fetchNow(false);
  };
  const retryRun = () => {
    write.reset();
    startRun(paramsKey, generation.current);
  };

  if (current === null) {
    return { phase: "loading", data: null, message: null, updatedAt: null, runId: null, reload, retryRun };
  }
  if (current.kind === "background") {
    if (runResult !== null) return { ...runResult, runId, reload, retryRun };
    // Không đọc được lần chạy (404, mạng): nói rõ thay vì đứng "đang chạy nền" mãi; Tải lại chạy lại từ đầu.
    if (run.isError)
      return { phase: "error", data: null, message: errorMessage(run.error), updatedAt: null, runId, reload, retryRun };
    return { phase: "background", data: null, message: write.message, updatedAt: null, runId, reload, retryRun };
  }
  return {
    phase: current.kind,
    data: current.data,
    message: current.message,
    updatedAt: current.updatedAt,
    runId: null,
    reload,
    retryRun,
  };
}
