// Khung chung của ba màn Báo cáo đơn, Hãng vận chuyển, Bán chạy (Task 240, M6-17): trang có dòng "Phạm vi", nút Tải lại
// và Xuất Excel ở góc phải, thông báo xuất tệp; và bộ chọn kỳ nối với địa chỉ.
import type { ReactNode } from "react";
import { ExportButton, ExportNotice } from "./ExportButton";
import { PeriodPicker } from "./PeriodPicker";
import { ReloadButton } from "./ReportBanners";
import { ReportsPage } from "./ReportsLayout";
import type { ReportPeriod } from "./useReportPeriod";
import type { ReportState } from "./useReport";
import type { ExportRun } from "./useRunNotice";

export interface SalesReportPageProps {
  title: string;
  sub: string;
  rep: Pick<ReportState<{ status: string }>, "phase" | "message" | "reload">;
  exp: ExportRun;
  /** Kỳ không hợp lệ: khóa Tải lại và Xuất Excel. */
  periodBad: boolean;
  children: ReactNode;
}

export function SalesReportPage({ title, sub, rep, exp, periodBad, children }: SalesReportPageProps) {
  return (
    <ReportsPage
      title={title}
      sub={sub}
      salesScope
      actions={
        <>
          <ReloadButton rep={rep} disabled={periodBad} />
          <ExportButton exp={exp} disabled={rep.phase === "loading" || periodBad} />
        </>
      }
    >
      <ExportNotice exp={exp} />
      {children}
    </ReportsPage>
  );
}

/** Bộ chọn kỳ; đổi kỳ thì dựng lại (ô ngày tự chọn theo kỳ mới). */
export function ReportPeriodPicker({ rp, disabled }: { rp: ReportPeriod; disabled: boolean }) {
  const { period } = rp;
  return (
    <PeriodPicker
      key={`${period.preset}|${period.from}|${period.to}`}
      value={period}
      today={rp.today}
      onChange={rp.onChange}
      disabled={disabled}
    />
  );
}
