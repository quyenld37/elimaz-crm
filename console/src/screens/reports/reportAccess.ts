// Quyền của người đang xem báo cáo (Task 239, M6-16), đọc từ /api/v1/me (máy chủ vẫn kiểm quyền thật ở từng lệnh gọi;
// đây chỉ quyết định hiện hay ẩn nút và dòng "Phạm vi").
import { useQuery } from "@tanstack/react-query";
import { getMe, type Grant } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";

export interface ReportAccess {
  /** Đã biết quyền (me đã tải xong). */
  known: boolean;
  /** report.export phạm vi toàn shop: thấy nút Xuất Excel (Q17). */
  canExport: boolean;
  /** report.sales.read phạm vi toàn shop (không bị giới hạn theo Page). */
  salesAll: boolean;
}

const hasAll = (grants: readonly Grant[], permission: string) =>
  grants.some((g) => g.permission === permission && g.scope.type === "all");

export function reportAccessOf(grants: readonly Grant[] | undefined): ReportAccess {
  if (grants === undefined) return { known: false, canExport: false, salesAll: false };
  return { known: true, canExport: hasAll(grants, "report.export"), salesAll: hasAll(grants, "report.sales.read") };
}

export function useReportAccess(): ReportAccess {
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  return reportAccessOf(me.data?.grants);
}
