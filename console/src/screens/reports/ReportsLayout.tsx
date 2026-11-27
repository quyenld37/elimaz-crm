// Khung trang của mục Báo cáo (Task 239, M6-16; mockup m6/*.html: .page-h, dòng "Phạm vi"). Dùng chung cho mọi màn báo
// cáo của M6 (Task 240, 241): đường dẫn "Báo cáo / <tên>", tiêu đề, dòng mô tả, nút ở góc phải, dòng "Phạm vi" khi người
// xem chỉ có quyền đọc báo cáo bán hàng theo Page.
import type { ReactNode } from "react";
import { useReportAccess } from "./reportAccess";
import "./reports.css";

export interface ReportsPageProps {
  title: string;
  sub?: string;
  /** Nút ở góc phải đầu trang ("Tải lại", "Xuất Excel"). */
  actions?: ReactNode;
  /** Báo cáo bán hàng: hiện dòng "Phạm vi" cho người chỉ có quyền theo Page. */
  salesScope?: boolean;
  children: ReactNode;
}

export const TEXT_PAGE_SCOPE_HEAD = "Phạm vi: chỉ đơn của Page bạn được giao.";
export const TEXT_PAGE_SCOPE_TAIL = "Không gồm đơn của Page khác, đơn website, đơn nhập tay.";

export function ReportsPage({ title, sub, actions, salesScope = false, children }: ReportsPageProps) {
  const access = useReportAccess();
  return (
    <div className="rp stack">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Báo cáo / {title}</p>
          <h1>{title}</h1>
          {sub !== undefined && <p className="sub">{sub}</p>}
        </div>
        {actions !== undefined && <div className="row">{actions}</div>}
      </div>
      {salesScope && access.known && !access.salesAll && (
        <div className="box box-info small">
          <b>{TEXT_PAGE_SCOPE_HEAD}</b> {TEXT_PAGE_SCOPE_TAIL}
        </div>
      )}
      {children}
    </div>
  );
}
