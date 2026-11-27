// Kỳ của một màn báo cáo bán hàng trên địa chỉ (Task 240, M6-17): đọc kỳ (?ky=…), hôm nay giờ Việt Nam, lỗi kỳ; hàm
// đổi kỳ ghi lại vào địa chỉ. `beforeWrite` chạy trên bản địa chỉ mới trước khi ghi kỳ (ví dụ bỏ ô số đang mở).
import { useSearchParams } from "react-router";
import { periodError, parsePeriod, vnToday, writePeriod, type Period } from "./period";

export interface ReportPeriod {
  sp: URLSearchParams;
  setSp: ReturnType<typeof useSearchParams>[1];
  today: string;
  period: Period;
  /** Lý do kỳ không xem được; null = xem được. */
  bad: string | null;
  onChange: (p: Period) => void;
}

export function useReportPeriod(beforeWrite?: (next: URLSearchParams) => void): ReportPeriod {
  const [sp, setSp] = useSearchParams();
  const today = vnToday(new Date());
  const period = parsePeriod(sp, today);
  const onChange = (p: Period) => {
    const next = new URLSearchParams(sp);
    beforeWrite?.(next);
    writePeriod(next, p);
    setSp(next);
  };
  return { sp, setSp, today, period, bad: periodError(period, today), onChange };
}
