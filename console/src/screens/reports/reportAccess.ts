// Quyền của người đang xem báo cáo (Task 239, M6-16; Task 241 thêm quyền tồn kho, xuất khách), đọc từ /api/v1/me (máy
// chủ vẫn kiểm quyền thật ở từng lệnh gọi; đây chỉ quyết định hiện hay ẩn nút, nhóm và dòng "Phạm vi").
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
  /** report.inventory.read phạm vi toàn shop (không bị giới hạn theo kho). */
  inventoryAll: boolean;
  /** Mã các kho được xem khi report.inventory.read chỉ theo kho (rỗng khi toàn shop hay không có quyền). */
  inventoryWarehouseIds: readonly string[];
  /** customer.export phạm vi toàn shop: thấy nút Xuất danh sách khách (hành vi (c)). */
  customersExport: boolean;
  /** Có quyền này ở phạm vi bất kỳ. */
  can: (permission: string) => boolean;
}

const hasAll = (grants: readonly Grant[], permission: string) =>
  grants.some((g) => g.permission === permission && g.scope.type === "all");

export function reportAccessOf(grants: readonly Grant[] | undefined): ReportAccess {
  const list = grants ?? [];
  const inventoryAll = hasAll(list, "report.inventory.read");
  return {
    known: grants !== undefined,
    canExport: hasAll(list, "report.export"),
    salesAll: hasAll(list, "report.sales.read"),
    inventoryAll,
    inventoryWarehouseIds: inventoryAll
      ? []
      : list
          .filter((g) => g.permission === "report.inventory.read" && g.scope.type === "warehouse")
          .map((g) => g.scope.id),
    customersExport: hasAll(list, "customer.export"),
    can: (permission) => list.some((g) => g.permission === permission),
  };
}

export function useReportAccess(): ReportAccess {
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  return reportAccessOf(me.data?.grants);
}
