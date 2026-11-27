// Nút "Xuất Excel" và thông báo của nó (Task 239, M6-16; hành vi (b), chủ shop chốt thay 27/11/2026): nút chỉ hiện khi
// /api/v1/me có report.export phạm vi toàn shop (máy chủ vẫn kiểm: 403 export_forbidden); bấm là tạo lần chạy nền,
// thông báo "Đang tạo tệp, xem ở Tệp xuất" có nút "Mở Tệp xuất"; tệp xong hiện "Tệp … đã sẵn sàng — Tải về".
import { Link } from "react-router";
import { RunNotice } from "./RunNotice";
import { useReportAccess } from "./reportAccess";
import type { ExportRun } from "./useRunNotice";

/** Đường tới màn Tệp xuất (Task 241). */
export const EXPORTS_PATH = "/bao-cao/tep-xuat";

export function ExportButton({ exp, disabled = false }: { exp: ExportRun; disabled?: boolean }) {
  const access = useReportAccess();
  if (!access.canExport) return null;
  return (
    <button type="button" className="btn btn-line" disabled={disabled || exp.pending} onClick={exp.start}>
      Xuất Excel
    </button>
  );
}

export function ExportNotice({ exp }: { exp: ExportRun }) {
  switch (exp.stage) {
    case "waiting":
      return (
        <RunNotice
          text="Đang tạo tệp, xem ở Tệp xuất"
          action={
            <Link className="btn btn-line" to={EXPORTS_PATH}>
              Mở Tệp xuất
            </Link>
          }
        />
      );
    case "ready":
      return (
        <>
          <RunNotice
            text={`Tệp ${exp.title} đã sẵn sàng —`}
            action={
              <button type="button" className="btn btn-line" onClick={exp.download}>
                Tải về
              </button>
            }
          />
          {exp.downloadError !== null && <p className="field-err">{exp.downloadError}</p>}
        </>
      );
    case "failed":
    case "error":
      return <RunNotice tone="err" text={exp.message ?? "Không tạo được tệp."} />;
    default:
      return null;
  }
}
