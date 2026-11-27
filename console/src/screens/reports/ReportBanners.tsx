// Thanh trạng thái chung của báo cáo tồn (Task 241, M6-18; mockup ton-kho.html trạng thái D, tong-quan.html trạng thái G):
// đang tải, lỗi tải (Thử lại), chạy nền (mời mở Tệp xuất), lần chạy nền hỏng; và nút "Tải lại".
import { Link } from "react-router";
import { EXPORTS_PATH } from "./ExportButton";
import { RunNotice } from "./RunNotice";
import type { ReportState } from "./useReport";

export const TEXT_BACKGROUND = "Báo cáo nặng, đang chạy nền. Sẽ báo khi xong.";

type Status = Pick<ReportState<{ status: string }>, "phase" | "message" | "reload">;

export function ReloadButton({ rep, disabled = false }: { rep: Status; disabled?: boolean }) {
  return (
    <button
      type="button"
      className="btn btn-quiet"
      disabled={disabled || rep.phase === "loading" || rep.phase === "background"}
      onClick={rep.reload}
    >
      Tải lại
    </button>
  );
}

export function ReportBanners({ rep, failedHint }: { rep: Status; failedHint: string }) {
  switch (rep.phase) {
    case "loading":
      return (
        <div className="box box-info stack-sm">
          <b>Đang tải số liệu…</b>
        </div>
      );
    case "error":
      return (
        <div className="box box-err stack-sm" role="alert">
          <b>Không tải được báo cáo.</b>
          <p className="small">{rep.message}</p>
          <div className="row">
            <button type="button" className="btn btn-line" onClick={rep.reload}>
              Thử lại
            </button>
          </div>
        </div>
      );
    case "background":
      return (
        <RunNotice
          text={TEXT_BACKGROUND}
          action={
            <Link className="btn btn-line" to={EXPORTS_PATH}>
              Tệp xuất
            </Link>
          }
        />
      );
    case "failed":
      return (
        <div className="box box-err stack-sm" role="alert">
          <b>{rep.message}</b>
          <p className="small">{failedHint}</p>
        </div>
      );
    default:
      return null;
  }
}
