// Xuất Excel của một báo cáo (Task 239, M6-16; hành vi (b)): POST …/runs {format: "xlsx"} qua useWriteAction (một khóa
// chống trùng cho một lần bấm, bấm lại khi đang chạy nhận lại đúng lời hứa), rồi nghe báo hiệu report.run_finished của
// đúng lần chạy đó: xong → "Tệp … đã sẵn sàng — Tải về" ngay trên màn đang mở. Không thêm API mới.
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { getSalesReportRunFile, type ReportKey, type ReportParams } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { queryKey } from "../../app/queryClient";
import { TOPIC_RUN_FINISHED, failedText, salesRunApi, type RunApi } from "./useReport";

export type ExportStage = "idle" | "waiting" | "ready" | "failed" | "error";

export interface ExportRun {
  stage: ExportStage;
  /** Tên tệp theo báo cáo và kỳ, ví dụ "Tổng quan 27/11/2026". */
  title: string;
  /** Lỗi của lần bấm cuối (câu tiếng Việt) hay lý do lần chạy hỏng. */
  message: string | null;
  pending: boolean;
  start: () => void;
  /** Tải tệp: lấy đường dẫn ký sẵn rồi mở. Lỗi → nói ở message. */
  download: () => void;
  downloadError: string | null;
}

export interface ExportOptions {
  report: ReportKey;
  params: ReportParams;
  title: string;
  runApi?: RunApi;
}

/** Chỉ mở đường dẫn http(s) của kho tệp, không mở chuỗi lạ. */
function openable(url: string): boolean {
  try {
    const p = new URL(url).protocol;
    return p === "https:" || p === "http:";
  } catch {
    return false;
  }
}

export function useExportRun(o: ExportOptions): ExportRun {
  const runApi = o.runApi ?? salesRunApi;
  // Lần chạy đang theo dõi, kèm tên tệp CHỐT lúc bấm (đổi kỳ sau đó không đổi tên tệp đang chờ).
  const [started, setStarted] = useState<{ id: string; title: string } | null>(null);
  const runId = started?.id ?? null;
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const write = useWriteAction(
    (key: string, body: { report: ReportKey; format: "json" | "xlsx"; params: ReportParams }) =>
      runApi.create(key, body),
  );
  const run = useQuery({
    queryKey: queryKey(TOPIC_RUN_FINISHED, "run", runId),
    queryFn: () => runApi.get(runId ?? ""),
    enabled: runId !== null,
  });

  const title = o.title;
  const start = () => {
    setDownloadError(null);
    void write.run({ report: o.report, format: "xlsx", params: o.params }).then((r) => {
      if (r.ok) setStarted({ id: r.data.id, title });
    });
  };

  const download = () => {
    if (runId === null) return;
    setDownloadError(null);
    void unwrap(getSalesReportRunFile({ path: { id: runId } })).then(
      (link) => {
        if (openable(link.url)) window.location.assign(link.url);
        else setDownloadError("Đường dẫn tải không hợp lệ. Thử lại sau.");
      },
      (e: unknown) => {
        setDownloadError(errorMessage(e));
      },
    );
  };

  let stage: ExportStage = "idle";
  let message: string | null = write.message;
  if (write.message !== null) stage = "error";
  else if (runId !== null) {
    const r = run.data;
    if (r?.status === "ready") stage = "ready";
    else if (r?.status === "failed") {
      stage = "failed";
      message = failedText(r.reason);
    } else if (r?.status === "expired") {
      stage = "failed";
      message = "Tệp đã hết hạn. Bấm Xuất Excel để tạo lại.";
    } else stage = "waiting";
  } else if (write.pending) stage = "waiting";

  return { stage, title: started?.title ?? o.title, message, pending: write.pending, start, download, downloadError };
}
